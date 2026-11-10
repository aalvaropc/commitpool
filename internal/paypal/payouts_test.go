package paypal_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/paypal"
)

func payoutParams(sender string) paypal.CreatePayoutParams {
	return paypal.CreatePayoutParams{
		RequestID:     "payout:" + sender,
		SenderBatchID: "cp-" + sender,
		EmailSubject:  "You have a payout from CommitPool",
		Receiver:      "iris@example.com",
		Amount:        paypal.PayoutAmount{Value: "27.48", Currency: "USD"},
		Note:          "CommitPool payout for Life drawing",
		SenderItemID:  sender,
	}
}

func TestPayouts_CreateSendsOneEmailItemWithTheKeys(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f)

	b, err := c.CreatePayout(context.Background(), payoutParams("w1"))
	if err != nil {
		t.Fatalf("CreatePayout: %v", err)
	}
	if b.BatchHeader.PayoutBatchID == "" || b.BatchHeader.BatchStatus != "PENDING" || b.BatchHeader.SenderBatchHeader.SenderBatchID != "cp-w1" {
		t.Fatalf("batch header = %+v, want a pending batch for cp-w1", b.BatchHeader)
	}

	reqs := f.Requests("create_payout")
	if len(reqs) != 1 {
		t.Fatalf("create_payout requests = %d, want 1", len(reqs))
	}
	if reqs[0].RequestID != "payout:w1" {
		t.Errorf("PayPal-Request-Id = %q, want payout:w1", reqs[0].RequestID)
	}
	if got := reqs[0].Headers.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	var body map[string]any
	if err := json.Unmarshal(reqs[0].Body, &body); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"sender_batch_header": map[string]any{"sender_batch_id": "cp-w1", "email_subject": "You have a payout from CommitPool"},
		"items": []any{map[string]any{
			"recipient_type": "EMAIL",
			"amount":         map[string]any{"value": "27.48", "currency": "USD"},
			"note":           "CommitPool payout for Life drawing",
			"receiver":       "iris@example.com",
			"sender_item_id": "w1",
		}},
	}
	gotJSON, _ := json.Marshal(body)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("payout body = %s, want %s", gotJSON, wantJSON)
	}
	if strings.Contains(string(reqs[0].Body), "currency_code") {
		t.Errorf("payout amounts use currency, not currency_code: %s", reqs[0].Body)
	}
}

func TestPayouts_GetDecodesTheBatchAndItsItem(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f)
	ctx := context.Background()
	created, err := c.CreatePayout(ctx, payoutParams("w1"))
	if err != nil {
		t.Fatal(err)
	}

	b, err := c.GetPayoutBatch(ctx, created.BatchHeader.PayoutBatchID)
	if err != nil {
		t.Fatalf("GetPayoutBatch: %v", err)
	}
	item := b.Item("w1")
	if item == nil {
		t.Fatalf("batch %+v has no item for w1", b)
	}
	if b.BatchHeader.BatchStatus != "SUCCESS" || item.TransactionStatus != "SUCCESS" || item.PayoutItemID == "" || item.TransactionID == "" {
		t.Errorf("batch = %+v, item = %+v; want a successful item with its ids", b.BatchHeader, item)
	}
	if item.PayoutItem.Receiver != "iris@example.com" || item.PayoutItem.Amount != (paypal.PayoutAmount{Value: "27.48", Currency: "USD"}) {
		t.Errorf("item = %+v, want what was sent", item.PayoutItem)
	}
	if reqs := f.Requests("get_payout_batch"); len(reqs) != 1 || reqs[0].RequestID != "" {
		t.Errorf("GET carried a request id or ran %d times", len(reqs))
	}
}

func TestPayouts_CreateRefusesAnIncompletePayoutWithoutCallingPayPal(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f)
	p := payoutParams("w1")
	p.Receiver = ""
	if _, err := c.CreatePayout(context.Background(), p); err == nil {
		t.Fatal("CreatePayout without a receiver succeeded")
	}
	if n := len(f.Requests("create_payout")); n != 0 {
		t.Errorf("create_payout requests = %d, want 0", n)
	}
}

func TestPayouts_ACreatedBatchWithoutAnIDIsAmbiguous(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f)
	f.RespondOnce("create_payout", http.StatusCreated, []byte(`{"batch_header":{"batch_status":"PENDING"}}`))
	_, err := c.CreatePayout(context.Background(), payoutParams("w1"))
	if !paypal.IsAmbiguous(err) {
		t.Fatalf("CreatePayout answered without an id = %v, want an ambiguous error", err)
	}
}

func TestPayouts_SecondBatchWithTheSameSenderIDIsADuplicateWithItsLink(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f)
	ctx := context.Background()
	first, err := c.CreatePayout(ctx, payoutParams("w1"))
	if err != nil {
		t.Fatal(err)
	}
	again := payoutParams("w1")
	again.RequestID = "another-request"
	_, err = c.CreatePayout(ctx, again)
	pe := paypal.DuplicatePayout(err)
	if pe == nil {
		t.Fatalf("second CreatePayout = %v, want a duplicate sender_batch_id", err)
	}
	if got := pe.ExistingPayoutBatchID(); got != first.BatchHeader.PayoutBatchID {
		t.Errorf("recovered batch id = %q, want %q", got, first.BatchHeader.PayoutBatchID)
	}
	if paypal.Definitive(err) == nil {
		t.Errorf("a duplicate is a definitive answer, got %v", err)
	}
}

func TestPayouts_DuplicateWithoutALinkHasNoBatchID(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f)
	ctx := context.Background()
	if _, err := c.CreatePayout(ctx, payoutParams("w1")); err != nil {
		t.Fatal(err)
	}
	f.DuplicatesWithoutLink()
	again := payoutParams("w1")
	again.RequestID = "another-request"
	_, err := c.CreatePayout(ctx, again)
	pe := paypal.DuplicatePayout(err)
	if pe == nil || pe.ExistingPayoutBatchID() != "" {
		t.Fatalf("duplicate without a link = %v (batch %q), want a duplicate with no batch id", err, pe.ExistingPayoutBatchID())
	}
}

func TestPayouts_DuplicateMatcherToleratesEveryKnownShape(t *testing.T) {
	cases := []struct {
		name, body string
		status     int
		batch      string
	}{
		{"business error with a detail link", `{"name":"USER_BUSINESS_ERROR","message":"User business error.","debug_id":"d1",
			"details":[{"field":"SENDER_BATCH_ID","location":"body","issue":"Batch with given sender_batch_id already exists",
			"link":[{"href":"https://api-m.sandbox.paypal.com/v1/payments/payouts/5UXD2E8A7EBQJ","rel":"self","method":"GET"}]}],"links":[]}`,
			400, "5UXD2E8A7EBQJ"},
		{"named, with a top-level link", `{"name":"SENDER_BATCH_ID_ALREADY_USED","message":"Batch already exists","debug_id":"d2",
			"links":[{"href":"https://api-m.sandbox.paypal.com/v1/payments/payouts/ABC123","rel":"self"}]}`, 422, "ABC123"},
		{"duplicate request id without a link", `{"name":"DUPLICATE_REQUEST_ID","message":"Duplicate request","debug_id":"d3"}`, 422, ""},
		{"named in the issue", `{"name":"UNPROCESSABLE_ENTITY","debug_id":"d4","details":[{"issue":"DUPLICATE_SENDER_BATCH_ID","links":[{"href":"https://api-m.paypal.com/v1/payments/payouts/Z9/"}]}]}`, 422, "Z9"},
		{"conflict mentioning the sender batch id", `{"name":"CONFLICT","message":"A batch with this sender_batch_id was already submitted.","debug_id":"d5"}`, 409, ""},
		{"an item link is not a batch link", `{"name":"SENDER_BATCH_ID_ALREADY_USED","debug_id":"d6","links":[{"href":"https://api-m.paypal.com/v1/payments/payouts-item/ITEM1"}]}`, 400, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := newStubServer(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/oauth2/token") {
					_, _ = w.Write([]byte(`{"access_token":"tok","expires_in":3600}`))
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			})
			client, err := paypal.New(paypal.Config{BaseURL: srv.URL + "/sandbox", ClientID: "id", ClientSecret: "secret"})
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.CreatePayout(context.Background(), payoutParams("w1"))
			pe := paypal.DuplicatePayout(err)
			if pe == nil {
				t.Fatalf("CreatePayout = %v, want a duplicate", err)
			}
			if got := pe.ExistingPayoutBatchID(); got != c.batch {
				t.Errorf("recovered batch id = %q, want %q", got, c.batch)
			}
			if pe.DebugID == "" {
				t.Errorf("the duplicate lost its debug id")
			}
		})
	}
}

func TestPayouts_OtherRefusalsAreNotDuplicates(t *testing.T) {
	for _, err := range []error{
		&paypal.Error{HTTPStatus: 422, Name: "INSUFFICIENT_FUNDS", Message: "Sender does not have sufficient funds."},
		&paypal.Error{HTTPStatus: 400, Name: "VALIDATION_ERROR", Details: []paypal.ErrorDetail{{Field: "items[0].receiver", Issue: "Invalid email"}}},
		&paypal.Error{HTTPStatus: 500, Name: "SENDER_BATCH_ID_ALREADY_USED"},
		errors.New("paypal: connection reset"),
		context.DeadlineExceeded,
	} {
		if paypal.DuplicatePayout(err) != nil {
			t.Errorf("DuplicatePayout(%v) matched", err)
		}
	}
}

func TestPayouts_HungCreateStillCreatesTheBatchAndReplaysIt(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f, withTimeout(200*time.Millisecond))
	ctx := context.Background()
	f.FailNext("create_payout", 0, "")
	_, err := c.CreatePayout(ctx, payoutParams("w1"))
	if !paypal.IsAmbiguous(err) {
		t.Fatalf("hung CreatePayout = %v, want ambiguous", err)
	}
	f.WaitHung()
	if f.PayoutBatches() != 1 {
		t.Fatalf("batches after the hang = %d, want 1", f.PayoutBatches())
	}
	b, err := c.CreatePayout(ctx, payoutParams("w1"))
	if err != nil {
		t.Fatalf("retry under the same request id: %v", err)
	}
	if id, _ := f.PayoutBatchFor("cp-w1"); b.BatchHeader.PayoutBatchID != id || f.PayoutBatches() != 1 {
		t.Errorf("retry returned batch %q, want the replayed %q and still one batch", b.BatchHeader.PayoutBatchID, id)
	}
}

func withTimeout(d time.Duration) func(*paypal.Config) {
	return func(c *paypal.Config) { c.HTTPClient = &http.Client{Timeout: d} }
}
