package paypaltest

import (
	"encoding/json"
	"net/http"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/paypal"
)

const (
	payoutItemSuccess   = "SUCCESS"
	payoutBatchPending  = "PENDING"
	payoutBatchSuccess  = "SUCCESS"
	payoutBatchProgress = "PROCESSING"
)

type fakePayout struct {
	id, senderBatchID string
	itemID, txID      string
	item              paypal.PayoutItemRequest
	itemStatus        string
	itemError         string
	batchStatus       string
	emailSubject      string
}

type duplicateDetail struct {
	Field    string        `json:"field"`
	Location string        `json:"location"`
	Issue    string        `json:"issue"`
	Link     []paypal.Link `json:"link,omitempty"`
}

type duplicateBody struct {
	Name            string            `json:"name"`
	Message         string            `json:"message"`
	DebugID         string            `json:"debug_id"`
	InformationLink string            `json:"information_link"`
	Details         []duplicateDetail `json:"details"`
	Links           []paypal.Link     `json:"links"`
}

func (f *Server) SetPayoutItemStatus(batchID, status, errorName string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.payout(batchID)
	p.itemStatus, p.itemError = status, errorName
}

func (f *Server) SetPayoutBatchStatus(batchID, status string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.payout(batchID).batchStatus = status
}

func (f *Server) DuplicatesWithoutLink() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bareDupes = true
}

func (f *Server) ForgetRequestIDs() {
	f.mu.Lock()
	defer f.mu.Unlock()
	clear(f.replays)
}

func (f *Server) PayoutBatches() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.payouts)
}

func (f *Server) PayoutBatchFor(senderBatchID string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, ok := f.senders[senderBatchID]
	return id, ok
}

func (f *Server) payout(batchID string) *fakePayout {
	p, ok := f.payouts[batchID]
	if !ok {
		panic("paypal fake: unknown payout batch " + batchID)
	}
	return p
}

func (f *Server) createPayout(r *http.Request, body []byte) fakeResponse {
	var req struct {
		SenderBatchHeader paypal.SenderBatchHeader   `json:"sender_batch_header"`
		Items             []paypal.PayoutItemRequest `json:"items"`
	}
	invalid := errorResponse(http.StatusBadRequest, "VALIDATION_ERROR", "INVALID_REQUEST", "Invalid request - see details.")
	if err := json.Unmarshal(body, &req); err != nil || req.SenderBatchHeader.SenderBatchID == "" || len(req.Items) != 1 {
		return invalid
	}
	item := req.Items[0]
	amount, err := domain.ParseMoney(item.Amount.Value, item.Amount.Currency)
	if err != nil || amount.Minor <= 0 || item.RecipientType != paypal.RecipientEmail || item.Receiver == "" {
		return invalid
	}
	if id, ok := f.senders[req.SenderBatchHeader.SenderBatchID]; ok {
		return f.duplicateBatch(id)
	}
	p := &fakePayout{
		id: newID(), senderBatchID: req.SenderBatchHeader.SenderBatchID,
		itemID: newID(), txID: newID(), item: item, itemStatus: payoutItemSuccess,
		emailSubject: req.SenderBatchHeader.EmailSubject,
	}
	f.payouts[p.id] = p
	f.senders[p.senderBatchID] = p.id
	return jsonResponse(http.StatusCreated, paypal.PayoutBatch{
		BatchHeader: paypal.PayoutBatchHeader{
			PayoutBatchID:     p.id,
			BatchStatus:       payoutBatchPending,
			SenderBatchHeader: paypal.SenderBatchHeader{SenderBatchID: p.senderBatchID, EmailSubject: p.emailSubject},
		},
		Links: []paypal.Link{f.payoutLink(p.id)},
	})
}

func (f *Server) duplicateBatch(batchID string) fakeResponse {
	detail := duplicateDetail{Field: "SENDER_BATCH_ID", Location: "body", Issue: "Batch with given sender_batch_id already exists"}
	if !f.bareDupes {
		detail.Link = []paypal.Link{f.payoutLink(batchID)}
	}
	return jsonResponse(http.StatusBadRequest, duplicateBody{
		Name:            "USER_BUSINESS_ERROR",
		Message:         "User business error.",
		DebugID:         randomHex(),
		InformationLink: "https://developer.paypal.com/docs/api/payments.payouts-batch/#errors",
		Details:         []duplicateDetail{detail},
		Links:           []paypal.Link{},
	})
}

func (f *Server) payoutLink(batchID string) paypal.Link {
	return paypal.Link{Rel: "self", Href: f.URL + "/v1/payments/payouts/" + batchID, Method: "GET"}
}

func (f *Server) getPayoutBatch(r *http.Request, body []byte) fakeResponse {
	p, ok := f.payouts[r.PathValue("id")]
	if !ok {
		return notFound()
	}
	batchStatus := p.batchStatus
	if batchStatus == "" {
		batchStatus = payoutBatchSuccess
		if p.itemStatus == "PENDING" || p.itemStatus == "ONHOLD" {
			batchStatus = payoutBatchProgress
		}
	}
	item := paypal.PayoutItem{
		PayoutItemID:      p.itemID,
		TransactionID:     p.txID,
		TransactionStatus: p.itemStatus,
		PayoutBatchID:     p.id,
		SenderBatchID:     p.senderBatchID,
		PayoutItem:        p.item,
	}
	if p.itemError != "" {
		item.Errors = &paypal.PayoutItemError{Name: p.itemError, Message: "The payout item could not be completed."}
	}
	amount := p.item.Amount
	return jsonResponse(http.StatusOK, paypal.PayoutBatch{
		BatchHeader: paypal.PayoutBatchHeader{
			PayoutBatchID:     p.id,
			BatchStatus:       batchStatus,
			SenderBatchHeader: paypal.SenderBatchHeader{SenderBatchID: p.senderBatchID, EmailSubject: p.emailSubject},
			Amount:            &amount,
		},
		Items: []paypal.PayoutItem{item},
		Links: []paypal.Link{f.payoutLink(p.id)},
	})
}
