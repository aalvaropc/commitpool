package invite_test

import (
	"strings"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/invite"
	"github.com/aalvaropc/commitpool/internal/store"
)

func TestFileName_IsASafeSlug(t *testing.T) {
	for in, want := range map[string]string{
		"Life drawing for beginners": "life-drawing-for-beginners.ics",
		`"; rm -rf / ; ".ics`:        "rm-rf-ics.ics",
		"面包":                         "workshop.ics",
	} {
		if got := invite.FileName(in); got != want {
			t.Errorf("FileName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEvent_FollowsTheChosenDateAndStatus(t *testing.T) {
	start := time.Date(2026, 11, 14, 15, 0, 0, 0, time.UTC)
	w := store.WorkshopRow{ID: "w1", Title: "Charcoal", Location: "Room", Status: domain.WorkshopConfirmed}
	p := &store.ProposalRow{Version: 1, ChosenDateID: "sat"}
	dates := []store.CandidateDateRow{{ID: "sun", StartsAt: start.AddDate(0, 0, 1)}, {ID: "sat", StartsAt: start, EndsAt: start.Add(2 * time.Hour)}}
	e, ok := invite.Event(w, p, dates, "With Iris.", "", start)
	if !ok || !e.Start.Equal(start) || e.Sequence != 11 || e.URL != "" || strings.Contains(e.Description, "Invite") {
		t.Errorf("event = %+v, %v", e, ok)
	}
	if _, ok := invite.Event(w, nil, dates, "", "", start); ok {
		t.Error("an event without a picked date")
	}
	withLink, _ := invite.Event(w, p, dates, "With Iris.", "https://example.test/w/abc", start)
	if withLink.URL != "https://example.test/w/abc" || !strings.HasSuffix(withLink.Description, "\nInvite: https://example.test/w/abc") {
		t.Errorf("event with link = %+v", withLink)
	}
}
