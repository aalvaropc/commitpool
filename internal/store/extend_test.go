package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/store"
	"github.com/aalvaropc/commitpool/internal/store/storetest"
)

var extendedTo = time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)

func extendableWorkshop(t *testing.T, s *store.Store) (store.WorkshopRow, store.ProposalRow) {
	t.Helper()
	w, _, _ := newWorkshop(t, s, 2, 4)
	p := openFunding(t, s, w, baseTime)
	params := reserveParams(w, p, newParticipant(t, s, w, "Ana").ID, baseTime.Add(time.Hour))
	params.ConsentVersion = "consent-v5"
	if _, _, err := s.ReserveSeatAndCreateEnrollment(context.Background(), params); err != nil {
		t.Fatalf("ReserveSeatAndCreateEnrollment: %v", err)
	}
	return w, p
}

func (p closeJob) want(t *testing.T, s *store.Store, status string, runAt time.Time) {
	t.Helper()
	j, err := s.JobByKey(context.Background(), string(p))
	if err != nil || j == nil {
		t.Fatalf("JobByKey(%s) = %v, %v", p, j, err)
	}
	if j.Status != status || !j.RunAt.Equal(runAt) {
		t.Errorf("close job = %s at %s, want %s at %s", j.Status, j.RunAt, status, runAt)
	}
}

type closeJob string

func closeOf(p store.ProposalRow) closeJob { return closeJob(store.JobKey(store.KindClose, p.ID)) }

func wantUnchanged(t *testing.T, s *store.Store, p store.ProposalRow) {
	t.Helper()
	got, err := s.ProposalByID(context.Background(), p.ID)
	if err != nil || got == nil {
		t.Fatalf("ProposalByID = %v, %v", got, err)
	}
	if !got.ClosesAt.Equal(p.ClosesAt) || got.ExtendedAt != nil || got.ExtendedFrom != nil {
		t.Errorf("proposal = closes %s, extended %v from %v; want it untouched at %s", got.ClosesAt, got.ExtendedAt, got.ExtendedFrom, p.ClosesAt)
	}
}

func TestExtendPaymentWindow_MovesTheDeadlineAndItsCloseTogether(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	_, p := extendableWorkshop(t, s)
	now := baseTime.Add(22 * time.Hour)

	got, err := s.ExtendPaymentWindow(ctx, p.ID, p.ClosesAt, extendedTo, now)
	if err != nil {
		t.Fatalf("ExtendPaymentWindow: %v", err)
	}
	if !got.ClosesAt.Equal(extendedTo) || got.ExtendedFrom == nil || !got.ExtendedFrom.Equal(p.ClosesAt) || got.ExtendedAt == nil || !got.ExtendedAt.Equal(now) {
		t.Errorf("extended proposal = closes %s, from %v at %v", got.ClosesAt, got.ExtendedFrom, got.ExtendedAt)
	}
	closeOf(p).want(t, s, store.JobPending, extendedTo)

	stored, err := s.ProposalByID(ctx, p.ID)
	if err != nil || stored == nil || !stored.ClosesAt.Equal(extendedTo) || stored.ExtendedFrom == nil {
		t.Errorf("stored proposal = %+v, %v", stored, err)
	}

	_, err = s.ExtendPaymentWindow(ctx, p.ID, extendedTo, extendedTo, now.Add(time.Minute))
	if !errors.Is(err, domain.ErrAlreadyExtended) {
		t.Errorf("a second extension = %v, want ErrAlreadyExtended", err)
	}
}

func TestExtendPaymentWindow_RollsBackUnlessTheCloseIsPending(t *testing.T) {
	for _, status := range []string{store.JobRunning, store.JobFailed, store.JobSucceeded, "missing"} {
		t.Run(status, func(t *testing.T) {
			ctx := context.Background()
			s := storetest.New(t)
			_, p := extendableWorkshop(t, s)
			key := store.JobKey(store.KindClose, p.ID)
			var err error
			if status == "missing" {
				_, err = s.Pool.Exec(ctx, `delete from jobs where idempotency_key = $1`, key)
			} else {
				_, err = s.Pool.Exec(ctx, `update jobs set status = $2 where idempotency_key = $1`, key, status)
			}
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.ExtendPaymentWindow(ctx, p.ID, p.ClosesAt, extendedTo, baseTime.Add(22*time.Hour))
			if !errors.Is(err, store.ErrCloseNotPending) {
				t.Fatalf("ExtendPaymentWindow with the close %s = %v, want ErrCloseNotPending", status, err)
			}
			wantUnchanged(t, s, p)
		})
	}
}

func TestExtendPaymentWindow_RefusesAnOptionThatChanged(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	_, p := extendableWorkshop(t, s)
	now := baseTime.Add(22 * time.Hour)

	if _, err := s.ExtendPaymentWindow(ctx, p.ID, p.ClosesAt.Add(-time.Hour), extendedTo, now); !errors.Is(err, store.ErrExtensionChanged) {
		t.Errorf("from another deadline = %v, want ErrExtensionChanged", err)
	}
	if _, err := s.ExtendPaymentWindow(ctx, p.ID, p.ClosesAt, extendedTo.Add(15*time.Minute), now); !errors.Is(err, store.ErrExtensionChanged) {
		t.Errorf("to a later deadline than on offer = %v, want ErrExtensionChanged", err)
	}
	if _, err := s.ExtendPaymentWindow(ctx, p.ID, p.ClosesAt, extendedTo.Add(-15*time.Minute), now); !errors.Is(err, store.ErrExtensionChanged) {
		t.Errorf("to an earlier deadline than on offer = %v, want ErrExtensionChanged", err)
	}
	wantUnchanged(t, s, p)
	closeOf(p).want(t, s, store.JobPending, p.ClosesAt)
}

func TestExtendPaymentWindow_RechecksTheRulesInsideTheTransaction(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	w, p := extendableWorkshop(t, s)
	older := reserveParams(w, p, newParticipant(t, s, w, "Ben").ID, baseTime.Add(2*time.Hour))
	older.ConsentVersion = "consent-v4"
	if _, _, err := s.ReserveSeatAndCreateEnrollment(ctx, older); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ExtendPaymentWindow(ctx, p.ID, p.ClosesAt, extendedTo, baseTime.Add(22*time.Hour)); !errors.Is(err, domain.ErrConsentPredatesExtension) {
		t.Errorf("with a v4 seat = %v, want ErrConsentPredatesExtension", err)
	}
	wantUnchanged(t, s, p)

	if _, err := s.ExtendPaymentWindow(ctx, p.ID, p.ClosesAt, extendedTo, p.ClosesAt.Add(-5*time.Minute)); !errors.Is(err, domain.ErrTooLateToExtend) {
		t.Errorf("five minutes before the close = %v, want ErrTooLateToExtend", err)
	}
}

func TestExtensionCheck_SeesQueuedCapturesAndHolds(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	w, p := extendableWorkshop(t, s)
	es, err := s.EnrollmentsByWorkshop(ctx, w.ID)
	if err != nil || len(es) != 1 {
		t.Fatalf("EnrollmentsByWorkshop = %d, %v", len(es), err)
	}
	a, err := s.CreateAttempt(ctx, es[0].ID, p.PriceMinor, p.Currency)
	if err != nil {
		t.Fatal(err)
	}
	now := baseTime.Add(22 * time.Hour)
	c, err := s.ExtensionCheck(ctx, domain.WorkshopFunding, p, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Holds) != 1 || c.Enrollments[0].ConsentVersion != "consent-v5" || c.Enrollments[0].CaptureStartedAt != nil || c.Extended {
		t.Errorf("check = %+v", c)
	}
	if _, err := s.EnqueueJob(ctx, store.KindCapture, store.JobKey(store.KindCapture, a.ID), &w.ID, &a.ID, nil, now); err != nil {
		t.Fatal(err)
	}
	if c, err = s.ExtensionCheck(ctx, domain.WorkshopFunding, p, now); err != nil || c.Enrollments[0].CaptureStartedAt == nil {
		t.Errorf("a queued capture is not seen: %+v, %v", c.Enrollments, err)
	}
	if _, err := domain.CanExtend(c); !errors.Is(err, domain.ErrChargingStarted) {
		t.Errorf("CanExtend with a queued capture = %v, want ErrChargingStarted", err)
	}
}

func TestShortenPaymentWindow_AfterAnExtension(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	_, p := extendableWorkshop(t, s)
	now := baseTime.Add(22 * time.Hour)
	if _, err := s.ExtendPaymentWindow(ctx, p.ID, p.ClosesAt, extendedTo, now); err != nil {
		t.Fatal(err)
	}
	later := now.Add(time.Hour)
	got, err := s.ShortenPaymentWindow(ctx, p.ID, later)
	if err != nil {
		t.Fatal(err)
	}
	if !got.ClosesAt.Equal(later) || got.ExtendedFrom == nil || !got.ExtendedFrom.Equal(p.ClosesAt) || got.ExtendedAt == nil {
		t.Errorf("shortened = closes %s, extended from %v at %v; want %s with the extension kept", got.ClosesAt, got.ExtendedFrom, got.ExtendedAt, later)
	}
}

func TestOverdueCloses_FollowTheExtendedDeadline(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	_, p := extendableWorkshop(t, s)
	if _, err := s.ExtendPaymentWindow(ctx, p.ID, p.ClosesAt, extendedTo, baseTime.Add(22*time.Hour)); err != nil {
		t.Fatal(err)
	}
	overdue, err := s.OverdueCloses(ctx, p.ClosesAt.Add(time.Hour))
	if err != nil || len(overdue) != 0 {
		t.Errorf("an hour past the old deadline = %+v, %v; want nothing overdue", overdue, err)
	}
	overdue, err = s.OverdueCloses(ctx, extendedTo.Add(time.Minute))
	if err != nil || len(overdue) != 1 || overdue[0].ProposalID != p.ID {
		t.Errorf("past the new deadline = %+v, %v; want the proposal", overdue, err)
	}
}

func waitForLockWaiter(t *testing.T, s *store.Store, done <-chan error) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		select {
		case err := <-done:
			t.Fatalf("the extension finished (%v) without waiting on the locked seat", err)
		default:
		}
		var waiting bool
		if err := s.Pool.QueryRow(context.Background(),
			`select exists (select 1 from pg_locks where not granted and locktype in ('transactionid', 'tuple'))`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the extension did not wait on the locked seat within 5s")
		}
	}
}

func TestExtendPaymentWindow_WaitsForAHoldBeingPlacedAndSeesIt(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	w, p := extendableWorkshop(t, s)
	es, err := s.EnrollmentsByWorkshop(ctx, w.ID)
	if err != nil || len(es) != 1 {
		t.Fatalf("EnrollmentsByWorkshop = %d, %v", len(es), err)
	}
	mustTransition(t, s, es[0].ID, []domain.EnrollmentStatus{domain.EnrollmentSeatReserved}, domain.EnrollmentAuthorized)
	ben := reserveParams(w, p, newParticipant(t, s, w, "Ben").ID, baseTime.Add(2*time.Hour))
	ben.ConsentVersion = "consent-v5"
	benSeat, _, err := s.ReserveSeatAndCreateEnrollment(ctx, ben)
	if err != nil {
		t.Fatal(err)
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `update enrollments set status = 'AUTHORIZED' where id = $1`, benSeat.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := s.ExtendPaymentWindow(ctx, p.ID, p.ClosesAt, extendedTo, baseTime.Add(22*time.Hour))
		done <- err
	}()
	waitForLockWaiter(t, s, done)
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, domain.ErrChargingStarted) {
		t.Fatalf("extension racing the hold that reaches the minimum = %v, want ErrChargingStarted", err)
	}
	wantUnchanged(t, s, p)
	closeOf(p).want(t, s, store.JobPending, p.ClosesAt)
}

func TestReReserveSeat_RefusesAPreExtensionConsentOnceExtended(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	w, p := extendableWorkshop(t, s)
	seat := func(name, consent string) store.EnrollmentRow {
		params := reserveParams(w, p, newParticipant(t, s, w, name).ID, baseTime.Add(2*time.Hour))
		params.ConsentVersion = consent
		en, _, err := s.ReserveSeatAndCreateEnrollment(ctx, params)
		if err != nil {
			t.Fatal(err)
		}
		mustTransition(t, s, en.ID, []domain.EnrollmentStatus{domain.EnrollmentSeatReserved}, domain.EnrollmentReleased)
		return en
	}
	older, newer := seat("Ben", "consent-v4"), seat("Cleo", "consent-v5")
	now := baseTime.Add(22 * time.Hour)
	if _, err := s.ExtendPaymentWindow(ctx, p.ID, p.ClosesAt, extendedTo, now); err != nil {
		t.Fatalf("a released v4 seat should not block the extension: %v", err)
	}

	if ok, err := s.ReReserveSeat(ctx, older.ID, now.Add(time.Hour), 30*time.Minute); err != nil || ok {
		t.Errorf("re-reserving a v4 seat after the extension = %v, %v; want refused", ok, err)
	}
	if en, err := s.EnrollmentByID(ctx, older.ID); err != nil || en.Status != domain.EnrollmentReleased {
		t.Errorf("v4 seat = %s, %v; want it still released", en.Status, err)
	}
	if ok, err := s.ReReserveSeat(ctx, newer.ID, now.Add(time.Hour), 30*time.Minute); err != nil || !ok {
		t.Errorf("re-reserving a v5 seat after the extension = %v, %v; want it taken again", ok, err)
	}
}

func TestSkipToLastHours_ShortensAndBringsTheCloseForwardTogether(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	_, p := extendableWorkshop(t, s)
	at := baseTime.Add(3 * time.Hour)

	got, err := s.SkipToLastHours(ctx, p.ID, at)
	if err != nil || !got.ClosesAt.Equal(at) {
		t.Fatalf("SkipToLastHours = %s, %v; want %s", got.ClosesAt, err, at)
	}
	closeOf(p).want(t, s, store.JobPending, at)
	if got, err := s.SkipToLastHours(ctx, p.ID, at.Add(time.Hour)); err != nil || !got.ClosesAt.Equal(at) {
		t.Errorf("a later skip = %s, %v; want the deadline kept at %s", got.ClosesAt, err, at)
	}
	closeOf(p).want(t, s, store.JobPending, at)

	if _, err := s.ExtendPaymentWindow(ctx, p.ID, at, extendedTo, baseTime.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SkipToLastHours(ctx, p.ID, baseTime.Add(2*time.Hour)); !errors.Is(err, domain.ErrAlreadyExtended) {
		t.Errorf("a skip after an extension = %v, want ErrAlreadyExtended", err)
	}
	closeOf(p).want(t, s, store.JobPending, extendedTo)
	if got, err := s.ProposalByID(ctx, p.ID); err != nil || !got.ClosesAt.Equal(extendedTo) {
		t.Errorf("proposal after a refused skip = %+v, %v", got, err)
	}
}

func TestReleaseExpiredSeats_WaitsForTheWorkshopLockSoItNeverDeadlocksAnExtension(t *testing.T) {
	ctx := context.Background()
	s := storetest.New(t)
	w, p := extendableWorkshop(t, s)
	for _, name := range []string{"Ben", "Cleo", "Dev"} {
		params := reserveParams(w, p, newParticipant(t, s, w, name).ID, baseTime.Add(time.Hour))
		params.ConsentVersion = "consent-v5"
		if _, _, err := s.ReserveSeatAndCreateEnrollment(ctx, params); err != nil {
			t.Fatal(err)
		}
	}
	now := baseTime.Add(22 * time.Hour)

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `select 1 from workshops where id = $1 for update`, w.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := s.ReleaseExpiredSeats(ctx, w.ID, now)
		done <- err
	}()
	waitForLockWaiter(t, s, done)
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("ReleaseExpiredSeats after the workshop lock was released: %v", err)
	}

	w2, p2 := extendableWorkshop(t, s)
	for _, name := range []string{"Eve", "Finn", "Gus"} {
		params := reserveParams(w2, p2, newParticipant(t, s, w2, name).ID, baseTime.Add(time.Hour))
		params.ConsentVersion = "consent-v5"
		if _, _, err := s.ReserveSeatAndCreateEnrollment(ctx, params); err != nil {
			t.Fatal(err)
		}
	}
	extended, released := make(chan error, 1), make(chan error, 1)
	go func() {
		_, err := s.ExtendPaymentWindow(ctx, p2.ID, p2.ClosesAt, extendedTo, now)
		extended <- err
	}()
	go func() {
		_, err := s.ReleaseExpiredSeats(ctx, w2.ID, now)
		released <- err
	}()
	if err := <-extended; err != nil {
		t.Errorf("extension running beside a seat release = %v", err)
	}
	if err := <-released; err != nil {
		t.Errorf("seat release running beside an extension = %v", err)
	}
	if n := count(t, s, `select count(*) from enrollments where workshop_id = $1 and status = 'RELEASED'`, w2.ID); n != 4 {
		t.Errorf("released seats = %d, want all 4 lapsed seats", n)
	}
}
