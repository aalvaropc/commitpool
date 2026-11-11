package worker_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/paypal"
	"github.com/aalvaropc/commitpool/internal/store"
	"github.com/aalvaropc/commitpool/internal/worker"
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
	j := e.wantJob(key, "failed")
	if j.HTTPStatus == nil || *j.HTTPStatus != 404 {
		t.Errorf("job http_status = %v, want 404", deref(j.HTTPStatus))
	}
	wantString(t, "provider_issue", j.ProviderIssue, "INVALID_RESOURCE_ID")
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
	j := e.wantJob(key, "failed")
	if j.HTTPStatus == nil || *j.HTTPStatus != 422 {
		t.Errorf("job http_status = %v, want 422", deref(j.HTTPStatus))
	}
	wantString(t, "provider_issue", j.ProviderIssue, "AUTHORIZATION_EXPIRED")
	if j.DebugID == nil || *j.DebugID == "" {
		t.Errorf("job debug_id = %v, want PayPal's debug_id", deref(j.DebugID))
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

	e.runKey(key)

	if n := len(e.fake.Requests("capture")); n != 0 {
		t.Fatalf("capture requests = %d, want 0 past the deadline", n)
	}
	e.wantStatus(en.ID, domain.EnrollmentCaptureFailed)
	wantString(t, "capture_issue", e.attempt(a.ID).CaptureIssue, "CAPTURE_DEADLINE_PASSED")
	j := e.wantJob(key, "failed")
	wantString(t, "provider_issue", j.ProviderIssue, "CAPTURE_DEADLINE_PASSED")
	wantString(t, "last_error", j.LastError, "capture deadline passed")
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

func TestCapture_CompletedAnswerRecordsTheHoldCapturedWithoutAReadBack(t *testing.T) {
	e := newEnv(t)
	en, a := e.authorized("Ben")
	wantString(t, "authorization_status before the capture", a.AuthorizationStatus, "CREATED")

	e.runKey(e.enqueue("capture", a))

	if n := len(e.fake.Requests("get_order")) + len(e.fake.Requests("get_authorization")); n != 0 {
		t.Fatalf("read-backs = %d, want none: only the capture's own answer is applied", n)
	}
	e.wantStatus(en.ID, domain.EnrollmentPaid)
	got := e.attempt(a.ID)
	wantString(t, "capture_status", got.CaptureStatus, "COMPLETED")
	wantString(t, "authorization_status", got.AuthorizationStatus, "CAPTURED")
	auth, err := e.api.GetAuthorization(e.ctx, *a.AuthorizationID)
	if err != nil || auth.Status != "CAPTURED" {
		t.Errorf("authorization at PayPal = %+v, %v; want CAPTURED", auth, err)
	}
}

func TestApply_StaleCreatedAuthorizationKeepsACapturedHold(t *testing.T) {
	e := newEnv(t)
	en, a := e.paid("Ben")

	for _, status := range []string{"CREATED", "PENDING"} {
		if err := e.runner.ApplyAuthorization(e.ctx, a.ID, paypal.Authorization{ID: *a.AuthorizationID, Status: status}); err != nil {
			t.Fatal(err)
		}
		wantString(t, "authorization_status after a stale "+status, e.attempt(a.ID).AuthorizationStatus, "CAPTURED")
	}
	e.wantStatus(en.ID, domain.EnrollmentPaid)
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

func TestCapture_ResumedPastDeadlineRefreshesBeforeFailing(t *testing.T) {
	e := newEnv(t)
	en, a := e.authorized("Ana")
	key := "capture:" + a.ID
	captured, err := e.api.CaptureAuthorization(e.ctx, paypal.CaptureParams{AuthorizationID: *a.AuthorizationID, RequestID: key})
	if err != nil {
		t.Fatalf("capture at PayPal: %v", err)
	}
	e.fake.FailNext("capture", 429, "RATE_LIMIT_REACHED")
	e.enqueue("capture", a)
	e.runOnce()
	e.wantStatus(en.ID, domain.EnrollmentCapturing)

	e.clock.Set(e.proposal.ClosesAt.Add(time.Minute))
	e.runOnce()

	e.wantStatus(en.ID, domain.EnrollmentPaid)
	wantString(t, "capture_id", e.attempt(a.ID).CaptureID, captured.ID)
	e.wantJob(key, "succeeded")
	if j, err := e.st.JobByKey(e.ctx, "void:"+a.ID); err != nil || j != nil {
		t.Errorf("void job = %+v, %v; want none for a captured authorization", j, err)
	}
	if n := len(e.fake.Requests("capture")); n != 2 {
		t.Errorf("capture requests = %d, want 2 (none sent past the deadline)", n)
	}
}

func TestCapture_FirstRunDoesNotResumeAnotherCapture(t *testing.T) {
	e := newEnv(t)
	en, older := e.enroll("Ana")
	latest, err := e.st.CreateAttempt(e.ctx, en.ID, e.proposal.PriceMinor, e.proposal.Currency)
	if err != nil {
		t.Fatal(err)
	}
	e.runKey(e.enqueue("create_order", latest))
	latest = e.attempt(latest.ID)
	e.fake.Approve(*latest.OrderID)
	e.runKey(e.enqueue("authorize", latest))
	e.wantStatus(en.ID, domain.EnrollmentAuthorized)
	e.transition(en.ID, domain.EnrollmentAuthorized, domain.EnrollmentCapturing)
	key := e.enqueue("capture", older)

	e.runOnce()

	j := e.wantJob(key, "failed")
	wantString(t, "last_error", j.LastError, "skipped: not the enrollment's latest attempt")
	if n := len(e.fake.Requests("capture")); n != 0 {
		t.Errorf("capture requests = %d, want 0", n)
	}
	e.wantStatus(en.ID, domain.EnrollmentCapturing)
}

func TestCapture_RevivedSkippedJobResumesUnderTheSameKey(t *testing.T) {
	e := newEnv(t)
	en, a := e.withOrder("Ana")
	key := e.enqueue("capture", a)
	e.runKey(key)
	j := e.wantJob(key, "failed")
	wantString(t, "last_error", j.LastError, "skipped: enrollment SEAT_RESERVED")

	e.fake.Approve(*a.OrderID)
	e.runKey(e.enqueue("authorize", a))
	created, err := e.runner.EnqueueOrRevive(e.ctx, "capture", key, &e.workshop.ID, &a.ID, nil, e.clock.Now())
	if err != nil || created {
		t.Fatalf("EnqueueOrRevive = %v, %v; want the skipped job revived (false, nil)", created, err)
	}
	e.fake.FailNext("capture", 429, "RATE_LIMIT_REACHED")
	e.runOnce()
	e.wantStatus(en.ID, domain.EnrollmentCapturing)
	e.clock.Set(e.wantJob(key, "pending").RunAt)
	e.runOnce()

	e.wantStatus(en.ID, domain.EnrollmentPaid)
	e.wantJob(key, "succeeded")
	if e.fake.Captures() != 1 {
		t.Errorf("captures = %d, want 1", e.fake.Captures())
	}
	reqs := e.fake.Requests("capture")
	if len(reqs) != 2 {
		t.Fatalf("capture requests = %d, want 2 (the rate-limited one and the capture)", len(reqs))
	}
	for i, r := range reqs {
		if r.RequestID != key {
			t.Errorf("capture request %d PayPal-Request-Id = %q, want %q", i+1, r.RequestID, key)
		}
	}
}

func TestRefund_OlderAttemptIsSkipped(t *testing.T) {
	e := newEnv(t)
	en, older := e.enroll("Ana")
	latest, err := e.st.CreateAttempt(e.ctx, en.ID, e.proposal.PriceMinor, e.proposal.Currency)
	if err != nil {
		t.Fatal(err)
	}
	e.runKey(e.enqueue("create_order", latest))
	latest = e.attempt(latest.ID)
	e.fake.Approve(*latest.OrderID)
	e.runKey(e.enqueue("authorize", latest))
	e.runKey(e.enqueue("capture", latest))
	e.wantStatus(en.ID, domain.EnrollmentPaid)
	key := e.enqueue("refund", older)

	e.runKey(key)

	j := e.wantJob(key, "failed")
	wantString(t, "last_error", j.LastError, "skipped: not the enrollment's latest attempt")
	e.wantStatus(en.ID, domain.EnrollmentPaid)
	if n := len(e.fake.Requests("refund")); n != 0 {
		t.Errorf("refund requests = %d, want 0", n)
	}
}

func TestAuthorize_OrderNotApprovedFailsJobAndCanBeRevived(t *testing.T) {
	e := newEnv(t)
	en, a := e.withOrder("Ana")
	key := e.enqueue("authorize", a)

	e.runKey(key)

	j := e.wantJob(key, "failed")
	wantString(t, "provider_issue", j.ProviderIssue, "ORDER_NOT_APPROVED")
	if j.HTTPStatus == nil || *j.HTTPStatus != 422 {
		t.Errorf("job http_status = %v, want 422", deref(j.HTTPStatus))
	}
	e.wantStatus(en.ID, domain.EnrollmentSeatReserved)

	e.fake.Approve(*a.OrderID)
	if created, err := e.runner.EnqueueOrRevive(e.ctx, "authorize", key, &e.workshop.ID, &a.ID, nil, e.clock.Now()); err != nil || created {
		t.Fatalf("EnqueueOrRevive = %v, %v; want false, nil", created, err)
	}
	e.wantJob(key, "failed")
	revived, err := e.runner.Revive(e.ctx, key)
	if err != nil || !revived {
		t.Fatalf("Revive = %v, %v; want the refused job revived", revived, err)
	}
	e.wantJob(key, "pending")
	e.runKey(key)

	e.wantStatus(en.ID, domain.EnrollmentAuthorized)
	reqs := e.fake.Requests("authorize")
	if len(reqs) != 2 || reqs[0].RequestID != key || reqs[1].RequestID != key {
		t.Fatalf("authorize requests = %+v, want two with PayPal-Request-Id %s", reqs, key)
	}
}

func TestVoid_SkippedWhileCapturingIsRevivedAfterCaptureFailure(t *testing.T) {
	e := newEnv(t)
	en, a := e.authorized("Ana")
	captureKey := e.enqueue("capture", a)
	e.fake.FailNext("capture", 429, "RATE_LIMIT_REACHED")
	e.runKey(captureKey)
	e.wantStatus(en.ID, domain.EnrollmentCapturing)
	voidKey := e.enqueue("void", a)
	e.runKey(voidKey)
	j := e.wantJob(voidKey, "failed")
	wantString(t, "last_error", j.LastError, "skipped: enrollment CAPTURING")

	e.advance(5 * time.Second)
	e.fake.FailNext("capture", 422, "TRANSACTION_REFUSED")
	e.runKey(captureKey)
	e.wantStatus(en.ID, domain.EnrollmentCaptureFailed)
	e.wantJob(voidKey, "pending")
	e.runOnce()

	e.wantJob(voidKey, "succeeded")
	if reqs := e.fake.Requests("void"); len(reqs) != 1 || reqs[0].RequestID != voidKey {
		t.Fatalf("void requests = %+v, want one with PayPal-Request-Id %s", reqs, voidKey)
	}
	auth, err := e.api.GetAuthorization(e.ctx, *a.AuthorizationID)
	if err != nil || auth.Status != "VOIDED" {
		t.Errorf("authorization at PayPal = %+v, %v; want VOIDED", auth, err)
	}
	e.wantStatus(en.ID, domain.EnrollmentCaptureFailed)
}

func TestRefund_SkippedOnNonPaidEnrollmentIsRevivable(t *testing.T) {
	e := newEnv(t)
	en, a := e.authorized("Ana")
	refundKey := e.enqueue("refund", a)
	e.runKey(refundKey)
	j := e.wantJob(refundKey, "failed")
	wantString(t, "last_error", j.LastError, "skipped: enrollment AUTHORIZED")

	e.runKey(e.enqueue("capture", a))
	e.wantStatus(en.ID, domain.EnrollmentPaid)
	created, err := e.runner.EnqueueOrRevive(e.ctx, "refund", refundKey, &e.workshop.ID, &a.ID, nil, e.clock.Now())
	if err != nil || created {
		t.Fatalf("EnqueueOrRevive = %v, %v; want the existing job revived (false, nil)", created, err)
	}
	e.runKey(refundKey)

	e.wantStatus(en.ID, domain.EnrollmentRefunded)
	e.wantJob(refundKey, "succeeded")
	if n := len(e.fake.Requests("refund")); n != 1 {
		t.Errorf("refund requests = %d, want 1", n)
	}
}

func TestAuthorize_SecondAuthorizationOnAnotherAttemptIsVoided(t *testing.T) {
	e := newEnv(t)
	en, first := e.withOrder("Ana")
	second, err := e.st.CreateAttempt(e.ctx, en.ID, e.proposal.PriceMinor, e.proposal.Currency)
	if err != nil {
		t.Fatal(err)
	}
	e.runKey(e.enqueue("create_order", second))
	second = e.attempt(second.ID)
	e.fake.Approve(*first.OrderID)
	e.fake.Approve(*second.OrderID)
	e.runKey(e.enqueue("authorize", second))
	e.wantStatus(en.ID, domain.EnrollmentAuthorized)
	second = e.attempt(second.ID)

	if _, err := e.api.AuthorizeOrder(e.ctx, *first.OrderID, "authorize:"+first.ID); err != nil {
		t.Fatalf("authorize the first checkout at PayPal: %v", err)
	}
	key := "reconcile:" + first.ID + ":1"
	e.enqueueKey("reconcile", key, first, e.clock.Now())
	e.runKey(key)

	got := e.attempt(first.ID)
	wantString(t, "first attempt authorization_status", got.AuthorizationStatus, "CREATED")
	e.wantStatus(en.ID, domain.EnrollmentAuthorized)
	e.wantJob("void:"+first.ID, "pending")
	e.runOnce()

	e.wantJob("void:"+first.ID, "succeeded")
	wantString(t, "first attempt authorization_status", e.attempt(first.ID).AuthorizationStatus, "VOIDED")
	e.wantStatus(en.ID, domain.EnrollmentAuthorized)
	if auth, err := e.api.GetAuthorization(e.ctx, *got.AuthorizationID); err != nil || auth.Status != "VOIDED" {
		t.Errorf("first authorization at PayPal = %+v, %v; want VOIDED", auth, err)
	}
	if auth, err := e.api.GetAuthorization(e.ctx, *second.AuthorizationID); err != nil || auth.Status != "CREATED" {
		t.Errorf("second authorization at PayPal = %+v, %v; want it untouched (CREATED)", auth, err)
	}
}

func TestAuthorize_AuthorizationOnReleasedSeatIsVoided(t *testing.T) {
	e := newEnv(t)
	en, a := e.withOrder("Ana")
	e.fake.Approve(*a.OrderID)
	e.transition(en.ID, domain.EnrollmentSeatReserved, domain.EnrollmentReleased)
	if _, err := e.api.AuthorizeOrder(e.ctx, *a.OrderID, "authorize:"+a.ID); err != nil {
		t.Fatalf("authorize at PayPal: %v", err)
	}
	key := "reconcile:" + a.ID + ":1"
	e.enqueueKey("reconcile", key, a, e.clock.Now())
	e.runKey(key)

	e.wantStatus(en.ID, domain.EnrollmentReleased)
	e.wantJob("void:"+a.ID, "pending")
	e.runOnce()

	e.wantJob("void:"+a.ID, "succeeded")
	got := e.attempt(a.ID)
	wantString(t, "authorization_status", got.AuthorizationStatus, "VOIDED")
	if auth, err := e.api.GetAuthorization(e.ctx, *got.AuthorizationID); err != nil || auth.Status != "VOIDED" {
		t.Errorf("authorization at PayPal = %+v, %v; want VOIDED", auth, err)
	}
	e.wantStatus(en.ID, domain.EnrollmentReleased)
}

func TestApplyCapture_DeclinedEnqueuesVoid(t *testing.T) {
	e := newEnv(t)
	en, a := e.authorized("Ana")
	e.transition(en.ID, domain.EnrollmentAuthorized, domain.EnrollmentCapturing)

	if err := e.runner.ApplyCapture(e.ctx, a.ID, paypal.Capture{ID: "DECLINEDCAPTURE", Status: "DECLINED"}); err != nil {
		t.Fatalf("ApplyCapture: %v", err)
	}

	e.wantStatus(en.ID, domain.EnrollmentCaptureFailed)
	e.wantJob("void:"+a.ID, "pending")
	e.runOnce()
	if auth, err := e.api.GetAuthorization(e.ctx, *a.AuthorizationID); err != nil || auth.Status != "VOIDED" {
		t.Errorf("authorization at PayPal = %+v, %v; want VOIDED", auth, err)
	}
	e.wantStatus(en.ID, domain.EnrollmentCaptureFailed)
}

func TestCreateOrder_DifferentOrderIsFlaggedWithoutApproveURL(t *testing.T) {
	e := newEnv(t)
	en, a := e.enroll("Ana")
	if _, err := e.st.UpdateAttemptFunc(e.ctx, a.ID, func(row *store.AttemptRow) error {
		other := "OTHERORDER"
		row.OrderID = &other
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	key := e.enqueue("create_order", a)

	e.runKey(key)

	j := e.wantJob(key, "failed")
	if strings.Contains(string(j.Payload), "approve_url") {
		t.Errorf("job payload = %s, want no approve_url for a conflicting order", j.Payload)
	}
	wantString(t, "order_id", e.attempt(a.ID).OrderID, "OTHERORDER")
	e.wantFlagged(en.ID, domain.EnrollmentSeatReserved)
}

func TestCreateOrder_UnknownWithoutOrderIsResentUnderTheSameKey(t *testing.T) {
	e := newEnv(t)
	_, a := e.enroll("Ana")
	e.fake.FailNext("create_order", 503, "")
	key := e.enqueue("create_order", a)
	e.runKey(key)
	e.wantJob(key, "unknown")

	e.advance(31 * time.Second)
	e.runKey("reconcile:" + a.ID + ":1")
	e.wantJob(key, "pending")
	if n := len(e.fake.Requests("get_order")); n != 0 {
		t.Errorf("get_order requests = %d, want 0 without an order id", n)
	}
	e.runOnce()

	j := e.wantJob(key, "succeeded")
	if !strings.Contains(string(j.Payload), "approve_url") {
		t.Errorf("job payload = %s, want the approve URL", j.Payload)
	}
	reqs := e.fake.Requests("create_order")
	if len(reqs) != 2 || reqs[0].RequestID != key || reqs[1].RequestID != key {
		t.Fatalf("create_order requests = %+v, want two with PayPal-Request-Id %s", reqs, key)
	}
	if e.attempt(a.ID).OrderID == nil {
		t.Errorf("order_id = nil, want the order")
	}
}

func TestCapture_401IsRetriedUnderTheSameKey(t *testing.T) {
	e := newEnv(t)
	en, a := e.authorized("Ana")
	e.fake.FailNext("capture", 401, "")
	e.fake.FailNext("capture", 401, "")
	key := e.enqueue("capture", a)

	e.runOnce()

	j := e.wantJob(key, "pending")
	if j.HTTPStatus == nil || *j.HTTPStatus != 401 {
		t.Errorf("job http_status = %v, want 401", deref(j.HTTPStatus))
	}
	e.wantStatus(en.ID, domain.EnrollmentCapturing)
	if issue := e.attempt(a.ID).CaptureIssue; issue != nil {
		t.Errorf("capture_issue = %q, want none: a rejected credential is not a payment refusal", *issue)
	}
	e.advance(5 * time.Second)
	e.runOnce()
	e.wantStatus(en.ID, domain.EnrollmentPaid)
	for i, r := range e.fake.Requests("capture") {
		if r.RequestID != key {
			t.Errorf("capture request %d PayPal-Request-Id = %q, want %q", i+1, r.RequestID, key)
		}
	}
}

func TestRefund_CompletedMarksRefunded(t *testing.T) {
	e := newEnv(t)
	en, a := e.paid("Ana")
	key := e.enqueue("refund", a)

	e.runKey(key)

	if reqs := e.fake.Requests("refund"); len(reqs) != 1 || reqs[0].RequestID != key {
		t.Fatalf("refund requests = %+v, want one with PayPal-Request-Id %s", reqs, key)
	}
	got := e.attempt(a.ID)
	if got.RefundID == nil {
		t.Errorf("refund_id = nil, want the refund")
	}
	wantString(t, "refund_status", got.RefundStatus, "COMPLETED")
	e.wantStatus(en.ID, domain.EnrollmentRefunded)
	e.wantJob(key, "succeeded")
}

func TestRefund_TheRefundJobAloneRecordsTheChargeRefunded(t *testing.T) {
	e := newEnv(t)
	en, a := e.paid("Ana")
	readBacks := len(e.fake.Requests("get_order")) + len(e.fake.Requests("get_capture"))

	e.runKey(e.enqueue("refund", a))

	if n := len(e.fake.Requests("get_order")) + len(e.fake.Requests("get_capture")); n != readBacks {
		t.Fatalf("read-backs = %d, want none after the refund job", n-readBacks)
	}
	got := e.attempt(a.ID)
	wantString(t, "refund_status", got.RefundStatus, "COMPLETED")
	wantString(t, "capture_status", got.CaptureStatus, "REFUNDED")
	e.wantStatus(en.ID, domain.EnrollmentRefunded)
	e.wantFees(a, 84, 916)
}

func TestRefund_OnlyAFullRefundMarksTheChargeAndItStaysMarked(t *testing.T) {
	e := newEnv(t)
	_, part := e.paid("Ana")
	_, unsized := e.paid("Ben")
	_, full := e.paid("Cleo")
	refunds := map[string]paypal.Refund{
		part.ID:    {ID: "REFUND-PART", Status: paypal.StatusCompleted, Amount: *usd("4.00")},
		unsized.ID: {ID: "REFUND-UNSIZED", Status: paypal.StatusCompleted},
		full.ID:    {ID: "REFUND-FULL", Status: paypal.StatusCompleted, Amount: *usd("10.00")},
	}
	for id, rf := range refunds {
		if err := e.runner.ApplyRefund(e.ctx, id, rf); err != nil {
			t.Fatal(err)
		}
	}
	wantString(t, "capture_status after a smaller refund", e.attempt(part.ID).CaptureStatus, "COMPLETED")
	wantString(t, "capture_status after a refund without an amount", e.attempt(unsized.ID).CaptureStatus, "COMPLETED")
	wantString(t, "capture_status after a full refund", e.attempt(full.ID).CaptureStatus, "REFUNDED")

	if err := e.runner.ApplyCapture(e.ctx, full.ID, paypal.Capture{ID: *full.CaptureID, Status: paypal.StatusCompleted}); err != nil {
		t.Fatal(err)
	}
	wantString(t, "capture_status after a stale completed capture", e.attempt(full.ID).CaptureStatus, "REFUNDED")
	e.wantFees(full, 84, 916)
}

func TestRefund_CaptureFullyRefundedCountsAsRefunded(t *testing.T) {
	e := newEnv(t)
	en, a := e.paid("Ana")
	e.fake.FailNext("refund", 422, "CAPTURE_FULLY_REFUNDED")
	key := e.enqueue("refund", a)

	e.runKey(key)

	got := e.attempt(a.ID)
	wantString(t, "refund_status", got.RefundStatus, "COMPLETED")
	wantString(t, "capture_status", got.CaptureStatus, "REFUNDED")
	e.wantStatus(en.ID, domain.EnrollmentRefunded)
	if j := e.wantJob(key, "succeeded"); j.HTTPStatus == nil || *j.HTTPStatus != 422 {
		t.Errorf("job http_status = %v, want 422", deref(j.HTTPStatus))
	}
}

func TestVoid_AlreadyGoneCountsAsVoidedOtherRefusalsFlag(t *testing.T) {
	e := newEnv(t)
	cases := []struct {
		issue string
		want  domain.EnrollmentStatus
		job   string
	}{
		{"PREVIOUSLY_VOIDED", domain.EnrollmentVoided, "succeeded"},
		{"AUTHORIZATION_EXPIRED", domain.EnrollmentVoided, "succeeded"},
		{"PREVIOUSLY_CAPTURED", domain.EnrollmentVoiding, "failed"},
	}
	for i, tc := range cases {
		en, a := e.authorized("Participant " + string(rune('A'+i)))
		e.fake.FailNext("void", 422, tc.issue)
		key := e.enqueue("void", a)

		e.runKey(key)

		got, err := e.st.EnrollmentByID(e.ctx, en.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != tc.want {
			t.Errorf("%s: enrollment = %s, want %s", tc.issue, got.Status, tc.want)
		}
		j := e.job(key)
		if j.Status != tc.job {
			t.Errorf("%s: job = %s, want %s", tc.issue, j.Status, tc.job)
		}
		if tc.job == "failed" {
			e.wantFlagged(en.ID, tc.want)
			wantString(t, tc.issue+" provider_issue", j.ProviderIssue, tc.issue)
			wantString(t, tc.issue+" authorization_status", e.attempt(a.ID).AuthorizationStatus, "CREATED")
		} else {
			wantString(t, tc.issue+" authorization_status", e.attempt(a.ID).AuthorizationStatus, "VOIDED")
		}
	}
}

func TestCreateOrder_OlderAttemptRevivedIsNotSent(t *testing.T) {
	e := newEnv(t)
	en, first := e.enroll("Ana")
	e.fake.FailNext("create_order", 422, "INVALID_PARAMETER_VALUE")
	firstKey := e.enqueue("create_order", first)
	e.runKey(firstKey)
	e.wantJob(firstKey, "failed")

	second, created, err := e.st.CheckoutAttempt(e.ctx, en.ID, 1000, "USD")
	if err != nil || !created || second.AttemptNo != 2 {
		t.Fatalf("CheckoutAttempt after the refusal = %+v, %v, %v; want a new attempt 2", second, created, err)
	}
	e.runKey(e.enqueue("create_order", second))

	if revived, err := e.runner.Revive(e.ctx, firstKey); err != nil || !revived {
		t.Fatalf("Revive(%s) = %v, %v", firstKey, revived, err)
	}
	e.runKey(firstKey)
	j := e.wantJob(firstKey, "failed")
	if j.LastError == nil || !strings.Contains(*j.LastError, "latest attempt") {
		t.Errorf("revived job last_error = %v, want the older-attempt skip", deref(j.LastError))
	}
	if n := e.requestsWithID("create_order", firstKey); n != 1 {
		t.Errorf("create_order requests under %s = %d, want only the refused one", firstKey, n)
	}
	if a := e.attempt(first.ID); a.OrderID != nil {
		t.Errorf("older attempt got order %s, want none: the enrollment follows attempt 2", *a.OrderID)
	}
}

func (e *env) pendingAuthorization(en store.EnrollmentRow, a store.AttemptRow) string {
	e.t.Helper()
	e.fake.Approve(*a.OrderID)
	e.fake.SetNextAuthorizationStatus("PENDING")
	key := e.enqueue("authorize", a)
	e.runKey(key)
	e.wantJob(key, "succeeded")
	got := e.attempt(a.ID)
	if got.AuthorizationID == nil {
		e.t.Fatalf("attempt authorization_id = nil, want the pending authorization")
	}
	wantString(e.t, "authorization_status", got.AuthorizationStatus, "PENDING")
	e.wantStatus(en.ID, domain.EnrollmentSeatReserved)

	e.advance(31 * time.Minute)
	e.runKey(e.releaseSeatsNow())
	e.wantStatus(en.ID, domain.EnrollmentSeatReserved)

	e.runPeriodic()
	e.wantStatus(en.ID, domain.EnrollmentSeatReserved)
	e.wantJob(e.pendingKey(a), "pending")
	e.wantNoJob("void:" + a.ID)
	return *got.AuthorizationID
}

func TestAuthorize_PendingAuthorizationKeepsTheSeatAndIsReconciled(t *testing.T) {
	e := newEnv(t)
	en, a := e.withOrder("Ana")
	authID := e.pendingAuthorization(en, a)

	e.fake.SetAuthorizationStatus(authID, "CREATED")
	e.runKey(e.pendingKey(a))

	e.wantStatus(en.ID, domain.EnrollmentAuthorized)
	wantString(t, "authorization_status", e.attempt(a.ID).AuthorizationStatus, "CREATED")
	e.wantNoJob("void:" + a.ID)
	if n := len(e.fake.Requests("authorize")); n != 1 {
		t.Errorf("authorize requests = %d, want 1", n)
	}

	e.clock.Set(e.clock.Now().Truncate(time.Hour).Add(time.Hour))
	e.runPeriodic()
	e.wantNoJob(e.pendingKey(a))
}

func TestAuthorize_PendingAuthorizationDeniedFreesTheSeat(t *testing.T) {
	e := newEnv(t)
	en, a := e.withOrder("Ana")
	authID := e.pendingAuthorization(en, a)

	e.fake.SetAuthorizationStatus(authID, "DENIED")
	e.runKey(e.pendingKey(a))

	e.wantStatus(en.ID, domain.EnrollmentCaptureFailed)
	wantString(t, "authorization_status", e.attempt(a.ID).AuthorizationStatus, "DENIED")
	e.wantNoJob("void:" + a.ID)
	e.wantNoJob("capture:" + a.ID)

	e.clock.Set(e.clock.Now().Truncate(time.Hour).Add(time.Hour))
	e.runPeriodic()
	e.wantNoJob(e.pendingKey(a))
}

func TestApproveURL_ReadsTheCreateOrderPayload(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    string
	}{
		{"order created", `{"approve_url":"https://www.sandbox.paypal.com/checkoutnow?token=O1","order_id":"O1"}`, "https://www.sandbox.paypal.com/checkoutnow?token=O1"},
		{"no payload yet", ``, ""},
		{"not json", `{`, ""},
	}
	for _, c := range cases {
		if got := worker.ApproveURL(store.Job{Payload: json.RawMessage(c.payload)}); got != c.want {
			t.Errorf("%s: ApproveURL = %q, want %q", c.name, got, c.want)
		}
	}
}
