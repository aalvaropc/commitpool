package ai

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/aalvaropc/commitpool/internal/domain"
)

// Fake is a deterministic keyword interpreter for tests and end-to-end runs.
// It never calls a model.
type Fake struct{}

var _ Interpreter = Fake{}

const (
	fakeProvider = "fake"
	fakeModel    = "keywords-v1"
)

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
	weekdays = map[string]string{
		"sun": "sunday", "mon": "monday", "tue": "tuesday", "wed": "wednesday",
		"thu": "thursday", "fri": "friday", "sat": "saturday",
	}
)

// reading is what one clause of the message says about the dates it covers.
type reading struct {
	verdict   domain.Verdict
	condition domain.Condition
	evidence  string
	ambiguity *domain.Ambiguity
}

// rank orders readings from the least to the most restrictive; when several
// clauses cover a date, the most restrictive one wins.
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

// Interpret reads the message clause by clause, splitting at punctuation and
// at "but". In each clause:
//   - "can't", "cannot" or "no" makes the dates it covers unavailable;
//   - "before N" or "by N" makes them conditional on ending by N:00, and
//     "after N" on starting after N:00 (1 to 7 without am/pm read as pm);
//   - "afternoon" or "evening" without an hour leaves them unknown and the
//     reading needs clarification;
//   - anything else makes them available.
//
// A clause naming a weekday covers the dates on that weekday, any other
// clause covers every date, and a date no clause covers is available.
func (Fake) Interpret(_ context.Context, in InterpretInput) (domain.Interpretation, Meta, error) {
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
		r := readClause(clause)
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

func readClause(clause string) reading {
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
			Question: "Which hours in the " + m + " work for you?",
			Why:      `"` + m + `" does not name a time.`,
		}
	}
	return r
}

// clockTime turns "six", "6", "6:30" or "18" plus an optional am/pm into
// "HH:MM".
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

// namedDates marks the dates whose weekday the clause names, or returns nil
// when the clause names no weekday at all; a clause naming only weekdays no
// date falls on covers no date. A date's weekday is the first word of its
// label.
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
		abbr, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(d.Label)), " ")
		if day, ok := weekdays[abbr]; ok && strings.Contains(lower, day) {
			named[i] = true
		}
	}
	return named
}

// Explain names the recommended date and the people who can attend it.
func (Fake) Explain(_ context.Context, in ExplainInput) (string, Meta, error) {
	meta := Meta{Provider: fakeProvider, Model: fakeModel}
	label := ""
	for _, d := range in.Dates {
		if d.ID == in.Recommendation.DateID {
			label = d.Label
		}
	}
	if label == "" {
		return "There is no candidate date to recommend yet.", meta, nil
	}
	var names []string
	for _, s := range in.Recommendation.PerDate {
		if s.DateID != in.Recommendation.DateID {
			continue
		}
		for _, id := range s.Compatible {
			if name, ok := in.Names[id]; ok {
				names = append(names, name)
			} else {
				names = append(names, id)
			}
		}
	}
	if len(names) == 0 {
		return label + " is the earliest date, but nobody can attend it yet.", meta, nil
	}
	return fmt.Sprintf("%s works for the most people (%d, minimum %d): %s.",
		label, len(names), in.Minimum, strings.Join(names, ", ")), meta, nil
}
