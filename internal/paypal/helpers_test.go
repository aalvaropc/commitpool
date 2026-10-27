package paypal

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

var usd10 = Money{CurrencyCode: "USD", Value: "10.00"}

const (
	testReturnURL = "https://app.example/paypal/return"
	testCancelURL = "https://app.example/paypal/cancel"
)

func newFake(t *testing.T) *FakeServer {
	t.Helper()
	f := NewFakeServer()
	t.Cleanup(f.Close)
	return f
}

func newTestClient(t *testing.T, f *FakeServer, edit ...func(*Config)) *Client {
	t.Helper()
	cfg := Config{
		BaseURL:      f.URL,
		ClientID:     "fake-client",
		ClientSecret: "fake-secret",
		WebhookID:    "fake-webhook",
		SandboxMocks: true,
	}
	for _, e := range edit {
		e(&cfg)
	}
	c, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func createParams(key string) CreateOrderParams {
	return CreateOrderParams{
		RequestID:   "create-" + key,
		ReferenceID: "attempt-" + key,
		CustomID:    "enrollment-" + key,
		Amount:      usd10,
		ReturnURL:   testReturnURL,
		CancelURL:   testCancelURL,
	}
}

func createOrder(t *testing.T, c *Client, key string) Order {
	t.Helper()
	o, err := c.CreateOrder(context.Background(), createParams(key))
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	return o
}

func authorizedOrder(t *testing.T, c *Client, f *FakeServer, key string) (Order, Authorization) {
	t.Helper()
	o := createOrder(t, c, key)
	f.Approve(o.ID)
	got, err := c.AuthorizeOrder(context.Background(), o.ID, "authorize-"+key)
	if err != nil {
		t.Fatalf("AuthorizeOrder: %v", err)
	}
	auths := got.PurchaseUnits[0].Payments.Authorizations
	if len(auths) != 1 {
		t.Fatalf("authorizations = %d, want 1", len(auths))
	}
	return got, auths[0]
}

func capturedOrder(t *testing.T, c *Client, f *FakeServer, key string) (Authorization, Capture) {
	t.Helper()
	_, auth := authorizedOrder(t, c, f, key)
	cp, err := c.CaptureAuthorization(context.Background(), CaptureParams{AuthorizationID: auth.ID, RequestID: "capture-" + key})
	if err != nil {
		t.Fatalf("CaptureAuthorization: %v", err)
	}
	return auth, cp
}

func requirePayPalError(t *testing.T, err error, status int, name, issue string) *Error {
	t.Helper()
	var pe *Error
	if !errors.As(err, &pe) {
		t.Fatalf("error = %v (%T), want *Error", err, err)
	}
	if pe.HTTPStatus != status || pe.Name != name || pe.Issue != issue {
		t.Fatalf("error = {%d %q %q}, want {%d %q %q}", pe.HTTPStatus, pe.Name, pe.Issue, status, name, issue)
	}
	if pe.DebugID == "" {
		t.Fatalf("error %v has an empty debug_id", err)
	}
	return pe
}

func newStubServer(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}
