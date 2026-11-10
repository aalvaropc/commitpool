package worker_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/paypal"
	"github.com/aalvaropc/commitpool/internal/store"
	"github.com/aalvaropc/commitpool/internal/worker"
)

const receiver = "iris@example.com"

func payoutEnv(t *testing.T, edit ...func(*paypal.Config)) *env {
	t.Helper()
	e := newEnv(t, edit...)
	worker.EnablePayouts(e.runner)
	e.setWorkshop(`payout_email = $2, owner_email = $3`, receiver, "owner@example.com")
	return e
}

func (e *env) setWorkshop(set string, args ...any) {
	e.t.Helper()
	if _, err := e.st.Pool.Exec(e.ctx, `update workshops set `+set+` where id = $1`, append([]any{e.workshop.ID}, args...)...); err != nil {
		e.t.Fatalf("update workshop: %v", err)
	}
	w, err := e.st.WorkshopByID(e.ctx, e.workshop.ID)
	if err != nil {
		e.t.Fatal(err)
	}
	e.workshop = w
}

func (e *env) payoutKey() string { return "payout:" + e.workshop.ID }

func (e *env) payoutRow() store.PayoutRow {
	e.t.Helper()
	po, err := e.st.PayoutByWorkshop(e.ctx, e.workshop.ID)
	if err != nil || po == nil {
		e.t.Fatalf("PayoutByWorkshop = %+v, %v; want a payout", po, err)
	}
	return *po
}

func (e *env) wantNoPayout() {
	e.t.Helper()
	po, err := e.st.PayoutByWorkshop(e.ctx, e.workshop.ID)
	if err != nil || po != nil {
		e.t.Fatalf("PayoutByWorkshop = %+v, %v; want none", po, err)
	}
	if w, _ := e.st.WorkshopByID(e.ctx, e.workshop.ID); w.PayoutLockedAt != nil {
		e.t.Fatalf("workshop locked for a payout it does not have")
	}
}

func (e *env) toStart() {
	e.t.Helper()
	e.clock.Set(e.proposal.StartsAt.Add(time.Minute))
}

func (e *env) checkKey(po store.PayoutRow, bucket string) string {
	return "payout_check:" + po.ID + ":" + bucket
}

func orEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func TestPayout_ScheduledAtConfirmationForTheStart(t *testing.T) {
	e := payoutEnv(t)
	e.confirmed()

	j := e.wantJob(e.payoutKey(), store.JobPending)
	if !j.RunAt.Equal(e.proposal.StartsAt) {
		t.Errorf("payout job runs at %s, want the class start %s", j.RunAt, e.proposal.StartsAt)
	}
	e.wantIdle()
	e.wantNoPayout()
}

func TestPayout_PaysTheNetAtTheStartAndChecksItOnce(t *testing.T) {
	e := payoutEnv(t)
	e.confirmed()
	e.toStart()
	e.runUntilIdle()

	po := e.payoutRow()
	if po.Status != domain.PayoutSucceeded || po.AmountMinor != 2748 || po.GrossMinor != 3000 || po.FeeMinor != 252 || po.Seats != 3 ||
		po.Receiver != receiver || po.SenderBatchID != "cp-"+e.workshop.ID || po.Early {
		t.Fatalf("payout = %+v, want 27.48 of 30.00 paid out to %s", po, receiver)
	}
	if po.PayoutBatchID == nil || po.PayoutItemID == nil || po.TransactionID == nil || orEmpty(po.ItemStatus) != "SUCCESS" || po.CompletedAt == nil {
		t.Fatalf("payout ids = %+v, want the batch, item and transaction recorded", po)
	}
	reqs := e.fake.Requests("create_payout")
	if len(reqs) != 1 || reqs[0].RequestID != e.payoutKey() {
		t.Fatalf("create_payout requests = %+v, want one under %s", reqs, e.payoutKey())
	}
	var body struct {
		Header struct {
			SenderBatchID string `json:"sender_batch_id"`
		} `json:"sender_batch_header"`
		Items []paypal.PayoutItemRequest `json:"items"`
	}
	if err := json.Unmarshal(reqs[0].Body, &body); err != nil {
		t.Fatal(err)
	}
	want := paypal.PayoutItemRequest{
		RecipientType: "EMAIL", Amount: paypal.PayoutAmount{Value: "27.48", Currency: "USD"},
		Note: "CommitPool payout for Go for operators", Receiver: receiver, SenderItemID: e.workshop.ID,
	}
	if body.Header.SenderBatchID != po.SenderBatchID || len(body.Items) != 1 || body.Items[0] != want {
		t.Errorf("payout body = %+v, want %+v under %s", body, want, po.SenderBatchID)
	}
	e.wantJob(e.payoutKey(), store.JobSucceeded)
	e.wantJob(e.checkKey(po, "created:1"), store.JobSucceeded)
	e.wantNoJob(e.checkKey(po, "created:2"))
	if w, _ := e.st.WorkshopByID(e.ctx, e.workshop.ID); w.PayoutLockedAt == nil {
		t.Errorf("workshop not locked by its payout")
	}
	n, err := e.st.NotificationByKey(e.ctx, "notify:owner_paid_out:"+e.workshop.ID)
	if err != nil || n == nil || n.To != "owner@example.com" || !strings.Contains(n.Body, "$27.48") || !strings.Contains(n.Body, receiver) {
		t.Fatalf("owner_paid_out notice = %+v, %v", n, err)
	}

	if err := e.runner.CancelWorkshop(e.ctx, e.workshop.ID, "too late"); !errors.Is(err, domain.ErrPayoutLocked) {
		t.Errorf("cancel after the payout = %v, want ErrPayoutLocked", err)
	}
	e.wantWorkshop(domain.WorkshopConfirmed, domain.ResolutionOpen)
}

func TestPayout_OneBatchAfterA5xxAndAHang(t *testing.T) {
	e := payoutEnv(t, withClientTimeout(300*time.Millisecond))
	e.confirmed()
	e.toStart()

	e.fake.FailNext("create_payout", http.StatusServiceUnavailable, "")
	e.runKey(e.payoutKey())
	j := e.wantJob(e.payoutKey(), store.JobPending)
	if po := e.payoutRow(); po.Status != domain.PayoutCreated || po.PayoutBatchID != nil {
		t.Fatalf("payout after a 503 = %+v, want created and unsent", po)
	}

	e.fake.FailNext("create_payout", 0, "")
	e.clock.Set(j.RunAt)
	e.runKey(e.payoutKey())
	e.fake.WaitHung()
	if e.fake.PayoutBatches() != 1 {
		t.Fatalf("batches after the hang = %d, want the one PayPal made", e.fake.PayoutBatches())
	}
	j = e.wantJob(e.payoutKey(), store.JobPending)
	if po := e.payoutRow(); po.Status != domain.PayoutCreated {
		t.Fatalf("payout after a hang = %s, want still created", po.Status)
	}

	e.clock.Set(j.RunAt)
	e.runUntilIdle()

	if got := e.requestsWithID("create_payout", e.payoutKey()); got != 3 {
		t.Errorf("create_payout requests under %s = %d, want 3", e.payoutKey(), got)
	}
	batch, _ := e.fake.PayoutBatchFor("cp-" + e.workshop.ID)
	if po := e.payoutRow(); e.fake.PayoutBatches() != 1 || orEmpty(po.PayoutBatchID) != batch || po.Status != domain.PayoutSucceeded {
		t.Fatalf("payout = %+v with %d batches, want the one batch %s paid", po, e.fake.PayoutBatches(), batch)
	}
	for _, r := range e.fake.Requests("create_payout") {
		if !strings.Contains(string(r.Body), `"sender_batch_id":"cp-`+e.workshop.ID+`"`) {
			t.Errorf("a retry used another sender_batch_id: %s", r.Body)
		}
	}
}

func TestPayout_DuplicateRecoversTheBatchItPointsTo(t *testing.T) {
	e := payoutEnv(t, withClientTimeout(300*time.Millisecond))
	e.confirmed()
	e.toStart()
	e.fake.FailNext("create_payout", 0, "")
	e.runKey(e.payoutKey())
	e.fake.WaitHung()
	e.fake.ForgetRequestIDs()

	e.clock.Set(e.job(e.payoutKey()).RunAt)
	e.runUntilIdle()

	batch, _ := e.fake.PayoutBatchFor("cp-" + e.workshop.ID)
	po := e.payoutRow()
	if orEmpty(po.PayoutBatchID) != batch || po.Status != domain.PayoutSucceeded || e.fake.PayoutBatches() != 1 {
		t.Fatalf("payout after a duplicate = %+v, want the existing batch %s recorded and paid", po, batch)
	}
	j := e.wantJob(e.payoutKey(), store.JobSucceeded)
	if j.HTTPStatus == nil || *j.HTTPStatus != http.StatusBadRequest {
		t.Errorf("job http status = %v, want the duplicate's 400", deref(j.HTTPStatus))
	}
}

func TestPayout_DuplicateWithoutALinkIsUnknownAndNeverSentAgain(t *testing.T) {
	e := payoutEnv(t, withClientTimeout(300*time.Millisecond))
	e.confirmed()
	e.toStart()
	e.fake.FailNext("create_payout", 0, "")
	e.runKey(e.payoutKey())
	e.fake.WaitHung()
	e.fake.ForgetRequestIDs()
	e.fake.DuplicatesWithoutLink()

	e.clock.Set(e.job(e.payoutKey()).RunAt)
	e.runUntilIdle()

	po := e.payoutRow()
	if po.Status != domain.PayoutUnknown || orEmpty(po.Issue) != worker.IssueDuplicateBatch || po.DebugID == nil || po.PayoutBatchID != nil {
		t.Fatalf("payout = %+v, want unknown with the duplicate's debug id", po)
	}
	j := e.wantJob(e.payoutKey(), store.JobFailed)
	if j.ProviderIssue == nil {
		t.Errorf("the refused job carries no provider issue, so a sweep could revive it")
	}
	posts := len(e.fake.Requests("create_payout"))
	e.advance(time.Hour)
	if err := e.runner.RunPeriodic(e.ctx); err != nil {
		t.Fatal(err)
	}
	e.runUntilIdle()
	if _, err := e.runner.Revive(e.ctx, e.payoutKey()); err != nil {
		t.Fatal(err)
	}
	e.runUntilIdle()
	if got := len(e.fake.Requests("create_payout")); got != posts {
		t.Errorf("create_payout requests = %d after the sweep and a revive, want still %d", got, posts)
	}
	h, err := e.st.Health(e.ctx)
	if err != nil || h.PayoutsUnknown != 1 {
		t.Errorf("health = %+v, %v; want one unknown payout", h, err)
	}
	e.wantWorkshop(domain.WorkshopConfirmed, domain.ResolutionOpen)
}

func TestPayout_RefusalFailsThePayoutWithoutFlaggingTheWorkshop(t *testing.T) {
	e := payoutEnv(t)
	e.confirmed()
	e.toStart()
	e.fake.FailNext("create_payout", http.StatusUnprocessableEntity, "INSUFFICIENT_FUNDS")
	e.runUntilIdle()

	po := e.payoutRow()
	if po.Status != domain.PayoutFailed || orEmpty(po.Issue) != "INSUFFICIENT_FUNDS" || po.DebugID == nil || po.CompletedAt == nil {
		t.Fatalf("payout = %+v, want failed with PayPal's issue and debug id", po)
	}
	j := e.wantJob(e.payoutKey(), store.JobFailed)
	wantString(t, "provider_issue", j.ProviderIssue, "INSUFFICIENT_FUNDS")
	e.wantWorkshop(domain.WorkshopConfirmed, domain.ResolutionOpen)
	if h, err := e.st.Health(e.ctx); err != nil || h.PayoutsFailed != 1 || h.WorkshopsFlagged != 0 {
		t.Errorf("health = %+v, %v; want one failed payout and no flagged workshop", h, err)
	}
}

func TestPayout_NeverSentPast25Days(t *testing.T) {
	e := payoutEnv(t)
	e.confirmed()
	e.toStart()
	if _, _, err := e.st.BeginPayout(e.ctx, e.workshop.ID, e.clock.Now(), false); err != nil {
		t.Fatal(err)
	}
	e.advance(26 * 24 * time.Hour)
	e.runUntilIdle()

	if n := len(e.fake.Requests("create_payout")); n != 0 {
		t.Fatalf("create_payout requests = %d, want none past 25 days", n)
	}
	po := e.payoutRow()
	if po.Status != domain.PayoutUnknown || orEmpty(po.Issue) != worker.IssuePayoutTooOld {
		t.Fatalf("payout = %+v, want unknown, too old to send", po)
	}
}

func TestPayout_CancelledWorkshopPaysNothing(t *testing.T) {
	e := payoutEnv(t)
	e.confirmed()
	if err := e.runner.CancelWorkshop(e.ctx, e.workshop.ID, "venue flooded"); err != nil {
		t.Fatal(err)
	}
	e.toStart()
	e.runUntilIdle()

	e.wantJob(e.payoutKey(), store.JobSucceeded)
	e.wantNoPayout()
	if n := len(e.fake.Requests("create_payout")); n != 0 {
		t.Errorf("create_payout requests = %d, want none", n)
	}
}

func TestPayout_WaitsForAPaymentInFlightAndTheSweepRevivesIt(t *testing.T) {
	e := payoutEnv(t, withClientTimeout(time.Second))
	e.confirmed()
	dee, d := e.authorized("Dee")
	captureID := e.capturePending(dee, d)
	e.toStart()

	e.runKey(e.payoutKey())
	j := e.wantJob(e.payoutKey(), store.JobFailed)
	if j.ProviderIssue != nil || j.HTTPStatus != nil || !strings.Contains(orEmpty(j.LastError), domain.PayoutInFlight) {
		t.Fatalf("waiting payout job = %+v, want a revivable wait for %s", j, domain.PayoutInFlight)
	}
	e.wantNoPayout()

	e.fake.SetCaptureStatus(captureID, "COMPLETED", "")
	for range 2 {
		if err := e.runner.RunPeriodic(e.ctx); err != nil {
			t.Fatal(err)
		}
		e.runUntilIdle()
	}

	e.wantStatus(dee.ID, domain.EnrollmentRefunded)
	po := e.payoutRow()
	if po.Status != domain.PayoutSucceeded || po.Seats != 3 || po.AmountMinor != 2748 {
		t.Fatalf("payout = %+v, want the three seats kept, without the capture that landed after the start", po)
	}
	if e.fake.PayoutBatches() != 1 {
		t.Errorf("batches = %d, want 1", e.fake.PayoutBatches())
	}
}

func TestPayout_LateCaptureAfterAnEarlyPayoutIsRefunded(t *testing.T) {
	e := payoutEnv(t)
	e.confirmed()
	dee, d := e.authorized("Dee")
	cp, err := e.api.CaptureAuthorization(e.ctx, paypal.CaptureParams{AuthorizationID: *d.AuthorizationID, RequestID: "capture:" + d.ID})
	if err != nil {
		t.Fatal(err)
	}
	e.transition(dee.ID, domain.EnrollmentAuthorized, domain.EnrollmentCaptureFailed)
	e.clock.Set(e.proposal.ClosesAt.Add(time.Minute))

	po, err := e.runner.StartPayoutNow(e.ctx, e.workshop.ID)
	if err != nil || po.Status != domain.PayoutSucceeded || !po.Early || po.Seats != 3 {
		t.Fatalf("StartPayoutNow = %+v, %v; want three seats paid out early", po, err)
	}
	if !e.clock.Now().Before(e.proposal.StartsAt) {
		t.Fatal("the early payout must happen before the class starts")
	}

	e.transition(dee.ID, domain.EnrollmentCaptureFailed, domain.EnrollmentCapturing)
	if err := e.runner.ApplyCapture(e.ctx, d.ID, cp); err != nil {
		t.Fatal(err)
	}
	e.wantJob("refund:"+d.ID, store.JobPending)
	e.runKey("refund:" + d.ID)
	e.wantStatus(dee.ID, domain.EnrollmentRefunded)
	if after := e.payoutRow(); after.Seats != 3 || after.AmountMinor != po.AmountMinor {
		t.Errorf("payout after the late capture = %+v, want it unchanged", after)
	}
	if !strings.Contains(e.logs.String(), "payment completed too late to keep") {
		t.Errorf("the late capture was not logged as refunded")
	}
}

func TestPayout_StartNowRefusesWhilePaymentsAreOpen(t *testing.T) {
	e := payoutEnv(t)
	e.confirmed()
	_, err := e.runner.StartPayoutNow(e.ctx, e.workshop.ID)
	var notReady *store.ErrPayoutNotReady
	if !errors.As(err, &notReady) || notReady.Reason != domain.PayoutPaymentsOpen {
		t.Fatalf("StartPayoutNow while payments are open = %v, want %s", err, domain.PayoutPaymentsOpen)
	}
	e.wantNoPayout()
}

func TestPayout_UnclaimedWaitsThenTheHourlyCheckSeesItPaid(t *testing.T) {
	e := payoutEnv(t)
	e.confirmed()
	e.toStart()
	e.runKey(e.payoutKey())
	po := e.payoutRow()
	if po.Status != domain.PayoutSent {
		t.Fatalf("payout after sending = %s, want sent", po.Status)
	}
	e.fake.SetPayoutItemStatus(*po.PayoutBatchID, "UNCLAIMED", "RECEIVER_UNREGISTERED")
	e.runKey(e.checkKey(po, "created:1"))
	if po = e.payoutRow(); po.Status != domain.PayoutUnclaimed || orEmpty(po.Issue) != "RECEIVER_UNREGISTERED" {
		t.Fatalf("payout = %+v, want unclaimed", po)
	}
	next := e.wantJob(e.checkKey(po, "created:2"), store.JobPending)
	if !next.RunAt.Equal(e.clock.Now().Add(30 * time.Second)) {
		t.Errorf("second check at %s, want 30s later", next.RunAt)
	}

	e.fake.SetPayoutItemStatus(*po.PayoutBatchID, "SUCCESS", "")
	if err := e.runner.RunPeriodic(e.ctx); err != nil {
		t.Fatal(err)
	}
	e.runKey(e.checkKey(po, "periodic:"+e.clock.Now().UTC().Format("2006-01-02T15")))
	if po = e.payoutRow(); po.Status != domain.PayoutSucceeded || po.Issue != nil {
		t.Fatalf("payout = %+v, want succeeded once claimed", po)
	}
}

func TestPayout_DeniedBatchFails(t *testing.T) {
	e := payoutEnv(t)
	e.confirmed()
	e.toStart()
	e.runKey(e.payoutKey())
	po := e.payoutRow()
	e.fake.SetPayoutItemStatus(*po.PayoutBatchID, "PENDING", "")
	e.fake.SetPayoutBatchStatus(*po.PayoutBatchID, "DENIED")
	e.runKey(e.checkKey(po, "created:1"))
	if po = e.payoutRow(); po.Status != domain.PayoutFailed || orEmpty(po.BatchStatus) != "DENIED" {
		t.Fatalf("payout = %+v, want failed on a denied batch", po)
	}
	e.wantNoJob(e.checkKey(po, "created:2"))
	e.wantWorkshop(domain.WorkshopConfirmed, domain.ResolutionOpen)
}

func TestPayout_FillsMissingFeesBeforeFreezingTheAmount(t *testing.T) {
	e := payoutEnv(t)
	attempts := e.confirmed()
	if _, err := e.st.Pool.Exec(e.ctx, `update payment_attempts set capture_fee_minor = null, capture_net_minor = null where id = $1`, attempts[0].ID); err != nil {
		t.Fatal(err)
	}
	e.toStart()
	e.runUntilIdle()
	if po := e.payoutRow(); po.AmountMinor != 2748 || po.FeeMinor != 252 {
		t.Fatalf("payout = %+v, want the missing fees read back from PayPal first", po)
	}
	if a := e.attempt(attempts[0].ID); a.CaptureNetMinor == nil || *a.CaptureNetMinor != 916 {
		t.Errorf("attempt net = %v, want 916 recorded", deref(a.CaptureNetMinor))
	}
}

func TestPayout_OffUntilSwitchedOnThenTheSweepRunsIt(t *testing.T) {
	e := newEnv(t)
	e.setWorkshop(`payout_email = $2`, receiver)
	e.confirmed()
	e.wantNoJob(e.payoutKey())
	e.toStart()
	if err := e.runner.RunPeriodic(e.ctx); err != nil {
		t.Fatal(err)
	}
	e.wantNoJob(e.payoutKey())

	worker.EnablePayouts(e.runner)
	if err := e.runner.RunPeriodic(e.ctx); err != nil {
		t.Fatal(err)
	}
	e.runUntilIdle()
	if po := e.payoutRow(); po.Status != domain.PayoutSucceeded {
		t.Fatalf("payout = %s, want succeeded once switched on", po.Status)
	}
}

func TestPayout_WebhookFillsAMissingBatchAndChecks(t *testing.T) {
	e := payoutEnv(t, withClientTimeout(300*time.Millisecond))
	e.confirmed()
	e.toStart()
	e.fake.FailNext("create_payout", 0, "")
	e.runKey(e.payoutKey())
	e.fake.WaitHung()
	batch, _ := e.fake.PayoutBatchFor("cp-" + e.workshop.ID)

	id, err := e.runner.PayoutWebhook(e.ctx, worker.PayoutEvent{ID: "WH-1", PayoutBatchID: batch, SenderBatchID: "cp-" + e.workshop.ID})
	if err != nil || id == "" {
		t.Fatalf("PayoutWebhook = %q, %v; want the payout matched by its sender_batch_id", id, err)
	}
	po := e.payoutRow()
	if orEmpty(po.PayoutBatchID) != batch || po.Status != domain.PayoutSent {
		t.Fatalf("payout = %+v, want the webhook's batch recorded", po)
	}
	e.runKey(e.checkKey(po, "webhook:WH-1"))
	if po = e.payoutRow(); po.Status != domain.PayoutSucceeded {
		t.Fatalf("payout after the webhook's check = %s, want succeeded", po.Status)
	}
	e.clock.Set(e.job(e.payoutKey()).RunAt)
	e.runUntilIdle()
	if n := e.fake.PayoutBatches(); n != 1 {
		t.Errorf("batches = %d, want 1: the retry only checks a payout that has its batch", n)
	}
	if id, err := e.runner.PayoutWebhook(e.ctx, worker.PayoutEvent{ID: "WH-2", PayoutBatchID: "OTHER"}); err != nil || id != "" {
		t.Errorf("an unknown batch = %q, %v; want no match", id, err)
	}
}

func TestPayout_CheckRetriesWhenPayPalDoesNotReturnTheBatchYet(t *testing.T) {
	e := payoutEnv(t)
	e.confirmed()
	e.toStart()
	e.runKey(e.payoutKey())
	po := e.payoutRow()
	e.fake.FailNext("get_payout_batch", http.StatusNotFound, "INVALID_RESOURCE_ID")
	e.runKey(e.checkKey(po, "created:1"))
	j := e.wantJob(e.checkKey(po, "created:1"), store.JobPending)
	if po = e.payoutRow(); po.Status != domain.PayoutSent {
		t.Fatalf("payout after a 404 = %s, want still sent", po.Status)
	}
	e.clock.Set(j.RunAt)
	e.runKey(e.checkKey(po, "created:1"))
	if po = e.payoutRow(); po.Status != domain.PayoutSucceeded {
		t.Fatalf("payout after the retry = %s, want succeeded", po.Status)
	}
}

func (e *env) periodic() {
	e.t.Helper()
	if err := e.runner.RunPeriodic(e.ctx); err != nil {
		e.t.Fatalf("RunPeriodic: %v", err)
	}
}

func (e *env) earlyLockWithALateCapture() (store.EnrollmentRow, store.AttemptRow, paypal.Capture, store.PayoutRow) {
	e.t.Helper()
	e.confirmed()
	dee, d := e.authorized("Dee")
	cp, err := e.api.CaptureAuthorization(e.ctx, paypal.CaptureParams{AuthorizationID: *d.AuthorizationID, RequestID: "capture:" + d.ID})
	if err != nil {
		e.t.Fatal(err)
	}
	e.transition(dee.ID, domain.EnrollmentAuthorized, domain.EnrollmentCaptureFailed)
	e.clock.Set(e.proposal.ClosesAt.Add(time.Minute))
	po, err := e.runner.StartPayoutNow(e.ctx, e.workshop.ID)
	if err != nil || po.Status != domain.PayoutSucceeded || po.Seats != 3 {
		e.t.Fatalf("StartPayoutNow = %+v, %v; want three seats paid out", po, err)
	}
	e.transition(dee.ID, domain.EnrollmentCaptureFailed, domain.EnrollmentCapturing)
	return dee, d, cp, po
}

func TestPayout_ALateCaptureReconciledBeforeThePayoutJobIsRefundedNotPaidOut(t *testing.T) {
	e := payoutEnv(t, withClientTimeout(time.Second))
	e.confirmed()
	dee, d := e.authorized("Dee")
	captureID := e.capturePending(dee, d)
	e.toStart()
	e.runKey(e.payoutKey())
	e.wantJob(e.payoutKey(), store.JobFailed)

	e.fake.SetCaptureStatus(captureID, "COMPLETED", "")
	e.periodic()
	e.runKey(e.pendingKey(d))
	e.wantStatus(dee.ID, domain.EnrollmentPaid)
	e.wantJob("refund:"+d.ID, store.JobPending)

	e.runKey(e.payoutKey())
	j := e.wantJob(e.payoutKey(), store.JobFailed)
	if !strings.Contains(orEmpty(j.LastError), domain.PayoutInFlight) {
		t.Fatalf("payout job ahead of the refund = %+v, want a wait for %s", j, domain.PayoutInFlight)
	}
	e.wantNoPayout()

	e.runKey("refund:" + d.ID)
	e.wantStatus(dee.ID, domain.EnrollmentRefunded)
	e.periodic()
	e.runUntilIdle()
	po := e.payoutRow()
	if po.Status != domain.PayoutSucceeded || po.Seats != 3 || po.AmountMinor != 2748 {
		t.Fatalf("payout = %+v, want the three seats kept, without the refunded one", po)
	}
	if a := e.attempt(d.ID); a.PayoutID != nil {
		t.Errorf("the refunded seat's attempt is stamped as paid out")
	}
}

func TestPayout_ARefundRacingThePayoutIsRefusedForAPaidOutSeat(t *testing.T) {
	e := payoutEnv(t)
	attempts := e.confirmed()
	e.toStart()
	e.runUntilIdle()
	po := e.payoutRow()
	if a := e.attempt(attempts[0].ID); orEmpty(a.PayoutID) != po.ID {
		t.Fatalf("paid attempt stamp = %v, want payout %s", a.PayoutID, po.ID)
	}

	e.enqueueKey("refund", "refund:"+attempts[0].ID, attempts[0], e.clock.Now())
	e.runKey("refund:" + attempts[0].ID)

	e.wantStatus(attempts[0].EnrollmentID, domain.EnrollmentPaid)
	e.wantJob("refund:"+attempts[0].ID, store.JobFailed)
	if n := len(e.fake.Requests("refund")); n != 0 {
		t.Errorf("refund requests = %d, want none for a paid out seat", n)
	}
}

func TestPayout_ACaptureLandingOnAStampedAttemptKeepsItsSeat(t *testing.T) {
	e := payoutEnv(t)
	attempts := e.confirmed()
	e.toStart()
	e.runUntilIdle()
	a := e.attempt(attempts[0].ID)
	e.transition(a.EnrollmentID, domain.EnrollmentPaid, domain.EnrollmentCapturing)
	cp, err := e.api.GetCapture(e.ctx, *a.CaptureID)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.runner.ApplyCapture(e.ctx, a.ID, cp); err != nil {
		t.Fatal(err)
	}
	e.wantStatus(a.EnrollmentID, domain.EnrollmentPaid)
	e.wantNoJob("refund:" + a.ID)
}

func TestPayout_RetryRefundAfterTheLockRefundsOnlyASeatThePayoutDidNotCount(t *testing.T) {
	e := payoutEnv(t)
	dee, d, cp, po := e.earlyLockWithALateCapture()
	e.setMockCode(d, "REFUND_AMOUNT_EXCEEDED")
	if err := e.runner.ApplyCapture(e.ctx, d.ID, cp); err != nil {
		t.Fatal(err)
	}
	e.runKey("refund:" + d.ID)
	e.wantFlagged(dee.ID, domain.EnrollmentRefundFailed)

	key, err := e.runner.RetryRefund(e.ctx, dee.ID)
	if err != nil {
		t.Fatalf("RetryRefund after the lock = %v", err)
	}
	e.runKey(key)
	e.wantStatus(dee.ID, domain.EnrollmentRefunded)
	if after := e.payoutRow(); after.Seats != po.Seats || after.AmountMinor != po.AmountMinor || after.Status != domain.PayoutSucceeded {
		t.Errorf("payout after the retried refund = %+v, want it unchanged", after)
	}

	paid := e.payoutRow()
	stamped, err := e.st.Pool.Exec(e.ctx, `update enrollments set status = 'REFUND_FAILED', needs_attention_at = now()
		where id = (select enrollment_id from payment_attempts where payout_id = $1 limit 1)`, paid.ID)
	if err != nil || stamped.RowsAffected() != 1 {
		t.Fatalf("mark a paid out seat refund failed: %v", err)
	}
	var seat string
	if err := e.st.Pool.QueryRow(e.ctx, `select enrollment_id from payment_attempts where payout_id = $1 limit 1`, paid.ID).Scan(&seat); err != nil {
		t.Fatal(err)
	}
	if _, err := e.runner.RetryRefund(e.ctx, seat); !errors.Is(err, store.ErrPaidOut) {
		t.Errorf("RetryRefund on a paid out seat = %v, want ErrPaidOut", err)
	}
}

func TestPayout_WaitsDoNotUseUpTheRetriesOfTheSend(t *testing.T) {
	e := payoutEnv(t)
	e.confirmed()
	dee, _ := e.authorized("Dee")
	e.toStart()
	for range 6 {
		e.periodic()
		e.runKey(e.payoutKey())
		if j := e.wantJob(e.payoutKey(), store.JobFailed); j.Attempts > 1 {
			t.Fatalf("a waiting payout job has %d attempts, want them reset on every wait", j.Attempts)
		}
	}
	e.transition(dee.ID, domain.EnrollmentAuthorized, domain.EnrollmentVoided)
	e.fake.FailNext("create_payout", http.StatusServiceUnavailable, "")
	e.periodic()
	e.runKey(e.payoutKey())
	j := e.wantJob(e.payoutKey(), store.JobPending)
	e.clock.Set(j.RunAt)
	e.runKey(e.payoutKey())
	if po := e.payoutRow(); po.Status != domain.PayoutSent || e.fake.PayoutBatches() != 1 {
		t.Fatalf("payout after a 503 that followed six waits = %+v, want sent on the retry", po)
	}
}

func TestPayout_AMissingFeeOnACaptureNoLongerCompletedWaits(t *testing.T) {
	e := payoutEnv(t)
	attempts := e.confirmed()
	a := attempts[0]
	if _, err := e.st.Pool.Exec(e.ctx, `update payment_attempts set capture_fee_minor = null, capture_net_minor = null where id = $1`, a.ID); err != nil {
		t.Fatal(err)
	}
	e.fake.SetCaptureStatus(*e.attempt(a.ID).CaptureID, "REFUNDED", "")
	e.toStart()
	e.runKey(e.payoutKey())

	j := e.wantJob(e.payoutKey(), store.JobFailed)
	if !strings.Contains(orEmpty(j.LastError), domain.PayoutInFlight) {
		t.Fatalf("payout job = %+v, want a wait for %s", j, domain.PayoutInFlight)
	}
	e.wantNoPayout()
	e.wantJob("reconcile:"+a.ID+":payout:"+store.Minute(e.clock.Now()), store.JobPending)
	if got := e.attempt(a.ID); got.CaptureNetMinor != nil {
		t.Errorf("fees recorded from a capture PayPal reports REFUNDED: %v", *got.CaptureNetMinor)
	}
}

func TestPayout_AWaitNoPaymentEndsOnItsOwnBacksOffAnHour(t *testing.T) {
	e := payoutEnv(t)
	e.confirmed()
	if err := e.st.SetWorkshopStatus(e.ctx, e.workshop.ID, domain.WorkshopConfirmed, domain.ResolutionNeedsAttention); err != nil {
		t.Fatal(err)
	}
	e.toStart()
	e.runKey(e.payoutKey())
	e.periodic()
	j := e.wantJob(e.payoutKey(), store.JobPending)
	if want := e.clock.Now().Add(time.Hour); !j.RunAt.Equal(want) {
		t.Fatalf("a payout waiting for an operator runs again at %s, want an hour later (%s)", j.RunAt, want)
	}
	e.advance(2 * time.Minute)
	e.periodic()
	if again := e.wantJob(e.payoutKey(), store.JobPending); !again.RunAt.Equal(j.RunAt) {
		t.Errorf("the next sweep moved the backed-off payout to %s", again.RunAt)
	}
}
