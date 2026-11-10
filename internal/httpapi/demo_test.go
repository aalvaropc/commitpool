package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

type demoJSON struct {
	OwnerURL     string `json:"owner_url"`
	ShareURL     string `json:"share_url"`
	Participants []struct {
		Name           string `json:"name"`
		ParticipantURL string `json:"participant_url"`
	} `json:"participants"`
}

func TestDemo_OnlyOnADemoServer(t *testing.T) {
	e := newEnv(t)
	e.wantError(http.MethodPost, "/api/v1/demo/workshops", nil, http.StatusNotFound, "not_found")
	e.wantError(http.MethodGet, "/api/v1/demo", nil, http.StatusNotFound, "not_found")
}

func TestDemo_CreatesTheLifeDrawingWorkshopAndItsAnswersArrive(t *testing.T) {
	e := newEnv(t, withDemoMode())
	var info struct {
		Workshops bool `json:"workshops"`
	}
	e.call(http.MethodGet, "/api/v1/demo", nil, http.StatusOK, &info)
	if !info.Workshops {
		t.Fatal("a demo server does not offer demo workshops")
	}

	var d demoJSON
	e.call(http.MethodPost, "/api/v1/demo/workshops", nil, http.StatusCreated, &d)
	if !strings.HasPrefix(d.OwnerURL, appBaseURL+"/owner/") || !strings.HasPrefix(d.ShareURL, appBaseURL+"/w/") {
		t.Fatalf("demo links = %+v", d)
	}
	if len(d.Participants) != 6 || d.Participants[0].Name != "Priya Raman" || !strings.HasPrefix(d.Participants[0].ParticipantURL, appBaseURL+"/p/") {
		t.Fatalf("participants = %+v", d.Participants)
	}

	e.background.Wait()
	var v ownerViewJSON
	e.call(http.MethodGet, "/api/v1/owner/"+strings.TrimPrefix(d.OwnerURL, appBaseURL+"/owner/"), nil, http.StatusOK, &v)
	if v.Workshop.HostName != "Iris Calder" || v.Workshop.Title != "Life drawing for beginners" {
		t.Fatalf("workshop = %+v", v.Workshop)
	}
	if len(v.Availabilities) != 6 {
		t.Fatalf("%d of 6 answers arrived", len(v.Availabilities))
	}
}

func TestDemo_AnswersArriveAfterTheVisitorLeaves(t *testing.T) {
	e := newEnv(t, withDemoMode())
	ctx, cancel := context.WithCancel(e.ctx)
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/demo/workshops", nil))
	cancel()
	if rec.Code != http.StatusCreated {
		t.Fatalf("demo = %d %s", rec.Code, rec.Body)
	}
	var d demoJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	e.background.Wait()
	var v ownerViewJSON
	e.call(http.MethodGet, "/api/v1/owner/"+strings.TrimPrefix(d.OwnerURL, appBaseURL+"/owner/"), nil, http.StatusOK, &v)
	if len(v.Availabilities) != 6 {
		t.Errorf("%d of 6 answers arrived after the visitor left", len(v.Availabilities))
	}
}

func TestDemo_AVisitorStartsFiveThenWaits(t *testing.T) {
	e := newEnv(t, withDemoMode(), withRateLimit(120))
	for range 5 {
		e.call(http.MethodPost, "/api/v1/demo/workshops", nil, http.StatusCreated, nil)
	}
	rec := e.request(http.MethodPost, "/api/v1/demo/workshops", nil)
	if rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), "demo_rate_limited") {
		t.Fatalf("sixth demo = %d %s, want 429 demo_rate_limited", rec.Code, rec.Body)
	}
	if wait, _ := strconv.Atoi(rec.Header().Get("Retry-After")); wait < 1100 || wait > 1200 {
		t.Errorf("Retry-After = %q, want about 20 minutes", rec.Header().Get("Retry-After"))
	}
	if !strings.Contains(rec.Body.String(), "20 minutes") {
		t.Errorf("message = %s, want the real wait", rec.Body)
	}
}
