package worker_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/paypal"
	"github.com/aalvaropc/commitpool/internal/store"
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
