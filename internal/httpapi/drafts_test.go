package httpapi_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/aalvaropc/commitpool/internal/ai"
)

const mayaDescription = "I'm Maya Okafor. I teach wheel-throwing for beginners at Maya's studio in Greenpoint, Brooklyn. " +
	"Two hours, $45 a person, and you take home what you make. The studio costs me $120 a session and I have 6 wheels. " +
	"Saturday or Sunday next week, 10 to 12."

type draftDateJSON struct {
	Date  string `json:"date"`
	Start string `json:"start"`
}

type draftJSON struct {
	Title       string          `json:"title"`
	Description string          `json:"description"`
	HostName    string          `json:"host_name"`
	Location    string          `json:"location"`
	DurationMin int             `json:"duration_min"`
	Price       string          `json:"price"`
	VenueCost   string          `json:"venue_cost"`
	Minimum     int             `json:"minimum"`
	Capacity    int             `json:"capacity"`
	Dates       []draftDateJSON `json:"dates"`
	Missing     []string        `json:"missing"`
}

type draftResponseJSON struct {
	Draft   draftJSON `json:"draft"`
	Drafted bool      `json:"drafted"`
}

const draftsPath = "/api/v1/workshop-drafts"

func draftBody(text string) map[string]string {
	return map[string]string{"text": text, "timezone": "America/New_York"}
}

type cannedDraftAI struct {
	ai.Fake
	draft ai.Draft
	err   error
}

func (c cannedDraftAI) Draft(context.Context, ai.DraftInput) (ai.Draft, ai.Meta, error) {
	return c.draft, ai.Meta{Provider: "test", Model: "canned"}, c.err
}

func TestDrafts_ClaudeFillsTheFormFromADescription(t *testing.T) {
	e := newEnv(t)
	var got draftResponseJSON
	e.call(http.MethodPost, draftsPath, draftBody(mayaDescription), http.StatusOK, &got)
	want := draftResponseJSON{Drafted: true, Draft: draftJSON{
		Title: "Wheel-throwing for beginners", HostName: "Maya Okafor", Location: "Maya's studio in Greenpoint, Brooklyn",
		DurationMin: 120, Price: "45.00", VenueCost: "120.00", Capacity: 6,
		Dates:   []draftDateJSON{{Date: "2026-11-14", Start: "10:00"}, {Date: "2026-11-15", Start: "10:00"}},
		Missing: []string{"description", "minimum"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("draft = %+v\nwant    %+v", got, want)
	}
}

func TestDrafts_RefuseEmptyOrOverlongTextAndUnknownZones(t *testing.T) {
	e := newEnv(t)
	e.wantError(http.MethodPost, draftsPath, draftBody("   "), http.StatusBadRequest, "invalid_text")
	e.wantError(http.MethodPost, draftsPath, draftBody(strings.Repeat("a", 2001)), http.StatusBadRequest, "invalid_text")
	e.wantError(http.MethodPost, draftsPath, map[string]string{"text": "pottery", "timezone": "Mars/Olympus"}, http.StatusBadRequest, "invalid_timezone")
}

func TestDrafts_AFailedModelStillAnswersWithAnEmptyDraft(t *testing.T) {
	e := newEnv(t, withAI(cannedDraftAI{err: errors.New("bedrock: throttled")}))
	var got draftResponseJSON
	e.call(http.MethodPost, draftsPath, draftBody(mayaDescription), http.StatusOK, &got)
	if got.Drafted {
		t.Error("drafted = true after the model failed")
	}
	want := []string{"title", "description", "host_name", "location", "duration_min", "price", "venue_cost", "minimum", "capacity", "dates"}
	if !reflect.DeepEqual(got.Draft.Missing, want) {
		t.Errorf("missing = %v, want every field", got.Draft.Missing)
	}
	if got.Draft.Dates == nil {
		t.Error("dates = null, want an empty list")
	}
	if !strings.Contains(e.logs.String(), "draft workshop") || strings.Contains(e.logs.String(), "Okafor") {
		t.Errorf("logs should name the failure without the description:\n%s", e.logs)
	}
}

func TestDrafts_KeepOnlyWhatTheServerWouldAccept(t *testing.T) {
	var dates []ai.DraftDate
	for day := 30; day >= 10; day-- {
		dates = append(dates, ai.DraftDate{Date: fmt.Sprintf("2026-11-%02d", day), Start: "10:00"})
	}
	dates = append(dates,
		ai.DraftDate{Date: "2026-11-01", Start: "10:00"},
		ai.DraftDate{Date: "2026-13-40", Start: "10:00"},
		ai.DraftDate{Date: "2026-11-20", Start: "25:00"},
		ai.DraftDate{Date: "2026-11-10", Start: "10:00"},
	)
	e := newEnv(t, withAI(cannedDraftAI{draft: ai.Draft{
		Title:       "  " + strings.Repeat("Clay", 50) + "  ",
		Description: strings.Repeat("d", 2500),
		HostName:    strings.Repeat("M", 90),
		Location:    " Greenpoint ",
		DurationMin: 5000,
		Price:       "-5",
		VenueCost:   "a lot",
		Minimum:     9,
		Capacity:    6,
		Dates:       dates,
	}}))
	var got draftResponseJSON
	e.call(http.MethodPost, draftsPath, draftBody("anything"), http.StatusOK, &got)
	d := got.Draft
	if n := len([]rune(d.Title)); n != 120 || strings.HasSuffix(d.Title, " ") || strings.HasPrefix(d.Title, " ") {
		t.Errorf("title has %d runes (%q), want a trimmed 120", n, d.Title)
	}
	if len(d.Description) != 2000 || len(d.HostName) != 80 || d.Location != "Greenpoint" {
		t.Errorf("description %d, host %d, location %q; want 2000, 80, Greenpoint", len(d.Description), len(d.HostName), d.Location)
	}
	if d.DurationMin != 0 || d.Price != "" || d.VenueCost != "" || d.Minimum != 0 || d.Capacity != 6 {
		t.Errorf("duration %d price %q venue %q minimum %d capacity %d; want 0, empty, empty, 0, 6", d.DurationMin, d.Price, d.VenueCost, d.Minimum, d.Capacity)
	}
	if len(d.Dates) != 10 || d.Dates[0].Date != "2026-11-10" || d.Dates[9].Date != "2026-11-19" {
		t.Errorf("dates = %+v, want the ten earliest valid future dates in order, once each", d.Dates)
	}
	want := []string{"duration_min", "price", "venue_cost", "minimum"}
	if !reflect.DeepEqual(d.Missing, want) {
		t.Errorf("missing = %v, want %v", d.Missing, want)
	}
}

func TestDrafts_WriteAmountsWithCents(t *testing.T) {
	e := newEnv(t, withAI(cannedDraftAI{draft: ai.Draft{Price: "45.5", VenueCost: "0"}}))
	var got draftResponseJSON
	e.call(http.MethodPost, draftsPath, draftBody("anything"), http.StatusOK, &got)
	if got.Draft.Price != "45.50" || got.Draft.VenueCost != "0.00" {
		t.Errorf("price %q venue %q, want 45.50 and 0.00", got.Draft.Price, got.Draft.VenueCost)
	}
}

func TestDrafts_HaveTheirOwnLimitPerPerson(t *testing.T) {
	e := newEnv(t, withRateLimit(600))
	for i := range 2 {
		if rec := e.request(http.MethodPost, draftsPath, draftBody(mayaDescription)); rec.Code != http.StatusOK {
			t.Fatalf("draft %d = %d %s", i+1, rec.Code, rec.Body)
		}
	}
	got := e.wantError(http.MethodPost, draftsPath, draftBody(mayaDescription), http.StatusTooManyRequests, "draft_rate_limited")
	if !strings.Contains(got.Message, "fill in the form") {
		t.Errorf("message = %q, want it to point at the form", got.Message)
	}
}
