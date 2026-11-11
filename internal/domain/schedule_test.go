package domain_test

import (
	"slices"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
)

func mustLoad(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("LoadLocation(%q): %v", name, err)
	}
	return loc
}

func verdictFor(dateID string, v domain.Verdict) domain.DateVerdict {
	return domain.DateVerdict{CandidateDateID: dateID, Verdict: v, Condition: domain.Condition{Type: domain.ConditionNone}}
}

func conditionalFor(dateID, condType, at string) domain.DateVerdict {
	return domain.DateVerdict{CandidateDateID: dateID, Verdict: domain.VerdictConditional, Condition: domain.Condition{Type: condType, Time: at}}
}

func interpretation(vs ...domain.DateVerdict) domain.Interpretation {
	return domain.Interpretation{PerDate: vs}
}

func TestCompatible_EndBeforeUsesWorkshopTimezone(t *testing.T) {
	d := domain.CandidateDate{
		ID:       "d1",
		StartsAt: time.Date(2026, 11, 14, 20, 0, 0, 0, time.UTC),
		EndsAt:   time.Date(2026, 11, 14, 23, 0, 0, 0, time.UTC),
	}
	in := interpretation(conditionalFor("d1", domain.ConditionEndBefore, "18:00"))

	ok, blocker := domain.Compatible(in, d, mustLoad(t, "America/Lima"))
	if !ok || blocker != "" {
		t.Fatalf("Compatible in America/Lima (ends 18:00 local) = %v, %q; want true, \"\"", ok, blocker)
	}

	ok, blocker = domain.Compatible(in, d, time.UTC)
	if ok || blocker != "ends after 18:00" {
		t.Fatalf("Compatible in UTC (ends 23:00 local) = %v, %q; want false, \"ends after 18:00\"", ok, blocker)
	}
}

func TestCompatible_NeedsClarificationBlocks(t *testing.T) {
	d := domain.CandidateDate{ID: "d1", StartsAt: startsAt, EndsAt: startsAt.Add(3 * time.Hour)}
	in := interpretation(verdictFor("d1", domain.VerdictAvailable))
	in.NeedsClarification = true

	ok, blocker := domain.Compatible(in, d, time.UTC)
	if ok || blocker != "needs clarification" {
		t.Fatalf("Compatible() = %v, %q; want false, \"needs clarification\"", ok, blocker)
	}
}

func TestCompatible_VerdictsAndConditions(t *testing.T) {
	lima := mustLoad(t, "America/Lima")
	saturday := domain.CandidateDate{
		ID:       "d1",
		StartsAt: time.Date(2026, 11, 14, 20, 0, 0, 0, time.UTC),
		EndsAt:   time.Date(2026, 11, 14, 23, 0, 0, 0, time.UTC),
	}
	overnight := domain.CandidateDate{
		ID:       "d1",
		StartsAt: time.Date(2026, 11, 15, 1, 0, 0, 0, time.UTC),
		EndsAt:   time.Date(2026, 11, 15, 5, 0, 0, 0, time.UTC),
	}
	tests := []struct {
		name        string
		in          domain.Interpretation
		date        domain.CandidateDate
		wantOK      bool
		wantBlocker string
	}{
		{"available", interpretation(verdictFor("d1", domain.VerdictAvailable)), saturday, true, ""},
		{"unavailable", interpretation(verdictFor("d1", domain.VerdictUnavailable)), saturday, false, "unavailable"},
		{"unknown", interpretation(verdictFor("d1", domain.VerdictUnknown)), saturday, false, "unknown"},
		{"no verdict for the date", interpretation(verdictFor("other", domain.VerdictAvailable)), saturday, false, "unknown"},
		{"end before, ends exactly then", interpretation(conditionalFor("d1", domain.ConditionEndBefore, "18:00")), saturday, true, ""},
		{"end before, ends a minute later", interpretation(conditionalFor("d1", domain.ConditionEndBefore, "17:59")), saturday, false, "ends after 17:59"},
		{"start after, starts exactly then", interpretation(conditionalFor("d1", domain.ConditionStartAfter, "15:00")), saturday, true, ""},
		{"start after, starts earlier", interpretation(conditionalFor("d1", domain.ConditionStartAfter, "15:30")), saturday, false, "starts before 15:30"},
		{"conditional without a usable condition", interpretation(conditionalFor("d1", domain.ConditionNone, "")), saturday, false, "invalid condition"},
		{"conditional with an unparseable time", interpretation(conditionalFor("d1", domain.ConditionEndBefore, "6pm")), saturday, false, "invalid condition"},
		{"conditional with an unknown type", interpretation(conditionalFor("d1", "around", "18:00")), saturday, false, "invalid condition"},
		{"end before, event crossing local midnight", interpretation(conditionalFor("d1", domain.ConditionEndBefore, "23:30")), overnight, false, "ends after 23:30"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, blocker := domain.Compatible(tt.in, tt.date, lima)
			if ok != tt.wantOK || blocker != tt.wantBlocker {
				t.Fatalf("Compatible() = %v, %q; want %v, %q", ok, blocker, tt.wantOK, tt.wantBlocker)
			}
		})
	}
}

func summaryFor(t *testing.T, r domain.Recommendation, dateID string) domain.DateSummary {
	t.Helper()
	for _, s := range r.PerDate {
		if s.DateID == dateID {
			return s
		}
	}
	t.Fatalf("no summary for date %q in %+v", dateID, r.PerDate)
	return domain.DateSummary{}
}

func TestRecommend_NoDateMeetsMinimumReportsBlockers(t *testing.T) {
	w := workshopIn(domain.WorkshopCollecting, domain.ResolutionOpen)
	dates := []domain.CandidateDate{
		{ID: "d1", StartsAt: startsAt, EndsAt: startsAt.Add(3 * time.Hour)},
		{ID: "d2", StartsAt: startsAt.Add(24 * time.Hour), EndsAt: startsAt.Add(27 * time.Hour)},
	}
	ins := map[string]domain.Interpretation{
		"p1": interpretation(verdictFor("d1", domain.VerdictAvailable), verdictFor("d2", domain.VerdictAvailable)),
		"p2": interpretation(verdictFor("d1", domain.VerdictUnavailable), verdictFor("d2", domain.VerdictAvailable)),
		"p3": interpretation(verdictFor("d1", domain.VerdictUnknown), verdictFor("d2", domain.VerdictUnknown)),
	}

	got := domain.Recommend(w, dates, ins, time.UTC)

	if got.DateID != "d2" || got.MeetsMinimum {
		t.Fatalf("Recommend() = date %q meets=%v; want d2, false", got.DateID, got.MeetsMinimum)
	}
	if len(got.PerDate) != 2 {
		t.Fatalf("PerDate has %d entries, want 2", len(got.PerDate))
	}
	d2 := summaryFor(t, got, "d2")
	if !slices.Equal(d2.Compatible, []string{"p1", "p2"}) {
		t.Errorf("d2 Compatible = %v, want [p1 p2]", d2.Compatible)
	}
	if d2.Blockers["p3"] != "unknown" || len(d2.Blockers) != 1 {
		t.Errorf("d2 Blockers = %v, want only p3: unknown", d2.Blockers)
	}
	d1 := summaryFor(t, got, "d1")
	if !slices.Equal(d1.Compatible, []string{"p1"}) {
		t.Errorf("d1 Compatible = %v, want [p1]", d1.Compatible)
	}
	if d1.Blockers["p2"] != "unavailable" || d1.Blockers["p3"] != "unknown" || len(d1.Blockers) != 2 {
		t.Errorf("d1 Blockers = %v, want p2: unavailable, p3: unknown", d1.Blockers)
	}
}

func TestRecommend_PicksDateWithMostCompatible(t *testing.T) {
	w := workshopIn(domain.WorkshopCollecting, domain.ResolutionOpen)
	w.Minimum = 2
	day := func(n int) domain.CandidateDate {
		s := time.Date(2026, 11, n, 15, 0, 0, 0, time.UTC)
		return domain.CandidateDate{ID: "d" + time.Date(2026, 11, n, 0, 0, 0, 0, time.UTC).Format("02"), StartsAt: s, EndsAt: s.Add(3 * time.Hour)}
	}
	unsorted := []domain.CandidateDate{day(16), day(14), day(15)}

	tests := []struct {
		name     string
		ins      map[string]domain.Interpretation
		wantDate string
		wantMeet bool
	}{
		{
			name: "unique maximum on the latest date",
			ins: map[string]domain.Interpretation{
				"p1": interpretation(verdictFor("d14", domain.VerdictUnavailable), verdictFor("d15", domain.VerdictAvailable), verdictFor("d16", domain.VerdictAvailable)),
				"p2": interpretation(verdictFor("d14", domain.VerdictUnavailable), verdictFor("d15", domain.VerdictUnavailable), verdictFor("d16", domain.VerdictAvailable)),
				"p3": interpretation(verdictFor("d14", domain.VerdictAvailable), verdictFor("d15", domain.VerdictUnavailable), verdictFor("d16", domain.VerdictAvailable)),
			},
			wantDate: "d16",
			wantMeet: true,
		},
		{
			name: "tie goes to the earliest start",
			ins: map[string]domain.Interpretation{
				"p1": interpretation(verdictFor("d14", domain.VerdictUnavailable), verdictFor("d15", domain.VerdictAvailable), verdictFor("d16", domain.VerdictAvailable)),
				"p2": interpretation(verdictFor("d14", domain.VerdictUnavailable), verdictFor("d15", domain.VerdictAvailable), verdictFor("d16", domain.VerdictAvailable)),
				"p3": interpretation(verdictFor("d14", domain.VerdictAvailable), verdictFor("d15", domain.VerdictUnavailable), verdictFor("d16", domain.VerdictUnavailable)),
			},
			wantDate: "d15",
			wantMeet: true,
		},
		{
			name: "best date below the minimum",
			ins: map[string]domain.Interpretation{
				"p1": interpretation(verdictFor("d14", domain.VerdictUnavailable), verdictFor("d15", domain.VerdictUnavailable), verdictFor("d16", domain.VerdictAvailable)),
				"p2": interpretation(verdictFor("d14", domain.VerdictUnavailable), verdictFor("d15", domain.VerdictUnavailable), verdictFor("d16", domain.VerdictUnavailable)),
			},
			wantDate: "d16",
			wantMeet: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := domain.Recommend(w, unsorted, tt.ins, time.UTC)
			if got.DateID != tt.wantDate || got.MeetsMinimum != tt.wantMeet {
				t.Fatalf("Recommend() = date %q meets=%v; want %q, %v", got.DateID, got.MeetsMinimum, tt.wantDate, tt.wantMeet)
			}
			order := make([]string, len(got.PerDate))
			for i, s := range got.PerDate {
				order[i] = s.DateID
			}
			if !slices.Equal(order, []string{"d14", "d15", "d16"}) {
				t.Errorf("PerDate order = %v, want [d14 d15 d16]", order)
			}
			for _, s := range got.PerDate {
				if !slices.IsSorted(s.Compatible) {
					t.Errorf("%s Compatible = %v, want sorted", s.DateID, s.Compatible)
				}
			}
		})
	}
}

func TestRecommend_NoCandidateDatesRecommendsNothing(t *testing.T) {
	w := workshopIn(domain.WorkshopCollecting, domain.ResolutionOpen)
	got := domain.Recommend(w, nil, nil, time.UTC)
	if got.DateID != "" || got.MeetsMinimum || len(got.PerDate) != 0 {
		t.Fatalf("Recommend() = %+v, want the zero Recommendation", got)
	}
}

func TestRecommend_TieIsMarked(t *testing.T) {
	w := workshopIn(domain.WorkshopCollecting, domain.ResolutionOpen)
	w.Minimum = 3
	day := func(n int) domain.CandidateDate {
		s := time.Date(2026, 11, n, 15, 0, 0, 0, time.UTC)
		return domain.CandidateDate{ID: "d" + time.Date(2026, 11, n, 0, 0, 0, 0, time.UTC).Format("02"), StartsAt: s, EndsAt: s.Add(3 * time.Hour)}
	}
	dates := []domain.CandidateDate{day(15), day(14)}
	available := func(a, b domain.Verdict) map[string]domain.Interpretation {
		return map[string]domain.Interpretation{
			"p1": interpretation(verdictFor("d14", domain.VerdictAvailable), verdictFor("d15", domain.VerdictUnavailable)),
			"p2": interpretation(verdictFor("d14", domain.VerdictAvailable), verdictFor("d15", domain.VerdictUnavailable)),
			"p3": interpretation(verdictFor("d14", domain.VerdictUnavailable), verdictFor("d15", domain.VerdictAvailable)),
			"p4": interpretation(verdictFor("d14", a), verdictFor("d15", b)),
		}
	}
	tied := domain.Recommend(w, dates, available(domain.VerdictUnavailable, domain.VerdictAvailable), time.UTC)
	if !tied.Tied || tied.DateID != "d14" {
		t.Fatalf("2-2 Recommend() = date %q tied=%v, want d14 and tied", tied.DateID, tied.Tied)
	}
	lead := domain.Recommend(w, dates, available(domain.VerdictAvailable, domain.VerdictUnavailable), time.UTC)
	if lead.Tied || lead.DateID != "d14" {
		t.Fatalf("3-1 Recommend() = date %q tied=%v, want d14 and not tied", lead.DateID, lead.Tied)
	}
	if ids := tied.LeadingDateIDs(); !slices.Equal(ids, []string{"d14", "d15"}) {
		t.Fatalf("tied LeadingDateIDs = %v, want [d14 d15]", ids)
	}
	if ids := lead.LeadingDateIDs(); !slices.Equal(ids, []string{"d14"}) {
		t.Fatalf("clear LeadingDateIDs = %v, want [d14]", ids)
	}
	if none := domain.Recommend(w, dates, map[string]domain.Interpretation{"p1": interpretation(verdictFor("d14", domain.VerdictUnavailable), verdictFor("d15", domain.VerdictUnavailable))}, time.UTC); none.Tied {
		t.Fatalf("nobody can come anywhere, got tied=%v", none.Tied)
	}
	if alone := domain.Recommend(w, dates[:1], available(domain.VerdictUnavailable, domain.VerdictAvailable), time.UTC); alone.Tied {
		t.Fatalf("one date cannot tie, got tied=%v", alone.Tied)
	}
}
