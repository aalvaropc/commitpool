package paypaltest

import (
	"context"
	"net/http"
	"testing"

	"github.com/aalvaropc/commitpool/internal/paypal"
)

func payoutParams(requestID string) paypal.CreatePayoutParams {
	return paypal.CreatePayoutParams{
		RequestID:     requestID,
		SenderBatchID: "cp-w1",
		Receiver:      "iris@example.com",
		Amount:        paypal.PayoutAmount{Value: "27.48", Currency: "USD"},
		Note:          "CommitPool payout for Life drawing",
		SenderItemID:  "w1",
	}
}

func TestFakePayouts_SameRequestIDReplaysTheSameBatch(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f)
	ctx := context.Background()
	first, err := c.CreatePayout(ctx, payoutParams("payout:w1"))
	if err != nil {
		t.Fatal(err)
	}
	again, err := c.CreatePayout(ctx, payoutParams("payout:w1"))
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if again.BatchHeader.PayoutBatchID != first.BatchHeader.PayoutBatchID || f.PayoutBatches() != 1 {
		t.Fatalf("replay = %q (batches %d), want %q and one batch", again.BatchHeader.PayoutBatchID, f.PayoutBatches(), first.BatchHeader.PayoutBatchID)
	}
}

func TestFakePayouts_SameSenderIDUnderANewRequestIsADuplicate(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f)
	ctx := context.Background()
	first, err := c.CreatePayout(ctx, payoutParams("payout:w1"))
	if err != nil {
		t.Fatal(err)
	}
	f.ForgetRequestIDs()
	_, err = c.CreatePayout(ctx, payoutParams("payout:w1"))
	pe := requirePayPalError(t, err, http.StatusBadRequest, "USER_BUSINESS_ERROR", "Batch with given sender_batch_id already exists")
	if got := pe.ExistingPayoutBatchID(); got != first.BatchHeader.PayoutBatchID {
		t.Errorf("duplicate links to %q, want %q", got, first.BatchHeader.PayoutBatchID)
	}
	if f.PayoutBatches() != 1 {
		t.Errorf("batches = %d, want 1", f.PayoutBatches())
	}
	f.DuplicatesWithoutLink()
	_, err = c.CreatePayout(ctx, payoutParams("payout:w1:again"))
	if pe := paypal.DuplicatePayout(err); pe == nil || pe.ExistingPayoutBatchID() != "" {
		t.Errorf("bare duplicate = %v, want one without a batch link", err)
	}
}

func TestFakePayouts_ItemAndBatchStatusesAreSettable(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f)
	ctx := context.Background()
	b, err := c.CreatePayout(ctx, payoutParams("payout:w1"))
	if err != nil {
		t.Fatal(err)
	}
	id := b.BatchHeader.PayoutBatchID

	f.SetPayoutItemStatus(id, "UNCLAIMED", "")
	got, err := c.GetPayoutBatch(ctx, id)
	if err != nil || got.Items[0].TransactionStatus != "UNCLAIMED" || got.BatchHeader.BatchStatus != "SUCCESS" {
		t.Fatalf("after UNCLAIMED: %+v, %v", got, err)
	}
	f.SetPayoutItemStatus(id, "PENDING", "")
	if got, _ := c.GetPayoutBatch(ctx, id); got.BatchHeader.BatchStatus != "PROCESSING" {
		t.Errorf("a pending item leaves the batch %s, want PROCESSING", got.BatchHeader.BatchStatus)
	}
	f.SetPayoutItemStatus(id, "FAILED", "RECEIVER_UNREGISTERED")
	if got, _ := c.GetPayoutBatch(ctx, id); got.Items[0].Errors == nil || got.Items[0].Errors.Name != "RECEIVER_UNREGISTERED" {
		t.Errorf("failed item errors = %+v, want RECEIVER_UNREGISTERED", got.Items[0].Errors)
	}
	f.SetPayoutBatchStatus(id, "DENIED")
	if got, _ := c.GetPayoutBatch(ctx, id); got.BatchHeader.BatchStatus != "DENIED" {
		t.Errorf("batch status = %s, want DENIED", got.BatchHeader.BatchStatus)
	}
	_, err = c.GetPayoutBatch(ctx, "NOPE")
	requirePayPalError(t, err, http.StatusNotFound, "RESOURCE_NOT_FOUND", "INVALID_RESOURCE_ID")
}

func TestFakePayouts_RejectsAMalformedPayout(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f)
	p := payoutParams("payout:w1")
	p.Amount = paypal.PayoutAmount{Value: "0.00", Currency: "USD"}
	_, err := c.CreatePayout(context.Background(), p)
	requirePayPalError(t, err, http.StatusBadRequest, "VALIDATION_ERROR", "INVALID_REQUEST")
	if f.PayoutBatches() != 0 {
		t.Errorf("a refused payout created a batch")
	}
}
