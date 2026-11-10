package paypaltest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/paypal"
)

var noRedirect = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

func waitFor(t *testing.T, limit time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("condition not met within %v", limit)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestFake_URLIsUnderASandboxPath(t *testing.T) {
	f := newFake(t)
	if !strings.Contains(f.URL, "sandbox") {
		t.Fatalf("URL = %q, want it to contain %q so SandboxMocks clients accept it", f.URL, "sandbox")
	}
}

func TestFake_SameRequestIdReturnsSameCapture(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f)
	_, auth := authorizedOrder(t, c, f, "1")
	ctx := context.Background()

	first, err := c.CaptureAuthorization(ctx, paypal.CaptureParams{AuthorizationID: auth.ID, RequestID: "capture-1"})
	if err != nil {
		t.Fatalf("first capture: %v", err)
	}
	second, err := c.CaptureAuthorization(ctx, paypal.CaptureParams{AuthorizationID: auth.ID, RequestID: "capture-1"})
	if err != nil {
		t.Fatalf("second capture with the same key: %v", err)
	}
	if first.ID == "" || first.ID != second.ID {
		t.Fatalf("capture ids = %q and %q, want the same one", first.ID, second.ID)
	}
	if first.Status != "COMPLETED" || first.Amount != usd10 {
		t.Errorf("capture = %+v, want COMPLETED for 10.00 USD", first)
	}
	if n := f.Captures(); n != 1 {
		t.Fatalf("Captures() = %d, want 1", n)
	}
}

func TestFake_SameRequestIdReturnsSameOrder(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f)
	orderCount := func() int {
		f.mu.Lock()
		defer f.mu.Unlock()
		return len(f.orders)
	}

	first := createOrder(t, c, "1")
	second := createOrder(t, c, "1")
	if first.ID == "" || first.ID != second.ID {
		t.Fatalf("order ids = %q and %q, want the same one", first.ID, second.ID)
	}
	if n := orderCount(); n != 1 {
		t.Fatalf("orders created = %d, want 1", n)
	}
	if other := createOrder(t, c, "2"); other.ID == first.ID {
		t.Fatal("a different key returned the same order")
	}

	f.FailNext("create_order", 500, "")
	if _, err := c.CreateOrder(context.Background(), createParams("3")); err == nil {
		t.Fatal("injected 500 was not returned")
	}
	if n := orderCount(); n != 2 {
		t.Fatalf("orders after an injected failure = %d, want 2 (the operation must not run)", n)
	}
	retried := createOrder(t, c, "3")
	if n := orderCount(); n != 3 || retried.ID == "" {
		t.Fatalf("orders after the same-key retry = %d, want 3: a failure is not recorded for replay", n)
	}
}

func TestFake_VoidAfterCaptureIs422(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f)
	auth, _ := capturedOrder(t, c, f, "1")

	err := c.VoidAuthorization(context.Background(), auth.ID, "void-1")
	requirePayPalError(t, err, 422, "UNPROCESSABLE_ENTITY", "PREVIOUSLY_CAPTURED")
	if paypal.IsAmbiguous(err) {
		t.Error("a 422 must not be ambiguous")
	}
}

func TestFake_ApprovePageRedirectsToReturnURL(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f)
	order := createOrder(t, c, "1")

	approveURL := order.ApproveURL()
	if want := f.URL + "/approve?order=" + order.ID; approveURL != want {
		t.Fatalf("ApproveURL() = %q, want %q", approveURL, want)
	}
	resp, err := noRedirect.Get(approveURL)
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("GET /approve = %d %q, want 200 text/html", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	for _, want := range []string{`method="post"`, `action="/sandbox/approve"`, `name="order"`, `value="` + order.ID + `"`, "Approve"} {
		if !strings.Contains(string(page), want) {
			t.Errorf("approve page does not contain %q:\n%s", want, page)
		}
	}
	got, err := c.GetOrder(context.Background(), order.ID)
	if err != nil || got.Status != "PAYER_ACTION_REQUIRED" {
		t.Fatalf("order after only viewing the page = %q, %v, want PAYER_ACTION_REQUIRED", got.Status, err)
	}

	resp, err = noRedirect.PostForm(f.URL+"/approve", url.Values{"order": {order.ID}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("POST /approve = %d, want 302", resp.StatusCode)
	}
	if got, want := resp.Header.Get("Location"), testReturnURL+"?token="+order.ID+"&PayerID=FAKE"; got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}
	got, err = c.GetOrder(context.Background(), order.ID)
	if err != nil || got.Status != "APPROVED" {
		t.Fatalf("order after approving = %q, %v, want APPROVED", got.Status, err)
	}

	withQuery, err := c.CreateOrder(context.Background(), paypal.CreateOrderParams{
		RequestID: "create-q", ReferenceID: "attempt-q", CustomID: "enrollment-q", Amount: usd10,
		ReturnURL: "https://app.example/paypal/return?attempt=q", CancelURL: testCancelURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, err = noRedirect.PostForm(f.URL+"/approve", url.Values{"order": {withQuery.ID}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got, want := resp.Header.Get("Location"), "https://app.example/paypal/return?attempt=q&token="+withQuery.ID+"&PayerID=FAKE"; got != want {
		t.Errorf("Location with an existing query = %q, want %q", got, want)
	}
}

func TestFake_CancelPageRedirectsToCancelURL(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f)
	order := createOrder(t, c, "1")

	resp, err := noRedirect.Get(f.URL + "/cancel?order=" + order.ID)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("GET /cancel = %d, want 302", resp.StatusCode)
	}
	if got, want := resp.Header.Get("Location"), testCancelURL+"?token="+order.ID; got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}
	got, err := c.GetOrder(context.Background(), order.ID)
	if err != nil || got.Status != "PAYER_ACTION_REQUIRED" {
		t.Errorf("order after cancelling = %q, %v, want it untouched", got.Status, err)
	}

	resp, err = noRedirect.Get(f.URL + "/approve?order=NOPE")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET /approve for an unknown order = %d, want 404", resp.StatusCode)
	}
}

func TestFake_RequiresTheBearerToken(t *testing.T) {
	f := newFake(t)
	for _, auth := range []string{"", "Bearer nope"} {
		req, _ := http.NewRequest(http.MethodGet, f.URL+"/v2/checkout/orders/ORDER", nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Authorization %q: status = %d, want 401", auth, resp.StatusCode)
		}
	}
}

func TestFake_RejectsAMalformedOrder(t *testing.T) {
	f := newFake(t)
	for name, body := range map[string]string{
		"not json":      `{`,
		"wrong intent":  `{"intent":"CAPTURE","purchase_units":[{"amount":{"currency_code":"USD","value":"10.00"}}]}`,
		"no amount":     `{"intent":"AUTHORIZE","purchase_units":[{"reference_id":"a"}]}`,
		"no units":      `{"intent":"AUTHORIZE"}`,
		"two units":     `{"intent":"AUTHORIZE","purchase_units":[{"amount":{"currency_code":"USD","value":"1.00"}},{"amount":{"currency_code":"USD","value":"1.00"}}]}`,
		"empty request": ``,
	} {
		req, _ := http.NewRequest(http.MethodPost, f.URL+"/v2/checkout/orders", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer fake-token")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, resp.StatusCode)
		}
	}
}

func TestFake_BusinessRules(t *testing.T) {
	type fixture struct {
		f *Server
		c *paypal.Client
	}
	ctx := context.Background()
	cases := []struct {
		name   string
		run    func(t *testing.T, x fixture) error
		status int
		pp     string
		issue  string
	}{
		{"authorize an order the buyer never approved", func(t *testing.T, x fixture) error {
			o := createOrder(t, x.c, "1")
			_, err := x.c.AuthorizeOrder(ctx, o.ID, "authorize-1")
			return err
		}, 422, "UNPROCESSABLE_ENTITY", "ORDER_NOT_APPROVED"},
		{"authorize an order that is already completed under a new key", func(t *testing.T, x fixture) error {
			o, _ := authorizedOrder(t, x.c, x.f, "1")
			_, err := x.c.AuthorizeOrder(ctx, o.ID, "authorize-other")
			return err
		}, 404, "RESOURCE_NOT_FOUND", "INVALID_RESOURCE_ID"},
		{"authorize an unknown order", func(t *testing.T, x fixture) error {
			_, err := x.c.AuthorizeOrder(ctx, "NOPE", "authorize-1")
			return err
		}, 404, "RESOURCE_NOT_FOUND", "INVALID_RESOURCE_ID"},
		{"capture a voided authorization", func(t *testing.T, x fixture) error {
			_, auth := authorizedOrder(t, x.c, x.f, "1")
			if err := x.c.VoidAuthorization(ctx, auth.ID, "void-1"); err != nil {
				t.Fatal(err)
			}
			_, err := x.c.CaptureAuthorization(ctx, paypal.CaptureParams{AuthorizationID: auth.ID, RequestID: "capture-1"})
			return err
		}, 422, "UNPROCESSABLE_ENTITY", "AUTHORIZATION_VOIDED"},
		{"capture an authorization twice under different keys", func(t *testing.T, x fixture) error {
			auth, _ := capturedOrder(t, x.c, x.f, "1")
			_, err := x.c.CaptureAuthorization(ctx, paypal.CaptureParams{AuthorizationID: auth.ID, RequestID: "capture-other"})
			return err
		}, 422, "UNPROCESSABLE_ENTITY", "AUTHORIZATION_ALREADY_CAPTURED"},
		{"capture with the AUTHORIZATION_EXPIRED mock", func(t *testing.T, x fixture) error {
			_, auth := authorizedOrder(t, x.c, x.f, "1")
			_, err := x.c.CaptureAuthorization(ctx, paypal.CaptureParams{AuthorizationID: auth.ID, RequestID: "capture-1", MockCode: "AUTHORIZATION_EXPIRED"})
			return err
		}, 422, "UNPROCESSABLE_ENTITY", "AUTHORIZATION_EXPIRED"},
		{"capture with the TRANSACTION_REFUSED mock", func(t *testing.T, x fixture) error {
			_, auth := authorizedOrder(t, x.c, x.f, "1")
			_, err := x.c.CaptureAuthorization(ctx, paypal.CaptureParams{AuthorizationID: auth.ID, RequestID: "capture-1", MockCode: "TRANSACTION_REFUSED"})
			return err
		}, 422, "UNPROCESSABLE_ENTITY", "TRANSACTION_REFUSED"},
		{"capture an unknown authorization", func(t *testing.T, x fixture) error {
			_, err := x.c.CaptureAuthorization(ctx, paypal.CaptureParams{AuthorizationID: "NOPE", RequestID: "capture-1"})
			return err
		}, 404, "RESOURCE_NOT_FOUND", "INVALID_RESOURCE_ID"},
		{"void a captured authorization", func(t *testing.T, x fixture) error {
			auth, _ := capturedOrder(t, x.c, x.f, "1")
			return x.c.VoidAuthorization(ctx, auth.ID, "void-1")
		}, 422, "UNPROCESSABLE_ENTITY", "PREVIOUSLY_CAPTURED"},
		{"void an authorization twice under different keys", func(t *testing.T, x fixture) error {
			_, auth := authorizedOrder(t, x.c, x.f, "1")
			if err := x.c.VoidAuthorization(ctx, auth.ID, "void-1"); err != nil {
				t.Fatal(err)
			}
			return x.c.VoidAuthorization(ctx, auth.ID, "void-other")
		}, 422, "UNPROCESSABLE_ENTITY", "PREVIOUSLY_VOIDED"},
		{"void an unknown authorization", func(t *testing.T, x fixture) error {
			return x.c.VoidAuthorization(ctx, "NOPE", "void-1")
		}, 404, "RESOURCE_NOT_FOUND", "INVALID_RESOURCE_ID"},
		{"refund a capture twice under different keys", func(t *testing.T, x fixture) error {
			_, cp := capturedOrder(t, x.c, x.f, "1")
			if _, err := x.c.RefundCapture(ctx, paypal.RefundParams{CaptureID: cp.ID, RequestID: "refund-1"}); err != nil {
				t.Fatal(err)
			}
			_, err := x.c.RefundCapture(ctx, paypal.RefundParams{CaptureID: cp.ID, RequestID: "refund-other"})
			return err
		}, 422, "UNPROCESSABLE_ENTITY", "CAPTURE_FULLY_REFUNDED"},
		{"refund with the REFUND_AMOUNT_EXCEEDED mock", func(t *testing.T, x fixture) error {
			_, cp := capturedOrder(t, x.c, x.f, "1")
			_, err := x.c.RefundCapture(ctx, paypal.RefundParams{CaptureID: cp.ID, RequestID: "refund-1", MockCode: "REFUND_AMOUNT_EXCEEDED"})
			return err
		}, 422, "UNPROCESSABLE_ENTITY", "REFUND_AMOUNT_EXCEEDED"},
		{"refund an unknown capture", func(t *testing.T, x fixture) error {
			_, err := x.c.RefundCapture(ctx, paypal.RefundParams{CaptureID: "NOPE", RequestID: "refund-1"})
			return err
		}, 404, "RESOURCE_NOT_FOUND", "INVALID_RESOURCE_ID"},
		{"read an unknown authorization", func(t *testing.T, x fixture) error {
			_, err := x.c.GetAuthorization(ctx, "NOPE")
			return err
		}, 404, "RESOURCE_NOT_FOUND", "INVALID_RESOURCE_ID"},
		{"read an unknown capture", func(t *testing.T, x fixture) error {
			_, err := x.c.GetCapture(ctx, "NOPE")
			return err
		}, 404, "RESOURCE_NOT_FOUND", "INVALID_RESOURCE_ID"},
		{"read an unknown refund", func(t *testing.T, x fixture) error {
			_, err := x.c.GetRefund(ctx, "NOPE")
			return err
		}, 404, "RESOURCE_NOT_FOUND", "INVALID_RESOURCE_ID"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake(t)
			err := tc.run(t, fixture{f: f, c: newTestClient(t, f)})
			requirePayPalError(t, err, tc.status, tc.pp, tc.issue)
			if paypal.IsAmbiguous(err) {
				t.Errorf("%v must not be ambiguous", err)
			}
		})
	}
}

func TestFake_MockedCaptureCreatesNothingAndUnknownMocksAreIgnored(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f)
	_, auth := authorizedOrder(t, c, f, "1")
	ctx := context.Background()

	_, err := c.CaptureAuthorization(ctx, paypal.CaptureParams{AuthorizationID: auth.ID, RequestID: "capture-1", MockCode: "TRANSACTION_REFUSED"})
	requirePayPalError(t, err, 422, "UNPROCESSABLE_ENTITY", "TRANSACTION_REFUSED")
	if n := f.Captures(); n != 0 {
		t.Fatalf("Captures() after a mocked refusal = %d, want 0", n)
	}
	got, err := c.GetAuthorization(ctx, auth.ID)
	if err != nil || got.Status != "CREATED" {
		t.Fatalf("authorization after a mocked refusal = %q, %v, want CREATED", got.Status, err)
	}

	cp, err := c.CaptureAuthorization(ctx, paypal.CaptureParams{AuthorizationID: auth.ID, RequestID: "capture-2", MockCode: "SOMETHING_UNKNOWN"})
	if err != nil || cp.Status != "COMPLETED" {
		t.Fatalf("capture with an unknown mock code = %+v, %v, want the real operation", cp, err)
	}
	if n := f.Captures(); n != 1 {
		t.Fatalf("Captures() = %d, want 1", n)
	}
}

func TestFake_ReplayIsVerbatimEvenAfterTheResourceChanged(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f)
	ctx := context.Background()

	order, auth := authorizedOrder(t, c, f, "1")
	replayed, err := c.AuthorizeOrder(ctx, order.ID, "authorize-1")
	if err != nil {
		t.Fatalf("authorize replay on a completed order: %v", err)
	}
	if got := replayed.PurchaseUnits[0].Payments.Authorizations[0].ID; got != auth.ID {
		t.Errorf("authorize replay returned authorization %q, want %q", got, auth.ID)
	}

	first, err := c.CaptureAuthorization(ctx, paypal.CaptureParams{AuthorizationID: auth.ID, RequestID: "capture-1"})
	if err != nil {
		t.Fatal(err)
	}
	f.SetCaptureStatus(first.ID, "PENDING", "PENDING_REVIEW")
	again, err := c.CaptureAuthorization(ctx, paypal.CaptureParams{AuthorizationID: auth.ID, RequestID: "capture-1"})
	if err != nil {
		t.Fatalf("capture replay on a captured authorization: %v", err)
	}
	if !reflect.DeepEqual(again, first) {
		t.Errorf("capture replay = %+v, want the recorded %+v", again, first)
	}

	refund, err := c.RefundCapture(ctx, paypal.RefundParams{CaptureID: first.ID, RequestID: "refund-1"})
	if err != nil {
		t.Fatal(err)
	}
	refundAgain, err := c.RefundCapture(ctx, paypal.RefundParams{CaptureID: first.ID, RequestID: "refund-1"})
	if err != nil || refundAgain != refund {
		t.Errorf("refund replay = %+v, %v, want the recorded %+v", refundAgain, err, refund)
	}

	_, other := authorizedOrder(t, c, f, "2")
	if err := c.VoidAuthorization(ctx, other.ID, "void-2"); err != nil {
		t.Fatal(err)
	}
	if err := c.VoidAuthorization(ctx, other.ID, "void-2"); err != nil {
		t.Errorf("void replay on a voided authorization: %v", err)
	}
}

func TestFake_FailuresAreNotRecordedForReplay(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f)
	_, auth := authorizedOrder(t, c, f, "1")
	ctx := context.Background()

	f.FailNext("capture", 500, "")
	_, err := c.CaptureAuthorization(ctx, paypal.CaptureParams{AuthorizationID: auth.ID, RequestID: "capture-1"})
	var pe *paypal.Error
	if !errors.As(err, &pe) || pe.HTTPStatus != 500 {
		t.Fatalf("first capture = %v, want a 500", err)
	}
	if n := f.Captures(); n != 0 {
		t.Fatalf("Captures() after an injected 500 = %d, want 0 (the operation must not run)", n)
	}
	cp, err := c.CaptureAuthorization(ctx, paypal.CaptureParams{AuthorizationID: auth.ID, RequestID: "capture-1"})
	if err != nil || cp.Status != "COMPLETED" {
		t.Fatalf("retry with the same key = %+v, %v, want the real capture", cp, err)
	}
	if n := f.Captures(); n != 1 {
		t.Fatalf("Captures() = %d, want 1", n)
	}
	if n := len(f.Requests("capture")); n != 2 {
		t.Errorf("recorded capture requests = %d, want 2", n)
	}
}

func TestFake_TimedOutCaptureStillHappensAndReplays(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f, func(cfg *paypal.Config) { cfg.HTTPClient = &http.Client{Timeout: time.Second} })
	order, auth := authorizedOrder(t, c, f, "1")
	ctx := context.Background()

	f.FailNext("capture", 0, "")
	_, err := c.CaptureAuthorization(ctx, paypal.CaptureParams{AuthorizationID: auth.ID, RequestID: "capture-1"})
	if err == nil || !paypal.IsAmbiguous(err) {
		t.Fatalf("hanging capture = %v, want an ambiguous error", err)
	}
	f.WaitHung()
	if n := f.Captures(); n != 1 {
		t.Fatalf("Captures() after WaitHung = %d, want 1: the hung capture must have run", n)
	}

	cp, err := c.CaptureAuthorization(ctx, paypal.CaptureParams{AuthorizationID: auth.ID, RequestID: "capture-1"})
	if err != nil {
		t.Fatalf("retry with the same key: %v", err)
	}
	if n := f.Captures(); n != 1 {
		t.Fatalf("Captures() = %d, want 1", n)
	}
	got, err := c.GetOrder(ctx, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	captures := got.PurchaseUnits[0].Payments.Captures
	if len(captures) != 1 || captures[0].ID != cp.ID {
		t.Errorf("order captures = %+v, want only %q", captures, cp.ID)
	}
}

func TestFake_WaitHungReturnsWhenIdleOrClosed(t *testing.T) {
	returnsWithin := func(t *testing.T, f *Server, limit time.Duration) {
		t.Helper()
		done := make(chan struct{})
		go func() { f.WaitHung(); close(done) }()
		select {
		case <-done:
		case <-time.After(limit):
			t.Fatalf("WaitHung did not return within %v", limit)
		}
	}

	idle := newFake(t)
	returnsWithin(t, idle, time.Second)

	f := NewServer()
	c := newTestClient(t, f)
	_, auth := authorizedOrder(t, c, f, "1")
	f.FailNext("capture", 0, "")
	go func() {
		_, _ = c.CaptureAuthorization(context.Background(), paypal.CaptureParams{AuthorizationID: auth.ID, RequestID: "capture-1"})
	}()
	waitFor(t, 5*time.Second, func() bool { return len(f.Requests("capture")) == 1 })

	waiting := make(chan struct{})
	go func() { f.WaitHung(); close(waiting) }()
	select {
	case <-waiting:
		t.Fatal("WaitHung returned while a capture was still hanging")
	case <-time.After(200 * time.Millisecond):
	}
	f.Close()
	select {
	case <-waiting:
	case <-time.After(2 * time.Second):
		t.Fatal("WaitHung did not return after Close")
	}
	returnsWithin(t, f, time.Second)
}

func TestFake_CloseReturnsWhileARequestHangs(t *testing.T) {
	f := NewServer()
	c := newTestClient(t, f)
	_, auth := authorizedOrder(t, c, f, "1")
	f.FailNext("capture", 0, "")

	errc := make(chan error, 1)
	go func() {
		_, err := c.CaptureAuthorization(context.Background(), paypal.CaptureParams{AuthorizationID: auth.ID, RequestID: "capture-1"})
		errc <- err
	}()
	waitFor(t, 5*time.Second, func() bool { return len(f.Requests("capture")) == 1 })

	start := time.Now()
	f.Close()
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Close() took %v with a hanging request, want it to return promptly", elapsed)
	}
	select {
	case err := <-errc:
		if err == nil {
			t.Error("the hanging capture succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the hanging client call never returned")
	}
	if n := f.Captures(); n != 0 {
		t.Errorf("Captures() = %d, want 0: a request cut short by Close must not execute", n)
	}
	f.Close()
}

func TestFake_FailNextShapesAndOrder(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f)
	order := createOrder(t, c, "1")
	ctx := context.Background()

	f.FailNext("get_order", 409, "")
	_, err := c.GetOrder(ctx, order.ID)
	var pe *paypal.Error
	if !errors.As(err, &pe) || pe.HTTPStatus != 409 || pe.Issue != "PREVIOUS_REQUEST_IN_PROGRESS" || !paypal.IsAmbiguous(err) {
		t.Fatalf("409 = %v, want an ambiguous PREVIOUS_REQUEST_IN_PROGRESS", err)
	}

	f.FailNext("get_order", 500, "")
	_, err = c.GetOrder(ctx, order.ID)
	if !errors.As(err, &pe) || pe.HTTPStatus != 500 || pe.Name != "INTERNAL_SERVER_ERROR" || pe.DebugID == "" || !paypal.IsAmbiguous(err) {
		t.Fatalf("500 = %+v (%v), want an ambiguous INTERNAL_SERVER_ERROR with a debug id", pe, err)
	}

	f.FailNext("get_order", 429, "")
	f.FailNext("get_order", 503, "")
	for i, want := range []int{429, 503} {
		_, err = c.GetOrder(ctx, order.ID)
		if !errors.As(err, &pe) || pe.HTTPStatus != want {
			t.Fatalf("failure %d = %v, want status %d (failures are consumed in order)", i, err, want)
		}
	}
	if _, err := c.GetOrder(ctx, order.ID); err != nil {
		t.Fatalf("GetOrder after the queued failures: %v", err)
	}
}

func TestFake_FailNextRejectsStatusesBelow400(t *testing.T) {
	f := newFake(t)
	mustPanic := func(name string, call func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Errorf("%s did not panic", name)
			}
		}()
		call()
	}
	for _, status := range []int{-1, 1, 100, 200, 201, 204, 302, 399} {
		mustPanic(fmt.Sprintf("FailNext with status %d", status), func() { f.FailNext("capture", status, "") })
	}
	mustPanic("RespondOnce with status 0", func() { f.RespondOnce("capture", 0, nil) })

	for _, status := range []int{0, 400, 404, 409, 422, 429, 500, 503} {
		f.FailNext("get_order", status, "")
	}
	if n := len(f.failures["get_order"]); n != 8 {
		t.Errorf("queued failures = %d, want 8", n)
	}
}

func TestFake_UnknownOpsPanic(t *testing.T) {
	f := newFake(t)
	for name, call := range map[string]func(){
		"FailNext": func() { f.FailNext("capure", 500, "") },
		"Requests": func() { f.Requests("capure") },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s with a misspelled op did not panic", name)
				}
			}()
			call()
		}()
	}
}

func TestFake_SetOrderAmountChangesOnlyWhatGetOrderReports(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f)
	ctx := context.Background()
	order, auth := authorizedOrder(t, c, f, "1")

	f.SetOrderAmount(order.ID, "12.50", "EUR")
	got, err := c.GetOrder(ctx, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if want := (paypal.Money{CurrencyCode: "EUR", Value: "12.50"}); got.PurchaseUnits[0].Amount != want {
		t.Errorf("GetOrder amount = %+v, want %+v", got.PurchaseUnits[0].Amount, want)
	}
	if a := got.PurchaseUnits[0].Payments.Authorizations[0].Amount; a != usd10 {
		t.Errorf("authorization amount = %+v, want it unchanged at %+v", a, usd10)
	}
	cp, err := c.CaptureAuthorization(ctx, paypal.CaptureParams{AuthorizationID: auth.ID, RequestID: "capture-1"})
	if err != nil || cp.Amount != usd10 {
		t.Errorf("capture = %+v, %v, want the authorized %+v", cp, err, usd10)
	}
}

func TestFake_OrderListsEveryPayment(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f)
	ctx := context.Background()
	order, auth := authorizedOrder(t, c, f, "1")
	cp, err := c.CaptureAuthorization(ctx, paypal.CaptureParams{AuthorizationID: auth.ID, RequestID: "capture-1"})
	if err != nil {
		t.Fatal(err)
	}
	refund, err := c.RefundCapture(ctx, paypal.RefundParams{CaptureID: cp.ID, RequestID: "refund-1"})
	if err != nil {
		t.Fatal(err)
	}

	got, err := c.GetOrder(ctx, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "COMPLETED" {
		t.Errorf("order status = %q, want COMPLETED", got.Status)
	}
	pay := got.PurchaseUnits[0].Payments
	if len(pay.Authorizations) != 1 || pay.Authorizations[0].ID != auth.ID || pay.Authorizations[0].Status != "CAPTURED" {
		t.Errorf("authorizations = %+v, want %q CAPTURED", pay.Authorizations, auth.ID)
	}
	if len(pay.Captures) != 1 || pay.Captures[0].ID != cp.ID || pay.Captures[0].Status != "REFUNDED" {
		t.Errorf("captures = %+v, want %q REFUNDED", pay.Captures, cp.ID)
	}
	if len(pay.Refunds) != 1 || pay.Refunds[0].ID != refund.ID || pay.Refunds[0].Status != "COMPLETED" {
		t.Errorf("refunds = %+v, want %q COMPLETED", pay.Refunds, refund.ID)
	}
	if id := order.ID; len(id) != 17 || id != strings.ToUpper(id) {
		t.Errorf("order id %q, want 17 uppercase alphanumerics", id)
	}
}

func TestFake_ConcurrentCapturesWithOneKeyCaptureOnce(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f)
	_, auth := authorizedOrder(t, c, f, "1")

	const n = 16
	ids := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cp, err := c.CaptureAuthorization(context.Background(), paypal.CaptureParams{AuthorizationID: auth.ID, RequestID: "capture-1"})
			ids[i], errs[i] = cp.ID, err
		}()
	}
	wg.Wait()
	for i := range n {
		if errs[i] != nil || ids[i] == "" || ids[i] != ids[0] {
			t.Fatalf("call %d = %q, %v, want the shared capture %q", i, ids[i], errs[i], ids[0])
		}
	}
	if got := f.Captures(); got != 1 {
		t.Fatalf("Captures() = %d, want 1", got)
	}
}

func TestFake_ConcurrentCapturesWithDistinctKeysCaptureOnce(t *testing.T) {
	f := newFake(t)
	c := newTestClient(t, f)
	_, auth := authorizedOrder(t, c, f, "1")

	const n = 16
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = c.CaptureAuthorization(context.Background(), paypal.CaptureParams{AuthorizationID: auth.ID, RequestID: "capture-" + string(rune('a'+i))})
		}()
	}
	wg.Wait()
	succeeded := 0
	for _, err := range errs {
		if err == nil {
			succeeded++
			continue
		}
		requirePayPalError(t, err, 422, "UNPROCESSABLE_ENTITY", "AUTHORIZATION_ALREADY_CAPTURED")
	}
	if succeeded != 1 || f.Captures() != 1 {
		t.Fatalf("succeeded = %d, Captures() = %d, want exactly one capture", succeeded, f.Captures())
	}
}

func TestFake_CapturesCarryAFeeOf349BasisPointsPlus49Cents(t *testing.T) {
	cases := []struct {
		gross, fee, net string
	}{
		{"10.00", "0.84", "9.16"},
		{"45.00", "2.06", "42.94"},
		{"50.00", "2.24", "47.76"},
		{"0.10", "0.10", "0.00"},
	}
	for _, tc := range cases {
		b := sellerBreakdown(paypal.Money{CurrencyCode: "USD", Value: tc.gross})
		want := paypal.SellerReceivableBreakdown{
			GrossAmount: &paypal.Money{CurrencyCode: "USD", Value: tc.gross},
			PayPalFee:   &paypal.Money{CurrencyCode: "USD", Value: tc.fee},
			NetAmount:   &paypal.Money{CurrencyCode: "USD", Value: tc.net},
		}
		if b == nil || *b.GrossAmount != *want.GrossAmount || *b.PayPalFee != *want.PayPalFee || *b.NetAmount != *want.NetAmount || b.ReceivableAmount != nil {
			t.Errorf("breakdown of %s = %+v, want fee %s and net %s", tc.gross, b, tc.fee, tc.net)
		}
	}
	if b := sellerBreakdown(paypal.Money{CurrencyCode: "USD", Value: "ten"}); b != nil {
		t.Errorf("breakdown of an unreadable amount = %+v, want none", b)
	}

	f := newFake(t)
	c := newTestClient(t, f)
	ctx := context.Background()
	_, cp := capturedOrder(t, c, f, "1")
	wantFee := func(where string, b *paypal.SellerReceivableBreakdown) {
		t.Helper()
		if b == nil || b.GrossAmount == nil || *b.GrossAmount != usd10 || b.PayPalFee == nil || b.PayPalFee.Value != "0.84" || b.NetAmount == nil || b.NetAmount.Value != "9.16" {
			t.Errorf("%s breakdown = %+v, want 10.00 gross, 0.84 fee and 9.16 net", where, b)
		}
	}
	wantFee("capture response", cp.SellerReceivableBreakdown)
	got, err := c.GetCapture(ctx, cp.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantFee("GET capture", got.SellerReceivableBreakdown)
	if _, err := c.RefundCapture(ctx, paypal.RefundParams{CaptureID: cp.ID, RequestID: "refund-1"}); err != nil {
		t.Fatal(err)
	}
	refunded, err := c.GetCapture(ctx, cp.ID)
	if err != nil || refunded.Status != "REFUNDED" {
		t.Fatalf("GET capture after the refund = %+v, %v", refunded, err)
	}
	wantFee("refunded capture", refunded.SellerReceivableBreakdown)
}
