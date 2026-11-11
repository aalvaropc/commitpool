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

type Interpretation struct {
	PerDate            []DateVerdict `json:"per_date"`
	HardConstraints    []string      `json:"hard_constraints"`
	Preferences        []string      `json:"preferences"`
	Ambiguities        []Ambiguity   `json:"ambiguities"`
	NeedsClarification bool          `json:"needs_clarification"`
}

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

type DateSummary struct {
	DateID     string
	Compatible []string
	Blockers   map[string]string
}

type Recommendation struct {
	DateID       string
	MeetsMinimum bool
	Tied         bool
	PerDate      []DateSummary
}

func (r Recommendation) LeadingDateIDs() []string {
	if r.DateID == "" {
		return nil
	}
	if !r.Tied {
		return []string{r.DateID}
	}
	lead := -1
	for _, s := range r.PerDate {
		if s.DateID == r.DateID {
			lead = len(s.Compatible)
		}
	}
	var ids []string
	for _, s := range r.PerDate {
		if len(s.Compatible) == lead {
			ids = append(ids, s.DateID)
		}
	}
	return ids
}

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
		for i, s := range rec.PerDate {
			if i != best && len(s.Compatible) > 0 && len(s.Compatible) == len(rec.PerDate[best].Compatible) {
				rec.Tied = true
			}
		}
	}
	return rec
}
