// Package paypal is a thin typed client over PayPal Orders v2 and Payments v2.
// Package paypaltest holds an in-memory stand-in for the sandbox.
package paypal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	headerRequestID    = "PayPal-Request-Id"
	headerMockResponse = "PayPal-Mock-Response"

	tokenSafetyMargin = 60 * time.Second
	maxResponseBytes  = 1 << 20
	defaultTimeout    = 20 * time.Second
)

// API is what the worker and the HTTP layer consume; *Client implements it.
type API interface {
	CreateOrder(ctx context.Context, p CreateOrderParams) (Order, error)
	GetOrder(ctx context.Context, id string) (Order, error)
	AuthorizeOrder(ctx context.Context, orderID, requestID string) (Order, error)
	GetAuthorization(ctx context.Context, id string) (Authorization, error)
	CaptureAuthorization(ctx context.Context, p CaptureParams) (Capture, error)
	VoidAuthorization(ctx context.Context, id, requestID string) error
	GetCapture(ctx context.Context, id string) (Capture, error)
	RefundCapture(ctx context.Context, p RefundParams) (Refund, error)
	GetRefund(ctx context.Context, id string) (Refund, error)
	VerifyWebhookSignature(ctx context.Context, h http.Header, body []byte) (bool, error)
}

var _ API = (*Client)(nil)

type Client struct {
	cfg  Config
	base string
	hc   *http.Client
	now  func() time.Time

	mu     sync.Mutex
	token  string
	expiry time.Time
}

// New validates the configuration. The base URL is never defaulted here: it is
// https://api-m.sandbox.paypal.com for the sandbox, and the fake server's URL
// in tests.
func New(cfg Config) (*Client, error) {
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if cfg.BaseURL == "" {
		return nil, errors.New("paypal: base URL is required")
	}
	if cfg.SandboxMocks && !strings.Contains(cfg.BaseURL, "sandbox") {
		return nil, errors.New("paypal: mocks require sandbox")
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: defaultTimeout}
	}
	return &Client{cfg: cfg, base: cfg.BaseURL, hc: hc, now: time.Now}, nil
}

type createOrderRequest struct {
	Intent        string               `json:"intent"`
	PurchaseUnits []createOrderUnit    `json:"purchase_units"`
	PaymentSource createOrderPaySource `json:"payment_source"`
}

type createOrderUnit struct {
	ReferenceID string `json:"reference_id"`
	CustomID    string `json:"custom_id"`
	Amount      Money  `json:"amount"`
}

type createOrderPaySource struct {
	PayPal struct {
		ExperienceContext experienceContext `json:"experience_context"`
	} `json:"paypal"`
}

type experienceContext struct {
	UserAction         string `json:"user_action"`
	ShippingPreference string `json:"shipping_preference"`
	ReturnURL          string `json:"return_url"`
	CancelURL          string `json:"cancel_url"`
}

func (c *Client) CreateOrder(ctx context.Context, p CreateOrderParams) (Order, error) {
	body := createOrderRequest{
		Intent:        "AUTHORIZE",
		PurchaseUnits: []createOrderUnit{{ReferenceID: p.ReferenceID, CustomID: p.CustomID, Amount: p.Amount}},
	}
	body.PaymentSource.PayPal.ExperienceContext = experienceContext{
		UserAction:         "PAY_NOW",
		ShippingPreference: "NO_SHIPPING",
		ReturnURL:          p.ReturnURL,
		CancelURL:          p.CancelURL,
	}
	var o Order
	if err := c.post(ctx, "/v2/checkout/orders", p.RequestID, "", body, &o); err != nil {
		return Order{}, err
	}
	if o.ID == "" {
		return Order{}, unidentified("create order", "order id")
	}
	return o, nil
}

func (c *Client) GetOrder(ctx context.Context, id string) (Order, error) {
	var o Order
	if err := c.get(ctx, "/v2/checkout/orders/", id, &o); err != nil {
		return Order{}, err
	}
	return o, nil
}

func (c *Client) AuthorizeOrder(ctx context.Context, orderID, requestID string) (Order, error) {
	path, err := resource("/v2/checkout/orders/", orderID, "/authorize")
	if err != nil {
		return Order{}, err
	}
	var o Order
	if err := c.post(ctx, path, requestID, "", struct{}{}, &o); err != nil {
		return Order{}, err
	}
	if len(o.PurchaseUnits) == 0 || len(o.PurchaseUnits[0].Payments.Authorizations) == 0 ||
		o.PurchaseUnits[0].Payments.Authorizations[0].ID == "" {
		return Order{}, unidentified("authorize order", "authorization")
	}
	return o, nil
}

func (c *Client) GetAuthorization(ctx context.Context, id string) (Authorization, error) {
	var a Authorization
	if err := c.get(ctx, "/v2/payments/authorizations/", id, &a); err != nil {
		return Authorization{}, err
	}
	return a, nil
}

func (c *Client) CaptureAuthorization(ctx context.Context, p CaptureParams) (Capture, error) {
	path, err := resource("/v2/payments/authorizations/", p.AuthorizationID, "/capture")
	if err != nil {
		return Capture{}, err
	}
	var cp Capture
	if err := c.post(ctx, path, p.RequestID, p.MockCode, struct {
		FinalCapture bool `json:"final_capture"`
	}{true}, &cp); err != nil {
		return Capture{}, err
	}
	if cp.ID == "" {
		return Capture{}, unidentified("capture", "capture id")
	}
	return cp, nil
}

// VoidAuthorization expects the 204 PayPal answers with; any 2xx is accepted.
func (c *Client) VoidAuthorization(ctx context.Context, id, requestID string) error {
	path, err := resource("/v2/payments/authorizations/", id, "/void")
	if err != nil {
		return err
	}
	return c.post(ctx, path, requestID, "", struct{}{}, nil)
}

func (c *Client) GetCapture(ctx context.Context, id string) (Capture, error) {
	var cp Capture
	if err := c.get(ctx, "/v2/payments/captures/", id, &cp); err != nil {
		return Capture{}, err
	}
	return cp, nil
}

func (c *Client) RefundCapture(ctx context.Context, p RefundParams) (Refund, error) {
	path, err := resource("/v2/payments/captures/", p.CaptureID, "/refund")
	if err != nil {
		return Refund{}, err
	}
	var r Refund
	if err := c.post(ctx, path, p.RequestID, p.MockCode, struct{}{}, &r); err != nil {
		return Refund{}, err
	}
	if r.ID == "" {
		return Refund{}, unidentified("refund", "refund id")
	}
	return r, nil
}

func (c *Client) GetRefund(ctx context.Context, id string) (Refund, error) {
	var r Refund
	if err := c.get(ctx, "/v2/payments/refunds/", id, &r); err != nil {
		return Refund{}, err
	}
	return r, nil
}

// VerifyWebhookSignature asks PayPal whether a webhook delivery is authentic.
// The event is embedded byte for byte: the signature covers the raw body.
func (c *Client) VerifyWebhookSignature(ctx context.Context, h http.Header, body []byte) (bool, error) {
	if !json.Valid(body) {
		return false, errors.New("paypal: webhook body is not valid JSON")
	}
	head, err := json.Marshal(struct {
		AuthAlgo         string `json:"auth_algo"`
		CertURL          string `json:"cert_url"`
		TransmissionID   string `json:"transmission_id"`
		TransmissionSig  string `json:"transmission_sig"`
		TransmissionTime string `json:"transmission_time"`
		WebhookID        string `json:"webhook_id"`
	}{
		AuthAlgo:         h.Get("PAYPAL-AUTH-ALGO"),
		CertURL:          h.Get("PAYPAL-CERT-URL"),
		TransmissionID:   h.Get("PAYPAL-TRANSMISSION-ID"),
		TransmissionSig:  h.Get("PAYPAL-TRANSMISSION-SIG"),
		TransmissionTime: h.Get("PAYPAL-TRANSMISSION-TIME"),
		WebhookID:        c.cfg.WebhookID,
	})
	if err != nil {
		return false, err
	}
	var payload bytes.Buffer
	payload.Write(head[:len(head)-1])
	payload.WriteString(`,"webhook_event":`)
	payload.Write(body)
	payload.WriteByte('}')

	var out struct {
		VerificationStatus string `json:"verification_status"`
	}
	err = c.do(ctx, request{method: http.MethodPost, path: "/v1/notifications/verify-webhook-signature", body: payload.Bytes()}, &out)
	if err != nil {
		return false, err
	}
	return out.VerificationStatus == "SUCCESS", nil
}

// unidentified is the error for a 2xx answer to a money POST that does not
// identify the resource it created. PayPal may well have created it, so the
// outcome is unconfirmed and the caller must reconcile, never persist an
// empty PayPal id.
func unidentified(op, what string) error {
	return &unconfirmedError{fmt.Errorf("paypal: %s response carries no %s", op, what)}
}

func resource(prefix, id, suffix string) (string, error) {
	if id == "" {
		return "", errors.New("paypal: resource id is required")
	}
	return prefix + url.PathEscape(id) + suffix, nil
}

func (c *Client) get(ctx context.Context, prefix, id string, out any) error {
	path, err := resource(prefix, id, "")
	if err != nil {
		return err
	}
	return c.do(ctx, request{method: http.MethodGet, path: path}, out)
}

// post sends a request that creates or changes money: it always carries the
// idempotency key, and it refuses to leave the process without one. A mock
// code is only ever sent by a client configured for the sandbox.
func (c *Client) post(ctx context.Context, path, requestID, mockCode string, body, out any) error {
	if requestID == "" {
		return errors.New("paypal: a PayPal-Request-Id is required")
	}
	if mockCode != "" && !c.cfg.SandboxMocks {
		return errors.New("paypal: mock response codes require SandboxMocks")
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("paypal: encode request: %w", err)
	}
	r := request{method: http.MethodPost, path: path, body: raw, requestID: requestID}
	if mockCode != "" {
		mock, err := json.Marshal(map[string]string{"mock_application_codes": mockCode})
		if err != nil {
			return fmt.Errorf("paypal: encode mock code: %w", err)
		}
		r.mock = string(mock)
	}
	return c.do(ctx, r, out)
}

type request struct {
	method    string
	path      string
	body      []byte
	requestID string
	mock      string
}

// do authenticates and sends r. A 401 means the cached token was rejected:
// it is replaced once and the same request, with the same PayPal-Request-Id,
// is sent again.
func (c *Client) do(ctx context.Context, r request, out any) error {
	token, err := c.accessToken(ctx, "")
	if err != nil {
		return err
	}
	status, body, err := c.send(ctx, r, token)
	if err != nil {
		return err
	}
	if status == http.StatusUnauthorized {
		if token, err = c.accessToken(ctx, token); err != nil {
			return err
		}
		if status, body, err = c.send(ctx, r, token); err != nil {
			return err
		}
	}
	if status < 200 || status > 299 {
		return parseError(status, body)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return &unconfirmedError{fmt.Errorf("paypal: %s %s: decode response: %w", r.method, r.path, err)}
	}
	return nil
}

func (c *Client) send(ctx context.Context, r request, token string) (int, []byte, error) {
	var body io.Reader
	if r.body != nil {
		body = bytes.NewReader(r.body)
	}
	req, err := http.NewRequestWithContext(ctx, r.method, c.base+r.path, body)
	if err != nil {
		return 0, nil, fmt.Errorf("paypal: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if r.body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if r.requestID != "" {
		req.Header.Set(headerRequestID, r.requestID)
		req.Header.Set("Prefer", "return=representation")
	}
	if r.mock != "" {
		req.Header.Set(headerMockResponse, r.mock)
	}
	return c.roundTrip(req)
}

func (c *Client) roundTrip(req *http.Request) (int, []byte, error) {
	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("paypal: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return 0, nil, &unconfirmedError{fmt.Errorf("paypal: read response: %w", err)}
	}
	return resp.StatusCode, body, nil
}

// accessToken returns the cached OAuth token, or fetches a new one when there
// is none, it is about to expire, or it is the one the caller just saw rejected.
func (c *Client) accessToken(ctx context.Context, rejected string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && c.token != rejected && c.now().Before(c.expiry) {
		return c.token, nil
	}
	form := url.Values{"grant_type": {"client_credentials"}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/v1/oauth2/token", strings.NewReader(form))
	if err != nil {
		return "", fmt.Errorf("paypal: build token request: %w", err)
	}
	req.SetBasicAuth(c.cfg.ClientID, c.cfg.ClientSecret)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	status, body, err := c.roundTrip(req)
	if err != nil {
		return "", err
	}
	if status < 200 || status > 299 {
		return "", parseError(status, body)
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.AccessToken == "" {
		return "", errors.New("paypal: token response is not a valid access token")
	}
	c.token = out.AccessToken
	c.expiry = c.now().Add(time.Duration(out.ExpiresIn)*time.Second - tokenSafetyMargin)
	return c.token, nil
}
