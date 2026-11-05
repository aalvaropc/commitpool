package worker_test

import (
	"strconv"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/paypal"
	"github.com/aalvaropc/commitpool/internal/store"
)

// holdOnReleasedSeat plays a buyer whose seat was released before PayPal
// authorized the order, and the reconcile that finds that hold and enqueues
// its void.
func (e *env) holdOnReleasedSeat(name string) (store.EnrollmentRow, store.AttemptRow) {
	e.t.Helper()
	en, a := e.withOrder(name)
	e.fake.Approve(*a.OrderID)
	e.transition(en.ID, domain.EnrollmentSeatReserved, domain.EnrollmentReleased)
	if _, err := e.api.AuthorizeOrder(e.ctx, *a.OrderID, "authorize:"+a.ID); err != nil {
		e.t.Fatalf("authorize at PayPal: %v", err)
	}
	e.runKey(e.reconcileNow(a))
	e.wantJob("void:"+a.ID, "pending")
	return en, e.attempt(a.ID)
}

// refuseVoid has PayPal refuse the attempt's void for good, with an issue
// that does not mean the hold is gone.
func (e *env) refuseVoid(a store.AttemptRow) {
	e.t.Helper()
	e.fake.FailNext("void", 422, "PREVIOUSLY_CAPTURED")
	e.runKey("void:" + a.ID)
	e.wantJob("void:"+a.ID, "failed")
	wantString(e.t, "authorization_status", e.attempt(a.ID).AuthorizationStatus, "CREATED")
}

func TestVoid_RefusedOnAReleasedSeatFlagsTheWorkshop(t *testing.T) {
	t.Run("cancelled workshop", func(t *testing.T) {
		e := newEnv(t)
		en, a := e.holdOnReleasedSeat("Ana")
		e.moveWorkshop(domain.WorkshopFunding, domain.WorkshopCancelled, domain.ResolutionResolving)

		e.refuseVoid(a)

		e.wantStatus(en.ID, domain.EnrollmentReleased)
		e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionNeedsAttention)
	})

	t.Run("funding workshop stays flagged once confirmed", func(t *testing.T) {
		e := newEnv(t)
		_, a := e.holdOnReleasedSeat("Ana")
		e.refuseVoid(a)
		e.wantWorkshop(domain.WorkshopFunding, domain.ResolutionNeedsAttention)
		e.runPeriodic()
		if n := e.requestsWithID("void", "void:"+a.ID); n != 1 {
			t.Errorf("void requests = %d, want 1: a refused void is never sent again automatically", n)
		}

		for _, name := range []string{"Ben", "Cleo", "Dan"} {
			e.authorized(name)
		}
		e.runUntilIdle()

		e.wantWorkshop(domain.WorkshopConfirmed, domain.ResolutionNeedsAttention)
	})

	t.Run("funding workshop stays flagged once cancelled", func(t *testing.T) {
		e := newEnv(t)
		_, a := e.holdOnReleasedSeat("Ana")
		e.refuseVoid(a)
		e.clock.Set(e.proposal.ClosesAt)

		e.runKey(e.closeKey())

		e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionNeedsAttention)
	})
}

func TestReconcile_LastGenerationOnAReleasedSeatFlagsTheWorkshop(t *testing.T) {
	e := newEnv(t)
	en, a := e.withOrder("Ana")
	e.fake.Approve(*a.OrderID)
	key := e.enqueue("authorize", a)
	for gen := 1; gen <= 5; gen++ {
		e.fake.FailNext("authorize", 503, "")
		e.runKey(key)
		e.wantJob(key, "unknown")
		reconcileKey := "reconcile:" + a.ID + ":" + strconv.Itoa(gen)
		e.clock.Set(e.job(reconcileKey).RunAt)
		if gen == 5 {
			break
		}
		e.runKey(reconcileKey)
		e.wantJob(key, "pending")
	}
	// The seat lapses and the workshop is cancelled before the last
	// reconcile, which still finds no authorization at PayPal.
	e.advance(31 * time.Minute)
	e.runKey(e.releaseSeatsNow())
	e.wantStatus(en.ID, domain.EnrollmentReleased)
	e.moveWorkshop(domain.WorkshopFunding, domain.WorkshopCancelled, domain.ResolutionResolving)

	e.runKey("reconcile:" + a.ID + ":5")

	e.wantJob(key, "unknown")
	e.wantStatus(en.ID, domain.EnrollmentReleased)
	e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionNeedsAttention)
}

func TestSettle_WaitsForARunningAuthorizeOnAReleasedSeat(t *testing.T) {
	e := newEnv(t)
	en, a := e.withOrder("Ana")
	e.enqueue("authorize", a)
	if j, err := e.st.ClaimJob(e.ctx, "other-worker", e.clock.Now()); err != nil || j == nil || j.Kind != "authorize" {
		t.Fatalf("ClaimJob(other-worker) = %+v, %v; want the authorize job", j, err)
	}
	e.transition(en.ID, domain.EnrollmentSeatReserved, domain.EnrollmentReleased)
	e.moveWorkshop(domain.WorkshopFunding, domain.WorkshopCancelled, domain.ResolutionResolving)

	if err := e.runner.SettleIfResolved(e.ctx, e.workshop.ID); err != nil {
		t.Fatalf("SettleIfResolved: %v", err)
	}

	// The authorize another worker is running may still place a hold.
	e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionResolving)
}

func TestConfirmed_EnrollmentNeedingAttentionFlagsTheWorkshop(t *testing.T) {
	t.Run("after the confirmation", func(t *testing.T) {
		e := newEnv(t)
		attempts := e.confirmed()

		if err := e.runner.ApplyCapture(e.ctx, attempts[0].ID, paypal.Capture{ID: "SECONDCAPTURE", Status: "COMPLETED"}); err != nil {
			t.Fatalf("ApplyCapture: %v", err)
		}

		e.wantWorkshop(domain.WorkshopConfirmed, domain.ResolutionNeedsAttention)
	})

	t.Run("before the confirmation", func(t *testing.T) {
		e := newEnv(t)
		dan, danAttempt := e.withOrder("Dan")
		if err := e.runner.ApplyOrder(e.ctx, danAttempt.ID, paypal.Order{ID: "SECONDORDER", Status: "CREATED"}); err != nil {
			t.Fatalf("ApplyOrder: %v", err)
		}
		e.wantFlagged(dan.ID, domain.EnrollmentSeatReserved)
		// A funding workshop is not flagged for it: participants still pay.
		e.wantWorkshop(domain.WorkshopFunding, domain.ResolutionOpen)

		for _, name := range []string{"Ana", "Ben", "Cleo"} {
			e.authorized(name)
		}
		e.runUntilIdle()

		e.wantWorkshop(domain.WorkshopConfirmed, domain.ResolutionNeedsAttention)
	})
}

func TestHeal_ConfirmedWorkshopWithAnEnrollmentNeedingAttentionIsFlagged(t *testing.T) {
	e := newEnv(t)
	attempts := e.confirmed()
	ana, err := e.st.EnrollmentByID(e.ctx, attempts[0].EnrollmentID)
	if err != nil {
		t.Fatal(err)
	}
	// Ana was flagged, but the hook that flags the workshop never ran.
	e.flag(ana.ID)
	e.wantWorkshop(domain.WorkshopConfirmed, domain.ResolutionOpen)

	if err := e.runner.HealWorkshop(e.ctx, e.workshop.ID); err != nil {
		t.Fatalf("HealWorkshop: %v", err)
	}

	e.wantWorkshop(domain.WorkshopConfirmed, domain.ResolutionNeedsAttention)
}

func TestHeal_CancelledWorkshopRefundsAFlaggedPayment(t *testing.T) {
	e := newEnv(t)
	en, a := e.paid("Ana")
	if err := e.runner.ApplyCapture(e.ctx, a.ID, paypal.Capture{ID: "SECONDCAPTURE", Status: "COMPLETED"}); err != nil {
		t.Fatalf("ApplyCapture: %v", err)
	}
	e.wantFlagged(en.ID, domain.EnrollmentPaid)
	e.moveWorkshop(domain.WorkshopFunding, domain.WorkshopCancelled, domain.ResolutionResolving)

	if err := e.runner.HealWorkshop(e.ctx, e.workshop.ID); err != nil {
		t.Fatalf("HealWorkshop: %v", err)
	}

	e.wantJob("refund:"+a.ID, "pending")
	e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionNeedsAttention)
}

// An abandoned capture (no trace at PayPal after the last reconcile) keeps
// its seat and its flag until the close, whose reconcile sends it once more:
// at the close instant it still counts, so the buyer is charged and refunded
// at once, and no money is kept.
func TestReconcile_AbandonedCaptureKeepsNoMoneyAfterTheClose(t *testing.T) {
	e := newEnv(t)
	en, a := e.authorized("Ana")
	key := e.enqueue("capture", a)
	for gen := 1; gen <= 5; gen++ {
		e.fake.FailNext("capture", 503, "")
		e.runKey(key)
		reconcileKey := "reconcile:" + a.ID + ":" + strconv.Itoa(gen)
		e.clock.Set(e.job(reconcileKey).RunAt)
		e.runKey(reconcileKey)
	}
	e.wantFlagged(en.ID, domain.EnrollmentCapturing)

	e.clock.Set(e.proposal.ClosesAt)
	e.enqueueClose()
	e.runUntilIdle()

	// PayPal never captured, and the cancelled workshop does not send the
	// capture again: the hold is voided and the seat stays flagged.
	if n := e.fake.Captures(); n != 0 {
		t.Fatalf("captures at PayPal = %d, want 0", n)
	}
	e.wantFlagged(en.ID, domain.EnrollmentCaptureFailed)
	wantString(t, "authorization", e.attempt(a.ID).AuthorizationStatus, "VOIDED")
	wantString(t, "capture_issue", e.attempt(a.ID).CaptureIssue, "WORKSHOP_CANCELLED")
	e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionNeedsAttention)
}
