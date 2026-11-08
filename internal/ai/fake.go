package ai

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/aalvaropc/commitpool/internal/domain"
)

type Fake struct {
	Script map[string]Scripted
}

type Scripted struct {
	Verdicts        []domain.DateVerdict
	Question        string
	Why             string
	HardConstraints []string
	Preferences     []string
	Follows         string
}

var _ Interpreter = Fake{}

const (
	fakeProvider = "fake"
	fakeModel    = "keywords-v1"
	scriptModel  = "script-v1"
)

var (
	plainPunctuation = strings.NewReplacer("’", "'", "‘", "'", "“", `"`, "”", `"`, "‐", "-", "‑", "-", "‒", "-", "–", "-", "—", "-", "―", "-", "−", "-")
	dashRun          = regexp.MustCompile(`\s*-+\s*`)
)

func scriptKey(message string) string {
	plain := dashRun.ReplaceAllString(plainPunctuation.Replace(message), " - ")
	return strings.Join(strings.Fields(plain), " ")
}

func (f Fake) scripted(in InterpretInput) (Scripted, bool) {
	key := scriptKey(in.Message)
	for message, s := range f.Script {
		if scriptKey(message) != key {
			continue
		}
		if s.Follows != "" && (in.Earlier == nil || scriptKey(in.Earlier.Message) != scriptKey(s.Follows)) {
			continue
		}
		return s, true
	}
	return Scripted{}, false
}

func (s Scripted) Reading(dates []DateOption) domain.Interpretation {
	out := domain.Interpretation{
		PerDate:         make([]domain.DateVerdict, len(dates)),
		HardConstraints: append([]string{}, s.HardConstraints...),
		Preferences:     append([]string{}, s.Preferences...),
		Ambiguities:     []domain.Ambiguity{},
	}
	for i, d := range dates {
		v := domain.DateVerdict{Verdict: domain.VerdictUnknown}
		if i < len(s.Verdicts) {
			v = s.Verdicts[i]
		}
		v.CandidateDateID = d.ID
		if v.Verdict == "" {
			v.Verdict = domain.VerdictUnknown
		}
		if v.Verdict != domain.VerdictConditional {
			v.Condition = domain.Condition{Type: domain.ConditionNone}
		}
		out.PerDate[i] = v
	}
	if s.Question != "" {
		why := s.Why
		if why == "" {
			why = "The answer decides which dates count."
		}
		out.NeedsClarification = true
		out.Ambiguities = append(out.Ambiguities, domain.Ambiguity{Question: s.Question, Why: why})
	}
	return out
}

var (
	clauseBreak = regexp.MustCompile(`(?i)[,;.!?]|\bbut\b`)
	meridiemDot = regexp.MustCompile(`(?i)\b([ap])\.m\.`)
	negation    = regexp.MustCompile(`\b(can't|cant|cannot|can not|no)\b`)
	timeLimit   = regexp.MustCompile(`\b(before|by|after)\s+(\d{1,2}(?::\d{2})?|one|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve|noon)\s*(am|pm)?\b`)
	vagueTime   = regexp.MustCompile(`\b(afternoon|evening)\b`)

	hourWords = map[string]int{
		"one": 1, "two": 2, "three": 3, "four": 4, "five": 5, "six": 6,
		"seven": 7, "eight": 8, "nine": 9, "ten": 10, "eleven": 11, "twelve": 12, "noon": 12,
	}
	weekdays = []string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"}
)

type reading struct {
	verdict   domain.Verdict
	condition domain.Condition
	evidence  string
	ambiguity *domain.Ambiguity
}

func (r reading) rank() int {
	switch r.verdict {
	case domain.VerdictUnavailable:
		return 3
	case domain.VerdictUnknown:
		return 2
	case domain.VerdictConditional:
		return 1
	}
	return 0
}

func (f Fake) Interpret(_ context.Context, in InterpretInput) (domain.Interpretation, Meta, error) {
	if s, ok := f.scripted(in); ok {
		return s.Reading(in.Dates), Meta{Provider: fakeProvider, Model: scriptModel}, nil
	}
	meta := Meta{Provider: fakeProvider, Model: fakeModel}
	out := domain.Interpretation{
		PerDate:         make([]domain.DateVerdict, 0, len(in.Dates)),
		HardConstraints: []string{},
		Preferences:     []string{},
		Ambiguities:     []domain.Ambiguity{},
	}
	best := make([]*reading, len(in.Dates))
	message := strings.ReplaceAll(meridiemDot.ReplaceAllString(in.Message, "${1}m"), "’", "'")
	for _, clause := range clauseBreak.Split(message, -1) {
		clause = strings.TrimSpace(clause)
		if clause == "" {
			continue
		}
		r := readClause(clause, in.Dates)
		switch {
		case r.ambiguity != nil:
			out.NeedsClarification = true
			out.Ambiguities = append(out.Ambiguities, *r.ambiguity)
		case r.verdict != domain.VerdictAvailable:
			out.HardConstraints = append(out.HardConstraints, clause)
		}
		covered := namedDates(strings.ToLower(clause), in.Dates)
		for i := range in.Dates {
			if covered != nil && !covered[i] {
				continue
			}
			if best[i] == nil || r.rank() > best[i].rank() {
				best[i] = &r
			}
		}
	}
	for i, d := range in.Dates {
		r := reading{verdict: domain.VerdictAvailable, condition: domain.Condition{Type: domain.ConditionNone}}
		if best[i] != nil {
			r = *best[i]
		}
		out.PerDate = append(out.PerDate, domain.DateVerdict{
			CandidateDateID: d.ID, Verdict: r.verdict, Condition: r.condition, Evidence: r.evidence,
		})
	}
	return out, meta, nil
}

func readClause(clause string, dates []DateOption) reading {
	lower := strings.ToLower(clause)
	r := reading{verdict: domain.VerdictAvailable, condition: domain.Condition{Type: domain.ConditionNone}, evidence: clause}
	if negation.MatchString(lower) {
		r.verdict = domain.VerdictUnavailable
		return r
	}
	if m := timeLimit.FindStringSubmatch(lower); m != nil {
		if at, ok := clockTime(m[2], m[3]); ok {
			r.verdict = domain.VerdictConditional
			r.condition = domain.Condition{Type: domain.ConditionEndBefore, Time: at}
			if m[1] == "after" {
				r.condition.Type = domain.ConditionStartAfter
			}
			return r
		}
	}
	if m := vagueTime.FindString(lower); m != "" {
		r.verdict = domain.VerdictUnknown
		r.ambiguity = &domain.Ambiguity{
			Question: hoursQuestion(dates),
			Why:      `"` + m + `" does not say whether the class hours work for them.`,
		}
	}
	return r
}

func hoursQuestion(dates []DateOption) string {
	labels := make([]string, len(dates))
	for i, d := range dates {
		labels[i] = d.Label
	}
	if len(labels) <= 1 {
		return "The class runs " + strings.Join(labels, "") + ". Can you make it then?"
	}
	return "The class runs " + orList(labels) + ". Can you make it then, and on which date?"
}

func orList(items []string) string {
	if len(items) <= 1 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], "; ") + " or " + items[len(items)-1]
}

func clockTime(value, meridiem string) (string, bool) {
	hour, minute := 0, 0
	if h, ok := hourWords[value]; ok {
		hour = h
		if value == "noon" {
			meridiem = "noon"
		}
	} else {
		hs, ms, hasMinutes := strings.Cut(value, ":")
		var err error
		if hour, err = strconv.Atoi(hs); err != nil {
			return "", false
		}
		if hasMinutes {
			if minute, err = strconv.Atoi(ms); err != nil {
				return "", false
			}
		}
	}
	switch {
	case meridiem == "pm" && hour < 12:
		hour += 12
	case meridiem == "am" && hour == 12:
		hour = 0
	case meridiem == "" && hour >= 1 && hour <= 7:
		hour += 12
	}
	if hour > 23 || minute > 59 {
		return "", false
	}
	return fmt.Sprintf("%02d:%02d", hour, minute), true
}

func namedDates(lower string, dates []DateOption) []bool {
	names := false
	for _, day := range weekdays {
		if strings.Contains(lower, day) {
			names = true
			break
		}
	}
	if !names {
		return nil
	}
	named := make([]bool, len(dates))
	for i, d := range dates {
		named[i] = strings.Contains(lower, strings.ToLower(d.StartsAt.Weekday().String()))
	}
	return named
}

func (Fake) Explain(_ context.Context, in ExplainInput) (string, Meta, error) {
	return PlainExplanation(in), Meta{Provider: fakeProvider, Model: fakeModel}, nil
}
