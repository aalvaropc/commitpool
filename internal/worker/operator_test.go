package worker_test

import (
	"errors"
	"testing"

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
