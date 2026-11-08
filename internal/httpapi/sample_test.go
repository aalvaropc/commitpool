package httpapi_test

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/ai"
	"github.com/aalvaropc/commitpool/internal/config"
	"github.com/aalvaropc/commitpool/internal/demo"
	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/httpapi"
)

const samplePath = "/api/v1/sample-reading"

type sampleDateJSON struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Hours string `json:"hours"`
}

type sampleVerdictJSON struct {
	Verdict   string           `json:"verdict"`
	Condition domain.Condition `json:"condition"`
	Evidence  string           `json:"evidence"`
	Counted   bool             `json:"counted"`
}

type sampleJSON struct {
	ReadBy   string                       `json:"read_by"`
	Dates    []sampleDateJSON             `json:"dates"`
	PerDate  map[string]sampleVerdictJSON `json:"per_date"`
	Question string                       `json:"question"`
}

func sampleBody(message string) map[string]string {
	return map[string]string{"message": message}
}

func withBedrock() option { return func(s *setup) { s.cfg.AIProvider = config.AIProviderBedrock } }

type cannedReadingAI struct {
	ai.Fake
	reading domain.Interpretation
	err     error
	mu      sync.Mutex
	got     []ai.InterpretInput
}

func (c *cannedReadingAI) Interpret(_ context.Context, in ai.InterpretInput) (domain.Interpretation, ai.Meta, error) {
	c.mu.Lock()
	c.got = append(c.got, in)
	c.mu.Unlock()
	return c.reading, ai.Meta{Provider: "test", Model: "canned"}, c.err
}

func (c *cannedReadingAI) calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.got)
}

func verdict(id string, v domain.Verdict, cond domain.Condition, evidence string) domain.DateVerdict {
	return domain.DateVerdict{CandidateDateID: id, Verdict: v, Condition: cond, Evidence: evidence}
}

var noCondition = domain.Condition{Type: domain.ConditionNone}

func TestSample_RefusesAnEmptyOrOverlongAnswer(t *testing.T) {
	e := newEnv(t)
	for _, message := range []string{"", "   ", strings.Repeat("a", 161)} {
		got := e.wantError(http.MethodPost, samplePath, sampleBody(message), http.StatusBadRequest, "invalid_message")
		if !strings.Contains(got.Message, "160") {
			t.Errorf("message for %d characters = %q, want the 160-character limit", len(message), got.Message)
		}
	}
	e.call(http.MethodPost, samplePath, sampleBody(strings.Repeat("é", 160)), http.StatusOK, nil)
}

func TestSample_ReadsAgainstTheDemosNextWeekend(t *testing.T) {
	model := &cannedReadingAI{}
	e := newEnv(t, withAI(model))
	var got sampleJSON
	e.call(http.MethodPost, samplePath, sampleBody("Saturday works"), http.StatusOK, &got)
	want := []sampleDateJSON{{Key: "sat", Label: "Sat, Nov 7", Hours: "10 AM–12 PM"}, {Key: "sun", Label: "Sun, Nov 8", Hours: "10 AM–12 PM"}}
	if !reflect.DeepEqual(got.Dates, want) {
		t.Errorf("dates = %+v, want %+v", got.Dates, want)
	}
	if model.calls() != 1 {
		t.Fatalf("Interpret called %d times, want 1", model.calls())
	}
	in := model.got[0]
	if in.WorkshopTitle != "Wheel-throwing for beginners" || in.Timezone != "America/New_York" || in.Message != "Saturday works" {
		t.Errorf("input = %+v", in)
	}
	labels := []string{"Sat, Nov 7, 10 AM–12 PM (New York time)", "Sun, Nov 8, 10 AM–12 PM (New York time)"}
	for i, d := range in.Dates {
		if d.ID != want[i].Key || d.Label != labels[i] {
			t.Errorf("date %d = %+v, want id %s label %q", i, d, want[i].Key, labels[i])
		}
	}
}

func TestSample_DatesMoveWithTheClockAcrossTheEndOfDaylightTime(t *testing.T) {
	cases := []struct {
		now      time.Time
		sat, sun string
	}{
		{time.Date(2026, 10, 27, 13, 30, 0, 0, time.UTC), "Sat, Oct 31", "Sun, Nov 1"},
		{time.Date(2026, 10, 27, 14, 30, 0, 0, time.UTC), "Sat, Nov 7", "Sun, Nov 8"},
		{time.Date(2026, 11, 3, 14, 30, 0, 0, time.UTC), "Sat, Nov 7", "Sun, Nov 8"},
		{time.Date(2026, 11, 26, 16, 0, 0, 0, time.UTC), "Sat, Dec 5", "Sun, Dec 6"},
	}
	for _, tc := range cases {
		t.Run(tc.now.Format(time.RFC3339), func(t *testing.T) {
			e := newEnv(t)
			e.clock.Set(tc.now)
			var got sampleJSON
			e.call(http.MethodPost, samplePath, sampleBody("Saturday works"), http.StatusOK, &got)
			if got.Dates[0].Label != tc.sat || got.Dates[1].Label != tc.sun || got.Dates[0].Hours != "10 AM–12 PM" || got.Dates[1].Hours != "10 AM–12 PM" {
				t.Errorf("dates = %+v, want %s and %s, 10 AM–12 PM", got.Dates, tc.sat, tc.sun)
			}
		})
	}
}

func TestSample_CountsOnlyWhatFitsTheClassHours(t *testing.T) {
	model := &cannedReadingAI{reading: domain.Interpretation{PerDate: []domain.DateVerdict{
		verdict("sat", domain.VerdictConditional, domain.Condition{Type: domain.ConditionEndBefore, Time: "11:00"}, "if we're done by 11"),
		verdict("sun", domain.VerdictAvailable, noCondition, "Sunday works"),
	}}}
	e := newEnv(t, withAI(model))
	var got sampleJSON
	e.call(http.MethodPost, samplePath, sampleBody("Saturday if we're done by 11, Sunday works"), http.StatusOK, &got)
	sat, sun := got.PerDate["sat"], got.PerDate["sun"]
	if sat.Counted || sat.Verdict != "conditional" || sat.Condition != (domain.Condition{Type: domain.ConditionEndBefore, Time: "11:00"}) {
		t.Errorf("sat = %+v, want a conditional end_before 11:00 that is not counted: the class ends at 12", sat)
	}
	if !sun.Counted || sun.Verdict != "available" || sun.Evidence != "Sunday works" {
		t.Errorf("sun = %+v, want available and counted", sun)
	}
	if got.Question != "" {
		t.Errorf("question = %q, want none", got.Question)
	}

	model.reading.PerDate[0].Condition.Time = "18:00"
	e.call(http.MethodPost, samplePath, sampleBody("Saturday if we're done by 6"), http.StatusOK, &got)
	if !got.PerDate["sat"].Counted {
		t.Errorf("sat = %+v, want a condition the class keeps to be counted", got.PerDate["sat"])
	}
}

func TestSample_PassesOnTheFirstQuestionAndCountsNobodyWhoMustAnswerIt(t *testing.T) {
	model := &cannedReadingAI{reading: domain.Interpretation{
		PerDate: []domain.DateVerdict{
			verdict("sat", domain.VerdictAvailable, noCondition, "Saturday works"),
			verdict("sun", domain.VerdictUnknown, noCondition, "afternoons"),
		},
		Ambiguities:        []domain.Ambiguity{{Question: " ", Why: "blank"}, {Question: "The class runs 10 AM–12 PM — can you make Saturday morning?", Why: "afternoons"}, {Question: "Second?", Why: "x"}},
		NeedsClarification: true,
	}}
	e := newEnv(t, withAI(model))
	var got sampleJSON
	e.call(http.MethodPost, samplePath, sampleBody("Saturday works, ideally afternoons"), http.StatusOK, &got)
	if got.Question != "The class runs 10 AM–12 PM — can you make Saturday morning?" {
		t.Errorf("question = %q", got.Question)
	}
	if got.PerDate["sat"].Counted || got.PerDate["sun"].Counted {
		t.Errorf("per_date = %+v, want nobody counted while a question is open", got.PerDate)
	}
}

func TestSample_DropsReadingsForDatesTheSampleDoesNotHave(t *testing.T) {
	model := &cannedReadingAI{reading: domain.Interpretation{PerDate: []domain.DateVerdict{
		verdict("mon", domain.VerdictAvailable, noCondition, "Monday"),
		verdict("sat", domain.VerdictConditional, domain.Condition{Type: "whenever"}, "Saturday"),
	}}}
	e := newEnv(t, withAI(model))
	var got sampleJSON
	e.call(http.MethodPost, samplePath, sampleBody("Monday, Saturday"), http.StatusOK, &got)
	if len(got.PerDate) != 2 || got.PerDate["sat"].Verdict != "unknown" || got.PerDate["sun"].Verdict != "unknown" || got.PerDate["sat"].Counted {
		t.Errorf("per_date = %+v, want sat and sun only, both unknown and not counted", got.PerDate)
	}
}

func TestSample_SaysWhoReadTheAnswer(t *testing.T) {
	cases := []struct {
		name string
		opts []option
		want string
	}{
		{"fake provider", nil, "rules"},
		{"bedrock", []option{withBedrock(), withAI(&cannedReadingAI{})}, "claude"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got sampleJSON
			newEnv(t, tc.opts...).call(http.MethodPost, samplePath, sampleBody("Saturday works"), http.StatusOK, &got)
			if got.ReadBy != tc.want {
				t.Errorf("read_by = %q, want %s", got.ReadBy, tc.want)
			}
		})
	}
}

func TestSample_HasItsOwnLimitPerVisitor(t *testing.T) {
	e := newEnv(t, withRateLimit(600))
	for i := range 3 {
		if rec := e.request(http.MethodPost, samplePath, sampleBody("Saturday works")); rec.Code != http.StatusOK {
			t.Fatalf("answer %d = %d %s", i+1, rec.Code, rec.Body)
		}
	}
	rec := e.request(http.MethodPost, samplePath, sampleBody("Saturday works"))
	if after := rec.Header().Get("Retry-After"); after != "20" {
		t.Errorf("Retry-After = %q, want 20 seconds", after)
	}
	e.wantError(http.MethodPost, samplePath, sampleBody("Saturday works"), http.StatusTooManyRequests, "sample_rate_limited")
	e.clock.Set(e.clock.Now().Add(20 * time.Second))
	e.call(http.MethodPost, samplePath, sampleBody("Saturday works"), http.StatusOK, nil)
}

func TestSample_HasItsOwnCapPerHour(t *testing.T) {
	e := newEnv(t)
	for i := range 60 {
		if rec := e.request(http.MethodPost, samplePath, sampleBody("Saturday works")); rec.Code != http.StatusOK {
			t.Fatalf("answer %d = %d %s", i+1, rec.Code, rec.Body)
		}
	}
	rec := e.request(http.MethodPost, samplePath, sampleBody("Saturday works"))
	if after := rec.Header().Get("Retry-After"); after != "3600" {
		t.Errorf("Retry-After = %q, want an hour", after)
	}
	got := e.wantError(http.MethodPost, samplePath, sampleBody("Saturday works"), http.StatusTooManyRequests, "sample_capped")
	if !strings.Contains(got.Message, "60 minutes") {
		t.Errorf("message = %q, want the wait", got.Message)
	}
	e.clock.Set(e.clock.Now().Add(time.Hour))
	e.call(http.MethodPost, samplePath, sampleBody("Saturday works"), http.StatusOK, nil)
}

func TestSample_ACappedModelIs429WithTheRealWaitAndDoesNotUseTheCap(t *testing.T) {
	e := newEnv(t, withAI(&cappedAI{wait: 7*time.Minute + 30*time.Second, readings: true}))
	rec := e.request(http.MethodPost, samplePath, sampleBody("Saturday works"))
	if after := rec.Header().Get("Retry-After"); after != "450" {
		t.Errorf("Retry-After = %q, want 450 seconds", after)
	}
	got := e.wantError(http.MethodPost, samplePath, sampleBody("Saturday works"), http.StatusTooManyRequests, "ai_capped")
	if got.Message != "Claude has read a lot of messages this hour. Please try again in 8 minutes." {
		t.Errorf("message = %q", got.Message)
	}
	for range 60 {
		e.wantError(http.MethodPost, samplePath, sampleBody("Saturday works"), http.StatusTooManyRequests, "ai_capped")
	}
	noErrorLogged(t, e)
}

type roomlessAI struct {
	cannedReadingAI
	wait time.Duration
}

func (r *roomlessAI) Room(int) time.Duration { return r.wait }

func TestSample_LeavesTheLastReadingsOfTheBudgetToParticipants(t *testing.T) {
	model := &roomlessAI{wait: 12 * time.Minute}
	e := newEnv(t, withAI(model))
	got := e.wantError(http.MethodPost, samplePath, sampleBody("Saturday works"), http.StatusTooManyRequests, "ai_capped")
	if got.Message != "Claude has read a lot of messages this hour. Please try again in 12 minutes." {
		t.Errorf("message = %q", got.Message)
	}
	if model.calls() != 0 {
		t.Errorf("Interpret called %d times while the budget had no room", model.calls())
	}
}

func TestSample_AFailedModelIs503(t *testing.T) {
	e := newEnv(t, withAI(&cannedReadingAI{err: errors.New("bedrock: throttled")}))
	e.wantError(http.MethodPost, samplePath, sampleBody("Saturday works"), http.StatusServiceUnavailable, "ai_unavailable")
}

type stalledAI struct {
	ai.Fake
	entered chan struct{}
	release chan struct{}
}

func (s stalledAI) Interpret(ctx context.Context, in ai.InterpretInput) (domain.Interpretation, ai.Meta, error) {
	if !strings.HasPrefix(in.Message, "sample") {
		return s.Fake.Interpret(ctx, in)
	}
	s.entered <- struct{}{}
	select {
	case <-s.release:
		return s.Fake.Interpret(ctx, in)
	case <-ctx.Done():
		return domain.Interpretation{}, ai.Meta{}, ctx.Err()
	}
}

func TestSample_GivesUpAfterItsTimeout(t *testing.T) {
	httpapi.SetSampleTimeout(t, 50*time.Millisecond)
	model := stalledAI{entered: make(chan struct{}, 1), release: make(chan struct{})}
	e := newEnv(t, withAI(model))
	e.wantError(http.MethodPost, samplePath, sampleBody("sample Saturday works"), http.StatusServiceUnavailable, "ai_unavailable")
}

func TestSample_NeverTakesTheReadersParticipantsNeed(t *testing.T) {
	model := stalledAI{entered: make(chan struct{}, 4), release: make(chan struct{})}
	e := newEnv(t, withAI(model))
	h := e.createWorkshop()
	var wg sync.WaitGroup
	var codes [2]atomic.Int32
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i].Store(int32(e.request(http.MethodPost, samplePath, sampleBody("sample Saturday works")).Code))
		}()
	}
	<-model.entered
	<-model.entered

	rec := e.request(http.MethodPost, samplePath, sampleBody("sample Sunday works"))
	if after := rec.Header().Get("Retry-After"); after != "5" {
		t.Errorf("Retry-After = %q, want 5 seconds", after)
	}
	e.wantError(http.MethodPost, samplePath, sampleBody("sample Sunday works"), http.StatusTooManyRequests, "sample_busy")
	e.joinAndSay(h, "Ana", "Any day works for me")

	close(model.release)
	wg.Wait()
	for i := range codes {
		if code := codes[i].Load(); code != http.StatusOK {
			t.Errorf("held answer %d = %d, want 200 once Claude answers", i+1, code)
		}
	}
}

func TestSample_NeverLogsTheAnswer(t *testing.T) {
	const secret = "Saturday works, ask for zebra-umbrella-42"
	earlier := map[string]any{"message": "afternoons, says quokka-lantern-7", "questions": []string{"Which morning, asks marmot-kettle-9?"}}
	models := map[string]ai.Interpreter{
		"read":   ai.Fake{},
		"failed": &cannedReadingAI{err: errors.New("bedrock: throttled")},
		"capped": &cappedAI{wait: time.Minute, readings: true},
	}
	for name, model := range models {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, withAI(model))
			e.request(http.MethodPost, samplePath, sampleBody(secret))
			e.request(http.MethodPost, samplePath, map[string]any{"message": secret, "earlier": earlier})
			logs := e.logs.String()
			if !strings.Contains(logs, "sample reading") || !strings.Contains(logs, "length=") {
				t.Fatalf("no sample reading line with its length; the check would be vacuous:\n%s", logs)
			}
			if !strings.Contains(logs, "follows_up=true") && name == "read" {
				t.Errorf("the answer to a question is not logged as one:\n%s", logs)
			}
			if strings.Contains(logs, "zebra") || strings.Contains(logs, "umbrella") || strings.Contains(logs, "quokka") || strings.Contains(logs, "marmot") {
				t.Errorf("log contains the visitor's answer:\n%s", logs)
			}
		})
	}
}

func TestSample_ReadsAnAnswerToAQuestionWithTheEarlierAnswerAsData(t *testing.T) {
	model := &cannedReadingAI{}
	e := newEnv(t, withAI(model))
	e.call(http.MethodPost, samplePath, sampleBody("Saturday works"), http.StatusOK, nil)
	e.call(http.MethodPost, samplePath, map[string]any{
		"message": "Saturday morning works",
		"earlier": map[string]any{"message": "  ideally afternoons  ", "questions": []string{" The class runs 10 AM–12 PM — can you make a morning? "}},
	}, http.StatusOK, nil)
	if model.calls() != 2 {
		t.Fatalf("Interpret called %d times, want 2", model.calls())
	}
	if model.got[0].Earlier != nil {
		t.Errorf("a first answer carries earlier context %+v", model.got[0].Earlier)
	}
	want := &ai.EarlierAnswer{Message: "ideally afternoons", Questions: []string{"The class runs 10 AM–12 PM — can you make a morning?"}}
	if got := model.got[1]; got.Message != "Saturday morning works" || !reflect.DeepEqual(got.Earlier, want) {
		t.Errorf("second answer = %q with earlier %+v, want the new message alone and %+v as context", got.Message, got.Earlier, want)
	}
}

func TestSample_TheDemoCastsReplyIsReadInContext(t *testing.T) {
	grace := demo.Maya.People[len(demo.Maya.People)-1]
	reply := demo.Maya.Replies[0]
	e := newEnv(t)
	var alone, inContext sampleJSON
	e.call(http.MethodPost, samplePath, sampleBody(reply.Message), http.StatusOK, &alone)
	e.call(http.MethodPost, samplePath, map[string]any{
		"message": reply.Message,
		"earlier": map[string]any{"message": grace.Message, "questions": []string{grace.Expect.Question}},
	}, http.StatusOK, &inContext)
	if !inContext.PerDate["sat"].Counted || inContext.PerDate["sun"].Counted {
		t.Errorf("reply read in context = %+v, want Saturday counted and Sunday not", inContext.PerDate)
	}
	if reflect.DeepEqual(alone.PerDate, inContext.PerDate) {
		t.Errorf("the earlier answer changed nothing: %+v", alone.PerDate)
	}
}

func TestSample_RefusesAnEarlierAnswerOutOfBounds(t *testing.T) {
	model := &cannedReadingAI{}
	e := newEnv(t, withAI(model))
	question := "Which morning?"
	cases := map[string]map[string]any{
		"empty message":      {"message": " ", "questions": []string{question}},
		"long message":       {"message": strings.Repeat("a", 161), "questions": []string{question}},
		"too many questions": {"message": "afternoons", "questions": []string{question, question, question, question}},
		"long question":      {"message": "afternoons", "questions": []string{strings.Repeat("q", 201)}},
		"blank question":     {"message": "afternoons", "questions": []string{" "}},
	}
	for name, earlier := range cases {
		got := e.wantError(http.MethodPost, samplePath, map[string]any{"message": "Saturday", "earlier": earlier}, http.StatusBadRequest, "invalid_earlier")
		if !strings.Contains(got.Message, "160") || !strings.Contains(got.Message, "200") {
			t.Errorf("%s: message = %q, want the limits", name, got.Message)
		}
	}
	ok := map[string]any{"message": strings.Repeat("é", 160), "questions": []string{strings.Repeat("q", 200), question, question}}
	e.call(http.MethodPost, samplePath, map[string]any{"message": "Saturday", "earlier": ok}, http.StatusOK, nil)
	if model.calls() != 1 {
		t.Errorf("Interpret called %d times, want only the valid request read", model.calls())
	}
}
