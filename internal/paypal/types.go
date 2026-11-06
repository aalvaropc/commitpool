package paypal

import (
	"net/http"
	"time"
)

type Config struct {
	BaseURL      string
	ClientID     string
	ClientSecret string
	WebhookID    string
	SandboxMocks bool
	HTTPClient   *http.Client
}

type Money struct {
	CurrencyCode string `json:"currency_code"`
	Value        string `json:"value"`
}

type Link struct {
	Rel    string `json:"rel"`
	Href   string `json:"href"`
	Method string `json:"method,omitempty"`
}

type StatusDetails struct {
	Reason string `json:"reason"`
}

type Authorization struct {
	ID             string         `json:"id"`
	Status         string         `json:"status"`
	ExpirationTime time.Time      `json:"expiration_time,omitzero"`
	Amount         Money          `json:"amount"`
	StatusDetails  *StatusDetails `json:"status_details,omitempty"`
}

type Capture struct {
	ID            string         `json:"id"`
	Status        string         `json:"status"`
	Amount        Money          `json:"amount"`
	StatusDetails *StatusDetails `json:"status_details,omitempty"`
}

type Refund struct {
	ID            string         `json:"id"`
	Status        string         `json:"status"`
	Amount        Money          `json:"amount"`
	StatusDetails *StatusDetails `json:"status_details,omitempty"`
}

type PurchaseUnit struct {
	ReferenceID string `json:"reference_id,omitempty"`
	CustomID    string `json:"custom_id,omitempty"`
	Amount      Money  `json:"amount"`
	Payments    struct {
		Authorizations []Authorization `json:"authorizations,omitempty"`
		Captures       []Capture       `json:"captures,omitempty"`
		Refunds        []Refund        `json:"refunds,omitempty"`
	} `json:"payments,omitzero"`
}

type Order struct {
	ID            string         `json:"id"`
	Status        string         `json:"status"`
	Links         []Link         `json:"links,omitempty"`
	PurchaseUnits []PurchaseUnit `json:"purchase_units,omitempty"`
}

func (o Order) ApproveURL() string {
	for _, rel := range []string{"payer-action", "approve"} {
		for _, l := range o.Links {
			if l.Rel == rel {
				return l.Href
			}
		}
	}
	return ""
}

type CreateOrderParams struct {
	RequestID   string
	ReferenceID string
	CustomID    string
	Amount      Money
	ReturnURL   string
	CancelURL   string
}

type CaptureParams struct {
	AuthorizationID string
	RequestID       string
	MockCode        string
}

type RefundParams struct {
	CaptureID string
	RequestID string
	MockCode  string
}
