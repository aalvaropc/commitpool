package worker_test

import (
	"errors"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/store"
)

func (e *env) cancel() {
	e.t.Helper()
	if err := e.runner.CancelWorkshop(e.ctx, e.workshop.ID, "venue unavailable"); err != nil {
		e.t.Fatalf("CancelWorkshop: %v", err)
	}
}

func (e *env) jobCount() int {
	e.t.Helper()
	var n int
	if err := e.st.Pool.QueryRow(e.ctx, `select count(*) from jobs`).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func TestCancel_FundingVoidsRefundsAndSettles(t *testing.T) {
	e := newEnv(t)
	ana, a := e.paid("Ana")
	ben, b := e.authorized("Ben")
	cleo, _ := e.enroll("Cleo")

	e.cancel()

	e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionResolving)
	e.wantStatus(cleo.ID, domain.EnrollmentReleased)
	e.wantJob("refund:"+a.ID, "pending")
	e.wantJob("void:"+b.ID, "pending")
	w, err := e.st.WorkshopByID(e.ctx, e.workshop.ID)
	if err != nil || w.CancelledAt == nil || w.CancelReason == nil || *w.CancelReason != "venue unavailable" {
		t.Fatalf("cancelled workshop = %+v, %v; want when and why", w, err)
	}

	e.runUntilIdle()

	e.wantStatus(ana.ID, domain.EnrollmentRefunded)
	e.wantStatus(ben.ID, domain.EnrollmentVoided)
	e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionSettled)
}

func TestCancel_ConfirmedRefundsEveryPayment(t *testing.T) {
	e := newEnv(t)
	attempts := e.confirmed()

	e.cancel()
	e.runUntilIdle()

	for _, a := range attempts {
		e.wantStatus(a.EnrollmentID, domain.EnrollmentRefunded)
	}
	e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionSettled)
}

func TestCancel_CollectingSettlesAtOnce(t *testing.T) {
	e := newEnv(t)
	w, _, _, err := e.st.CreateWorkshop(e.ctx, store.CreateWorkshopParams{
		Title: "Never funded", Description: "Payments were never opened.", DurationMin: 60, Timezone: "UTC",
		Minimum: 1, Capacity: 2, PriceMinor: 1000, Currency: "USD", PaymentWindowMin: 1440,
		Dates: []struct{ StartsAt, EndsAt time.Time }{{StartsAt: t0.Add(72 * time.Hour), EndsAt: t0.Add(73 * time.Hour)}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := e.runner.CancelWorkshop(e.ctx, w.ID, ""); err != nil {
		t.Fatalf("CancelWorkshop: %v", err)
	}

	got, err := e.st.WorkshopByID(e.ctx, w.ID)
	if err != nil || got.Status != domain.WorkshopCancelled || got.Resolution != domain.ResolutionSettled {
		t.Fatalf("workshop = %+v, %v; want CANCELLED/SETTLED: no money was ever held", got, err)
	}
}

func TestCancel_RepeatedIsRefusedAndEnqueuesNothingNew(t *testing.T) {
	e := newEnv(t)
	e.paid("Ana")
	e.cancel()
	before := e.jobCount()

	err := e.runner.CancelWorkshop(e.ctx, e.workshop.ID, "again")

	if !errors.Is(err, domain.ErrAlreadyCancelled) {
		t.Fatalf("second cancel = %v, want ErrAlreadyCancelled", err)
	}
	if n := e.jobCount(); n != before {
		t.Errorf("jobs after a second cancel = %d, want %d", n, before)
	}
}

func TestCancel_CloseAfterCancelIsNoop(t *testing.T) {
	e := newEnv(t)
	e.paid("Ana")
	e.cancel()
	e.runUntilIdle()
	before := e.jobCount()
	e.clock.Set(e.proposal.ClosesAt)

	e.runKey(e.closeKey())

	if n := e.jobCount(); n != before {
		t.Errorf("jobs after the close = %d, want %d: the cancel already owed everything", n, before)
	}
	e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionSettled)
}

func TestCancel_RefusedAfterTheWorkshopStarted(t *testing.T) {
	e := newEnv(t)
	e.confirmed()
	e.clock.Set(e.proposal.StartsAt)

	err := e.runner.CancelWorkshop(e.ctx, e.workshop.ID, "too late")

	if !errors.Is(err, domain.ErrWorkshopStarted) {
		t.Fatalf("cancel after the start = %v, want ErrWorkshopStarted", err)
	}
	e.wantWorkshop(domain.WorkshopConfirmed, domain.ResolutionOpen)
}

func TestCancel_QueuedCapturesNeverCharge(t *testing.T) {
	e := newEnv(t)
	ana, _ := e.authorized("Ana")
	ben, _ := e.authorized("Ben")
	cleo, c := e.authorized("Cleo")
	e.wantJob("capture:"+c.ID, "pending")

	e.cancel()
	e.runUntilIdle()

	if n := e.fake.Captures(); n != 0 {
		t.Fatalf("captures at PayPal after the organizer cancelled = %d, want 0", n)
	}
	for _, id := range []string{ana.ID, ben.ID, cleo.ID} {
		e.wantStatus(id, domain.EnrollmentVoided)
	}
	e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionSettled)
}

func TestCancel_CaptureWaitingOutABackoffIsNotSentAgain(t *testing.T) {
	e := newEnv(t)
	ana, a := e.authorized("Ana")
	e.fake.FailNext("capture", 429, "RATE_LIMIT_REACHED")
	e.runKey(e.enqueue("capture", a))
	e.wantStatus(ana.ID, domain.EnrollmentCapturing)

	e.cancel()
	e.advance(10 * time.Second)
	e.runUntilIdle()

	if n := len(e.fake.Requests("capture")); n != 1 {
		t.Fatalf("capture requests = %d, want only the one refused before the cancel", n)
	}
	e.wantStatus(ana.ID, domain.EnrollmentCaptureFailed)
	if got := e.attempt(a.ID).AuthorizationStatus; got == nil || *got != "VOIDED" {
		t.Fatalf("authorization status = %v, want the hold voided", got)
	}
	e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionSettled)
}
