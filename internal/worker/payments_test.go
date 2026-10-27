package worker_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/paypal"
)

func TestCreateOrder_PersistsKeyBeforeSendAndStoresApproveURL(t *testing.T) {
	e := newEnv(t)
	en, a := e.enroll("Ana")
	key := e.enqueue("create_order", a)
	e.wantJob(key, "pending")
	if n := len(e.fake.Requests("create_order")); n != 0 {
		t.Fatalf("create_order requests before the job ran = %d, want 0", n)
	}

	e.runKey(key)

	reqs := e.fake.Requests("create_order")
	if len(reqs) != 1 || reqs[0].RequestID != "create_order:"+a.ID {
		t.Fatalf("create_order requests = %+v, want one with PayPal-Request-Id create_order:%s", reqs, a.ID)
	}
	var body struct {
		Intent        string                `json:"intent"`
		PurchaseUnits []paypal.PurchaseUnit `json:"purchase_units"`
		PaymentSource struct {
			PayPal struct {
				ExperienceContext struct {
					ReturnURL string `json:"return_url"`
					CancelURL string `json:"cancel_url"`
				} `json:"experience_context"`
			} `json:"paypal"`
		} `json:"payment_source"`
	}
	if err := json.Unmarshal(reqs[0].Body, &body); err != nil {
		t.Fatalf("create_order body: %v", err)
	}
	if body.Intent != "AUTHORIZE" || len(body.PurchaseUnits) != 1 {
		t.Fatalf("create_order body = %+v, want one AUTHORIZE purchase unit", body)
	}
	unit := body.PurchaseUnits[0]
	if unit.ReferenceID != a.ID || unit.CustomID != en.ID || unit.Amount != (paypal.Money{CurrencyCode: "USD", Value: "10.00"}) {
		t.Errorf("purchase unit = %+v, want reference %s, custom %s, 10.00 USD", unit, a.ID, en.ID)
	}
	ec := body.PaymentSource.PayPal.ExperienceContext
	if ec.ReturnURL != appBaseURL+"/paypal/return?e="+en.ID || ec.CancelURL != appBaseURL+"/paypal/cancel?e="+en.ID {
		t.Errorf("return/cancel = %s / %s, want the app's /paypal/return and /paypal/cancel for %s", ec.ReturnURL, ec.CancelURL, en.ID)
	}

	got := e.attempt(a.ID)
	if got.OrderID == nil {
		t.Fatalf("attempt order_id = nil, want the created order")
	}
	wantString(t, "order_status", got.OrderStatus, "PAYER_ACTION_REQUIRED")

	j := e.wantJob(key, "succeeded")
	var payload struct {
		ApproveURL string `json:"approve_url"`
		OrderID    string `json:"order_id"`
	}
	if err := json.Unmarshal(j.Payload, &payload); err != nil {
		t.Fatalf("job payload %s: %v", j.Payload, err)
	}
	if !strings.Contains(payload.ApproveURL, "/approve?order=") || payload.OrderID != *got.OrderID {
		t.Errorf("job payload = %+v, want the approve URL and order %s", payload, *got.OrderID)
	}
	e.wantStatus(en.ID, domain.EnrollmentSeatReserved)
	e.assertLogsClean()
}

func TestAuthorize_MovesSeatReservedToAuthorizedAndStoresExpiry(t *testing.T) {
	e := newEnv(t)
	en, a := e.withOrder("Ana")
	e.fake.Approve(*a.OrderID)
	key := e.enqueue("authorize", a)

	e.runKey(key)

	if reqs := e.fake.Requests("authorize"); len(reqs) != 1 || reqs[0].RequestID != key {
		t.Fatalf("authorize requests = %+v, want one with PayPal-Request-Id %s", reqs, key)
	}
	got := e.attempt(a.ID)
	if got.AuthorizationID == nil {
		t.Fatalf("attempt authorization_id = nil, want the authorization")
	}
	wantString(t, "authorization_status", got.AuthorizationStatus, "CREATED")
	wantString(t, "order_status", got.OrderStatus, "COMPLETED")
	auth, err := e.api.GetAuthorization(e.ctx, *got.AuthorizationID)
	if err != nil {
		t.Fatal(err)
	}
	if got.AuthExpiresAt == nil || !got.AuthExpiresAt.Equal(auth.ExpirationTime) {
		t.Errorf("auth_expires_at = %v, want PayPal's expiration_time %s", deref(got.AuthExpiresAt), auth.ExpirationTime)
	}
	e.wantStatus(en.ID, domain.EnrollmentAuthorized)
	e.wantJob(key, "succeeded")
}

func TestAuthorize_ExpiredOrderIsDefinitive(t *testing.T) {
	e := newEnv(t)
	en, a := e.withOrder("Ana")
	e.fake.ExpireOrder(*a.OrderID)
	key := e.enqueue("authorize", a)

	e.runKey(key)

	got := e.attempt(a.ID)
	wantString(t, "order_status", got.OrderStatus, "EXPIRED")
	if got.AuthorizationID != nil {
		t.Errorf("authorization_id = %s, want nil", *got.AuthorizationID)
	}
	e.wantStatus(en.ID, domain.EnrollmentSeatReserved)
	if j := e.wantJob(key, "succeeded"); j.HTTPStatus == nil || *j.HTTPStatus != 404 {
		t.Errorf("job http_status = %v, want 404", deref(j.HTTPStatus))
	}
	e.wantIdle()
}

func TestCapture_422MarksFailedAndWorkshopStaysFunding(t *testing.T) {
	e := newEnv(t)
	en, a := e.authorized("Ana")
	e.fake.FailNext("capture", 422, "AUTHORIZATION_EXPIRED")
	key := e.enqueue("capture", a)

	e.runOnce()

	e.wantStatus(en.ID, domain.EnrollmentCaptureFailed)
	got := e.attempt(a.ID)
	wantString(t, "capture_issue", got.CaptureIssue, "AUTHORIZATION_EXPIRED")
	wantString(t, "capture_status", got.CaptureStatus, "FAILED")
	if got.CaptureDebugID == nil || *got.CaptureDebugID == "" {
		t.Errorf("capture_debug_id = %v, want PayPal's debug_id", deref(got.CaptureDebugID))
	}
	if got.CaptureID != nil {
		t.Errorf("capture_id = %s, want nil", *got.CaptureID)
	}
	if j := e.wantJob(key, "succeeded"); j.HTTPStatus == nil || *j.HTTPStatus != 422 {
		t.Errorf("job http_status = %v, want 422", deref(j.HTTPStatus))
	}
	w, err := e.st.WorkshopByID(e.ctx, e.workshop.ID)
	if err != nil {
		t.Fatal(err)
	}
	if w.Status != domain.WorkshopFunding {
		t.Errorf("workshop status = %s, want FUNDING", w.Status)
	}
	if e.fake.Captures() != 0 {
		t.Errorf("captures = %d, want 0", e.fake.Captures())
	}
}

func TestCaptureFailure_EnqueuesVoidThatRecordsVoidedWithoutChangingEnrollment(t *testing.T) {
	e := newEnv(t)
	en, a := e.authorized("Ana")
	e.fake.FailNext("capture", 422, "TRANSACTION_REFUSED")
	e.runKey(e.enqueue("capture", a))
	e.wantStatus(en.ID, domain.EnrollmentCaptureFailed)
	voidKey := "void:" + a.ID
	e.wantJob(voidKey, "pending")

	e.runOnce()

	if reqs := e.fake.Requests("void"); len(reqs) != 1 || reqs[0].RequestID != voidKey {
		t.Fatalf("void requests = %+v, want one with PayPal-Request-Id %s", reqs, voidKey)
	}
	wantString(t, "authorization_status", e.attempt(a.ID).AuthorizationStatus, "VOIDED")
	e.wantStatus(en.ID, domain.EnrollmentCaptureFailed)
	e.wantJob(voidKey, "succeeded")
	auth, err := e.api.GetAuthorization(e.ctx, *a.AuthorizationID)
	if err != nil || auth.Status != "VOIDED" {
		t.Errorf("authorization at PayPal = %+v, %v; want VOIDED", auth, err)
	}
}

func TestCapture_DeadlinePassedFailsWithoutCallingPayPal(t *testing.T) {
	e := newEnv(t)
	en, a := e.authorized("Ana")
	e.clock.Set(e.proposal.ClosesAt.Add(time.Minute))
	key := e.enqueue("capture", a)

	e.runOnce()

	if n := len(e.fake.Requests("capture")); n != 0 {
		t.Fatalf("capture requests = %d, want 0 past the deadline", n)
	}
	e.wantStatus(en.ID, domain.EnrollmentCaptureFailed)
	wantString(t, "capture_issue", e.attempt(a.ID).CaptureIssue, "CAPTURE_DEADLINE_PASSED")
	e.wantJob(key, "succeeded")
	e.wantJob("void:"+a.ID, "pending")
}

func TestCapture_TimeoutThenReconcileDoesNotDoubleCharge(t *testing.T) {
	e := newEnv(t, withClientTimeout(time.Second))
	en, a := e.authorized("Ana")
	e.fake.FailNext("capture", 0, "")
	key := e.enqueue("capture", a)

	e.runOnce()
	e.fake.WaitHung()

	e.wantJob(key, "unknown")
	e.wantStatus(en.ID, domain.EnrollmentCapturing)
	reconcileKey := "reconcile:" + a.ID + ":1"
	if r := e.wantJob(reconcileKey, "pending"); !r.RunAt.Equal(e.clock.Now().Add(30 * time.Second)) {
		t.Errorf("reconcile run_at = %s, want now+30s", r.RunAt)
	}
	if e.fake.Captures() != 1 {
		t.Fatalf("captures at PayPal = %d, want 1 (PayPal ran the timed-out request)", e.fake.Captures())
	}

	e.advance(31 * time.Second)
	e.runKey(reconcileKey)

	e.wantStatus(en.ID, domain.EnrollmentPaid)
	got := e.attempt(a.ID)
	if got.CaptureID == nil {
		t.Fatalf("capture_id = nil, want the capture PayPal made")
	}
	wantString(t, "capture_status", got.CaptureStatus, "COMPLETED")
	if e.fake.Captures() != 1 || len(e.fake.Requests("capture")) != 1 {
		t.Errorf("captures/capture requests = %d/%d, want 1/1", e.fake.Captures(), len(e.fake.Requests("capture")))
	}
	e.wantJob(key, "succeeded")
	e.wantJob(reconcileKey, "succeeded")
	e.wantIdle()
}

func TestCapture_PendingThenCompletedViaReconcile(t *testing.T) {
	e := newEnv(t, withClientTimeout(time.Second))
	en, a := e.authorized("Ana")
	e.fake.FailNext("capture", 0, "")
	key := e.enqueue("capture", a)
	e.runOnce()
	e.fake.WaitHung()
	captures := e.order(*a.OrderID).PurchaseUnits[0].Payments.Captures
	if len(captures) != 1 {
		t.Fatalf("captures on the order = %d, want 1", len(captures))
	}
	captureID := captures[0].ID
	e.fake.SetCaptureStatus(captureID, "PENDING", "PENDING_REVIEW")

	e.advance(31 * time.Second)
	e.runKey("reconcile:" + a.ID + ":1")

	e.wantStatus(en.ID, domain.EnrollmentCapturePending)
	got := e.attempt(a.ID)
	wantString(t, "capture_id", got.CaptureID, captureID)
	wantString(t, "capture_status", got.CaptureStatus, "PENDING")
	wantString(t, "capture_reason", got.CaptureReason, "PENDING_REVIEW")
	e.wantJob(key, "succeeded")

	e.fake.SetCaptureStatus(captureID, "COMPLETED", "")
	later := "reconcile:" + a.ID + ":2"
	e.enqueueKey("reconcile", later, a, e.clock.Now())
	e.runKey(later)

	e.wantStatus(en.ID, domain.EnrollmentPaid)
	got = e.attempt(a.ID)
	wantString(t, "capture_status", got.CaptureStatus, "COMPLETED")
	if got.CaptureReason != nil {
		t.Errorf("capture_reason = %q, want cleared once completed", *got.CaptureReason)
	}
	if e.fake.Captures() != 1 {
		t.Errorf("captures = %d, want 1", e.fake.Captures())
	}
}

func TestVoid_204MarksVoided(t *testing.T) {
	e := newEnv(t)
	en, a := e.authorized("Ana")
	key := e.enqueue("void", a)

	e.runOnce()

	if reqs := e.fake.Requests("void"); len(reqs) != 1 || reqs[0].RequestID != key {
		t.Fatalf("void requests = %+v, want one with PayPal-Request-Id %s", reqs, key)
	}
	wantString(t, "authorization_status", e.attempt(a.ID).AuthorizationStatus, "VOIDED")
	e.wantStatus(en.ID, domain.EnrollmentVoided)
	e.wantJob(key, "succeeded")
	auth, err := e.api.GetAuthorization(e.ctx, *a.AuthorizationID)
	if err != nil || auth.Status != "VOIDED" {
		t.Errorf("authorization at PayPal = %+v, %v; want VOIDED", auth, err)
	}
}

func TestRefund_PendingStaysPendingUntilGetSaysCompleted(t *testing.T) {
	e := newEnv(t, withClientTimeout(time.Second))
	en, a := e.paid("Ana")
	e.fake.FailNext("refund", 0, "")
	key := e.enqueue("refund", a)
	e.runOnce()
	e.fake.WaitHung()
	e.wantJob(key, "unknown")
	e.wantStatus(en.ID, domain.EnrollmentRefunding)
	refunds := e.order(*a.OrderID).PurchaseUnits[0].Payments.Refunds
	if len(refunds) != 1 {
		t.Fatalf("refunds on the order = %d, want 1", len(refunds))
	}
	refundID := refunds[0].ID
	e.fake.SetRefundStatus(refundID, "PENDING")

	e.advance(31 * time.Second)
	e.runKey("reconcile:" + a.ID + ":1")

	e.wantStatus(en.ID, domain.EnrollmentRefundPending)
	got := e.attempt(a.ID)
	wantString(t, "refund_id", got.RefundID, refundID)
	wantString(t, "refund_status", got.RefundStatus, "PENDING")
	e.wantJob(key, "succeeded")

	second := "reconcile:" + a.ID + ":2"
	e.enqueueKey("reconcile", second, a, e.clock.Now())
	e.runKey(second)
	e.wantStatus(en.ID, domain.EnrollmentRefundPending)

	e.fake.SetRefundStatus(refundID, "COMPLETED")
	third := "reconcile:" + a.ID + ":3"
	e.enqueueKey("reconcile", third, a, e.clock.Now())
	e.runKey(third)

	e.wantStatus(en.ID, domain.EnrollmentRefunded)
	wantString(t, "refund_status", e.attempt(a.ID).RefundStatus, "COMPLETED")
	if n := len(e.fake.Requests("refund")); n != 1 {
		t.Errorf("refund requests = %d, want 1", n)
	}
}
