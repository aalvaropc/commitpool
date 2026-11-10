package paypal

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strings"
)

const (
	RecipientEmail = "EMAIL"
	payoutsPath    = "/v1/payments/payouts"
)

type PayoutAmount struct {
	Value    string `json:"value"`
	Currency string `json:"currency"`
}

type SenderBatchHeader struct {
	SenderBatchID string `json:"sender_batch_id"`
	EmailSubject  string `json:"email_subject,omitempty"`
	EmailMessage  string `json:"email_message,omitempty"`
}

type PayoutBatchHeader struct {
	PayoutBatchID     string            `json:"payout_batch_id"`
	BatchStatus       string            `json:"batch_status"`
	SenderBatchHeader SenderBatchHeader `json:"sender_batch_header"`
	Amount            *PayoutAmount     `json:"amount,omitempty"`
	Fees              *PayoutAmount     `json:"fees,omitempty"`
}

type PayoutItemRequest struct {
	RecipientType string       `json:"recipient_type"`
	Amount        PayoutAmount `json:"amount"`
	Note          string       `json:"note,omitempty"`
	Receiver      string       `json:"receiver"`
	SenderItemID  string       `json:"sender_item_id,omitempty"`
}

type PayoutItemError struct {
	Name    string `json:"name"`
	Message string `json:"message,omitempty"`
}

type PayoutItem struct {
	PayoutItemID      string            `json:"payout_item_id"`
	TransactionID     string            `json:"transaction_id,omitempty"`
	TransactionStatus string            `json:"transaction_status"`
	PayoutBatchID     string            `json:"payout_batch_id"`
	SenderBatchID     string            `json:"sender_batch_id,omitempty"`
	PayoutItemFee     *PayoutAmount     `json:"payout_item_fee,omitempty"`
	PayoutItem        PayoutItemRequest `json:"payout_item"`
	Errors            *PayoutItemError  `json:"errors,omitempty"`
}

type PayoutBatch struct {
	BatchHeader PayoutBatchHeader `json:"batch_header"`
	Items       []PayoutItem      `json:"items,omitempty"`
	Links       []Link            `json:"links,omitempty"`
}

func (b PayoutBatch) Item(senderItemID string) *PayoutItem {
	for i := range b.Items {
		if b.Items[i].PayoutItem.SenderItemID == senderItemID {
			return &b.Items[i]
		}
	}
	if len(b.Items) == 1 {
		return &b.Items[0]
	}
	return nil
}

type CreatePayoutParams struct {
	RequestID     string
	SenderBatchID string
	EmailSubject  string
	EmailMessage  string
	Receiver      string
	Amount        PayoutAmount
	Note          string
	SenderItemID  string
}

type createPayoutRequest struct {
	SenderBatchHeader SenderBatchHeader   `json:"sender_batch_header"`
	Items             []PayoutItemRequest `json:"items"`
}

func (c *Client) CreatePayout(ctx context.Context, p CreatePayoutParams) (PayoutBatch, error) {
	if p.SenderBatchID == "" || p.Receiver == "" || p.Amount.Value == "" || p.Amount.Currency == "" {
		return PayoutBatch{}, errors.New("paypal: a payout needs a sender_batch_id, a receiver and an amount")
	}
	body := createPayoutRequest{
		SenderBatchHeader: SenderBatchHeader{SenderBatchID: p.SenderBatchID, EmailSubject: p.EmailSubject, EmailMessage: p.EmailMessage},
		Items: []PayoutItemRequest{{
			RecipientType: RecipientEmail,
			Amount:        p.Amount,
			Note:          p.Note,
			Receiver:      p.Receiver,
			SenderItemID:  p.SenderItemID,
		}},
	}
	var b PayoutBatch
	if err := c.post(ctx, payoutsPath, p.RequestID, "", body, &b); err != nil {
		return PayoutBatch{}, err
	}
	if b.BatchHeader.PayoutBatchID == "" {
		return PayoutBatch{}, unidentified("create payout", "payout batch id")
	}
	return b, nil
}

func (c *Client) GetPayoutBatch(ctx context.Context, id string) (PayoutBatch, error) {
	var b PayoutBatch
	if err := c.get(ctx, payoutsPath+"/", id, &b); err != nil {
		return PayoutBatch{}, err
	}
	return b, nil
}

var duplicatePayoutNames = []string{
	"SENDER_BATCH_ID_ALREADY_USED",
	"DUPLICATE_SENDER_BATCH_ID",
	"DUPLICATE_REQUEST_ID",
	"BATCH_ALREADY_EXISTS",
}

func DuplicatePayout(err error) *Error {
	var pe *Error
	if !errors.As(err, &pe) || pe.HTTPStatus < 400 || pe.HTTPStatus >= 500 {
		return nil
	}
	if namedDuplicate(pe.Name) || mentionsUsedBatch(pe.Message) {
		return pe
	}
	for _, d := range pe.Details {
		if namedDuplicate(d.Issue) || mentionsUsedBatch(strings.Join([]string{d.Field, d.Issue, d.Description}, " ")) {
			return pe
		}
	}
	return nil
}

func namedDuplicate(name string) bool {
	return slices.Contains(duplicatePayoutNames, strings.ToUpper(strings.TrimSpace(name)))
}

func mentionsUsedBatch(text string) bool {
	t := strings.ToLower(text)
	return strings.Contains(t, "sender_batch_id") && (strings.Contains(t, "already") || strings.Contains(t, "duplicate"))
}

func (e *Error) ExistingPayoutBatchID() string {
	links := slices.Clone(e.Links)
	for _, d := range e.Details {
		links = append(links, d.Links...)
	}
	for _, l := range links {
		if id := payoutBatchIDFromHref(l.Href); id != "" {
			return id
		}
	}
	return ""
}

func payoutBatchIDFromHref(href string) string {
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	_, rest, ok := strings.Cut(u.Path, payoutsPath+"/")
	if !ok {
		return ""
	}
	id := strings.TrimSuffix(rest, "/")
	if id == "" || len(id) > 64 {
		return ""
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if !('A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' || '0' <= c && c <= '9' || c == '-' || c == '_') {
			return ""
		}
	}
	return id
}
