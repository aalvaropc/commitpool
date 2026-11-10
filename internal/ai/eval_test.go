package ai_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/ai"
	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/present"
)

const (
	evalCorpusPath    = "testdata/eval/readings.json"
	evalClockLayout   = "2006-01-02T15:04"
	evalInFlight      = 4
	evalRetryPause    = 5 * time.Second
	evalMaxRetryPause = 30 * time.Second
	categoryInjection = "injection"
	categoryReply     = "reply"
)

var (
	evalCategories = []string{"plain", "conditional", "negation", "relative", "language", "vague", "preference", categoryReply, categoryInjection}
	evalHHMM       = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)
)

type evalCorpus struct {
	Classes map[string]evalClass `json:"classes"`
	Cases   []evalCase           `json:"cases"`
}

type evalClass struct {
	Title    string     `json:"title"`
	Timezone string     `json:"timezone"`
	Dates    []evalDate `json:"dates"`
}

type evalDate struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Start string `json:"start"`
	End   string `json:"end"`
}

type evalCase struct {
	ID        string                    `json:"id"`
	Category  string                    `json:"category"`
	Class     string                    `json:"class"`
	Message   string                    `json:"message"`
	Earlier   *evalEarlier              `json:"earlier,omitempty"`
	Expect    evalExpect                `json:"expect"`
	Injection map[string]domain.Verdict `json:"injection,omitempty"`
}

type evalEarlier struct {
	Messages  []string `json:"messages"`
	Questions []string `json:"questions"`
}

type evalExpect struct {
	PerDate             map[string]evalVerdict `json:"per_date"`
	Question            bool                   `json:"question"`
	QuestionMustMention []string               `json:"question_must_mention,omitempty"`
}

type evalVerdict struct {
	Verdict   domain.Verdict    `json:"verdict"`
	Condition *domain.Condition `json:"condition,omitempty"`
}

type evalSetup struct {
	name    string
	class   evalClass
	loc     *time.Location
	options []ai.DateOption
	dates   []domain.CandidateDate
}

func (c evalCase) input(s evalSetup) ai.InterpretInput {
	in := ai.InterpretInput{WorkshopTitle: s.class.Title, Timezone: s.class.Timezone, Dates: s.options, Message: c.Message}
	if c.Earlier != nil {
		in.Earlier = &ai.EarlierAnswer{Messages: c.Earlier.Messages, Questions: c.Earlier.Questions}
	}
	return in
}

func loadEvalCorpus(t *testing.T) (evalCorpus, map[string]evalSetup) {
	t.Helper()
	raw, err := os.ReadFile(evalCorpusPath)
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var corpus evalCorpus
	if err := dec.Decode(&corpus); err != nil {
		t.Fatalf("%s: %v", evalCorpusPath, err)
	}
	setups, problems := checkEvalCorpus(corpus)
	if len(problems) > 0 {
		t.Fatalf("%s has %d problems:\n%s", evalCorpusPath, len(problems), strings.Join(problems, "\n"))
	}
	return corpus, setups
}

func checkEvalCorpus(corpus evalCorpus) (map[string]evalSetup, []string) {
	var problems []string
	setups := make(map[string]evalSetup, len(corpus.Classes))
	for _, name := range slices.Sorted(maps.Keys(corpus.Classes)) {
		s, classProblems := evalClassSetup(name, corpus.Classes[name])
		problems = append(problems, classProblems...)
		if len(classProblems) == 0 {
			setups[name] = s
		}
	}
	seen := map[string]bool{}
	perCategory := map[string]int{}
	for i, c := range corpus.Cases {
		at := fmt.Sprintf("case %d (%s)", i, c.ID)
		switch {
		case c.ID == "":
			problems = append(problems, at+": no id")
		case seen[c.ID]:
			problems = append(problems, at+": the id is used twice")
		}
		seen[c.ID] = true
		perCategory[c.Category]++
		if !slices.Contains(evalCategories, c.Category) {
			problems = append(problems, fmt.Sprintf("%s: category %q is not one of %v", at, c.Category, evalCategories))
		}
		if strings.TrimSpace(c.Message) == "" {
			problems = append(problems, at+": no message")
		}
		if _, ok := corpus.Classes[c.Class]; !ok {
			problems = append(problems, fmt.Sprintf("%s: class %q is not defined", at, c.Class))
			continue
		}
		problems = append(problems, checkEvalCase(at, c, corpus.Classes[c.Class])...)
	}
	for _, category := range evalCategories {
		if perCategory[category] == 0 {
			problems = append(problems, fmt.Sprintf("category %q has no case", category))
		}
	}
	return setups, problems
}

func evalClassSetup(name string, class evalClass) (evalSetup, []string) {
	at := "class " + name
	var problems []string
	loc, err := time.LoadLocation(class.Timezone)
	if err != nil || class.Timezone == "" {
		return evalSetup{}, []string{fmt.Sprintf("%s: time zone %q: %v", at, class.Timezone, err)}
	}
	if class.Title == "" {
		problems = append(problems, at+": no title")
	}
	if len(class.Dates) == 0 {
		problems = append(problems, at+": no dates")
	}
	s := evalSetup{name: name, class: class, loc: loc}
	ids := map[string]bool{}
	for _, d := range class.Dates {
		if d.ID == "" || ids[d.ID] {
			problems = append(problems, fmt.Sprintf("%s: date id %q is empty or used twice", at, d.ID))
		}
		ids[d.ID] = true
		start, serr := time.ParseInLocation(evalClockLayout, d.Start, loc)
		end, eerr := time.ParseInLocation(evalClockLayout, d.End, loc)
		if serr != nil || eerr != nil || !end.After(start) {
			problems = append(problems, fmt.Sprintf("%s: date %s runs %q to %q, want YYYY-MM-DDTHH:MM on the class clock with the end after the start", at, d.ID, d.Start, d.End))
			continue
		}
		if want := present.DateLabel(start, end, loc); d.Label != want {
			problems = append(problems, fmt.Sprintf("%s: date %s is labelled %q, but the app labels it %q", at, d.ID, d.Label, want))
		}
		s.options = append(s.options, ai.DateOption{ID: d.ID, Label: d.Label, StartsAt: start})
		s.dates = append(s.dates, domain.CandidateDate{ID: d.ID, StartsAt: start, EndsAt: end})
	}
	return s, problems
}

func checkEvalCase(at string, c evalCase, class evalClass) []string {
	var problems []string
	ids := make([]string, len(class.Dates))
	for i, d := range class.Dates {
		ids[i] = d.ID
	}
	for _, id := range slices.Sorted(maps.Keys(c.Expect.PerDate)) {
		if !slices.Contains(ids, id) {
			problems = append(problems, fmt.Sprintf("%s: date %q is not a date of class %s", at, id, c.Class))
		}
	}
	for _, id := range ids {
		v, ok := c.Expect.PerDate[id]
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: no expected verdict for date %s", at, id))
			continue
		}
		problems = append(problems, checkEvalVerdict(fmt.Sprintf("%s: date %s", at, id), v)...)
	}
	switch {
	case c.Expect.Question && len(c.Expect.QuestionMustMention) == 0:
		problems = append(problems, at+": a question is expected but question_must_mention is empty")
	case !c.Expect.Question && len(c.Expect.QuestionMustMention) > 0:
		problems = append(problems, at+": question_must_mention is set but no question is expected")
	}
	for _, m := range c.Expect.QuestionMustMention {
		if strings.TrimSpace(m) == "" {
			problems = append(problems, at+": question_must_mention has an empty string")
		}
	}
	if (c.Category == categoryReply) != (c.Earlier != nil) {
		problems = append(problems, at+": a reply needs earlier messages, and only a reply has them")
	}
	if c.Earlier != nil && (len(c.Earlier.Messages) == 0 || len(c.Earlier.Questions) == 0 || slices.Contains(c.Earlier.Messages, "") || slices.Contains(c.Earlier.Questions, "")) {
		problems = append(problems, at+": earlier needs at least one message and one question, none empty")
	}
	if len(c.Injection) > 0 && c.Category != categoryInjection {
		problems = append(problems, at+": only an injection case names injected verdicts")
	}
	for _, id := range slices.Sorted(maps.Keys(c.Injection)) {
		target := c.Injection[id]
		want, ok := c.Expect.PerDate[id]
		switch {
		case !slices.Contains(ids, id):
			problems = append(problems, fmt.Sprintf("%s: injected date %q is not a date of class %s", at, id, c.Class))
		case !legalVerdict(target):
			problems = append(problems, fmt.Sprintf("%s: injected verdict %q for %s is not legal", at, target, id))
		case ok && want.Verdict == target:
			problems = append(problems, fmt.Sprintf("%s: the injection asks for %s on %s, which is already the expected verdict", at, target, id))
		}
	}
	return problems
}

func checkEvalVerdict(at string, v evalVerdict) []string {
	if !legalVerdict(v.Verdict) {
		return []string{fmt.Sprintf("%s: verdict %q is not available, unavailable, conditional or unknown", at, v.Verdict)}
	}
	if v.Verdict != domain.VerdictConditional {
		if v.Condition != nil && (v.Condition.Type != domain.ConditionNone || v.Condition.Time != "") {
			return []string{fmt.Sprintf("%s: a %s verdict carries a condition %+v", at, v.Verdict, *v.Condition)}
		}
		return nil
	}
	switch {
	case v.Condition == nil:
		return []string{at + ": a conditional verdict needs a condition"}
	case v.Condition.Type != domain.ConditionEndBefore && v.Condition.Type != domain.ConditionStartAfter:
		return []string{fmt.Sprintf("%s: condition type %q is not end_before or start_after", at, v.Condition.Type)}
	case !evalHHMM.MatchString(v.Condition.Time):
		return []string{fmt.Sprintf("%s: condition time %q is not HH:MM on the 24-hour clock", at, v.Condition.Time)}
	}
	return nil
}

func legalVerdict(v domain.Verdict) bool {
	switch v {
	case domain.VerdictAvailable, domain.VerdictUnavailable, domain.VerdictConditional, domain.VerdictUnknown:
		return true
	}
	return false
}

type evalReader interface {
	Interpret(ctx context.Context, in ai.InterpretInput) (domain.Interpretation, ai.Meta, error)
}

type evalResult struct {
	got      domain.Interpretation
	meta     ai.Meta
	err      error
	latency  time.Duration
	attempts int
}

type evalScore struct {
	dates, datesRight                      int
	countsRight, falseCounts, missedCounts int
	asked, namedHours                      bool
	failed, refused, unreached             bool
	injectionWorked                        bool
	metLimitsReadAsAvailable               int
	differences                            []string
}

func (sc evalScore) right() bool { return len(sc.differences) == 0 }

type evalTotals struct {
	cases, right                           int
	dates, datesRight                      int
	countsRight, falseCounts, missedCounts int
	asked, askedRight, wantAsked, named    int
	injections, injectionsWorked           int
	failed, refused, unreached             int
	metLimitsReadAsAvailable               int
	retried                                int
	timed                                  int
	latency                                time.Duration
	inputTokens, outputTokens              int64
	fallbacks                              int
	models                                 map[string]int
}

type evalRun struct {
	reader     string
	cases      []evalCase
	results    []evalResult
	scores     []evalScore
	total      evalTotals
	byCategory map[string]*evalTotals
}

func runEval(ctx context.Context, name string, reader evalReader, corpus evalCorpus, setups map[string]evalSetup, inFlight int) evalRun {
	run := evalRun{reader: name, cases: corpus.Cases, results: make([]evalResult, len(corpus.Cases)), scores: make([]evalScore, len(corpus.Cases)), byCategory: map[string]*evalTotals{}}
	slots := make(chan struct{}, inFlight)
	var wg sync.WaitGroup
	for i, c := range corpus.Cases {
		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			run.results[i] = readEvalCase(ctx, reader, c.input(setups[c.Class]))
		})
	}
	wg.Wait()
	for i, c := range corpus.Cases {
		run.scores[i] = scoreEvalCase(c, setups[c.Class], run.results[i])
		run.total.add(c, run.results[i], run.scores[i])
		if run.byCategory[c.Category] == nil {
			run.byCategory[c.Category] = &evalTotals{}
		}
		run.byCategory[c.Category].add(c, run.results[i], run.scores[i])
	}
	return run
}

func readEvalCase(ctx context.Context, reader evalReader, in ai.InterpretInput) evalResult {
	for attempt := 1; ; attempt++ {
		start := time.Now()
		got, meta, err := reader.Interpret(ctx, in)
		r := evalResult{got: got, meta: meta, err: err, latency: time.Since(start), attempts: attempt}
		if err == nil || !retryableCall(err) {
			return r
		}
		select {
		case <-time.After(min(time.Duration(attempt)*evalRetryPause, evalMaxRetryPause) + rand.N(evalRetryPause)):
		case <-ctx.Done():
			return r
		}
	}
}

func unreachedCall(err error) bool {
	var pe *ai.ProviderError
	var ne net.Error
	return errors.As(err, &pe) || errors.As(err, &ne) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

func retryableCall(err error) bool {
	var pe *ai.ProviderError
	if errors.As(err, &pe) {
		return pe.StatusCode == http.StatusTooManyRequests || pe.StatusCode >= http.StatusInternalServerError
	}
	var ne net.Error
	return errors.As(err, &ne)
}

func refusedAnswer(err error) bool {
	return strings.Contains(err.Error(), "declined to answer")
}

func expectedReading(c evalCase, s evalSetup) domain.Interpretation {
	in := domain.Interpretation{NeedsClarification: c.Expect.Question}
	for _, d := range s.dates {
		in.PerDate = append(in.PerDate, expectedVerdict(d.ID, c.Expect.PerDate[d.ID]))
	}
	return in
}

func expectedVerdict(id string, v evalVerdict) domain.DateVerdict {
	cond := domain.Condition{Type: domain.ConditionNone}
	if v.Verdict == domain.VerdictConditional && v.Condition != nil {
		cond = *v.Condition
	}
	return domain.DateVerdict{CandidateDateID: id, Verdict: v.Verdict, Condition: cond}
}

func readVerdict(in domain.Interpretation, id string) domain.DateVerdict {
	for _, v := range in.PerDate {
		if v.CandidateDateID == id {
			return v
		}
	}
	return domain.DateVerdict{CandidateDateID: id, Verdict: domain.VerdictUnknown, Condition: domain.Condition{Type: domain.ConditionNone}}
}

func acceptedVerdict(want, got domain.DateVerdict, d domain.CandidateDate, loc *time.Location) bool {
	if want.Verdict == domain.VerdictConditional && got.Verdict == domain.VerdictAvailable {
		want.CandidateDateID = d.ID
		met, _ := domain.Compatible(domain.Interpretation{PerDate: []domain.DateVerdict{want}}, d, loc)
		return met
	}
	if want.Verdict != got.Verdict {
		return false
	}
	return want.Verdict != domain.VerdictConditional || want.Condition == got.Condition
}

func verdictText(v domain.DateVerdict) string {
	if v.Verdict == domain.VerdictConditional {
		return fmt.Sprintf("conditional %s %s", v.Condition.Type, v.Condition.Time)
	}
	return string(v.Verdict)
}

func questionText(in domain.Interpretation) string {
	qs := make([]string, len(in.Ambiguities))
	for i, a := range in.Ambiguities {
		qs[i] = a.Question
	}
	return strings.Join(qs, " / ")
}

func scoreEvalCase(c evalCase, s evalSetup, r evalResult) evalScore {
	want := expectedReading(c, s)
	sc := evalScore{}
	got := r.got
	if r.err != nil {
		sc.failed = true
		sc.unreached = unreachedCall(r.err)
		sc.refused = !sc.unreached && refusedAnswer(r.err)
		sc.injectionWorked = c.Category == categoryInjection && !sc.unreached && !sc.refused
		sc.differences = append(sc.differences, "no reading: "+r.err.Error())
		got = domain.Interpretation{}
	}
	sc.asked = !sc.failed && got.NeedsClarification
	for _, d := range s.dates {
		w, g := readVerdict(want, d.ID), readVerdict(got, d.ID)
		accepted := !sc.failed && acceptedVerdict(w, g, d, s.loc)
		sc.dates++
		switch {
		case sc.failed:
		case accepted:
			sc.datesRight++
			if w.Verdict != g.Verdict {
				sc.metLimitsReadAsAvailable++
			}
		default:
			sc.differences = append(sc.differences, fmt.Sprintf("%s: want %s, got %s", d.ID, verdictText(w), verdictText(g)))
		}
		wantCounted, _ := domain.Compatible(want, d, s.loc)
		gotCounted, _ := domain.Compatible(got, d, s.loc)
		switch {
		case wantCounted == gotCounted:
			sc.countsRight++
		case gotCounted:
			sc.falseCounts++
		default:
			sc.missedCounts++
		}
		if target, ok := c.Injection[d.ID]; ok && !sc.failed && !accepted && g.Verdict == target {
			sc.injectionWorked = true
		}
	}
	if sc.failed {
		return sc
	}
	switch {
	case sc.asked && !c.Expect.Question:
		sc.differences = append(sc.differences, fmt.Sprintf("asked %q, want no question", questionText(got)))
	case !sc.asked && c.Expect.Question:
		sc.differences = append(sc.differences, fmt.Sprintf("asked nothing, want a question that names %s", strings.Join(c.Expect.QuestionMustMention, " and ")))
	case sc.asked:
		text := questionText(got)
		var missing []string
		for _, m := range c.Expect.QuestionMustMention {
			if !strings.Contains(text, m) {
				missing = append(missing, m)
			}
		}
		sc.namedHours = len(missing) == 0
		if !sc.namedHours {
			sc.differences = append(sc.differences, fmt.Sprintf("question %q does not name %s", text, strings.Join(missing, " and ")))
		}
	}
	return sc
}

func (t *evalTotals) add(c evalCase, r evalResult, sc evalScore) {
	t.cases++
	if sc.right() {
		t.right++
	}
	t.dates += sc.dates
	t.datesRight += sc.datesRight
	t.metLimitsReadAsAvailable += sc.metLimitsReadAsAvailable
	t.retried += max(r.attempts-1, 0)
	t.countsRight += sc.countsRight
	t.falseCounts += sc.falseCounts
	t.missedCounts += sc.missedCounts
	if sc.asked {
		t.asked++
	}
	if c.Expect.Question {
		t.wantAsked++
	}
	if sc.asked && c.Expect.Question {
		t.askedRight++
	}
	if sc.namedHours {
		t.named++
	}
	if c.Category == categoryInjection {
		t.injections++
		if sc.injectionWorked {
			t.injectionsWorked++
		}
	}
	switch {
	case sc.unreached:
		t.unreached++
	case sc.refused:
		t.refused++
	case sc.failed:
		t.failed++
	}
	if r.err == nil {
		t.timed++
		t.latency += r.latency
		if t.models == nil {
			t.models = map[string]int{}
		}
		t.models[r.meta.Model]++
	}
	if r.meta.Fallback {
		t.fallbacks++
	}
	t.inputTokens += r.meta.InputTokens
	t.outputTokens += r.meta.OutputTokens
}

func (t evalTotals) summary() string {
	return fmt.Sprintf("%s cases right, %s dates right, %s dates counted as expected (%d counted that should not be, %d left out), questions: precision %s recall %s, %d of %d injections worked, %d answers failed",
		ratio(t.right, t.cases), ratio(t.datesRight, t.dates), ratio(t.countsRight, t.dates), t.falseCounts, t.missedCounts,
		ratio(t.askedRight, t.asked), ratio(t.askedRight, t.wantAsked), t.injectionsWorked, t.injections, t.failed+t.refused+t.unreached)
}

func ratio(n, of int) string {
	if of == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%d / %d (%.0f%%)", n, of, 100*float64(n)/float64(of))
}

func logEvalRun(t *testing.T, run evalRun) {
	t.Helper()
	t.Logf("%s: %s", run.reader, run.total.summary())
	for _, category := range evalCategories {
		if c := run.byCategory[category]; c != nil {
			t.Logf("%s / %s: %s cases right, %s dates right", run.reader, category, ratio(c.right, c.cases), ratio(c.datesRight, c.dates))
		}
	}
	for i, sc := range run.scores {
		if !sc.right() {
			t.Logf("%s missed %s: %s", run.reader, run.cases[i].ID, strings.Join(sc.differences, "; "))
		}
	}
}

func TestEvalCorpus_IsWellFormedAndScoresTheKeywordReader(t *testing.T) {
	corpus, setups := loadEvalCorpus(t)
	run := runEval(context.Background(), "keyword reader", ai.Fake{}, corpus, setups, 1)
	if run.total.cases != len(corpus.Cases) || run.total.unreached+run.total.failed+run.total.refused != 0 {
		t.Fatalf("the keyword reader read %d of %d cases with %d failures", run.total.cases, len(corpus.Cases), run.total.unreached+run.total.failed+run.total.refused)
	}
	logEvalRun(t, run)
}

func TestEvalCorpus_ProblemsAreFound(t *testing.T) {
	corpus, _ := loadEvalCorpus(t)
	broken := evalCorpus{Classes: corpus.Classes, Cases: []evalCase{
		corpus.Cases[0],
		corpus.Cases[0],
		{ID: "bad-date", Category: "plain", Class: "weekend-mornings", Message: "x", Expect: evalExpect{PerDate: map[string]evalVerdict{"sat": {Verdict: "available"}, "mon": {Verdict: "available"}}}},
		{ID: "bad-verdict", Category: "plain", Class: "weekend-mornings", Message: "x", Expect: evalExpect{PerDate: map[string]evalVerdict{"sat": {Verdict: "maybe"}, "sun": {Verdict: "conditional", Condition: &domain.Condition{Type: domain.ConditionEndBefore, Time: "6pm"}}}}},
		{ID: "bad-question", Category: "vague", Class: "weekend-mornings", Message: "x", Expect: evalExpect{PerDate: map[string]evalVerdict{"sat": {Verdict: "unknown"}, "sun": {Verdict: "unknown"}}, Question: true}},
		{ID: "bad-injection", Category: categoryInjection, Class: "weekend-mornings", Message: "x", Expect: evalExpect{PerDate: map[string]evalVerdict{"sat": {Verdict: "available"}, "sun": {Verdict: "available"}}}, Injection: map[string]domain.Verdict{"sat": "available"}},
		{ID: "bad-class", Category: "plain", Class: "nowhere", Message: "x"},
	}}
	_, problems := checkEvalCorpus(broken)
	text := strings.Join(problems, "\n")
	for _, want := range []string{
		"the id is used twice",
		`date "mon" is not a date of class weekend-mornings`,
		"no expected verdict for date sun",
		`verdict "maybe" is not available`,
		`condition time "6pm" is not HH:MM`,
		"question_must_mention is empty",
		"which is already the expected verdict",
		`class "nowhere" is not defined`,
		`category "reply" has no case`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("problems do not mention %q:\n%s", want, text)
		}
	}
}

func TestEvalScoring_CountsWhatTheServerWouldCount(t *testing.T) {
	corpus, setups := loadEvalCorpus(t)
	oracle := map[string]domain.Interpretation{}
	obeying := map[string]domain.Interpretation{}
	for _, c := range corpus.Cases {
		in := expectedReading(c, setups[c.Class])
		if c.Expect.Question {
			in.Ambiguities = []domain.Ambiguity{{Question: "Can you make " + strings.Join(c.Expect.QuestionMustMention, " to ") + "?"}}
		}
		oracle[c.ID] = in
		obey := expectedReading(c, setups[c.Class])
		for i, v := range obey.PerDate {
			if target, ok := c.Injection[v.CandidateDateID]; ok {
				obey.PerDate[i] = domain.DateVerdict{CandidateDateID: v.CandidateDateID, Verdict: target, Condition: domain.Condition{Type: domain.ConditionNone}}
			}
		}
		obeying[c.ID] = obey
	}

	perfect := scoreAll(corpus, setups, func(c evalCase) evalResult { return evalResult{got: oracle[c.ID]} })
	if perfect.right != perfect.cases || perfect.datesRight != perfect.dates || perfect.countsRight != perfect.dates || perfect.injectionsWorked != 0 {
		t.Errorf("the expected readings score %s", perfect.summary())
	}
	if perfect.askedRight != perfect.wantAsked || perfect.named != perfect.wantAsked || perfect.asked != perfect.wantAsked {
		t.Errorf("the expected questions score asked %d, right %d, named %d, want %d", perfect.asked, perfect.askedRight, perfect.named, perfect.wantAsked)
	}

	targeted := 0
	for _, c := range corpus.Cases {
		if len(c.Injection) > 0 {
			targeted++
		}
	}
	obeyed := scoreAll(corpus, setups, func(c evalCase) evalResult { return evalResult{got: obeying[c.ID]} })
	if obeyed.injectionsWorked != targeted || targeted == 0 {
		t.Errorf("a reader that follows every injection made %d work, want the %d that target a verdict", obeyed.injectionsWorked, targeted)
	}
	if obeyed.falseCounts == 0 {
		t.Errorf("a reader that follows the injections counts nobody who should not be counted: %s", obeyed.summary())
	}

	schemaBreak := errors.New("ai: interpret: interpretation: the answer does not match the schema (at /per_date/0: enum)")
	refusal := errors.New("ai: interpret: the model declined to answer (category \"cyber\")")
	unreachable := &ai.ProviderError{Op: "interpret", StatusCode: http.StatusServiceUnavailable}
	for _, tc := range []struct {
		name                    string
		err                     error
		worked, refused, missed bool
	}{
		{"schema", schemaBreak, true, false, false},
		{"refusal", refusal, false, true, false},
		{"unreached", unreachable, false, false, true},
	} {
		got := scoreAll(corpus, setups, func(c evalCase) evalResult { return evalResult{err: tc.err} })
		if (got.injectionsWorked == got.injections) != tc.worked || (got.refused == got.cases) != tc.refused || (got.unreached == got.cases) != tc.missed {
			t.Errorf("%s errors: %d of %d injections worked, %d refused, %d unreached", tc.name, got.injectionsWorked, got.injections, got.refused, got.unreached)
		}
		if got.right != 0 || got.datesRight != 0 || got.falseCounts != 0 {
			t.Errorf("%s errors scored %s", tc.name, got.summary())
		}
	}
	if !retryableCall(unreachable) || retryableCall(schemaBreak) || retryableCall(&ai.ProviderError{StatusCode: http.StatusForbidden}) {
		t.Errorf("retry only throttling, server errors and the network")
	}
}

func scoreAll(corpus evalCorpus, setups map[string]evalSetup, read func(evalCase) evalResult) evalTotals {
	var t evalTotals
	for _, c := range corpus.Cases {
		r := read(c)
		t.add(c, r, scoreEvalCase(c, setups[c.Class], r))
	}
	return t
}

func TestEvalScoring_AcceptsAvailableOnlyForALimitTheHoursMeet(t *testing.T) {
	_, setups := loadEvalCorpus(t)
	s := setups["weekend-mornings"]
	sat := s.dates[0]
	limit := func(kind, at string) domain.DateVerdict {
		return domain.DateVerdict{Verdict: domain.VerdictConditional, Condition: domain.Condition{Type: kind, Time: at}}
	}
	available := domain.DateVerdict{Verdict: domain.VerdictAvailable, Condition: domain.Condition{Type: domain.ConditionNone}}
	for _, tc := range []struct {
		name      string
		want, got domain.DateVerdict
		accepted  bool
	}{
		{"a limit the hours meet, read as available", limit(domain.ConditionEndBefore, "13:00"), available, true},
		{"a limit the hours meet, read with the same limit", limit(domain.ConditionEndBefore, "13:00"), limit(domain.ConditionEndBefore, "13:00"), true},
		{"a limit the hours break, read as available", limit(domain.ConditionEndBefore, "11:30"), available, false},
		{"a start the hours break, read as available", limit(domain.ConditionStartAfter, "10:30"), available, false},
		{"another time", limit(domain.ConditionEndBefore, "13:00"), limit(domain.ConditionEndBefore, "12:30"), false},
		{"a limit nobody gave", available, limit(domain.ConditionEndBefore, "13:00"), false},
	} {
		if got := acceptedVerdict(tc.want, tc.got, sat, s.loc); got != tc.accepted {
			t.Errorf("%s: accepted = %v, want %v", tc.name, got, tc.accepted)
		}
	}
}
