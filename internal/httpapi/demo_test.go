package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
	"time"
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

func TestDemo_CreatesMayasWorkshopAndItsAnswersArrive(t *testing.T) {
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

	ownerPath := "/api/v1/owner/" + strings.TrimPrefix(d.OwnerURL, appBaseURL+"/owner/")
	deadline := time.Now().Add(20 * time.Second)
	for {
		var v ownerViewJSON
		e.call(http.MethodGet, ownerPath, nil, http.StatusOK, &v)
		if v.Workshop.HostName != "Maya Okafor" || v.Workshop.Title != "Wheel-throwing for beginners" {
			t.Fatalf("workshop = %+v", v.Workshop)
		}
		if len(v.Availabilities) == 6 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d of 6 answers arrived", len(v.Availabilities))
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestDemo_OneWorkshopAMinutePerVisitor(t *testing.T) {
	e := newEnv(t, withDemoMode(), withRateLimit(120))
	e.call(http.MethodPost, "/api/v1/demo/workshops", nil, http.StatusCreated, nil)
	e.wantError(http.MethodPost, "/api/v1/demo/workshops", nil, http.StatusTooManyRequests, "rate_limited")
}
