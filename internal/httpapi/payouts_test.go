package httpapi_test

import (
	"bytes"
	"net/http"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/store"
)

const payoutEmail = "iris@example.com"

func withPayouts() option { return func(s *setup) { s.cfg.Payouts = true } }

func withDemoPayoutEmail(email string) option {
	return func(s *setup) { s.cfg.DemoPayoutEmail = email }
}

func withPayoutEmail(email string) func(map[string]any) {
	return func(b map[string]any) { b["payout_email"] = email }
}

type payoutJSON struct {
	State         string     `json:"state"`
	Locked        bool       `json:"locked"`
	Receiver      string     `json:"receiver"`
	ScheduledAt   *time.Time `json:"scheduled_at"`
	Waiting       string     `json:"waiting"`
	Estimated     bool       `json:"estimated"`
	Amount        *moneyJSON `json:"amount"`
	Gross         *moneyJSON `json:"gross"`
	Fees          *moneyJSON `json:"fees"`
	Seats         int        `json:"seats"`
	Early         bool       `json:"early"`
	SenderBatchID string     `json:"sender_batch_id"`
	PayoutBatchID string     `json:"payout_batch_id"`
	PayoutItemID  string     `json:"payout_item_id"`
	TransactionID string     `json:"transaction_id"`
	ItemStatus    string     `json:"item_status"`
	Issue         string     `json:"issue"`
	DebugID       string     `json:"debug_id"`
}

type ownerPayoutJSON struct {
	Workshop struct {
		Status      string `json:"status"`
		PayoutEmail string `json:"payout_email"`
	} `json:"workshop"`
	Payout *payoutJSON `json:"payout"`
}

func (e *env) ownerPayout(h workshopHandle) ownerPayoutJSON {
	e.t.Helper()
	var v ownerPayoutJSON
	e.call(http.MethodGet, h.ownerPath, nil, http.StatusOK, &v)
	return v
}

func (e *env) confirmedWorkshop(edit ...func(map[string]any)) (workshopHandle, proposalJSON) {
	e.t.Helper()
	h, p, ps := e.fundingWorkshop(edit...)
	for _, pt := range ps {
		e.paidSeat(h, pt, p.ID)
	}
	if v := e.ownerPayout(h); v.Workshop.Status != "CONFIRMED" {
		e.t.Fatalf("workshop after two payments = %s, want CONFIRMED", v.Workshop.Status)
	}
	return h, p
}

func (e *env) startClass(h workshopHandle) payoutJSON {
	e.t.Helper()
	var out struct {
		Payout payoutJSON `json:"payout"`
	}
	e.call(http.MethodPost, h.ownerPath+"/demo/start-class", nil, http.StatusAccepted, &out)
	return out.Payout
}

func usd(minor int64) *moneyJSON {
	return &moneyJSON{Minor: minor, Currency: "USD", Value: map[int64]string{1832: "18.32", 2000: "20.00", 168: "1.68"}[minor]}
}

func sameMoney(got, want *moneyJSON) bool {
	return (got == nil) == (want == nil) && (got == nil || *got == *want)
}

func TestPayoutEmail_StoredAtCreationAndShownOnlyToTheOrganizer(t *testing.T) {
	e := newEnv(t)
	h := e.createWorkshop(withPayoutEmail(" " + payoutEmail + " "))
	if v := e.ownerPayout(h); v.Workshop.PayoutEmail != payoutEmail {
		t.Errorf("owner view payout_email = %q, want %q", v.Workshop.PayoutEmail, payoutEmail)
	}
	rec := e.request(http.MethodGet, h.sharePath, nil)
	if rec.Code != http.StatusOK || bytes.Contains(rec.Body.Bytes(), []byte(payoutEmail)) {
		t.Errorf("public view = %d and names the payout email: %s", rec.Code, rec.Body.String())
	}
	e.wantError(http.MethodPost, "/api/v1/workshops", withField("payout_email", "iris at example"), http.StatusBadRequest, "invalid_payout_email")
}

func TestPayoutEmail_DemoWorkshopsGetTheDemoReceiver(t *testing.T) {
	e := newEnv(t, withDemoMode(), withDemoPayoutEmail("receiver@example.com"))
	demo := e.createWorkshop(func(b map[string]any) { b["demo"] = true })
	if got := e.ownerPayout(demo).Workshop.PayoutEmail; got != "receiver@example.com" {
		t.Errorf("demo workshop payout_email = %q, want the demo receiver", got)
	}
	plain := e.createWorkshop()
	if got := e.ownerPayout(plain).Workshop.PayoutEmail; got != "" {
		t.Errorf("a workshop that is not a demo got payout_email %q", got)
	}
}

func TestOwnerView_PayoutBlockFollowsTheClass(t *testing.T) {
	e := newEnv(t, withPayouts(), withDemoMode())
	h, p, ps := e.fundingWorkshop(withPayoutEmail(payoutEmail))
	if v := e.ownerPayout(h); v.Payout != nil {
		t.Fatalf("payout while funding = %+v, want none", v.Payout)
	}
	for _, pt := range ps {
		e.paidSeat(h, pt, p.ID)
	}

	got := e.ownerPayout(h).Payout
	if got == nil || got.State != "scheduled" || got.ScheduledAt == nil || !got.ScheduledAt.Equal(satStart) || !got.Estimated ||
		got.Receiver != payoutEmail || got.Seats != 2 || !sameMoney(got.Amount, usd(1832)) || !sameMoney(got.Gross, usd(2000)) || !sameMoney(got.Fees, usd(168)) {
		t.Fatalf("payout before the start = %+v, want scheduled for %s with an estimate of 18.32", got, satStart)
	}

	e.clock.Set(satStart.Add(time.Minute))
	if err := e.runner.RunPending(e.ctx); err != nil {
		t.Fatal(err)
	}
	got = e.ownerPayout(h).Payout
	if got == nil || got.State != "paid" || got.Estimated || !sameMoney(got.Amount, usd(1832)) || got.PayoutBatchID == "" ||
		got.PayoutItemID == "" || got.TransactionID == "" || got.SenderBatchID != "cp-"+h.created.Workshop.ID || got.ItemStatus != "SUCCESS" {
		t.Fatalf("payout after the start = %+v, want paid with PayPal's ids", got)
	}
}

func TestOwnerView_PayoutWithoutAnEmailOrSwitchedOff(t *testing.T) {
	e := newEnv(t)
	h, _ := e.confirmedWorkshop()
	if got := e.ownerPayout(h).Payout; got == nil || got.State != "no_email" {
		t.Errorf("payout without an email = %+v, want no_email", got)
	}
	off, _ := e.confirmedWorkshop(withPayoutEmail(payoutEmail))
	if got := e.ownerPayout(off).Payout; got == nil || got.State != "off" || got.Receiver != payoutEmail {
		t.Errorf("payout while payouts are off = %+v, want off", got)
	}
}

func TestOwnerView_PayoutWaitsForAHoldStillInFlight(t *testing.T) {
	e := newEnv(t, withPayouts())
	h, p := e.confirmedWorkshop(withPayoutEmail(payoutEmail))
	third := e.joinAndSay(h, "Mia", "Any day works for me")
	en, _ := e.checkout(third, p.ID)
	e.authorize(h.created.Workshop.ID, en.EnrollmentID)
	e.clock.Set(satStart.Add(time.Minute))

	got := e.ownerPayout(h).Payout
	if got == nil || got.State != "waiting" || got.Waiting != "payments_in_flight" || got.Seats != 2 {
		t.Fatalf("payout with a hold in flight = %+v, want waiting for payments_in_flight", got)
	}
}

func TestDemoStartClass_PaysOutAndTheOrganizerCanNoLongerCancel(t *testing.T) {
	e := newEnv(t, withPayouts(), withDemoMode())
	h, _ := e.confirmedWorkshop(withPayoutEmail(payoutEmail))
	e.call(http.MethodPost, h.ownerPath+"/demo/close-now", nil, http.StatusAccepted, nil)

	got := e.startClass(h)
	if got.State != "paid" || !got.Early || !sameMoney(got.Amount, usd(1832)) || got.Receiver != payoutEmail {
		t.Fatalf("start-class = %+v, want an early payout of 18.32 paid", got)
	}
	if again := e.startClass(h); again.PayoutBatchID != got.PayoutBatchID || e.fake.PayoutBatches() != 1 {
		t.Errorf("a second start-class = %+v with %d batches, want the same single batch", again, e.fake.PayoutBatches())
	}
	e.wantError(http.MethodPost, h.ownerPath+"/cancel", map[string]string{}, http.StatusConflict, "workshop_started")
	if v := e.ownerPayout(h); v.Workshop.Status != "CONFIRMED" || v.Payout == nil || v.Payout.State != "paid" {
		t.Errorf("owner view after a refused cancel = %+v", v)
	}
}

func TestDemoStartClass_OnlyOnADemoServer(t *testing.T) {
	e := newEnv(t, withPayouts())
	h, _ := e.confirmedWorkshop(withPayoutEmail(payoutEmail))
	e.wantError(http.MethodPost, h.ownerPath+"/demo/start-class", nil, http.StatusNotFound, "not_found")
}

func TestDemoStartClass_RefusedUntilItCanPay(t *testing.T) {
	e := newEnv(t, withPayouts(), withDemoMode())
	funding, _, _ := e.fundingWorkshop(withPayoutEmail(payoutEmail))
	if got := e.wantError(http.MethodPost, funding.ownerPath+"/demo/start-class", nil, http.StatusConflict, "payout_not_ready"); got.Reason != "not_confirmed" {
		t.Errorf("start-class while funding: reason %q, want not_confirmed", got.Reason)
	}
	h, _ := e.confirmedWorkshop(withPayoutEmail(payoutEmail))
	if got := e.wantError(http.MethodPost, h.ownerPath+"/demo/start-class", nil, http.StatusConflict, "payout_not_ready"); got.Reason != "payments_open" {
		t.Errorf("start-class while reservations are open: reason %q, want payments_open", got.Reason)
	}
	noEmail, _ := e.confirmedWorkshop()
	e.call(http.MethodPost, noEmail.ownerPath+"/demo/close-now", nil, http.StatusAccepted, nil)
	if got := e.wantError(http.MethodPost, noEmail.ownerPath+"/demo/start-class", nil, http.StatusConflict, "payout_not_ready"); got.Reason != "no_payout_email" {
		t.Errorf("start-class without a payout email: reason %q, want no_payout_email", got.Reason)
	}
	if n := e.fake.PayoutBatches(); n != 0 {
		t.Errorf("refused start-class calls created %d batches", n)
	}
}

func TestDemoStartClass_RefusedWhilePayoutsAreOff(t *testing.T) {
	e := newEnv(t, withDemoMode())
	h, _ := e.confirmedWorkshop(withPayoutEmail(payoutEmail))
	e.call(http.MethodPost, h.ownerPath+"/demo/close-now", nil, http.StatusAccepted, nil)
	if got := e.wantError(http.MethodPost, h.ownerPath+"/demo/start-class", nil, http.StatusConflict, "payout_not_ready"); got.Reason != "payouts_off" {
		t.Errorf("start-class with payouts off: reason %q, want payouts_off", got.Reason)
	}
	if n := e.fake.PayoutBatches(); n != 0 {
		t.Errorf("start-class with payouts off created %d batches", n)
	}
}

func TestOperatorCheckPayout_ReadsTheBatchAgain(t *testing.T) {
	e := newEnv(t, withPayouts(), withDemoMode())
	h, _ := e.confirmedWorkshop(withPayoutEmail(payoutEmail))
	e.wantError(http.MethodPost, h.ownerPath+"/operator/check-payout", nil, http.StatusConflict, "no_payout")
	e.call(http.MethodPost, h.ownerPath+"/demo/close-now", nil, http.StatusAccepted, nil)
	paid := e.startClass(h)

	e.fake.SetPayoutItemStatus(paid.PayoutBatchID, "RETURNED", "RECEIVER_UNREGISTERED")
	var out struct {
		Payout payoutJSON `json:"payout"`
	}
	e.call(http.MethodPost, h.ownerPath+"/operator/check-payout", nil, http.StatusOK, &out)
	if out.Payout.State != "failed" || out.Payout.Issue != "RECEIVER_UNREGISTERED" {
		t.Fatalf("checked payout = %+v, want failed with the item's error", out.Payout)
	}
}

func TestDemoStartClass_RefusedPayoutIsShownFailed(t *testing.T) {
	e := newEnv(t, withPayouts(), withDemoMode())
	h, _ := e.confirmedWorkshop(withPayoutEmail(payoutEmail))
	e.call(http.MethodPost, h.ownerPath+"/demo/close-now", nil, http.StatusAccepted, nil)
	e.fake.FailNext("create_payout", http.StatusUnprocessableEntity, "INSUFFICIENT_FUNDS")

	got := e.startClass(h)
	if got.State != "failed" || got.Issue != "INSUFFICIENT_FUNDS" || got.DebugID == "" || got.PayoutBatchID != "" {
		t.Fatalf("refused payout = %+v, want failed with PayPal's issue and debug id", got)
	}
	var health struct {
		Payouts struct {
			Failed  int `json:"failed"`
			Unknown int `json:"unknown"`
		} `json:"payouts"`
		WorkshopsFlagged int `json:"workshops_flagged"`
	}
	e.call(http.MethodGet, "/healthz", nil, http.StatusOK, &health)
	if health.Payouts.Failed != 1 || health.Payouts.Unknown != 0 || health.WorkshopsFlagged != 0 {
		t.Errorf("healthz = %+v, want one failed payout and no flagged workshop", health)
	}
}

func TestWebhook_PayoutEventsQueueACheck(t *testing.T) {
	e := newEnv(t, withPayouts(), withDemoMode(), withWebhookID())
	h, _ := e.confirmedWorkshop(withPayoutEmail(payoutEmail))
	e.call(http.MethodPost, h.ownerPath+"/demo/close-now", nil, http.StatusAccepted, nil)
	paid := e.startClass(h)
	po, err := e.st.PayoutByWorkshop(e.ctx, h.created.Workshop.ID)
	if err != nil || po == nil {
		t.Fatalf("PayoutByWorkshop = %+v, %v", po, err)
	}

	item := event("WH-PAYOUT-1", "PAYMENT.PAYOUTS-ITEM.SUCCEEDED", "payouts_item", map[string]any{
		"payout_item_id": paid.PayoutItemID, "payout_batch_id": paid.PayoutBatchID, "transaction_status": "SUCCESS",
		"payout_item": map[string]any{"sender_item_id": h.created.Workshop.ID},
	})
	if !e.deliver(item) {
		t.Fatal("payout item event not inserted")
	}
	e.job("payout_check:" + po.ID + ":webhook:WH-PAYOUT-1")

	batch := event("WH-PAYOUT-2", "PAYMENT.PAYOUTSBATCH.SUCCESS", "payouts", map[string]any{
		"batch_header": map[string]any{
			"payout_batch_id":     paid.PayoutBatchID,
			"batch_status":        "SUCCESS",
			"sender_batch_header": map[string]any{"sender_batch_id": paid.SenderBatchID},
		},
	})
	e.deliver(batch)
	e.job("payout_check:" + po.ID + ":webhook:WH-PAYOUT-2")
	if ev := e.storedEvent("WH-PAYOUT-2"); !ev.Processed || !ev.Verified {
		t.Errorf("stored payout event = %+v, want verified and processed", ev)
	}

	stranger := event("WH-PAYOUT-3", "PAYMENT.PAYOUTSBATCH.DENIED", "payouts", map[string]any{
		"batch_header": map[string]any{"payout_batch_id": "SOMEONE-ELSE"},
	})
	e.deliver(stranger)
	if n := e.count(`select count(*) from jobs where kind = $1 and idempotency_key like '%:webhook:WH-PAYOUT-3'`, store.KindPayoutCheck); n != 0 {
		t.Errorf("an unknown batch queued %d checks", n)
	}
}

func TestApproval_ShowsWhereThePayoutGoes(t *testing.T) {
	e := newEnv(t)
	h := e.createWorkshop(withPayoutEmail(payoutEmail))
	e.joinAndSay(h, "Ana", "Saturday works if we finish before six")
	e.joinAndSay(h, "Luis", "Any day works for me")
	var out struct {
		Approval struct {
			PayoutEmail string `json:"payout_email"`
		} `json:"approval"`
	}
	e.call(http.MethodPost, h.ownerPath+"/approvals", map[string]string{"date_id": h.satID}, http.StatusCreated, &out)
	if out.Approval.PayoutEmail != payoutEmail {
		t.Errorf("approval payout_email = %q, want %q", out.Approval.PayoutEmail, payoutEmail)
	}
}

func TestOwnerView_AFrozenPayoutPayPalHasNotConfirmedIsCreatedThenParked(t *testing.T) {
	e := newEnv(t, withPayouts(), withDemoMode())
	h, _ := e.confirmedWorkshop(withPayoutEmail(payoutEmail))
	if got := e.ownerPayout(h).Payout; got == nil || got.Locked {
		t.Fatalf("payout before the start = %+v, want unlocked", got)
	}
	e.call(http.MethodPost, h.ownerPath+"/demo/close-now", nil, http.StatusAccepted, nil)
	e.fake.FailNext("create_payout", http.StatusServiceUnavailable, "")

	got := e.startClass(h)
	if got.State != "created" || !got.Locked || got.PayoutBatchID != "" {
		t.Fatalf("payout after a 503 = %+v, want created, locked and unsent", got)
	}
	if _, err := e.st.Pool.Exec(e.ctx, `update jobs set status = 'needs_attention' where idempotency_key = $1`, "payout:"+h.created.Workshop.ID); err != nil {
		t.Fatal(err)
	}
	if got := e.ownerPayout(h).Payout; got == nil || got.State != "parked" || !got.Locked {
		t.Fatalf("payout whose job is parked = %+v, want parked", got)
	}
}

func TestOperatorRetryRefund_RefusesAPaymentThatWasPaidOut(t *testing.T) {
	e := newEnv(t, withPayouts(), withDemoMode())
	h, _ := e.confirmedWorkshop(withPayoutEmail(payoutEmail))
	e.call(http.MethodPost, h.ownerPath+"/demo/close-now", nil, http.StatusAccepted, nil)
	e.startClass(h)
	var enrollmentID string
	if err := e.st.Pool.QueryRow(e.ctx, `select enrollment_id from payment_attempts where payout_id is not null limit 1`).Scan(&enrollmentID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.Pool.Exec(e.ctx, `update enrollments set status = 'REFUND_FAILED', needs_attention_at = now() where id = $1`, enrollmentID); err != nil {
		t.Fatal(err)
	}
	e.wantError(http.MethodPost, h.ownerPath+"/operator/retry-refund", map[string]string{"enrollment_id": enrollmentID}, http.StatusConflict, "paid_out")
	if n := e.fake.Requests("refund"); len(n) != 0 {
		t.Errorf("refund requests = %d, want none", len(n))
	}
}
