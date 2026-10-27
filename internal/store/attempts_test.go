package store_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/store"
)

func newEnrollment(t *testing.T, s *store.Store) store.EnrollmentRow {
	t.Helper()
	w, _, _ := newWorkshop(t, s, 1, 3)
	p := openFunding(t, s, w, baseTime)
	return reserve(t, s, w, p, newParticipant(t, s, w, "Ana").ID, baseTime.Add(time.Hour))
}

func ptr[T any](v T) *T { return &v }

func TestCreateAttempt_RefusesUnlessSeatReserved(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	w, _, _ := newWorkshop(t, s, 1, 6)
	p := openFunding(t, s, w, baseTime)
	statuses := []domain.EnrollmentStatus{
		domain.EnrollmentAuthorized, domain.EnrollmentCapturing, domain.EnrollmentPaid, domain.EnrollmentCaptureFailed,
	}
	for i, status := range statuses {
		e := reserve(t, s, w, p, newParticipant(t, s, w, fmt.Sprintf("Participant %d", i)).ID, baseTime.Add(time.Hour))
		if _, err := s.CreateAttempt(ctx, e.ID, 1000, "USD"); err != nil {
			t.Fatalf("CreateAttempt while SEAT_RESERVED: %v", err)
		}
		mustTransition(t, s, e.ID, []domain.EnrollmentStatus{domain.EnrollmentSeatReserved}, status)

		if a, err := s.CreateAttempt(ctx, e.ID, 1000, "USD"); !errors.Is(err, store.ErrAttemptNotAllowed) {
			t.Errorf("CreateAttempt on a %s enrollment = %+v, %v; want ErrAttemptNotAllowed", status, a, err)
		}
		if n := count(t, s, `select count(*) from payment_attempts where enrollment_id = $1`, e.ID); n != 1 {
			t.Errorf("%s: attempts = %d, want only the first", status, n)
		}
	}

	e := reserve(t, s, w, p, newParticipant(t, s, w, "Still reserved").ID, baseTime.Add(time.Hour))
	for want := 1; want <= 2; want++ {
		a, err := s.CreateAttempt(ctx, e.ID, 1000, "USD")
		if err != nil || a.AttemptNo != want {
			t.Fatalf("CreateAttempt #%d on a SEAT_RESERVED enrollment = %+v, %v", want, a, err)
		}
	}
}

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

	second.CaptureReason, second.MockCode = nil, nil
	if err := s.UpdateAttempt(ctx, second); err != nil {
		t.Fatalf("UpdateAttempt clearing fields: %v", err)
	}
	cleared, _ := s.AttemptByID(ctx, second.ID)
	if cleared.CaptureReason != nil || cleared.MockCode != nil {
		t.Errorf("UpdateAttempt left cleared reason/mock fields behind: %+v", cleared)
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

var errRollback = errors.New("rollback requested by the callback")

func TestUpdateAttempt_NilIdentityColumnsDoNotClearStoredIDs(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	a, err := s.CreateAttempt(ctx, newEnrollment(t, s).ID, 1000, "USD")
	if err != nil {
		t.Fatal(err)
	}

	expires := time.Date(2026, 11, 24, 7, 28, 4, 0, time.UTC)
	stored := a
	stored.OrderID, stored.AuthorizationID, stored.CaptureID, stored.RefundID = ptr("ORDER-1"), ptr("AUTH-1"), ptr("CAPTURE-1"), ptr("REFUND-1")
	stored.AuthExpiresAt = &expires
	stored.CaptureStatus, stored.CaptureReason = ptr("PENDING"), ptr("PENDING_REVIEW")
	if err := s.UpdateAttempt(ctx, stored); err != nil {
		t.Fatalf("UpdateAttempt storing identity columns: %v", err)
	}

	stale := a
	stale.CaptureStatus = ptr("COMPLETED")
	if err := s.UpdateAttempt(ctx, stale); err != nil {
		t.Fatalf("UpdateAttempt from a stale row: %v", err)
	}

	got, err := s.AttemptByID(ctx, a.ID)
	if err != nil || got == nil {
		t.Fatalf("AttemptByID = %+v, %v", got, err)
	}
	ids := map[string]*string{"order_id": got.OrderID, "authorization_id": got.AuthorizationID, "capture_id": got.CaptureID, "refund_id": got.RefundID}
	wantIDs := map[string]string{"order_id": "ORDER-1", "authorization_id": "AUTH-1", "capture_id": "CAPTURE-1", "refund_id": "REFUND-1"}
	for column, want := range wantIDs {
		if ids[column] == nil || *ids[column] != want {
			t.Errorf("%s = %v after a stale update, want %q to survive", column, ids[column], want)
		}
	}
	if got.AuthExpiresAt == nil || !got.AuthExpiresAt.Equal(expires) {
		t.Errorf("auth_expires_at = %v after a stale update, want %v to survive", got.AuthExpiresAt, expires)
	}
	if got.CaptureStatus == nil || *got.CaptureStatus != "COMPLETED" {
		t.Errorf("capture_status = %v, want the stale update's COMPLETED to apply", got.CaptureStatus)
	}
	if got.CaptureReason != nil {
		t.Errorf("capture_reason = %v, want it cleared: only identity columns are non-clearing", got.CaptureReason)
	}
	if found, err := s.AttemptByResourceID(ctx, "CAPTURE-1"); err != nil || found == nil || found.ID != a.ID {
		t.Errorf("AttemptByResourceID(CAPTURE-1) = %+v, %v; the capture id must stay resolvable", found, err)
	}

	replaced := *got
	replaced.CaptureID = ptr("CAPTURE-2")
	if err := s.UpdateAttempt(ctx, replaced); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.AttemptByID(ctx, a.ID); got.CaptureID == nil || *got.CaptureID != "CAPTURE-2" {
		t.Errorf("capture_id = %v, want a non-nil value to replace the stored one", got.CaptureID)
	}
}

func TestUpdateAttemptFunc_ConcurrentUpdatesBothPersist(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	a, err := s.CreateAttempt(ctx, newEnrollment(t, s).ID, 1000, "USD")
	if err != nil {
		t.Fatal(err)
	}

	// The pause inside each callback makes the interleaving a lost update
	// would need (both read, then both write) the normal outcome unless the
	// row is locked for the whole read-modify-write.
	edits := []func(*store.AttemptRow){
		func(a *store.AttemptRow) { a.CaptureID, a.CaptureStatus = ptr("CAPTURE-1"), ptr("COMPLETED") },
		func(a *store.AttemptRow) { a.RefundStatus = ptr("PENDING") },
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, edit := range edits {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := s.UpdateAttemptFunc(ctx, a.ID, func(row *store.AttemptRow) error {
				time.Sleep(50 * time.Millisecond)
				edit(row)
				return nil
			})
			if err != nil {
				t.Errorf("UpdateAttemptFunc %d: %v", i, err)
			}
		}()
	}
	close(start)
	wg.Wait()

	got, err := s.AttemptByID(ctx, a.ID)
	if err != nil || got == nil {
		t.Fatalf("AttemptByID = %+v, %v", got, err)
	}
	if got.CaptureID == nil || *got.CaptureID != "CAPTURE-1" || got.CaptureStatus == nil || *got.CaptureStatus != "COMPLETED" {
		t.Errorf("capture fields = %v / %v, want CAPTURE-1 / COMPLETED", got.CaptureID, got.CaptureStatus)
	}
	if got.RefundStatus == nil || *got.RefundStatus != "PENDING" {
		t.Errorf("refund_status = %v, want PENDING: a concurrent update overwrote it", got.RefundStatus)
	}
}

func TestUpdateAttemptFunc_ReadsLockedRowAndWritesAllColumns(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	a, err := s.CreateAttempt(ctx, newEnrollment(t, s).ID, 1000, "USD")
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Date(2026, 11, 24, 7, 28, 4, 0, time.UTC)
	a.OrderID, a.CaptureID, a.CaptureReason, a.AuthExpiresAt = ptr("ORDER-1"), ptr("CAPTURE-1"), ptr("PENDING_REVIEW"), &expires
	if err := s.UpdateAttempt(ctx, a); err != nil {
		t.Fatal(err)
	}
	var createdAt time.Time
	if err := s.Pool.QueryRow(ctx, `select created_at from payment_attempts where id = $1`, a.ID).Scan(&createdAt); err != nil {
		t.Fatal(err)
	}

	saved, err := s.UpdateAttemptFunc(ctx, a.ID, func(row *store.AttemptRow) error {
		if row.ID != a.ID || row.OrderID == nil || *row.OrderID != "ORDER-1" || row.CaptureID == nil || row.AuthExpiresAt == nil {
			t.Errorf("callback received %+v, want the stored attempt", *row)
		}
		row.CaptureReason = nil
		row.CaptureID = nil
		row.RefundID = ptr("REFUND-1")
		row.AmountMinor, row.AttemptNo = 99999, 7
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateAttemptFunc: %v", err)
	}

	got, _ := s.AttemptByID(ctx, a.ID)
	for name, row := range map[string]store.AttemptRow{"returned": saved, "stored": *got} {
		if row.CaptureReason != nil || row.CaptureID != nil {
			t.Errorf("%s row kept fields the callback cleared: %+v", name, row)
		}
		if row.RefundID == nil || *row.RefundID != "REFUND-1" || row.OrderID == nil || *row.OrderID != "ORDER-1" {
			t.Errorf("%s row = %+v, want refund REFUND-1 and order ORDER-1", name, row)
		}
		if row.AmountMinor != 1000 || row.AttemptNo != 1 || row.ID != a.ID {
			t.Errorf("%s row changed an immutable column: amount %d, attempt_no %d, id %s", name, row.AmountMinor, row.AttemptNo, row.ID)
		}
	}
	var updatedAt time.Time
	if err := s.Pool.QueryRow(ctx, `select updated_at from payment_attempts where id = $1`, a.ID).Scan(&updatedAt); err != nil {
		t.Fatal(err)
	}
	if !updatedAt.After(createdAt) {
		t.Errorf("updated_at %v was not bumped past created_at %v", updatedAt, createdAt)
	}
}

func TestUpdateAttemptFunc_ErrorRollsBack(t *testing.T) {
	ctx := context.Background()
	s := store.TestStore(t)
	a, err := s.CreateAttempt(ctx, newEnrollment(t, s).ID, 1000, "USD")
	if err != nil {
		t.Fatal(err)
	}
	a.OrderID, a.OrderStatus = ptr("ORDER-1"), ptr("CREATED")
	if err := s.UpdateAttempt(ctx, a); err != nil {
		t.Fatal(err)
	}

	_, err = s.UpdateAttemptFunc(ctx, a.ID, func(row *store.AttemptRow) error {
		row.OrderStatus, row.CaptureID = ptr("COMPLETED"), ptr("CAPTURE-1")
		return errRollback
	})
	if err != errRollback {
		t.Errorf("UpdateAttemptFunc error = %v, want the callback's own error returned unwrapped", err)
	}
	got, _ := s.AttemptByID(ctx, a.ID)
	if got.OrderStatus == nil || *got.OrderStatus != "CREATED" || got.CaptureID != nil {
		t.Errorf("a failed callback persisted changes: %+v", got)
	}

	called := false
	_, err = s.UpdateAttemptFunc(ctx, "00000000-0000-4000-8000-000000000000", func(*store.AttemptRow) error {
		called = true
		return nil
	})
	if !errors.Is(err, store.ErrNotFound) || called {
		t.Errorf("UpdateAttemptFunc(unknown id) error = %v, callback called = %v; want ErrNotFound without calling it", err, called)
	}
	if _, err := s.UpdateAttemptFunc(ctx, "not-a-uuid", func(*store.AttemptRow) error { return nil }); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("UpdateAttemptFunc(malformed id) error = %v, want ErrNotFound", err)
	}
}
