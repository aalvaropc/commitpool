package store_test

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/store"
)

var (
	baseTime = time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	friday   = time.Date(2026, 11, 13, 15, 0, 0, 0, time.UTC)
	saturday = time.Date(2026, 11, 14, 15, 0, 0, 0, time.UTC)
)

type dateWindow = struct{ StartsAt, EndsAt time.Time }

func newWorkshop(t *testing.T, s *store.Store, minimum, capacity int) (w store.WorkshopRow, ownerToken, shareToken string) {
	t.Helper()
	w, ownerToken, shareToken, err := s.CreateWorkshop(context.Background(), store.CreateWorkshopParams{
		Title:            "Go for operators",
		Description:      "Two hours of hands-on Go.",
		DurationMin:      180,
		Timezone:         "America/Lima",
		Minimum:          minimum,
		Capacity:         capacity,
		PriceMinor:       1000,
		Currency:         "USD",
		PaymentWindowMin: 1440,
		Dates: []dateWindow{
			{StartsAt: saturday, EndsAt: saturday.Add(3 * time.Hour)},
			{StartsAt: friday, EndsAt: friday.Add(3 * time.Hour)},
		},
	})
	if err != nil {
		t.Fatalf("CreateWorkshop: %v", err)
	}
	return w, ownerToken, shareToken
}

func openFunding(t *testing.T, s *store.Store, w store.WorkshopRow, opensAt time.Time) store.ProposalRow {
	t.Helper()
	ctx := context.Background()
	dates, err := s.CandidateDates(ctx, w.ID)
	if err != nil || len(dates) == 0 {
		t.Fatalf("CandidateDates: %v (%d dates)", err, len(dates))
	}
	p, err := s.OpenProposal(ctx, store.ProposalRow{
		WorkshopID:   w.ID,
		ChosenDateID: dates[0].ID,
		PriceMinor:   w.PriceMinor,
		Currency:     w.Currency,
		Minimum:      w.Minimum,
		Capacity:     w.Capacity,
		OpensAt:      opensAt,
		ClosesAt:     opensAt.Add(24 * time.Hour),
		StartsAt:     dates[0].StartsAt,
	})
	if err != nil {
		t.Fatalf("OpenProposal: %v", err)
	}
	return p
}

func newParticipant(t *testing.T, s *store.Store, w store.WorkshopRow, name string) store.ParticipantRow {
	t.Helper()
	p, _, err := s.CreateParticipant(context.Background(), w.ID, name)
	if err != nil {
		t.Fatalf("CreateParticipant(%s): %v", name, err)
	}
	return p
}

func reserveParams(w store.WorkshopRow, p store.ProposalRow, participantID string, now time.Time) store.ReserveParams {
	return store.ReserveParams{
		WorkshopID:     w.ID,
		ParticipantID:  participantID,
		ProposalID:     p.ID,
		ConsentVersion: "consent-v1",
		Now:            now,
		SeatTTL:        30 * time.Minute,
	}
}

func reserve(t *testing.T, s *store.Store, w store.WorkshopRow, p store.ProposalRow, participantID string, now time.Time) store.EnrollmentRow {
	t.Helper()
	e, created, err := s.ReserveSeatAndCreateEnrollment(context.Background(), reserveParams(w, p, participantID, now))
	if err != nil {
		t.Fatalf("ReserveSeatAndCreateEnrollment: %v", err)
	}
	if !created {
		t.Fatalf("ReserveSeatAndCreateEnrollment: created = false, want true")
	}
	return e
}

func mustTransition(t *testing.T, s *store.Store, id string, from []domain.EnrollmentStatus, to domain.EnrollmentStatus) {
	t.Helper()
	ok, err := s.TransitionEnrollment(context.Background(), id, from, to)
	if err != nil || !ok {
		t.Fatalf("TransitionEnrollment(%s %v -> %s) = %v, %v; want true, nil", id, from, to, ok, err)
	}
}

func count(t *testing.T, s *store.Store, query string, args ...any) int {
	t.Helper()
	var n int
	if err := s.Pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func assertJSONEqual(t *testing.T, name string, got, want json.RawMessage) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("%s: got is not JSON (%q): %v", name, got, err)
	}
	if err := json.Unmarshal(want, &w); err != nil {
		t.Fatalf("%s: want is not JSON (%q): %v", name, want, err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Errorf("%s = %s, want %s", name, got, want)
	}
}

func participantNames(n int) []string {
	names := make([]string, n)
	for i := range names {
		names[i] = fmt.Sprintf("Participant %02d", i+1)
	}
	return names
}
