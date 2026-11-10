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
