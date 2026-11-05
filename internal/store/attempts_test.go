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
	"github.com/aalvaropc/commitpool/internal/store/storetest"
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
	s := storetest.New(t)
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

	held := reserve(t, s, w, p, newParticipant(t, s, w, "Authorized, seat kept").ID, baseTime.Add(time.Hour))
	a, err := s.CreateAttempt(ctx, held.ID, 1000, "USD")
	if err != nil {
		t.Fatalf("CreateAttempt while SEAT_RESERVED: %v", err)
	}
	a.AuthorizationID = ptr("AUTH-HELD")
	if err := s.UpdateAttempt(ctx, a); err != nil {
		t.Fatalf("UpdateAttempt: %v", err)
	}
	if got, err := s.CreateAttempt(ctx, held.ID, 1000, "USD"); !errors.Is(err, store.ErrAttemptNotAllowed) {
		t.Errorf("CreateAttempt behind an authorized latest attempt = %+v, %v; want ErrAttemptNotAllowed", got, err)
	}
	if n := count(t, s, `select count(*) from payment_attempts where enrollment_id = $1`, held.ID); n != 1 {
		t.Errorf("attempts behind an authorization = %d, want only the authorized one", n)
	}
}

func TestAttempts_NumberingAndLookupByResource(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
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
	s := storetest.New(t)
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
	s := storetest.New(t)
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
	s := storetest.New(t)
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
	s := storetest.New(t)
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
	s := storetest.New(t)
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
	s := storetest.New(t)
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

func TestCreateAttempt_WaitsForAnAuthorizationBeingRecorded(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	e := newEnrollment(t, s)
	a, err := s.CreateAttempt(ctx, e.ID, 1000, "USD")
	if err != nil {
		t.Fatal(err)
	}
	locked, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	recorded := make(chan error, 1)
	go func() {
		_, err := s.UpdateAttemptFunc(ctx, a.ID, func(row *store.AttemptRow) error {
			close(locked)
			<-release
			row.AuthorizationID = ptr("AUTH-IN-FLIGHT")
			return nil
		})
		recorded <- err
	}()
	<-locked

	created := make(chan error, 1)
	go func() {
		_, err := s.CreateAttempt(ctx, e.ID, 1000, "USD")
		created <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for waiting := false; !waiting; {
		select {
		case err := <-created:
			t.Fatalf("CreateAttempt returned %v while the latest attempt was being authorized; want it to wait for that write", err)
		default:
		}
		if err := s.Pool.QueryRow(ctx,
			`select exists (select 1 from pg_locks where not granted and locktype in ('transactionid', 'tuple'))`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("CreateAttempt never waited on the attempt row")
		}
	}
	once.Do(func() { close(release) })

	if err := <-recorded; err != nil {
		t.Fatalf("UpdateAttemptFunc: %v", err)
	}
	if err := <-created; !errors.Is(err, store.ErrAttemptNotAllowed) {
		t.Errorf("CreateAttempt after the authorization was recorded = %v, want ErrAttemptNotAllowed", err)
	}
	if n := count(t, s, `select count(*) from payment_attempts where enrollment_id = $1`, e.ID); n != 1 {
		t.Errorf("attempts = %d, want only the authorized one", n)
	}
}

func TestCheckoutAttempt_ReusesTheOpenAttemptAndReplacesAnExpiredOrder(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	e := newEnrollment(t, s)

	first, created, err := s.CheckoutAttempt(ctx, e.ID, 1000, "USD")
	if err != nil || !created || first.AttemptNo != 1 || first.AmountMinor != 1000 || first.Currency != "USD" {
		t.Fatalf("first CheckoutAttempt = %+v, %v, %v; want a new attempt 1 for 1000 USD", first, created, err)
	}
	again, created, err := s.CheckoutAttempt(ctx, e.ID, 1000, "USD")
	if err != nil || created || again.ID != first.ID {
		t.Fatalf("second CheckoutAttempt = %+v, %v, %v; want attempt %s back", again, created, err, first.ID)
	}

	first.OrderID = ptr("ORDER-1")
	first.OrderStatus = ptr("PAYER_ACTION_REQUIRED")
	if err := s.UpdateAttempt(ctx, first); err != nil {
		t.Fatalf("UpdateAttempt: %v", err)
	}
	if got, created, err := s.CheckoutAttempt(ctx, e.ID, 1000, "USD"); err != nil || created || got.ID != first.ID {
		t.Fatalf("CheckoutAttempt with an order awaiting approval = %+v, %v, %v; want attempt %s back", got, created, err, first.ID)
	}

	first.OrderStatus = ptr("EXPIRED")
	if err := s.UpdateAttempt(ctx, first); err != nil {
		t.Fatalf("UpdateAttempt: %v", err)
	}
	next, created, err := s.CheckoutAttempt(ctx, e.ID, 1000, "USD")
	if err != nil || !created || next.AttemptNo != 2 {
		t.Fatalf("CheckoutAttempt after the order expired = %+v, %v, %v; want a new attempt 2", next, created, err)
	}

	next.AuthorizationID = ptr("AUTH-1")
	if err := s.UpdateAttempt(ctx, next); err != nil {
		t.Fatalf("UpdateAttempt: %v", err)
	}
	if got, _, err := s.CheckoutAttempt(ctx, e.ID, 1000, "USD"); !errors.Is(err, store.ErrAttemptNotAllowed) {
		t.Errorf("CheckoutAttempt behind an authorization = %+v, %v; want ErrAttemptNotAllowed", got, err)
	}
	mustTransition(t, s, e.ID, []domain.EnrollmentStatus{domain.EnrollmentSeatReserved}, domain.EnrollmentAuthorized)
	if got, _, err := s.CheckoutAttempt(ctx, e.ID, 1000, "USD"); !errors.Is(err, store.ErrAttemptNotAllowed) {
		t.Errorf("CheckoutAttempt on an AUTHORIZED enrollment = %+v, %v; want ErrAttemptNotAllowed", got, err)
	}
	if _, _, err := s.CheckoutAttempt(ctx, missingUUID, 1000, "USD"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("CheckoutAttempt(unknown enrollment) error = %v, want ErrNotFound", err)
	}
}

func TestCheckoutAttempt_ConcurrentCheckoutsShareOneAttempt(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	e := newEnrollment(t, s)

	const tabs = 8
	ids := make([]string, tabs)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range tabs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			a, _, err := s.CheckoutAttempt(ctx, e.ID, 1000, "USD")
			if err != nil {
				t.Errorf("CheckoutAttempt %d: %v", i, err)
			}
			ids[i] = a.ID
		}()
	}
	close(start)
	wg.Wait()

	for _, id := range ids {
		if id != ids[0] {
			t.Fatalf("concurrent checkouts got attempts %v, want one shared attempt", ids)
		}
	}
	if n := count(t, s, `select count(*) from payment_attempts where enrollment_id = $1`, e.ID); n != 1 {
		t.Errorf("attempts after %d concurrent checkouts = %d, want 1", tabs, n)
	}
}

func TestCheckoutAttempt_ReplacesAnAttemptWhoseOrderPayPalRefused(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	now := baseTime.Add(time.Hour)

	checkout := func(e store.EnrollmentRow) (store.AttemptRow, bool) {
		t.Helper()
		a, created, err := s.CheckoutAttempt(ctx, e.ID, 1000, "USD")
		if err != nil {
			t.Fatalf("CheckoutAttempt: %v", err)
		}
		return a, created
	}
	// runCreateOrder enqueues and claims the attempt's create_order job and
	// leaves it as finish says.
	runCreateOrder := func(a store.AttemptRow, finish func(id string) error) {
		t.Helper()
		key := store.JobKey(store.KindCreateOrder, a.ID)
		if _, err := s.EnqueueJob(ctx, store.KindCreateOrder, key, nil, &a.ID, nil, now); err != nil {
			t.Fatalf("EnqueueJob: %v", err)
		}
		job, err := s.ClaimJobByKey(ctx, key, "w1", now)
		if err != nil || job == nil {
			t.Fatalf("ClaimJobByKey = %+v, %v", job, err)
		}
		if finish != nil {
			if err := finish(job.ID); err != nil {
				t.Fatalf("finish job: %v", err)
			}
		}
	}
	status := 422
	refuse := func(id string) error {
		return s.FailJob(ctx, id, "422 UNPROCESSABLE_ENTITY", &status, ptr("INVALID_PARAMETER_VALUE"), ptr("debug-1"))
	}

	w, _, _ := newWorkshop(t, s, 1, 6)
	p := openFunding(t, s, w, baseTime)

	refused := reserve(t, s, w, p, newParticipant(t, s, w, "Refused").ID, now)
	first, _ := checkout(refused)
	runCreateOrder(first, refuse)
	if next, created := checkout(refused); !created || next.AttemptNo != 2 {
		t.Errorf("checkout after PayPal refused the order = attempt %d, created %v; want a new attempt 2", next.AttemptNo, created)
	}

	for name, finish := range map[string]func(id string) error{
		"running": nil,
		"unknown": func(id string) error { return s.MarkJobUnknown(ctx, id, "timeout", nil) },
	} {
		e := reserve(t, s, w, p, newParticipant(t, s, w, name).ID, now)
		a, _ := checkout(e)
		runCreateOrder(a, finish)
		if again, created := checkout(e); created || again.ID != a.ID {
			t.Errorf("checkout while create_order is %s = attempt %d, created %v; want attempt 1 kept, an order may exist", name, again.AttemptNo, created)
		}
	}

	withOrder := reserve(t, s, w, p, newParticipant(t, s, w, "Order kept").ID, now)
	a, _ := checkout(withOrder)
	a.OrderID = ptr("ORDER-KEPT")
	if err := s.UpdateAttempt(ctx, a); err != nil {
		t.Fatalf("UpdateAttempt: %v", err)
	}
	runCreateOrder(a, refuse)
	if again, created := checkout(withOrder); created || again.ID != a.ID {
		t.Errorf("checkout with an order on a failed job = attempt %d, created %v; want the attempt with the order kept", again.AttemptNo, created)
	}
}

func TestMarkOrderExpired_OnlyWithoutAnAuthorization(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	e := newEnrollment(t, s)
	a, err := s.CreateAttempt(ctx, e.ID, 1000, "USD")
	if err != nil {
		t.Fatal(err)
	}
	approved := "APPROVED"
	a.OrderID, a.OrderStatus = ptr("ORDER-1"), &approved
	if err := s.UpdateAttempt(ctx, a); err != nil {
		t.Fatal(err)
	}

	marked, err := s.MarkOrderExpired(ctx, a.ID)
	if err != nil || !marked {
		t.Fatalf("MarkOrderExpired = %v, %v; want true, nil", marked, err)
	}
	got, _ := s.AttemptByID(ctx, a.ID)
	if got.OrderStatus == nil || *got.OrderStatus != "EXPIRED" || got.OrderID == nil || *got.OrderID != "ORDER-1" {
		t.Errorf("attempt after MarkOrderExpired = status %v, order %v; want EXPIRED with the order kept", got.OrderStatus, got.OrderID)
	}

	held := reserve(t, s, mustWorkshop(t, s, e.WorkshopID), mustProposal(t, s, e.ProposalID), newParticipantIn(t, s, e.WorkshopID, "Ben").ID, baseTime.Add(time.Hour))
	b, err := s.CreateAttempt(ctx, held.ID, 1000, "USD")
	if err != nil {
		t.Fatal(err)
	}
	completed := "COMPLETED"
	b.OrderID, b.OrderStatus, b.AuthorizationID = ptr("ORDER-2"), &completed, ptr("AUTH-2")
	if err := s.UpdateAttempt(ctx, b); err != nil {
		t.Fatal(err)
	}
	if marked, err := s.MarkOrderExpired(ctx, b.ID); err != nil || marked {
		t.Errorf("MarkOrderExpired on an authorized attempt = %v, %v; want false, nil", marked, err)
	}
	if got, _ := s.AttemptByID(ctx, b.ID); got.OrderStatus == nil || *got.OrderStatus != "COMPLETED" {
		t.Errorf("authorized attempt's order status = %v, want COMPLETED kept", got.OrderStatus)
	}

	for _, id := range []string{"00000000-0000-4000-8000-000000000000", "not-a-uuid"} {
		if _, err := s.MarkOrderExpired(ctx, id); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("MarkOrderExpired(%q) error = %v, want ErrNotFound", id, err)
		}
	}
}

func mustWorkshop(t *testing.T, s *store.Store, id string) store.WorkshopRow {
	t.Helper()
	w, err := s.WorkshopByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func mustProposal(t *testing.T, s *store.Store, id string) store.ProposalRow {
	t.Helper()
	p, err := s.ProposalByID(context.Background(), id)
	if err != nil || p == nil {
		t.Fatalf("ProposalByID(%s) = %v, %v", id, p, err)
	}
	return *p
}

func newParticipantIn(t *testing.T, s *store.Store, workshopID, name string) store.ParticipantRow {
	t.Helper()
	return newParticipant(t, s, mustWorkshop(t, s, workshopID), name)
}

func TestSetCaptureMock_OnlyBeforeTheCaptureStarts(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	w, _, _ := newWorkshop(t, s, 1, 6)
	p := openFunding(t, s, w, baseTime)
	attempt := func(name string, status domain.EnrollmentStatus) store.AttemptRow {
		t.Helper()
		e := reserve(t, s, w, p, newParticipant(t, s, w, name).ID, baseTime.Add(time.Hour))
		a, err := s.CreateAttempt(ctx, e.ID, 1000, "USD")
		if err != nil {
			t.Fatal(err)
		}
		if status != domain.EnrollmentSeatReserved {
			mustTransition(t, s, e.ID, []domain.EnrollmentStatus{domain.EnrollmentSeatReserved}, status)
		}
		return a
	}
	refusedIssue := attempt("Cleo", domain.EnrollmentAuthorized)
	if _, err := s.UpdateAttemptFunc(ctx, refusedIssue.ID, func(row *store.AttemptRow) error {
		row.CaptureIssue = ptr("CAPTURE_DEADLINE_PASSED")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		a    store.AttemptRow
		want bool
	}{
		{"seat reserved", attempt("Ana", domain.EnrollmentSeatReserved), true},
		{"authorized", attempt("Ben", domain.EnrollmentAuthorized), true},
		{"authorized with a capture issue", refusedIssue, false},
		{"capturing", attempt("Dan", domain.EnrollmentCapturing), false},
		{"paid", attempt("Eve", domain.EnrollmentPaid), false},
	}
	for _, tc := range cases {
		stored, err := s.SetCaptureMock(ctx, tc.a.ID, "AUTHORIZATION_EXPIRED")
		if err != nil || stored != tc.want {
			t.Errorf("%s: SetCaptureMock = %v, %v; want %v", tc.name, stored, err, tc.want)
		}
		got, err := s.AttemptByID(ctx, tc.a.ID)
		if err != nil {
			t.Fatal(err)
		}
		if has := got.MockCode != nil; has != tc.want {
			t.Errorf("%s: mock code = %v, want stored %v", tc.name, got.MockCode, tc.want)
		}
	}
	for _, id := range []string{"00000000-0000-4000-8000-000000000000", "not-a-uuid"} {
		if _, err := s.SetCaptureMock(ctx, id, "AUTHORIZATION_EXPIRED"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("SetCaptureMock(%s) = %v, want ErrNotFound", id, err)
		}
	}
}
