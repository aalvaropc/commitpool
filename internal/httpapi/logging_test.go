package httpapi_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestLogs_NeverContainTokensOrSecrets(t *testing.T) {
	e := newEnv(t, withWebhookID())
	_, p, ps := e.fundingWorkshop()
	en, a := e.checkout(ps[0], p.ID)
	e.fake.Approve(*a.OrderID)
	const bodySecret = "client_secret=EBWKjlELKMYqRNQ6sYvFo64FtaRLRR5BdHEESmha49TM"

	view := e.request(http.MethodGet, ps[0].path, nil)
	back := e.request(http.MethodGet, "/paypal/return?"+url.Values{"e": {en.EnrollmentID}, "token": {*a.OrderID}}.Encode(), nil)
	hook := e.webhook(mustJSON(t, event("WH-LOGS-1", "CHECKOUT.ORDER.APPROVED", "checkout-order",
		map[string]any{"id": *a.OrderID, "status": "APPROVED", "note": bodySecret})))

	logs := e.logs.String()
	for _, route := range []string{"GET /api/v1/p/{token}", "GET /paypal/return", "POST /paypal/webhook"} {
		if !strings.Contains(logs, "route=\""+route+"\"") {
			t.Fatalf("no request line for %s; the check would be vacuous:\n%s", route, logs)
		}
	}
	ids := map[string]bool{}
	for _, rec := range []interface{ Header() http.Header }{view, back, hook} {
		id := rec.Header().Get("X-Request-Id")
		if id == "" || ids[id] {
			t.Fatalf("X-Request-Id = %q, want a new id for every request (seen %v)", id, ids)
		}
		ids[id] = true
		if !strings.Contains(logs, "request_id="+id) {
			t.Errorf("no log line carries request_id=%s", id)
		}
	}
	if !strings.Contains(logs, "duration_ms=") {
		t.Errorf("request lines carry no duration_ms:\n%s", logs)
	}
	secrets := []string{ps[0].token, *a.OrderID, "token=", bodySecret, "EBWKjlELKMYqRNQ6", "fake-secret",
		"Bearer", "Authorization", webhookHeaders["PAYPAL-TRANSMISSION-SIG"], "approve?order="}
	for _, secret := range secrets {
		if strings.Contains(logs, secret) {
			t.Errorf("log contains %q", secret)
		}
	}
}
