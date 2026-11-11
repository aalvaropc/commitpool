package paypal_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/aalvaropc/commitpool/internal/paypal"
)

func TestClient_SendsIdempotencyHeaderOnEveryPost(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f)
	ctx := context.Background()

	order := createOrder(t, c, "1")
	f.Approve(order.ID)
	authOrder, err := c.AuthorizeOrder(ctx, order.ID, "authorize-1")
	if err != nil {
		t.Fatalf("AuthorizeOrder: %v", err)
	}
	authID := authOrder.PurchaseUnits[0].Payments.Authorizations[0].ID
	capture, err := c.CaptureAuthorization(ctx, paypal.CaptureParams{AuthorizationID: authID, RequestID: "capture-1"})
	if err != nil {
		t.Fatalf("CaptureAuthorization: %v", err)
	}
	if _, err := c.RefundCapture(ctx, paypal.RefundParams{CaptureID: capture.ID, RequestID: "refund-1"}); err != nil {
		t.Fatalf("RefundCapture: %v", err)
	}
	_, voidable := authorizedOrder(t, c, f, "2")
	if err := c.VoidAuthorization(ctx, voidable.ID, "void-1"); err != nil {
		t.Fatalf("VoidAuthorization: %v", err)
	}

	posts := []struct{ op, want string }{
		{"create_order", "create-1"},
		{"authorize", "authorize-1"},
		{"capture", "capture-1"},
		{"refund", "refund-1"},
		{"void", "void-1"},
	}
	for _, p := range posts {
		reqs := f.Requests(p.op)
		if len(reqs) == 0 {
			t.Fatalf("%s: no request recorded", p.op)
		}
		if got := reqs[0].RequestID; got != p.want {
			t.Errorf("%s: PayPal-Request-Id = %q, want %q", p.op, got, p.want)
		}
		if got := reqs[0].Headers.Get("Prefer"); got != "return=representation" {
			t.Errorf("%s: Prefer = %q, want return=representation", p.op, got)
		}
		if got := reqs[0].Headers.Get("Authorization"); got != "Bearer fake-token" {
			t.Errorf("%s: Authorization = %q, want the cached bearer token", p.op, got)
		}
	}

	if _, err := c.GetOrder(ctx, order.ID); err != nil {
		t.Fatalf("GetOrder: %v", err)
	}
	if _, err := c.GetAuthorization(ctx, authID); err != nil {
		t.Fatalf("GetAuthorization: %v", err)
	}
	if _, err := c.GetCapture(ctx, capture.ID); err != nil {
		t.Fatalf("GetCapture: %v", err)
	}
	for _, op := range []string{"get_order", "get_authorization", "get_capture"} {
		reqs := f.Requests(op)
		if len(reqs) != 1 {
			t.Fatalf("%s: %d requests, want 1", op, len(reqs))
		}
		if reqs[0].RequestID != "" || reqs[0].Headers.Get("Prefer") != "" {
			t.Errorf("%s: a GET must send neither PayPal-Request-Id nor Prefer, got %q / %q", op, reqs[0].RequestID, reqs[0].Headers.Get("Prefer"))
		}
	}
}

func TestClient_MockHeaderRefusedOutsideSandbox(t *testing.T) {
	_, err := paypal.New(paypal.Config{BaseURL: "https://api-m.paypal.com", ClientID: "fake-client", ClientSecret: "fake-secret", SandboxMocks: true})
	if err == nil || !strings.Contains(err.Error(), "mocks require sandbox") {
		t.Fatalf("New with SandboxMocks outside the sandbox = %v, want a %q error", err, "mocks require sandbox")
	}
	if _, err := paypal.New(paypal.Config{BaseURL: "https://api-m.paypal.com", ClientID: "fake-client", ClientSecret: "fake-secret"}); err != nil {
		t.Fatalf("New without mocks outside the sandbox: %v", err)
	}

	f := newFake(t)
	c := newTestClient(t, f)
	_, auth := authorizedOrder(t, c, f, "1")
	_, err = c.CaptureAuthorization(context.Background(), paypal.CaptureParams{AuthorizationID: auth.ID, RequestID: "capture-1", MockCode: "AUTHORIZATION_EXPIRED"})
	requirePayPalError(t, err, 422, "UNPROCESSABLE_ENTITY", "AUTHORIZATION_EXPIRED")

	_, err = c.CaptureAuthorization(context.Background(), paypal.CaptureParams{AuthorizationID: auth.ID, RequestID: "capture-1"})
	if err != nil {
		t.Fatalf("CaptureAuthorization without mock: %v", err)
	}
	reqs := f.Requests("capture")
	if len(reqs) != 2 {
		t.Fatalf("capture requests = %d, want 2", len(reqs))
	}
	if got, want := reqs[0].Headers.Get("PayPal-Mock-Response"), `{"mock_application_codes":"AUTHORIZATION_EXPIRED"}`; got != want {
		t.Errorf("PayPal-Mock-Response = %q, want %q", got, want)
	}
	if got := reqs[1].Headers.Get("PayPal-Mock-Response"); got != "" {
		t.Errorf("PayPal-Mock-Response without MockCode = %q, want it absent", got)
	}
}

func TestClient_MockCodeWithoutSandboxMocksFailsBeforeSending(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f, func(cfg *paypal.Config) { cfg.SandboxMocks = false })

	_, err := c.CaptureAuthorization(context.Background(), paypal.CaptureParams{AuthorizationID: "AUTH", RequestID: "capture-1", MockCode: "AUTHORIZATION_EXPIRED"})
	if err == nil || paypal.IsAmbiguous(err) {
		t.Fatalf("capture with a mock code and SandboxMocks off = %v, want a definite error", err)
	}
	_, err = c.RefundCapture(context.Background(), paypal.RefundParams{CaptureID: "CAP", RequestID: "refund-1", MockCode: "REFUND_AMOUNT_EXCEEDED"})
	if err == nil || paypal.IsAmbiguous(err) {
		t.Fatalf("refund with a mock code and SandboxMocks off = %v, want a definite error", err)
	}
	for _, op := range []string{"token", "capture", "refund"} {
		if n := len(f.Requests(op)); n != 0 {
			t.Errorf("%s: %d requests reached the server, want 0", op, n)
		}
	}
}

func TestClient_MoneyPostWithoutRequestIDFailsBeforeSending(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f)
	ctx := context.Background()

	calls := map[string]func() error{
		"create_order": func() error {
			_, err := c.CreateOrder(ctx, paypal.CreateOrderParams{ReferenceID: "a", CustomID: "e", Amount: usd10, ReturnURL: testReturnURL, CancelURL: testCancelURL})
			return err
		},
		"authorize": func() error { _, err := c.AuthorizeOrder(ctx, "ORDER", ""); return err },
		"capture": func() error {
			_, err := c.CaptureAuthorization(ctx, paypal.CaptureParams{AuthorizationID: "AUTH"})
			return err
		},
		"void":   func() error { return c.VoidAuthorization(ctx, "AUTH", "") },
		"refund": func() error { _, err := c.RefundCapture(ctx, paypal.RefundParams{CaptureID: "CAP"}); return err },
	}
	for op, call := range calls {
		err := call()
		if err == nil || paypal.IsAmbiguous(err) {
			t.Errorf("%s without a request id = %v, want a definite error", op, err)
		}
		if n := len(f.Requests(op)); n != 0 {
			t.Errorf("%s: %d requests reached the server, want 0", op, n)
		}
	}
	if n := len(f.Requests("token")); n != 0 {
		t.Errorf("token requests = %d, want 0", n)
	}
}

func TestClient_ParsesIssueAndDebugID(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f)
	_, auth := authorizedOrder(t, c, f, "1")

	f.FailNext("capture", 422, "AUTHORIZATION_EXPIRED")
	_, err := c.CaptureAuthorization(context.Background(), paypal.CaptureParams{AuthorizationID: auth.ID, RequestID: "capture-1"})
	pe := requirePayPalError(t, err, 422, "UNPROCESSABLE_ENTITY", "AUTHORIZATION_EXPIRED")
	if pe.Message == "" {
		t.Error("Message is empty")
	}
	for _, want := range []string{"422", "UNPROCESSABLE_ENTITY", "AUTHORIZATION_EXPIRED", pe.DebugID} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Error() = %q, want it to contain %q", err.Error(), want)
		}
	}
	if strings.Contains(err.Error(), pe.Message) {
		t.Errorf("Error() = %q must not print the response message", err.Error())
	}
	if paypal.IsAmbiguous(err) {
		t.Error("a 422 must not be ambiguous")
	}
}

func TestClient_NonJSONErrorBodyKeepsStatus(t *testing.T) {
	srv := newStubServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/v1/oauth2/token") {
			fmt.Fprint(w, `{"access_token":"stub-token","expires_in":32400}`)
			return
		}
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprint(w, "<html>bad gateway</html>")
	})
	c, err := paypal.New(paypal.Config{BaseURL: srv.URL, ClientID: "fake-client", ClientSecret: "fake-secret"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.GetOrder(context.Background(), "ORDER")
	var pe *paypal.Error
	if !errors.As(err, &pe) || pe.HTTPStatus != 502 || pe.Name != "" || pe.Issue != "" {
		t.Fatalf("error = %v, want *Error{502} with empty name and issue", err)
	}
	if !paypal.IsAmbiguous(err) {
		t.Error("a 502 must be ambiguous")
	}
	if strings.Contains(err.Error(), "bad gateway") {
		t.Errorf("Error() = %q must not print the body", err.Error())
	}
}

func TestClient_MalformedSuccessBodyIsAmbiguous(t *testing.T) {
	srv := newStubServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/v1/oauth2/token") {
			fmt.Fprint(w, `{"access_token":"stub-token","expires_in":32400}`)
			return
		}
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"id": `)
	})
	c, err := paypal.New(paypal.Config{BaseURL: srv.URL, ClientID: "fake-client", ClientSecret: "fake-secret"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.CaptureAuthorization(context.Background(), paypal.CaptureParams{AuthorizationID: "AUTH", RequestID: "capture-1"})
	if err == nil || !paypal.IsAmbiguous(err) {
		t.Fatalf("capture with a truncated 201 body = %v, want an ambiguous error", err)
	}
}

func TestClient_ShapelessSuccessIsUnconfirmed(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		op   string
		body string
		call func(c *paypal.Client) (any, error)
	}{
		{"create order without an id", "create_order", `{}`, func(c *paypal.Client) (any, error) {
			return c.CreateOrder(ctx, createParams("1"))
		}},
		{"create order with status only", "create_order", `{"status":"PAYER_ACTION_REQUIRED"}`, func(c *paypal.Client) (any, error) {
			return c.CreateOrder(ctx, createParams("1"))
		}},
		{"authorize without purchase units", "authorize", `{"id":"ORDER","status":"COMPLETED"}`, func(c *paypal.Client) (any, error) {
			return c.AuthorizeOrder(ctx, "ORDER", "authorize-1")
		}},
		{"authorize without authorizations", "authorize", `{"id":"ORDER","purchase_units":[{"payments":{}}]}`, func(c *paypal.Client) (any, error) {
			return c.AuthorizeOrder(ctx, "ORDER", "authorize-1")
		}},
		{"authorize with an authorization lacking an id", "authorize", `{"id":"ORDER","purchase_units":[{"payments":{"authorizations":[{"status":"CREATED"}]}}]}`, func(c *paypal.Client) (any, error) {
			return c.AuthorizeOrder(ctx, "ORDER", "authorize-1")
		}},
		{"capture with an empty object", "capture", `{}`, func(c *paypal.Client) (any, error) {
			return c.CaptureAuthorization(ctx, paypal.CaptureParams{AuthorizationID: "AUTH", RequestID: "capture-1"})
		}},
		{"capture with status only", "capture", `{"status":"COMPLETED"}`, func(c *paypal.Client) (any, error) {
			return c.CaptureAuthorization(ctx, paypal.CaptureParams{AuthorizationID: "AUTH", RequestID: "capture-1"})
		}},
		{"refund with an empty object", "refund", `{}`, func(c *paypal.Client) (any, error) {
			return c.RefundCapture(ctx, paypal.RefundParams{CaptureID: "CAP", RequestID: "refund-1"})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake(t)
			c := newTestClient(t, f)
			f.RespondOnce(tc.op, http.StatusCreated, []byte(tc.body))

			got, err := tc.call(c)
			if err == nil {
				t.Fatalf("a 201 with %s was accepted as %+v", tc.body, got)
			}
			if !paypal.IsAmbiguous(err) {
				t.Errorf("error %v is not ambiguous, the worker would not reconcile", err)
			}
			var pe *paypal.Error
			if errors.As(err, &pe) {
				t.Errorf("error %v is a PayPal *Error, want an unconfirmed outcome", err)
			}
			if !reflect.ValueOf(got).IsZero() {
				t.Errorf("returned %+v, want no resource", got)
			}
			if n := len(f.Requests(tc.op)); n != 1 {
				t.Errorf("%s requests = %d, want 1", tc.op, n)
			}
			if n := f.Captures(); n != 0 {
				t.Errorf("Captures() = %d: a canned answer must not run the operation", n)
			}
		})
	}
}

func TestClient_ReadsKeepWhateverPayPalSent(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f)
	ctx := context.Background()

	f.RespondOnce("get_capture", http.StatusOK, []byte(`{}`))
	if got, err := c.GetCapture(ctx, "CAP"); err != nil || got.ID != "" {
		t.Errorf("GetCapture = %+v, %v, want the empty object as sent", got, err)
	}
	f.RespondOnce("get_order", http.StatusOK, []byte(`{}`))
	if got, err := c.GetOrder(ctx, "ORDER"); err != nil || got.ID != "" {
		t.Errorf("GetOrder = %+v, %v, want the empty object as sent", got, err)
	}
}

func TestClient_TimeoutIsAmbiguous(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f, func(cfg *paypal.Config) { cfg.HTTPClient = &http.Client{Timeout: time.Second} })
	_, auth := authorizedOrder(t, c, f, "1")

	f.FailNext("capture", 0, "")
	start := time.Now()
	_, err := c.CaptureAuthorization(context.Background(), paypal.CaptureParams{AuthorizationID: auth.ID, RequestID: "capture-1"})
	if err == nil {
		t.Fatal("capture against a hanging server succeeded")
	}
	if !paypal.IsAmbiguous(err) {
		t.Fatalf("timeout error %v is not ambiguous", err)
	}
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Fatalf("timeout took %v, want about 1s", elapsed)
	}
}

func TestClient_ExpiredOrderIsAPlainNotFound(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f)
	order := createOrder(t, c, "1")

	f.ExpireOrder(order.ID)
	_, err := c.GetOrder(context.Background(), order.ID)
	requirePayPalError(t, err, 404, "RESOURCE_NOT_FOUND", "INVALID_RESOURCE_ID")
	if paypal.IsAmbiguous(err) {
		t.Error("an expired order 404 must not be ambiguous")
	}

	f.Approve(order.ID)
	_, err = c.AuthorizeOrder(context.Background(), order.ID, "authorize-1")
	requirePayPalError(t, err, 404, "RESOURCE_NOT_FOUND", "INVALID_RESOURCE_ID")
}

func TestClient_OAuthTokenRequestAndCache(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f)
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	paypal.SetNow(c, func() time.Time { return now })
	order := createOrder(t, c, "1")

	tokens := f.Requests("token")
	if len(tokens) != 1 {
		t.Fatalf("token requests = %d, want 1", len(tokens))
	}
	user, pass, ok := (&http.Request{Header: tokens[0].Headers}).BasicAuth()
	if !ok || user != "fake-client" || pass != "fake-secret" {
		t.Errorf("token request basic auth = %q/%q/%v, want the configured client credentials", user, pass, ok)
	}
	if got := string(tokens[0].Body); got != "grant_type=client_credentials" {
		t.Errorf("token request body = %q, want grant_type=client_credentials", got)
	}
	if tokens[0].RequestID != "" {
		t.Errorf("token request carries PayPal-Request-Id %q", tokens[0].RequestID)
	}

	get := func() {
		t.Helper()
		if _, err := c.GetOrder(context.Background(), order.ID); err != nil {
			t.Fatalf("GetOrder: %v", err)
		}
	}
	get()
	now = now.Add(32339 * time.Second)
	get()
	if n := len(f.Requests("token")); n != 1 {
		t.Fatalf("token requests before expires_in-60s = %d, want 1", n)
	}
	now = now.Add(2 * time.Second)
	get()
	if n := len(f.Requests("token")); n != 2 {
		t.Fatalf("token requests after expires_in-60s = %d, want 2", n)
	}
}

func TestClient_RefreshesTokenOnceOn401KeepingRequestID(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f)
	order := createOrder(t, c, "1")
	f.Approve(order.ID)

	f.ExpireToken()
	if _, err := c.AuthorizeOrder(context.Background(), order.ID, "authorize-1"); err != nil {
		t.Fatalf("AuthorizeOrder after the token expired: %v", err)
	}
	reqs := f.Requests("authorize")
	if len(reqs) != 2 {
		t.Fatalf("authorize requests = %d, want the original and one retry", len(reqs))
	}
	if reqs[0].RequestID != "authorize-1" || reqs[1].RequestID != "authorize-1" {
		t.Errorf("request ids = %q, %q, want the same key twice", reqs[0].RequestID, reqs[1].RequestID)
	}
	if n := len(f.Requests("token")); n != 2 {
		t.Errorf("token requests = %d, want the initial one and one refresh", n)
	}
}

func TestClient_GivesUpAfterOneRefresh(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f)
	order := createOrder(t, c, "1")
	f.Approve(order.ID)

	f.FailNext("authorize", 401, "")
	f.FailNext("authorize", 401, "")
	_, err := c.AuthorizeOrder(context.Background(), order.ID, "authorize-1")
	var pe *paypal.Error
	if !errors.As(err, &pe) || pe.HTTPStatus != 401 {
		t.Fatalf("error = %v, want *Error with status 401", err)
	}
	if paypal.IsAmbiguous(err) {
		t.Error("a 401 must not be ambiguous")
	}
	if n := len(f.Requests("authorize")); n != 2 {
		t.Errorf("authorize requests = %d, want exactly 2", n)
	}
	if n := len(f.Requests("token")); n != 2 {
		t.Errorf("token requests = %d, want exactly 2", n)
	}
}

func TestClient_TokenFailureNeverLeaksCredentials(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f)

	f.FailNext("token", 500, "")
	_, err := c.GetOrder(context.Background(), "ORDER")
	var pe *paypal.Error
	if !errors.As(err, &pe) || pe.HTTPStatus != 500 {
		t.Fatalf("error = %v, want *Error with status 500", err)
	}
	for _, secret := range []string{"fake-secret", "fake-token", "Basic ", "Bearer "} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("error %q leaks %q", err.Error(), secret)
		}
	}
}

func TestClient_CreateOrderBodyMatchesContract(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f)
	order := createOrder(t, c, "1")

	var got map[string]any
	if err := json.Unmarshal(f.Requests("create_order")[0].Body, &got); err != nil {
		t.Fatalf("create order body is not JSON: %v", err)
	}
	var want map[string]any
	if err := json.Unmarshal([]byte(`{
		"intent": "AUTHORIZE",
		"purchase_units": [{
			"reference_id": "attempt-1",
			"custom_id": "enrollment-1",
			"amount": {"currency_code": "USD", "value": "10.00"}
		}],
		"payment_source": {"paypal": {"experience_context": {
			"brand_name": "CommitPool",
			"locale": "en-US",
			"user_action": "CONTINUE",
			"shipping_preference": "NO_SHIPPING",
			"return_url": "https://app.example/paypal/return",
			"cancel_url": "https://app.example/paypal/cancel"
		}}}
	}`), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("create order body = %v, want %v", got, want)
	}

	if order.Status != "PAYER_ACTION_REQUIRED" {
		t.Errorf("order status = %q, want PAYER_ACTION_REQUIRED", order.Status)
	}
	pu := order.PurchaseUnits[0]
	if pu.ReferenceID != "attempt-1" || pu.CustomID != "enrollment-1" || pu.Amount != usd10 {
		t.Errorf("purchase unit = %+v, want the echoed reference, custom id and amount", pu)
	}
}

func TestClient_ActionBodies(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f)
	ctx := context.Background()
	_, capture := capturedOrder(t, c, f, "1")
	if _, err := c.RefundCapture(ctx, paypal.RefundParams{CaptureID: capture.ID, RequestID: "refund-1"}); err != nil {
		t.Fatal(err)
	}
	_, auth := authorizedOrder(t, c, f, "2")
	if err := c.VoidAuthorization(ctx, auth.ID, "void-2"); err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		"capture": `{"final_capture":true}`,
		"refund":  `{}`,
	}
	for op, body := range want {
		if got := string(f.Requests(op)[0].Body); got != body {
			t.Errorf("%s body = %s, want %s", op, got, body)
		}
	}
	for _, op := range []string{"authorize", "void"} {
		if got := string(f.Requests(op)[0].Body); got != `{}` {
			t.Errorf("%s body = %s, want {}", op, got)
		}
	}
}

func TestClient_ReadsEveryResourceBack(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f)
	ctx := context.Background()
	auth, capture := capturedOrder(t, c, f, "1")
	refund, err := c.RefundCapture(ctx, paypal.RefundParams{CaptureID: capture.ID, RequestID: "refund-1"})
	if err != nil {
		t.Fatal(err)
	}
	f.SetCaptureStatus(capture.ID, "PENDING", "PENDING_REVIEW")
	f.SetRefundStatus(refund.ID, "PENDING")

	gotAuth, err := c.GetAuthorization(ctx, auth.ID)
	if err != nil || gotAuth.Status != "CAPTURED" || gotAuth.Amount != usd10 {
		t.Errorf("GetAuthorization = %+v, %v, want CAPTURED for 10.00 USD", gotAuth, err)
	}
	if d := time.Until(gotAuth.ExpirationTime); d < 28*24*time.Hour || d > 30*24*time.Hour {
		t.Errorf("authorization expires in %v, want about 29 days", d)
	}
	gotCapture, err := c.GetCapture(ctx, capture.ID)
	if err != nil || gotCapture.Status != "PENDING" || gotCapture.StatusDetails == nil || gotCapture.StatusDetails.Reason != "PENDING_REVIEW" {
		t.Errorf("GetCapture = %+v, %v, want PENDING with reason PENDING_REVIEW", gotCapture, err)
	}
	gotRefund, err := c.GetRefund(ctx, refund.ID)
	if err != nil || gotRefund.Status != "PENDING" {
		t.Errorf("GetRefund = %+v, %v, want PENDING", gotRefund, err)
	}

	_, err = c.GetOrder(ctx, "unknown")
	requirePayPalError(t, err, 404, "RESOURCE_NOT_FOUND", "INVALID_RESOURCE_ID")
}

func TestClient_VerifyWebhookSignature(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f)
	ctx := context.Background()
	h := http.Header{}
	h.Set("PAYPAL-AUTH-ALGO", "SHA256withRSA")
	h.Set("PAYPAL-CERT-URL", "https://api.sandbox.paypal.com/v1/notifications/certs/CERT")
	h.Set("PAYPAL-TRANSMISSION-ID", "tid-1")
	h.Set("PAYPAL-TRANSMISSION-SIG", "sig-1")
	h.Set("PAYPAL-TRANSMISSION-TIME", "2026-10-27T10:00:00Z")
	event := `{"id":"WH-1",  "event_type":"PAYMENT.CAPTURE.COMPLETED", "resource":{"id":"CAP"}}`

	ok, err := c.VerifyWebhookSignature(ctx, h, []byte(event))
	if err != nil || !ok {
		t.Fatalf("VerifyWebhookSignature = %v, %v, want true", ok, err)
	}
	req := f.Requests("verify_webhook")[0]
	if req.RequestID != "" || req.Headers.Get("Prefer") != "" {
		t.Errorf("verification sent PayPal-Request-Id %q / Prefer %q, want neither", req.RequestID, req.Headers.Get("Prefer"))
	}
	if !strings.Contains(string(req.Body), `"webhook_event":`+event) {
		t.Errorf("body %s does not carry the event bytes untouched", req.Body)
	}
	var sent map[string]json.RawMessage
	if err := json.Unmarshal(req.Body, &sent); err != nil {
		t.Fatal(err)
	}
	wantStrings := map[string]string{
		"auth_algo":         "SHA256withRSA",
		"cert_url":          "https://api.sandbox.paypal.com/v1/notifications/certs/CERT",
		"transmission_id":   "tid-1",
		"transmission_sig":  "sig-1",
		"transmission_time": "2026-10-27T10:00:00Z",
		"webhook_id":        "fake-webhook",
	}
	for k, want := range wantStrings {
		var got string
		if err := json.Unmarshal(sent[k], &got); err != nil || got != want {
			t.Errorf("%s = %s, want %q", k, sent[k], want)
		}
	}

	f.SetVerify(false)
	ok, err = c.VerifyWebhookSignature(ctx, h, []byte(event))
	if err != nil || ok {
		t.Fatalf("VerifyWebhookSignature with a failing verification = %v, %v, want false and no error", ok, err)
	}

	before := len(f.Requests("verify_webhook"))
	if _, err := c.VerifyWebhookSignature(ctx, h, []byte("not json")); err == nil {
		t.Error("a body that is not JSON must be rejected")
	}
	if n := len(f.Requests("verify_webhook")); n != before {
		t.Error("a body that is not JSON must not be sent")
	}
}

func TestOrder_ApproveURLPrefersPayerAction(t *testing.T) {
	cases := []struct {
		name  string
		links []paypal.Link
		want  string
	}{
		{"payer-action", []paypal.Link{{Rel: "self", Href: "https://x/self"}, {Rel: "payer-action", Href: "https://x/payer"}}, "https://x/payer"},
		{"approve fallback", []paypal.Link{{Rel: "self", Href: "https://x/self"}, {Rel: "approve", Href: "https://x/approve"}}, "https://x/approve"},
		{"payer-action wins over approve", []paypal.Link{{Rel: "approve", Href: "https://x/approve"}, {Rel: "payer-action", Href: "https://x/payer"}}, "https://x/payer"},
		{"none", []paypal.Link{{Rel: "self", Href: "https://x/self"}}, ""},
		{"no links", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := (paypal.Order{Links: tc.links}).ApproveURL(); got != tc.want {
				t.Errorf("ApproveURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

type timeoutNetError struct{}

func (timeoutNetError) Error() string   { return "i/o timeout" }
func (timeoutNetError) Timeout() bool   { return true }
func (timeoutNetError) Temporary() bool { return true }

func TestIsAmbiguous(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"context deadline", context.DeadlineExceeded, true},
		{"wrapped context deadline", fmt.Errorf("capture: %w", context.DeadlineExceeded), true},
		{"net timeout", timeoutNetError{}, true},
		{"net dns error", &net.DNSError{Err: "no such host", Name: "api-m.sandbox.paypal.com"}, true},
		{"url error from the transport", &url.Error{Op: "Post", URL: "https://x", Err: errors.New("connection reset by peer")}, true},
		{"500", &paypal.Error{HTTPStatus: 500}, true},
		{"503 wrapped", fmt.Errorf("worker: %w", &paypal.Error{HTTPStatus: 503}), true},
		{"409", &paypal.Error{HTTPStatus: 409, Issue: "PREVIOUS_REQUEST_IN_PROGRESS"}, true},
		{"422", &paypal.Error{HTTPStatus: 422, Issue: "AUTHORIZATION_VOIDED"}, false},
		{"429", &paypal.Error{HTTPStatus: 429}, false},
		{"404", &paypal.Error{HTTPStatus: 404, Name: "RESOURCE_NOT_FOUND", Issue: "INVALID_RESOURCE_ID"}, false},
		{"401", &paypal.Error{HTTPStatus: 401}, false},
		{"plain error", errors.New("paypal: request id is required"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := paypal.IsAmbiguous(tc.err); got != tc.want {
				t.Errorf("IsAmbiguous(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestNew_Validation(t *testing.T) {
	if _, err := paypal.New(paypal.Config{ClientID: "fake-client", ClientSecret: "fake-secret"}); err == nil {
		t.Error("New without a base URL must fail")
	}
	c, err := paypal.New(paypal.Config{BaseURL: "https://api-m.sandbox.paypal.com/", ClientID: "fake-client", ClientSecret: "fake-secret", SandboxMocks: true})
	if err != nil {
		t.Fatalf("New with a sandbox URL and mocks: %v", err)
	}
	if got := paypal.BaseURL(c); got != "https://api-m.sandbox.paypal.com" {
		t.Errorf("base = %q, want the trailing slash trimmed", got)
	}
	if hc := paypal.HTTPClient(c); hc == nil || hc.Timeout != 20*time.Second {
		t.Errorf("default HTTP client = %+v, want a 20s timeout", hc)
	}
}

func TestDefinitive_OnlyFinalAnswersAboutTheResource(t *testing.T) {
	cases := []struct {
		name  string
		err   error
		final bool
	}{
		{"422 refusal", &paypal.Error{HTTPStatus: 422, Issue: "INSTRUMENT_DECLINED"}, true},
		{"404 missing order", &paypal.Error{HTTPStatus: 404, Issue: paypal.IssueInvalidResourceID}, true},
		{"400 malformed", &paypal.Error{HTTPStatus: 400}, true},
		{"wrapped 422", fmt.Errorf("capture: %w", &paypal.Error{HTTPStatus: 422}), true},
		{"401 credentials", &paypal.Error{HTTPStatus: 401}, false},
		{"403 credentials", &paypal.Error{HTTPStatus: 403}, false},
		{"409 in progress", &paypal.Error{HTTPStatus: 409}, false},
		{"429 rate limited", &paypal.Error{HTTPStatus: 429}, false},
		{"500", &paypal.Error{HTTPStatus: 500}, false},
		{"timeout", context.DeadlineExceeded, false},
		{"none", nil, false},
	}
	for _, c := range cases {
		if got := paypal.Definitive(c.err); (got != nil) != c.final {
			t.Errorf("%s: Definitive = %v, want final %v", c.name, got, c.final)
		}
	}
}

func TestClient_DecodesTheSellerReceivableBreakdown(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f)
	ctx := context.Background()

	f.RespondOnce("get_capture", http.StatusOK, []byte(`{
		"id": "2GG279541U471931P",
		"status": "COMPLETED",
		"amount": {"currency_code": "USD", "value": "45.00"},
		"final_capture": true,
		"seller_protection": {"status": "ELIGIBLE"},
		"seller_receivable_breakdown": {
			"gross_amount": {"currency_code": "USD", "value": "45.00"},
			"paypal_fee": {"currency_code": "USD", "value": "2.06"},
			"net_amount": {"currency_code": "USD", "value": "42.94"}
		}
	}`))
	got, err := c.GetCapture(ctx, "2GG279541U471931P")
	if err != nil {
		t.Fatal(err)
	}
	b := got.SellerReceivableBreakdown
	if b == nil || b.GrossAmount == nil || *b.GrossAmount != (paypal.Money{CurrencyCode: "USD", Value: "45.00"}) ||
		b.PayPalFee == nil || *b.PayPalFee != (paypal.Money{CurrencyCode: "USD", Value: "2.06"}) ||
		b.NetAmount == nil || *b.NetAmount != (paypal.Money{CurrencyCode: "USD", Value: "42.94"}) || b.ReceivableAmount != nil {
		t.Errorf("breakdown = %+v, want 45.00 gross, 2.06 fee, 42.94 net and no receivable amount", b)
	}

	f.RespondOnce("get_capture", http.StatusOK, []byte(`{
		"id": "3C679366HH908993F",
		"status": "COMPLETED",
		"amount": {"currency_code": "USD", "value": "45.00"},
		"seller_receivable_breakdown": {
			"gross_amount": {"currency_code": "USD", "value": "45.00"},
			"paypal_fee": {"currency_code": "USD", "value": "2.06"},
			"net_amount": {"currency_code": "USD", "value": "42.94"},
			"receivable_amount": {"currency_code": "EUR", "value": "39.50"},
			"exchange_rate": {"source_currency": "USD", "target_currency": "EUR", "value": "0.92"}
		}
	}`))
	converted, err := c.GetCapture(ctx, "3C679366HH908993F")
	if err != nil {
		t.Fatal(err)
	}
	if b := converted.SellerReceivableBreakdown; b == nil || b.ReceivableAmount == nil || *b.ReceivableAmount != (paypal.Money{CurrencyCode: "EUR", Value: "39.50"}) {
		t.Errorf("converted breakdown = %+v, want the receivable amount in EUR", b)
	}

	f.RespondOnce("get_capture", http.StatusOK, []byte(`{"id": "PENDINGCAPTURE", "status": "PENDING", "amount": {"currency_code": "USD", "value": "45.00"}}`))
	pending, err := c.GetCapture(ctx, "PENDINGCAPTURE")
	if err != nil || pending.SellerReceivableBreakdown != nil {
		t.Errorf("capture without a breakdown = %+v, %v; want no breakdown", pending, err)
	}
}

func TestCreateOrder_SendsBrandAndDescription(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f)
	p := createParams("1")
	p.Description = paypal.OrderDescription("Go for operators", "Sat, Nov 14, 10 AM–12 PM (New York time)")
	p.SoftDescriptor = paypal.SoftDescriptor

	order, err := c.CreateOrder(context.Background(), p)
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}

	reqs := f.Requests("create_order")
	if len(reqs) != 1 {
		t.Fatalf("create_order requests = %d, want 1", len(reqs))
	}
	var body struct {
		PurchaseUnits []struct {
			Description    string            `json:"description"`
			SoftDescriptor string            `json:"soft_descriptor"`
			Items          []json.RawMessage `json:"items"`
		} `json:"purchase_units"`
		PaymentSource struct {
			PayPal struct {
				ExperienceContext struct {
					BrandName string `json:"brand_name"`
					Locale    string `json:"locale"`
				} `json:"experience_context"`
			} `json:"paypal"`
		} `json:"payment_source"`
	}
	if err := json.Unmarshal(reqs[0].Body, &body); err != nil {
		t.Fatalf("create_order body: %v", err)
	}
	ec := body.PaymentSource.PayPal.ExperienceContext
	if ec.BrandName != "CommitPool" || ec.Locale != "en-US" {
		t.Errorf("experience_context brand_name/locale = %q/%q, want CommitPool/en-US", ec.BrandName, ec.Locale)
	}
	unit := body.PurchaseUnits[0]
	if want := "Go for operators · Sat, Nov 14, 10 AM–12 PM (New York time) · hold for 1 spot"; unit.Description != want {
		t.Errorf("description = %q, want %q", unit.Description, want)
	}
	if unit.SoftDescriptor != "COMMITPOOL" {
		t.Errorf("soft_descriptor = %q, want COMMITPOOL", unit.SoftDescriptor)
	}
	if len(unit.Items) != 0 {
		t.Errorf("items = %d, want none", len(unit.Items))
	}
	if got := order.PurchaseUnits[0].Description; got != p.Description {
		t.Errorf("order description = %q, want the fake to echo %q", got, p.Description)
	}
}

func TestOrderDescription_Truncates(t *testing.T) {
	title := strings.Repeat("é", 200)
	got := paypal.OrderDescription(title, "Sat, Nov 14, 10 AM–12 PM (New York time)")
	if n := len([]rune(got)); n > 127 {
		t.Errorf("description runes = %d, want at most 127", n)
	}
	if !strings.HasSuffix(got, " · Sat, Nov 14, 10 AM–12 PM (New York time) · hold for 1 spot") {
		t.Errorf("description = %q, want the date and the hold kept after a long title", got)
	}
	if n := len([]rune(paypal.OrderDescription("Go", strings.Repeat("x", 300)))); n != 127 {
		t.Errorf("description with an oversized date = %d runes, want 127", n)
	}
}

func TestOrderDescription_CollapsesNewlines(t *testing.T) {
	got := paypal.OrderDescription("Go\n\nfor \t operators\r\n\x00now", "Sat, Nov 14")
	if want := "Go for operators now · Sat, Nov 14 · hold for 1 spot"; got != want {
		t.Errorf("description = %q, want %q", got, want)
	}
}

func TestOrderDescription_EmojiTitleFitsUTF16Limit(t *testing.T) {
	label := "Sat, Nov 14, 10 AM–12 PM (New York time)"
	got := paypal.OrderDescription(strings.Repeat("🎨", 120), label)
	if n := len(utf16.Encode([]rune(got))); n > 127 {
		t.Errorf("description UTF-16 units = %d, want at most 127", n)
	}
	if !strings.HasSuffix(got, " · "+label+" · hold for 1 spot") {
		t.Errorf("description = %q, want the date and the hold kept", got)
	}
	if !utf8.ValidString(got) || strings.ContainsRune(got, '\uFFFD') {
		t.Errorf("description = %q, want valid text", got)
	}
	if n := len(utf16.Encode([]rune(paypal.OrderDescription(strings.Repeat("🎨", 120), strings.Repeat("🎨", 100))))); n > 127 {
		t.Errorf("description with an oversized emoji date = %d units, want at most 127", n)
	}
}
