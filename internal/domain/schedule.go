package domain

import (
	"maps"
	"slices"
	"time"
)

type Verdict string

const (
	VerdictAvailable   Verdict = "available"
	VerdictUnavailable Verdict = "unavailable"
	VerdictConditional Verdict = "conditional"
	VerdictUnknown     Verdict = "unknown"
)

const (
	ConditionNone       = "none"
	ConditionEndBefore  = "end_before"
	ConditionStartAfter = "start_after"
)

// Condition qualifies a conditional verdict. Time is "HH:MM" on the
// workshop's local clock.
type Condition struct {
	Type string `json:"type"`
	Time string `json:"time"`
}

type DateVerdict struct {
	CandidateDateID string    `json:"candidate_date_id"`
	Verdict         Verdict   `json:"verdict"`
	Condition       Condition `json:"condition"`
	Evidence        string    `json:"evidence"`
}

type Ambiguity struct {
	Question string `json:"question"`
	Why      string `json:"why"`
}

// Interpretation is the structured reading of a participant's free-text
// availability, one verdict per candidate date.
type Interpretation struct {
	PerDate            []DateVerdict `json:"per_date"`
	HardConstraints    []string      `json:"hard_constraints"`
	Preferences        []string      `json:"preferences"`
	Ambiguities        []Ambiguity   `json:"ambiguities"`
	NeedsClarification bool          `json:"needs_clarification"`
}

// Compatible reports whether a participant can attend date. Conditions are
// evaluated on the wall clock of loc, the workshop's time zone; a missing
// verdict counts as unknown. When the answer is no, blocker says why.
func Compatible(in Interpretation, d CandidateDate, loc *time.Location) (ok bool, blocker string) {
	if in.NeedsClarification {
		return false, "needs clarification"
	}
	v := DateVerdict{Verdict: VerdictUnknown}
	for _, dv := range in.PerDate {
		if dv.CandidateDateID == d.ID {
			v = dv
			break
		}
	}
	switch v.Verdict {
	case VerdictAvailable:
		return true, ""
	case VerdictUnavailable:
		return false, "unavailable"
	case VerdictConditional:
		return conditionHolds(v.Condition, d, loc)
	default:
		return false, "unknown"
	}
}

// conditionHolds anchors the "HH:MM" limit on the local day the date starts,
// so a date running past local midnight cannot satisfy end_before the next day.
func conditionHolds(c Condition, d CandidateDate, loc *time.Location) (bool, string) {
	if c.Type != ConditionEndBefore && c.Type != ConditionStartAfter {
		return false, "invalid condition"
	}
	clock, err := time.Parse("15:04", c.Time)
	if err != nil {
		return false, "invalid condition"
	}
	year, month, day := d.StartsAt.In(loc).Date()
	limit := time.Date(year, month, day, clock.Hour(), clock.Minute(), 0, 0, loc)
	if c.Type == ConditionEndBefore {
		if d.EndsAt.After(limit) {
			return false, "ends after " + c.Time
		}
		return true, ""
	}
	if d.StartsAt.Before(limit) {
		return false, "starts before " + c.Time
	}
	return true, ""
}

// DateSummary lists, for one candidate date, the compatible participant IDs
// (sorted) and why each of the others cannot attend.
type DateSummary struct {
	DateID     string
	Compatible []string
	Blockers   map[string]string
}

type Recommendation struct {
	DateID       string
	MeetsMinimum bool
	PerDate      []DateSummary
}

// Recommend picks the date with the most compatible participants, the
// earliest one on a tie. When no date reaches the workshop minimum it still
// returns the best date with MeetsMinimum false. PerDate follows start order.
func Recommend(w Workshop, dates []CandidateDate, ins map[string]Interpretation, loc *time.Location) Recommendation {
	sorted := slices.Clone(dates)
	slices.SortStableFunc(sorted, func(a, b CandidateDate) int { return a.StartsAt.Compare(b.StartsAt) })
	participants := slices.Sorted(maps.Keys(ins))

	rec := Recommendation{PerDate: make([]DateSummary, 0, len(sorted))}
	best := -1
	for i, d := range sorted {
		s := DateSummary{DateID: d.ID, Compatible: []string{}, Blockers: map[string]string{}}
		for _, id := range participants {
			if ok, blocker := Compatible(ins[id], d, loc); ok {
				s.Compatible = append(s.Compatible, id)
			} else {
				s.Blockers[id] = blocker
			}
		}
		rec.PerDate = append(rec.PerDate, s)
		if best < 0 || len(s.Compatible) > len(rec.PerDate[best].Compatible) {
			best = i
		}
	}
	if best >= 0 {
		rec.DateID = rec.PerDate[best].DateID
		rec.MeetsMinimum = len(rec.PerDate[best].Compatible) >= w.Minimum
	}
	return rec
}
