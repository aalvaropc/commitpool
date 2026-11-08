package inproc_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/aalvaropc/commitpool/internal/inproc"
)

func TestClient_CallsTheHandlerInProcess(t *testing.T) {
	var sawInProcess bool
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawInProcess = inproc.Is(r.Context())
		var in struct{ Name string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content type = %q", r.Header.Get("Content-Type"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"greeting": "hi " + in.Name})
	})
	var out struct{ Greeting string }
	err := inproc.Client{Handler: h}.Call(context.Background(), http.MethodPost, "/x", map[string]string{"name": "Maya"}, &out)
	if err != nil || out.Greeting != "hi Maya" || !sawInProcess {
		t.Fatalf("Call = %v, out %+v, in process %v", err, out, sawInProcess)
	}
}

func TestClient_TurnsAnErrorAnswerIntoAnError(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"capacity_full","message":"Every spot is taken."}`))
	})
	err := inproc.Client{Handler: h}.Call(context.Background(), http.MethodGet, "/x", nil, nil)
	var apiErr *inproc.Error
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusConflict || apiErr.Code != "capacity_full" || apiErr.Message != "Every spot is taken." {
		t.Fatalf("Call error = %#v", err)
	}
}

func TestClient_NamesAnErrorWithoutABody(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })
	err := inproc.Client{Handler: h}.Call(context.Background(), http.MethodGet, "/x", nil, nil)
	var apiErr *inproc.Error
	if !errors.As(err, &apiErr) || apiErr.Code != "http_418" {
		t.Fatalf("Call error = %#v", err)
	}
}

func TestClient_RefusesWithoutAHandler(t *testing.T) {
	if err := (inproc.Client{}).Call(context.Background(), http.MethodGet, "/x", nil, nil); err == nil {
		t.Fatal("Call without a handler: want an error")
	}
}

func TestError_TemporaryOnlyForBusyOrUpstreamFailures(t *testing.T) {
	for status, want := range map[int]bool{429: true, 502: true, 503: true, 504: true, 400: false, 404: false, 409: false, 500: false} {
		if got := (&inproc.Error{Status: status}).Temporary(); got != want {
			t.Errorf("status %d temporary = %v, want %v", status, got, want)
		}
	}
}

func TestError_ThrottledOnlyForTooManyRequests(t *testing.T) {
	for status, want := range map[int]bool{429: true, 502: false, 503: false, 400: false, 409: false} {
		if got := (&inproc.Error{Status: status}).Throttled(); got != want {
			t.Errorf("status %d throttled = %v, want %v", status, got, want)
		}
	}
}
