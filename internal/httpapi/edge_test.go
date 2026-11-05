package httpapi_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/httpapi"
)

func (e *env) postFrom(remote, path string, body any) *httptest.ResponseRecorder {
	e.t.Helper()
	req := httptest.NewRequestWithContext(e.ctx, http.MethodPost, path, bytes.NewReader(mustJSON(e.t, body)))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = remote
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

func TestRateLimit_429AfterBurstPerAddress(t *testing.T) {
	e := newEnv(t, withRateLimit(30))
	limited := 0
	for range 60 {
		if rec := e.postFrom("203.0.113.7:4000", "/api/v1/workshops", workshopRequest()); rec.Code == http.StatusTooManyRequests {
			limited++
			if rec.Header().Get("Retry-After") == "" {
				t.Fatalf("429 without Retry-After")
			}
		}
	}
	if limited == 0 {
		t.Fatalf("60 POSTs in a burst from one address were never limited")
	}
	if rec := e.postFrom("198.51.100.9:4000", "/api/v1/workshops", workshopRequest()); rec.Code != http.StatusCreated {
		t.Errorf("another address = %d, want 201: limits are per address", rec.Code)
	}
	if rec := e.request(http.MethodGet, "/healthz", nil); rec.Code != http.StatusOK {
		t.Errorf("GET /healthz = %d, want 200: reads are not limited", rec.Code)
	}
}

func TestJoin_RefusedAboveTheParticipantCap(t *testing.T) {
	httpapi.SetLimits(t, 2, 20, 4, time.Second)
	e := newEnv(t)
	h := e.createWorkshop()
	e.join(h, "Ana")
	e.join(h, "Luis")

	e.wantError(http.MethodPost, h.sharePath+"/participants", map[string]string{"display_name": "Marta"}, http.StatusConflict, "too_many_participants")
}

func TestAvailability_RefusedAfterTheSubmissionCap(t *testing.T) {
	httpapi.SetLimits(t, 200, 2, 4, time.Second)
	e := newEnv(t)
	h := e.createWorkshop()
	ana := e.join(h, "Ana")
	e.say(ana, "Saturday works")
	e.say(ana, "Sunday works too")

	e.wantError(http.MethodPost, ana.path+"/availability", map[string]string{"message": "Actually any day"}, http.StatusConflict, "too_many_submissions")
}

func TestInterpret_BusyWhenEverySlotIsTaken(t *testing.T) {
	httpapi.SetLimits(t, 200, 20, 0, 10*time.Millisecond)
	e := newEnv(t)
	h := e.createWorkshop()
	ana := e.join(h, "Ana")

	e.wantError(http.MethodPost, ana.path+"/availability", map[string]string{"message": "Saturday works"}, http.StatusServiceUnavailable, "ai_busy")
}

func TestWebhook_ForeignCertURLRejectedWithoutAskingPayPal(t *testing.T) {
	e := newEnv(t, withWebhookID())
	for name, header := range map[string][2]string{
		"foreign cert host": {"PAYPAL-CERT-URL", "https://evil.example.com/v1/notifications/certs/CERT"},
		"plain http cert":   {"PAYPAL-CERT-URL", "http://api-m.sandbox.paypal.com/v1/notifications/certs/CERT"},
		"from the future":   {"PAYPAL-TRANSMISSION-TIME", "2026-11-02T10:00:00Z"},
	} {
		req := httptest.NewRequestWithContext(e.ctx, http.MethodPost, "/paypal/webhook",
			bytes.NewReader([]byte(`{"id":"WH-1","event_type":"PAYMENT.CAPTURE.COMPLETED","resource_type":"capture","resource":{"id":"CAP-1"}}`)))
		req.Header.Set("Content-Type", "application/json")
		for k, v := range webhookHeaders {
			req.Header.Set(k, v)
		}
		req.Header.Set(header[0], header[1])
		rec := httptest.NewRecorder()
		e.h.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: webhook = %d, want 400", name, rec.Code)
		}
	}
	if n := len(e.fake.Requests("verify_webhook")); n != 0 {
		t.Errorf("PayPal asked to verify %d deliveries, want 0", n)
	}
}

func TestDecode_RejectsAnythingButJSON(t *testing.T) {
	e := newEnv(t)
	req := httptest.NewRequestWithContext(e.ctx, http.MethodPost, "/api/v1/workshops", bytes.NewReader(mustJSON(t, workshopRequest())))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain POST = %d, want 415: a cross-site form must not create workshops", rec.Code)
	}
}

func TestSecurityHeaders(t *testing.T) {
	e := newEnv(t)
	rec := e.request(http.MethodGet, "/healthz", nil)
	for header, want := range map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"Content-Security-Policy": "frame-ancestors 'none'",
		"Referrer-Policy":         "no-referrer",
	} {
		if got := rec.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
}

func TestHealthz_ReportsTheWorkerAndTheQueues(t *testing.T) {
	e := newEnv(t)
	var got struct {
		OK     bool `json:"ok"`
		Worker struct {
			DrainAgeS *float64 `json:"drain_age_s"`
		} `json:"worker"`
		Jobs struct {
			Unknown      int `json:"unknown"`
			Parked       int `json:"parked"`
			StuckPending int `json:"stuck_pending"`
		} `json:"jobs"`
		WorkshopsFlagged *int `json:"workshops_flagged"`
	}
	e.call(http.MethodGet, "/healthz", nil, http.StatusOK, &got)
	if !got.OK || got.WorkshopsFlagged == nil {
		t.Errorf("healthz = %+v, want ok with the queue and flag counts", got)
	}
}
