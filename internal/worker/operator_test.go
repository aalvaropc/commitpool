package worker_test

import (
	"errors"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
)

// refundRefused cancels a workshop with Ana paid and has PayPal refuse her
// refund, so she ends REFUND_FAILED, flagged, and the workshop needs an
// operator.
func (e *env) refundRefused() (enrollmentID, attemptID string) {
	e.t.Helper()
	ana, a := e.paid("Ana")
	e.setMockCode(a, "REFUND_AMOUNT_EXCEEDED")
	e.clock.Set(e.proposal.ClosesAt)
	e.runKey(e.closeKey())
	e.runKey("refund:" + a.ID)
	e.wantFlagged(ana.ID, domain.EnrollmentRefundFailed)
	e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionNeedsAttention)
	return ana.ID, a.ID
}

func TestOperator_RetryRefundSendsANewRefundOnce(t *testing.T) {
	e := newEnv(t)
	ana, a := e.refundRefused()

	key, err := e.runner.RetryRefund(e.ctx, ana)
	if err != nil {
		t.Fatalf("RetryRefund: %v", err)
	}
	if key != "refund:"+a+":retry:1" {
		t.Fatalf("retry key = %q, want refund:%s:retry:1", key, a)
	}
	e.runUntilIdle()

	e.wantStatus(ana, domain.EnrollmentRefunded)
	if got, _ := e.st.EnrollmentByID(e.ctx, ana); got.NeedsAttentionAt != nil {
		t.Errorf("enrollment still flagged after its refund completed")
	}
	e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionSettled)
	if n := e.requestsWithID("refund", key); n != 1 {
		t.Errorf("refund requests under %s = %d, want 1", key, n)
	}

	if _, err := e.runner.RetryRefund(e.ctx, ana); !errors.Is(err, domain.ErrNotRefundFailed) {
		t.Errorf("a second retry = %v, want ErrNotRefundFailed", err)
	}
}

func TestOperator_ClearWorkshopFlagRefusedWhileAnEnrollmentNeedsAnOperator(t *testing.T) {
	e := newEnv(t)
	ana, _ := e.refundRefused()

	if err := e.runner.ClearWorkshopFlag(e.ctx, e.workshop.ID); !errors.Is(err, domain.ErrStillUnresolved) {
		t.Fatalf("ClearWorkshopFlag while Ana's refund failed = %v, want ErrStillUnresolved", err)
	}

	// The operator settled Ana's money at PayPal by hand and records it.
	e.transition(ana, domain.EnrollmentRefundFailed, domain.EnrollmentRefunded)
	if err := e.runner.ClearEnrollmentFlag(e.ctx, ana); err != nil {
		t.Fatalf("ClearEnrollmentFlag: %v", err)
	}
	if err := e.runner.ClearWorkshopFlag(e.ctx, e.workshop.ID); err != nil {
		t.Fatalf("ClearWorkshopFlag: %v", err)
	}

	e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionSettled)
}

func TestOperator_ReconcileEnqueuesAReadOnlyReconcile(t *testing.T) {
	e := newEnv(t)
	en, a := e.authorized("Ana")

	key, err := e.runner.OperatorReconcile(e.ctx, en.ID)
	if err != nil {
		t.Fatalf("OperatorReconcile: %v", err)
	}
	j := e.wantJob(key, "pending")
	if j.Kind != "reconcile" || j.AttemptID == nil || *j.AttemptID != a.ID {
		t.Errorf("operator job = %+v, want a reconcile of Ana's attempt", j)
	}
	e.runKey(key)
	e.wantJob(key, "succeeded")
	e.wantStatus(en.ID, domain.EnrollmentAuthorized)
}

func TestRetryRefund_ReconcileAfterAnAmbiguousRetryRecordsTheNewRefund(t *testing.T) {
	e := newEnv(t, withClientTimeout(time.Second))
	ana, a := e.paid("Ana")
	e.clock.Set(e.proposal.ClosesAt)
	e.runKey(e.closeKey())
	oldRefund := e.refundPending(ana, a)
	e.fake.SetRefundStatus(oldRefund, "FAILED")
	e.runKey(e.reconcileNow(a))
	e.wantStatus(ana.ID, domain.EnrollmentRefundFailed)
	e.fake.SetCaptureStatus(*e.attempt(a.ID).CaptureID, "COMPLETED", "")

	// PayPal refunds, but the answer never comes back.
	e.fake.FailNext("refund", 0, "")
	key, err := e.runner.RetryRefund(e.ctx, ana.ID)
	if err != nil {
		t.Fatalf("RetryRefund: %v", err)
	}
	e.runKey(key)
	e.fake.WaitHung()
	e.advance(3 * time.Minute)
	e.runUntilIdle()

	at := e.attempt(a.ID)
	if at.RefundID == nil || *at.RefundID == oldRefund {
		t.Fatalf("refund_id = %v, want the new refund, not the failed %s", deref(at.RefundID), oldRefund)
	}
	wantString(t, "refund_status", at.RefundStatus, "COMPLETED")
	e.wantStatus(ana.ID, domain.EnrollmentRefunded)
	if got, _ := e.st.EnrollmentByID(e.ctx, ana.ID); got.NeedsAttentionAt != nil {
		t.Fatalf("enrollment still flagged: %v", deref(got.AttentionReason))
	}
}

func TestRetryRefund_OperatorReconcileBeforeTheRetryRunsIgnoresTheFailedRefund(t *testing.T) {
	e := newEnv(t, withClientTimeout(time.Second))
	ana, a := e.paid("Ana")
	e.clock.Set(e.proposal.ClosesAt)
	e.runKey(e.closeKey())
	oldRefund := e.refundPending(ana, a)
	e.fake.SetRefundStatus(oldRefund, "FAILED")
	e.runKey(e.reconcileNow(a))
	e.wantStatus(ana.ID, domain.EnrollmentRefundFailed)
	e.fake.SetCaptureStatus(*e.attempt(a.ID).CaptureID, "COMPLETED", "")

	key, err := e.runner.RetryRefund(e.ctx, ana.ID)
	if err != nil {
		t.Fatalf("RetryRefund: %v", err)
	}
	reconcile, err := e.runner.OperatorReconcile(e.ctx, ana.ID)
	if err != nil {
		t.Fatalf("OperatorReconcile: %v", err)
	}
	e.runKey(reconcile)
	if at := e.attempt(a.ID); at.RefundID != nil {
		t.Fatalf("operator reconcile recorded refund %s, want the failed one ignored", *at.RefundID)
	}
	e.wantStatus(ana.ID, domain.EnrollmentPaid)

	e.runKey(key)
	e.runUntilIdle()
	e.wantStatus(ana.ID, domain.EnrollmentRefunded)
}

func TestOperator_ClearEnrollmentFlagRefusedWhileItsHoldIsStillOpen(t *testing.T) {
	e := newEnv(t)
	en, a := e.authorized("Ana")
	e.fake.FailNext("capture", 422, "INSTRUMENT_DECLINED")
	e.runKey(e.enqueue("capture", a))
	e.refuseVoid(a)
	e.wantFlagged(en.ID, domain.EnrollmentCaptureFailed)

	if err := e.runner.ClearEnrollmentFlag(e.ctx, en.ID); !errors.Is(err, domain.ErrStillUnresolved) {
		t.Fatalf("ClearEnrollmentFlag while PayPal still holds the money = %v, want ErrStillUnresolved", err)
	}
	e.wantFlagged(en.ID, domain.EnrollmentCaptureFailed)

	// The operator voids the hold at PayPal by hand and reads it back.
	e.fake.SetAuthorizationStatus(*e.attempt(a.ID).AuthorizationID, "VOIDED")
	key, err := e.runner.OperatorReconcile(e.ctx, en.ID)
	if err != nil {
		t.Fatalf("OperatorReconcile: %v", err)
	}
	e.runKey(key)
	if err := e.runner.ClearEnrollmentFlag(e.ctx, en.ID); err != nil {
		t.Fatalf("ClearEnrollmentFlag once the hold is gone: %v", err)
	}
}

func TestOperator_ClearWorkshopFlagRefusedWhileAReleasedSeatStillHoldsMoney(t *testing.T) {
	e := newEnv(t)
	en, a := e.holdOnReleasedSeat("Ana")
	e.moveWorkshop(domain.WorkshopFunding, domain.WorkshopCancelled, domain.ResolutionResolving)
	e.refuseVoid(a)
	e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionNeedsAttention)

	if err := e.runner.ClearWorkshopFlag(e.ctx, e.workshop.ID); !errors.Is(err, domain.ErrStillUnresolved) {
		t.Fatalf("ClearWorkshopFlag while a released seat's hold is open = %v, want ErrStillUnresolved", err)
	}

	e.fake.SetAuthorizationStatus(*e.attempt(a.ID).AuthorizationID, "VOIDED")
	key, err := e.runner.OperatorReconcile(e.ctx, en.ID)
	if err != nil {
		t.Fatalf("OperatorReconcile: %v", err)
	}
	e.runKey(key)
	if err := e.runner.ClearWorkshopFlag(e.ctx, e.workshop.ID); err != nil {
		t.Fatalf("ClearWorkshopFlag once the hold is gone: %v", err)
	}
	e.wantWorkshop(domain.WorkshopCancelled, domain.ResolutionSettled)
}

func TestOperator_ClearEnrollmentFlagRefusedWhileItsVoidKeepsFailing(t *testing.T) {
	e := newEnv(t)
	en, _ := e.authorized("Ana")
	_, key, err := e.runner.Withdraw(e.ctx, en.ID)
	if err != nil {
		t.Fatalf("Withdraw: %v", err)
	}
	backoff := []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute}
	for i := range 5 {
		e.fake.FailNext("void", 429, "RATE_LIMIT_REACHED")
		e.runKey(key)
		if i < 4 {
			e.advance(backoff[i])
		}
	}
	e.wantJob(key, "pending")
	e.wantFlagged(en.ID, domain.EnrollmentVoiding)

	if err := e.runner.ClearEnrollmentFlag(e.ctx, en.ID); !errors.Is(err, domain.ErrStillUnresolved) {
		t.Fatalf("ClearEnrollmentFlag while its void keeps failing = %v, want ErrStillUnresolved", err)
	}
}
