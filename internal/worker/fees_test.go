package worker_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/paypal"
	"github.com/aalvaropc/commitpool/internal/store"
	"github.com/aalvaropc/commitpool/internal/worker"
)

func (e *env) wantFees(a store.AttemptRow, fee, net int64) {
	e.t.Helper()
	got := e.attempt(a.ID)
	if got.CaptureFeeMinor == nil || *got.CaptureFeeMinor != fee || got.CaptureNetMinor == nil || *got.CaptureNetMinor != net {
		e.t.Fatalf("fee %v, net %v; want %d and %d", deref(got.CaptureFeeMinor), deref(got.CaptureNetMinor), fee, net)
	}
}

func (e *env) wantNoFees(a store.AttemptRow) {
	e.t.Helper()
	got := e.attempt(a.ID)
	if got.CaptureFeeMinor != nil || got.CaptureNetMinor != nil {
		e.t.Fatalf("fee %v, net %v; want none recorded", deref(got.CaptureFeeMinor), deref(got.CaptureNetMinor))
	}
}

func usd(value string) *paypal.Money { return &paypal.Money{CurrencyCode: "USD", Value: value} }

func TestFees_ACompletedCaptureRecordsPayPalsFeeAndNet(t *testing.T) {
	e := newEnv(t)
	_, a := e.paid("Ana")

	e.wantFees(a, 84, 916)
}

func TestFees_SurviveARefundThatAppliesACaptureWithOnlyAStatus(t *testing.T) {
	e := newEnv(t)
	en, a := e.paid("Ana")
	e.fake.FailNext("refund", 422, "CAPTURE_FULLY_REFUNDED")

	e.runKey(e.enqueue("refund", a))

	e.wantStatus(en.ID, domain.EnrollmentRefunded)
	wantString(t, "capture_status", e.attempt(a.ID).CaptureStatus, "REFUNDED")
	e.wantFees(a, 84, 916)
}

func TestFees_SurviveARefundAndItsReconcile(t *testing.T) {
	e := newEnv(t)
	en, a := e.paid("Ana")

	e.runKey(e.enqueue("refund", a))
	e.runKey(e.reconcileNow(a))

	e.wantStatus(en.ID, domain.EnrollmentRefunded)
	wantString(t, "capture_status", e.attempt(a.ID).CaptureStatus, "REFUNDED")
	e.wantFees(a, 84, 916)
}

func TestFees_ReconcileFillsTheFeesOfACaptureRecordedWithoutThem(t *testing.T) {
	e := newEnv(t)
	_, a := e.paid("Ana")
	if _, err := e.st.Pool.Exec(e.ctx, `update payment_attempts set capture_fee_minor = null, capture_net_minor = null where id = $1`, a.ID); err != nil {
		t.Fatal(err)
	}
	e.wantNoFees(a)

	e.runKey(e.reconcileNow(a))

	e.wantFees(a, 84, 916)
}

func TestFees_RecordedOnceAPendingCaptureCompletes(t *testing.T) {
	e := newEnv(t, withClientTimeout(time.Second))
	en, a := e.authorized("Ana")
	captureID := e.capturePending(en, a)
	e.wantNoFees(a)

	e.fake.SetCaptureStatus(captureID, "COMPLETED", "")
	e.advance(time.Minute)
	e.runKey(e.reconcileNow(a))

	e.wantStatus(en.ID, domain.EnrollmentPaid)
	e.wantFees(a, 84, 916)
}

func TestFees_NeverOverwrittenByALaterBreakdown(t *testing.T) {
	e := newEnv(t)
	_, a := e.paid("Ana")
	e.fake.RespondOnce("get_capture", http.StatusOK, []byte(`{"id":"`+*a.CaptureID+`","status":"COMPLETED",
		"amount":{"currency_code":"USD","value":"10.00"},
		"seller_receivable_breakdown":{"gross_amount":{"currency_code":"USD","value":"10.00"},
		"paypal_fee":{"currency_code":"USD","value":"1.00"},"net_amount":{"currency_code":"USD","value":"9.00"}}}`))

	e.runKey(e.reconcileNow(a))

	e.wantFees(a, 84, 916)
}

func TestFees_RefusedWhenTheBreakdownIsConvertedOrDoesNotAddUp(t *testing.T) {
	cases := map[string]*paypal.SellerReceivableBreakdown{
		"converted":           {GrossAmount: usd("10.00"), PayPalFee: usd("0.84"), NetAmount: usd("9.16"), ReceivableAmount: &paypal.Money{CurrencyCode: "EUR", Value: "8.40"}},
		"net off by a cent":   {GrossAmount: usd("10.00"), PayPalFee: usd("0.84"), NetAmount: usd("9.15")},
		"gross not the price": {GrossAmount: usd("12.00"), PayPalFee: usd("0.91"), NetAmount: usd("11.09")},
		"unreadable fee":      {GrossAmount: usd("10.00"), PayPalFee: usd("0.8.4"), NetAmount: usd("9.16")},
		"no net":              {GrossAmount: usd("10.00"), PayPalFee: usd("0.84")},
	}
	for name, breakdown := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			en, a := e.authorized("Ana")
			capture := paypal.Capture{ID: "CAPTURE" + strings.ToUpper(strings.ReplaceAll(name, " ", "")), Status: "COMPLETED",
				Amount: *usd("10.00"), SellerReceivableBreakdown: breakdown}

			if err := e.runner.ApplyCapture(e.ctx, a.ID, capture); err != nil {
				t.Fatalf("ApplyCapture: %v", err)
			}

			e.wantStatus(en.ID, domain.EnrollmentPaid)
			e.wantNoFees(a)
			if !strings.Contains(e.logs.String(), "fees not recorded") {
				t.Errorf("log does not say the fees were not recorded:\n%s", e.logs.String())
			}

			capture.SellerReceivableBreakdown = &paypal.SellerReceivableBreakdown{GrossAmount: usd("10.00"), NetAmount: usd("10.00")}
			if err := e.runner.ApplyCapture(e.ctx, a.ID, capture); err != nil {
				t.Fatalf("ApplyCapture with a valid breakdown: %v", err)
			}
			e.wantFees(a, 0, 1000)
		})
	}
}

func (e *env) forgetFees(a store.AttemptRow) {
	e.t.Helper()
	if _, err := e.st.Pool.Exec(e.ctx, `update payment_attempts set capture_fee_minor = null, capture_net_minor = null where id = $1`, a.ID); err != nil {
		e.t.Fatal(err)
	}
}

func TestFeeBackfill_RecordsTheFeesOfPaidAndRefundedSeatsAndNothingElse(t *testing.T) {
	e := newEnv(t)
	paidEn, paid := e.paid("Ana")
	refundedEn, refunded := e.paid("Luis")
	e.runKey(e.enqueue("refund", refunded))
	e.wantStatus(refundedEn.ID, domain.EnrollmentRefunded)
	e.forgetFees(paid)
	e.forgetFees(refunded)
	before := map[string]store.AttemptRow{paid.ID: e.attempt(paid.ID), refunded.ID: e.attempt(refunded.ID)}

	e.runPeriodic()

	for _, a := range []store.AttemptRow{paid, refunded} {
		e.wantFees(a, 84, 916)
		got, was := e.attempt(a.ID), before[a.ID]
		if deref(got.CaptureStatus) != deref(was.CaptureStatus) || deref(got.RefundStatus) != deref(was.RefundStatus) ||
			deref(got.AuthorizationStatus) != deref(was.AuthorizationStatus) || deref(got.OrderStatus) != deref(was.OrderStatus) {
			t.Errorf("backfill changed more than the fees: %+v -> %+v", was, got)
		}
	}
	e.wantStatus(paidEn.ID, domain.EnrollmentPaid)
	e.wantStatus(refundedEn.ID, domain.EnrollmentRefunded)
	if n := len(e.fake.Requests("get_capture")); n != 2 {
		t.Errorf("get_capture requests = %d, want one per seat missing fees", n)
	}

	e.runPeriodic()
	if n := len(e.fake.Requests("get_capture")); n != 2 {
		t.Errorf("get_capture requests = %d after a second pass, want no more once the fees are recorded", n)
	}
}

func TestFeeBackfill_LeavesSeatsThatAreNotPaidOrRefundedAlone(t *testing.T) {
	e := newEnv(t)
	e.authorized("Ana")
	en, a := e.paid("Luis")
	e.forgetFees(a)
	e.transition(en.ID, domain.EnrollmentPaid, domain.EnrollmentRefunding)

	e.runPeriodic()

	if n := len(e.fake.Requests("get_capture")); n != 0 {
		t.Errorf("get_capture requests = %d, want none: no paid or refunded seat is missing fees", n)
	}
	e.wantNoFees(a)
}

func TestFeeBackfill_ReadsAtMostABatchEachPass(t *testing.T) {
	worker.SetFeeBackfillBatch(t, 1)
	e := newEnv(t)
	_, first := e.paid("Ana")
	_, second := e.paid("Luis")
	e.forgetFees(first)
	e.forgetFees(second)

	e.runPeriodic()

	if n := len(e.fake.Requests("get_capture")); n != 1 {
		t.Fatalf("get_capture requests = %d after one pass, want the batch of 1", n)
	}
	e.runPeriodic()
	e.wantFees(first, 84, 916)
	e.wantFees(second, 84, 916)
}

func TestFeeBackfill_WaitsAnHourAfterPayPalFailsOrSendsNoBreakdown(t *testing.T) {
	e := newEnv(t)
	_, failing := e.paid("Ana")
	_, bare := e.paid("Luis")
	e.forgetFees(failing)
	e.forgetFees(bare)
	e.fake.RespondOnce("get_capture", http.StatusInternalServerError, []byte(`{"name":"INTERNAL_SERVER_ERROR","debug_id":"f00d"}`))
	e.fake.RespondOnce("get_capture", http.StatusOK, []byte(`{"id":"`+*bare.CaptureID+`","status":"COMPLETED","amount":{"currency_code":"USD","value":"10.00"}}`))

	e.runPeriodic()

	e.wantNoFees(failing)
	e.wantNoFees(bare)
	if !strings.Contains(e.logs.String(), "fee backfill: fees not recorded: PayPal reported the capture without a fee breakdown") {
		t.Errorf("log does not say the capture came without a breakdown:\n%s", e.logs.String())
	}
	e.runPeriodic()
	if n := len(e.fake.Requests("get_capture")); n != 2 {
		t.Errorf("get_capture requests = %d after a second pass within the hour, want still 2", n)
	}

	e.advance(time.Hour)
	e.runPeriodic()
	e.wantFees(failing, 84, 916)
	e.wantFees(bare, 84, 916)
}

func TestFees_WarnWhenACompletedCaptureCarriesNoBreakdown(t *testing.T) {
	e := newEnv(t)
	en, a := e.authorized("Ana")

	if err := e.runner.ApplyCapture(e.ctx, a.ID, paypal.Capture{ID: "BARECAPTURE", Status: "COMPLETED", Amount: *usd("10.00")}); err != nil {
		t.Fatalf("ApplyCapture: %v", err)
	}

	e.wantStatus(en.ID, domain.EnrollmentPaid)
	e.wantNoFees(a)
	logs := e.logs.String()
	if !strings.Contains(logs, "level=WARN") || !strings.Contains(logs, "fees not recorded: PayPal reported the capture without a fee breakdown") {
		t.Errorf("log lacks the warning about a capture without a breakdown:\n%s", logs)
	}
}
