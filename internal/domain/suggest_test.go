package domain_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
)

var noon = time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

func TestSuggestions_WhileFindingTheDate(t *testing.T) {
	got := domain.Suggestions(domain.SuggestInput{
		Status:  domain.WorkshopCollecting,
		Unclear: []string{"grace"},
		Date: domain.DateSummary{DateID: "sat", Compatible: []string{"priya"}, Blockers: map[string]string{
			"ben": "ends after 18:00", "lucia": "unavailable", "grace": "needs clarification", "ana": "starts before 11:00",
		}},
	})
	want := []domain.Suggestion{
		{Kind: domain.SuggestClarify, ParticipantID: "grace"},
		{Kind: domain.SuggestCondition, ParticipantID: "ana", Blocker: "starts before 11:00"},
		{Kind: domain.SuggestCondition, ParticipantID: "ben", Blocker: "ends after 18:00"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("suggestions = %+v\nwant %+v", got, want)
	}
}

func TestSuggestions_WhileReservationsAreOpen(t *testing.T) {
	in := domain.SuggestInput{
		Status: domain.WorkshopFunding, PaymentsOpen: true, ClosesAt: noon.Add(30 * time.Hour), Now: noon,
		Minimum: 3, Committed: 1,
		Date:     domain.DateSummary{DateID: "sat", Compatible: []string{"priya", "ben", "hannah", "tomas"}},
		Reserved: map[string]bool{"priya": true},
	}
	want := []domain.Suggestion{
		{Kind: domain.SuggestReserve, ParticipantID: "ben"},
		{Kind: domain.SuggestReserve, ParticipantID: "hannah"},
		{Kind: domain.SuggestReserve, ParticipantID: "tomas"},
	}
	if got := domain.Suggestions(in); !reflect.DeepEqual(got, want) {
		t.Errorf("suggestions = %+v", got)
	}
	in.Now = in.ClosesAt.Add(-domain.ShareWhenLeft)
	if got := domain.Suggestions(in); got[0] != (domain.Suggestion{Kind: domain.SuggestShare}) || len(got) != 4 {
		t.Errorf("near the deadline = %+v, want sharing again first", got)
	}
	in.Committed = 3
	if got := domain.Suggestions(in); len(got) != 0 {
		t.Errorf("minimum met = %+v, want nobody nudged: holds are being charged", got)
	}
	in.Committed, in.Capacity, in.SeatsTaken = 1, 4, 4
	if got := domain.Suggestions(in); len(got) != 0 {
		t.Errorf("every spot taken = %+v, want nobody sent to reserve", got)
	}
}

func TestSuggestions_KeepTheListShort(t *testing.T) {
	ids := []string{"a", "b", "c", "d", "e", "f"}
	got := domain.Suggestions(domain.SuggestInput{
		Status: domain.WorkshopFunding, PaymentsOpen: true, Now: noon, ClosesAt: noon.Add(time.Hour), Minimum: 3,
		Date: domain.DateSummary{Compatible: ids},
	})
	if len(got) != 1+domain.MaxPersonalSuggestions || got[0].Kind != domain.SuggestShare {
		t.Errorf("suggestions = %+v, want share plus %d people", got, domain.MaxPersonalSuggestions)
	}
}

func TestSuggestions_NothingOnceDecided(t *testing.T) {
	for _, s := range []domain.WorkshopStatus{domain.WorkshopConfirmed, domain.WorkshopCancelled} {
		if got := domain.Suggestions(domain.SuggestInput{Status: s, Unclear: []string{"grace"}, Date: domain.DateSummary{Compatible: []string{"a"}}}); len(got) != 0 {
			t.Errorf("%s: %+v", s, got)
		}
	}
	if got := domain.Suggestions(domain.SuggestInput{Status: domain.WorkshopFunding, PaymentsOpen: false, Date: domain.DateSummary{Compatible: []string{"a"}}}); len(got) != 0 {
		t.Errorf("reservations closed: %+v", got)
	}
}
