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

func TestSuggestions_GroupReplyFirstWhileCollecting(t *testing.T) {
	in := domain.SuggestInput{
		Status:     domain.WorkshopCollecting,
		GroupReply: true,
		Unclear:    []string{"grace"},
		Date: domain.DateSummary{DateID: "sat", Compatible: []string{"priya", "ben"}, Blockers: map[string]string{
			"grace": "needs clarification", "hannah": "ends after 11:30",
		}},
	}
	want := []domain.Suggestion{
		{Kind: domain.SuggestGroupReply},
		{Kind: domain.SuggestClarify, ParticipantID: "grace"},
		{Kind: domain.SuggestCondition, ParticipantID: "hannah", Blocker: "ends after 11:30"},
	}
	if got := domain.Suggestions(in); !reflect.DeepEqual(got, want) {
		t.Errorf("suggestions = %+v\nwant %+v", got, want)
	}

	in.Unclear = []string{"a", "b", "c", "d", "e"}
	if got := domain.Suggestions(in); len(got) != 1+domain.MaxPersonalSuggestions || got[0].Kind != domain.SuggestGroupReply {
		t.Errorf("with many people to ask = %+v, want the group reply plus %d people", got, domain.MaxPersonalSuggestions)
	}

	funding := domain.SuggestInput{
		Status: domain.WorkshopFunding, PaymentsOpen: true, ClosesAt: noon.Add(30 * time.Hour), Now: noon, Minimum: 3,
		GroupReply: true, Date: domain.DateSummary{DateID: "sat", Compatible: []string{"priya"}},
	}
	for _, sg := range domain.Suggestions(funding) {
		if sg.Kind == domain.SuggestGroupReply {
			t.Errorf("reservations open: %+v, want no group reply once the date is chosen", sg)
		}
	}
}

func TestSuggestions_NoGroupReplyWithFewerThanTwoAnswers(t *testing.T) {
	cases := []struct {
		name     string
		answered []time.Time
		want     bool
	}{
		{"nobody", nil, false},
		{"one answer", []time.Time{noon.Add(-time.Hour)}, false},
		{"two answers, one today", []time.Time{noon.Add(-72 * time.Hour), noon.Add(-time.Hour)}, true},
		{"two answers, one just inside a day", []time.Time{noon.Add(-72 * time.Hour), noon.Add(-domain.GroupReplyFreshFor)}, true},
		{"two answers, both older than a day", []time.Time{noon.Add(-72 * time.Hour), noon.Add(-domain.GroupReplyFreshFor - time.Minute)}, false},
	}
	for _, c := range cases {
		if got := domain.GroupReplyDue(c.answered, noon); got != c.want {
			t.Errorf("%s: GroupReplyDue = %v, want %v", c.name, got, c.want)
		}
	}
	got := domain.Suggestions(domain.SuggestInput{
		Status:     domain.WorkshopCollecting,
		GroupReply: domain.GroupReplyDue([]time.Time{noon}, noon),
		Unclear:    []string{"grace"},
		Date:       domain.DateSummary{DateID: "sat", Compatible: []string{}, Blockers: map[string]string{"grace": "needs clarification"}},
	})
	if want := []domain.Suggestion{{Kind: domain.SuggestClarify, ParticipantID: "grace"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("one answer: suggestions = %+v, want only Grace's question", got)
	}
}
