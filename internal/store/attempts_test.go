package store_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/store"
)

func newEnrollment(t *testing.T, s *store.Store) store.EnrollmentRow {
	t.Helper()
	w, _, _ := newWorkshop(t, s, 1, 3)
	p := openFunding(t, s, w, baseTime)
	return reserve(t, s, w, p, newParticipant(t, s, w, "Ana").ID, baseTime.Add(time.Hour))
}

func ptr[T any](v T) *T { return &v }

func TestAttempts_NumberingAndLookupByResource(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	e := newEnrollment(t, s)

	if a, err := s.LatestAttempt(ctx, e.ID); err != nil || a != nil {
		t.Fatalf("LatestAttempt before any attempt = %+v, %v; want nil, nil", a, err)
	}

	first, err := s.CreateAttempt(ctx, e.ID, 1000, "USD")
	if err != nil {
		t.Fatalf("CreateAttempt #1: %v", err)
	}
	second, err := s.CreateAttempt(ctx, e.ID, 1000, "USD")
	if err != nil {
		t.Fatalf("CreateAttempt #2: %v", err)
	}
	if first.AttemptNo != 1 || second.AttemptNo != 2 {
		t.Errorf("attempt numbers = %d, %d; want 1, 2", first.AttemptNo, second.AttemptNo)
	}
	if first.ID == "" || first.ID == second.ID || first.EnrollmentID != e.ID || first.AmountMinor != 1000 || first.Currency != "USD" {
		t.Errorf("CreateAttempt row = %+v", first)
	}
	if first.OrderID != nil || first.AuthorizationID != nil || first.CaptureID != nil || first.RefundID != nil || first.CaptureStatus != nil {
		t.Errorf("a new attempt already carries provider data: %+v", first)
	}

	expires := time.Date(2026, 11, 24, 7, 28, 4, 0, time.UTC)
	second.OrderID, second.OrderStatus = ptr("ORDER-2"), ptr("COMPLETED")
	second.AuthorizationID, second.AuthorizationStatus, second.AuthExpiresAt = ptr("AUTH-2"), ptr("CAPTURED"), &expires
	second.CaptureID, second.CaptureStatus = ptr("CAPTURE-2"), ptr("PENDING")
	second.CaptureReason, second.CaptureIssue, second.CaptureDebugID = ptr("PENDING_REVIEW"), ptr("ISSUE"), ptr("debug-123")
	second.RefundID, second.RefundStatus, second.RefundReason = ptr("REFUND-2"), ptr("PENDING"), ptr("workshop cancelled")
	second.MockCode = ptr("AUTHORIZATION_EXPIRED")
	if err := s.UpdateAttempt(ctx, second); err != nil {
		t.Fatalf("UpdateAttempt: %v", err)
	}

	for name, id := range map[string]string{"order": "ORDER-2", "authorization": "AUTH-2", "capture": "CAPTURE-2", "refund": "REFUND-2"} {
		got, err := s.AttemptByResourceID(ctx, id)
		if err != nil || got == nil {
			t.Fatalf("AttemptByResourceID(%s %s) = %+v, %v", name, id, got, err)
		}
		if got.ID != second.ID || got.AttemptNo != 2 {
			t.Errorf("AttemptByResourceID(%s %s) found attempt %d (%s), want attempt 2 (%s)", name, id, got.AttemptNo, got.ID, second.ID)
		}
	}
	if got, err := s.AttemptByResourceID(ctx, "NOPE"); err != nil || got != nil {
		t.Errorf("AttemptByResourceID(unknown) = %+v, %v; want nil, nil", got, err)
	}
	if got, err := s.AttemptByResourceID(ctx, ""); err != nil || got != nil {
		t.Errorf("AttemptByResourceID(empty) = %+v, %v; want nil, nil", got, err)
	}

	stored, err := s.AttemptByID(ctx, second.ID)
	if err != nil || stored == nil {
		t.Fatalf("AttemptByID = %+v, %v", stored, err)
	}
	if stored.AuthExpiresAt == nil || !stored.AuthExpiresAt.Equal(expires) {
		t.Errorf("auth_expires_at = %v, want %v", stored.AuthExpiresAt, expires)
	}
	wantStrings := map[string]*string{
		"order_id": ptr("ORDER-2"), "order_status": ptr("COMPLETED"), "authorization_id": ptr("AUTH-2"),
		"authorization_status": ptr("CAPTURED"), "capture_id": ptr("CAPTURE-2"), "capture_status": ptr("PENDING"),
		"capture_reason": ptr("PENDING_REVIEW"), "capture_issue": ptr("ISSUE"), "capture_debug_id": ptr("debug-123"),
		"refund_id": ptr("REFUND-2"), "refund_status": ptr("PENDING"), "refund_reason": ptr("workshop cancelled"), "mock_code": ptr("AUTHORIZATION_EXPIRED"),
	}
	gotStrings := map[string]*string{
		"order_id": stored.OrderID, "order_status": stored.OrderStatus, "authorization_id": stored.AuthorizationID,
		"authorization_status": stored.AuthorizationStatus, "capture_id": stored.CaptureID, "capture_status": stored.CaptureStatus,
		"capture_reason": stored.CaptureReason, "capture_issue": stored.CaptureIssue, "capture_debug_id": stored.CaptureDebugID,
		"refund_id": stored.RefundID, "refund_status": stored.RefundStatus, "refund_reason": stored.RefundReason, "mock_code": stored.MockCode,
	}
	for column, want := range wantStrings {
		if got := gotStrings[column]; got == nil || *got != *want {
			t.Errorf("%s = %v, want %q", column, got, *want)
		}
	}

	byOrder, err := s.AttemptByOrderID(ctx, "ORDER-2")
	if err != nil || byOrder == nil || byOrder.ID != second.ID {
		t.Errorf("AttemptByOrderID = %+v, %v; want attempt 2", byOrder, err)
	}
	if got, err := s.AttemptByOrderID(ctx, "AUTH-2"); err != nil || got != nil {
		t.Errorf("AttemptByOrderID(an authorization id) = %+v, %v; want nil, nil", got, err)
	}

	latest, err := s.LatestAttempt(ctx, e.ID)
	if err != nil || latest == nil || latest.AttemptNo != 2 || latest.ID != second.ID {
		t.Errorf("LatestAttempt = %+v, %v; want attempt 2", latest, err)
	}

	second.CaptureReason, second.MockCode, second.AuthExpiresAt = nil, nil, nil
	if err := s.UpdateAttempt(ctx, second); err != nil {
		t.Fatalf("UpdateAttempt clearing fields: %v", err)
	}
	cleared, _ := s.AttemptByID(ctx, second.ID)
	if cleared.CaptureReason != nil || cleared.MockCode != nil || cleared.AuthExpiresAt != nil {
		t.Errorf("UpdateAttempt left cleared fields behind: %+v", cleared)
	}
	untouched, _ := s.AttemptByID(ctx, first.ID)
	if untouched.OrderID != nil {
		t.Errorf("UpdateAttempt on attempt 2 modified attempt 1: %+v", untouched)
	}
}

func TestAttempts_UnknownIDs(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	const missing = "00000000-0000-4000-8000-000000000000"

	if _, err := s.CreateAttempt(ctx, missing, 1000, "USD"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("CreateAttempt(unknown enrollment) error = %v, want ErrNotFound", err)
	}
	if err := s.UpdateAttempt(ctx, store.AttemptRow{ID: missing}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("UpdateAttempt(unknown) error = %v, want ErrNotFound", err)
	}
	for _, id := range []string{missing, "not-a-uuid", ""} {
		if got, err := s.AttemptByID(ctx, id); err != nil || got != nil {
			t.Errorf("AttemptByID(%q) = %+v, %v; want nil, nil", id, got, err)
		}
		if got, err := s.LatestAttempt(ctx, id); err != nil || got != nil {
			t.Errorf("LatestAttempt(%q) = %+v, %v; want nil, nil", id, got, err)
		}
	}
}

func TestAttempts_ConcurrentCreatesNumberWithoutGaps(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	e := newEnrollment(t, s)

	const creators = 8
	numbers := make([]int, creators)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range creators {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			a, err := s.CreateAttempt(ctx, e.ID, 1000, "USD")
			if err != nil {
				t.Errorf("CreateAttempt %d: %v", i, err)
			}
			numbers[i] = a.AttemptNo
		}()
	}
	close(start)
	wg.Wait()

	slices.Sort(numbers)
	for i, n := range numbers {
		if n != i+1 {
			t.Fatalf("attempt numbers = %v, want 1..%d with no gaps or repeats", numbers, creators)
		}
	}
}
