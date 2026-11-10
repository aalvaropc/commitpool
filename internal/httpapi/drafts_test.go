package httpapi_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/aalvaropc/commitpool/internal/ai"
)

const classDescription = "I'm Iris Calder. I run life drawing for beginners at The Long Room in Red Hook, Brooklyn. " +
	"Two hours, $45 a person, the room and the model cost me $120 a session, and I have 6 easels. " +
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
	Timezone    string          `json:"timezone"`
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
	e.call(http.MethodPost, draftsPath, draftBody(classDescription), http.StatusOK, &got)
	want := draftResponseJSON{Drafted: true, Draft: draftJSON{
		Title: "Life drawing for beginners", HostName: "Iris Calder", Location: "The Long Room in Red Hook, Brooklyn", Timezone: "America/New_York",
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
	e.wantError(http.MethodPost, draftsPath, map[string]string{"text": "life drawing", "timezone": "Mars/Olympus"}, http.StatusBadRequest, "invalid_timezone")
}

func TestDrafts_AFailedModelStillAnswersWithAnEmptyDraft(t *testing.T) {
	e := newEnv(t, withAI(cannedDraftAI{err: errors.New("bedrock: throttled")}))
	var got draftResponseJSON
	e.call(http.MethodPost, draftsPath, draftBody(classDescription), http.StatusOK, &got)
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
	if !strings.Contains(e.logs.String(), "draft workshop") || strings.Contains(e.logs.String(), "Iris Calder") {
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
		Title:       "  " + strings.Repeat("Charcoal", 50) + "  ",
		Description: strings.Repeat("d", 2500),
		HostName:    strings.Repeat("M", 90),
		Location:    " Red Hook ",
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
	if len(d.Description) != 2000 || len(d.HostName) != 80 || d.Location != "Red Hook" {
		t.Errorf("description %d, host %d, location %q; want 2000, 80, Red Hook", len(d.Description), len(d.HostName), d.Location)
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
		if rec := e.request(http.MethodPost, draftsPath, draftBody(classDescription)); rec.Code != http.StatusOK {
			t.Fatalf("draft %d = %d %s", i+1, rec.Code, rec.Body)
		}
	}
	got := e.wantError(http.MethodPost, draftsPath, draftBody(classDescription), http.StatusTooManyRequests, "draft_rate_limited")
	if !strings.Contains(got.Message, "fill in the form") {
		t.Errorf("message = %q, want it to point at the form", got.Message)
	}
}

func TestDrafts_ReadDatesOnTheClockOfThePlace(t *testing.T) {
	e := newEnv(t, withAI(cannedDraftAI{draft: ai.Draft{Timezone: "Europe/London", Dates: []ai.DraftDate{{Date: "2026-11-02", Start: "08:30"}}}}))
	var got draftResponseJSON
	e.call(http.MethodPost, draftsPath, map[string]string{"text": "anything", "timezone": "America/Chicago"}, http.StatusOK, &got)
	if got.Draft.Timezone != "Europe/London" || len(got.Draft.Dates) != 0 {
		t.Errorf("timezone %q dates %+v; 08:30 in London has passed, so the date must go", got.Draft.Timezone, got.Draft.Dates)
	}
}

func TestDrafts_KeepTheBrowsersZoneWhenThePlacesIsUnknown(t *testing.T) {
	e := newEnv(t, withAI(cannedDraftAI{draft: ai.Draft{Timezone: "Mars/Olympus", Dates: []ai.DraftDate{{Date: "2026-11-02", Start: "08:30"}}}}))
	var got draftResponseJSON
	e.call(http.MethodPost, draftsPath, map[string]string{"text": "anything", "timezone": "America/Chicago"}, http.StatusOK, &got)
	if got.Draft.Timezone != "America/Chicago" || len(got.Draft.Dates) != 1 {
		t.Errorf("timezone %q dates %+v; 08:30 in Chicago is still ahead", got.Draft.Timezone, got.Draft.Dates)
	}
}

func TestDrafts_DropTimesTheClockSkips(t *testing.T) {
	e := newEnv(t, withAI(cannedDraftAI{draft: ai.Draft{Dates: []ai.DraftDate{{Date: "2027-03-14", Start: "02:30"}, {Date: "2027-03-14", Start: "03:30"}}}}))
	var got draftResponseJSON
	e.call(http.MethodPost, draftsPath, draftBody("anything"), http.StatusOK, &got)
	if len(got.Draft.Dates) != 1 || got.Draft.Dates[0].Start != "03:30" {
		t.Errorf("dates = %+v, want only 03:30: 02:30 does not exist on the night clocks spring forward", got.Draft.Dates)
	}
}

func TestDrafts_BoundAmountsAndCounts(t *testing.T) {
	e := newEnv(t, withAI(cannedDraftAI{draft: ai.Draft{Price: "99999999999", VenueCost: "10000000", Minimum: 900}}))
	var got draftResponseJSON
	e.call(http.MethodPost, draftsPath, draftBody("anything"), http.StatusOK, &got)
	if got.Draft.Price != "" || got.Draft.VenueCost != "" || got.Draft.Minimum != 0 {
		t.Errorf("price %q venue %q minimum %d, want amounts the form takes and at most 500 people", got.Draft.Price, got.Draft.VenueCost, got.Draft.Minimum)
	}
}

type blockingDraftAI struct {
	ai.Fake
	entered chan struct{}
	release chan struct{}
}

func (b blockingDraftAI) Draft(ctx context.Context, in ai.DraftInput) (ai.Draft, ai.Meta, error) {
	b.entered <- struct{}{}
	select {
	case <-b.release:
	case <-ctx.Done():
	}
	return ai.Fake{}.Draft(ctx, in)
}

func TestDrafts_NeverTakeTheReadersParticipantsNeed(t *testing.T) {
	stub := blockingDraftAI{entered: make(chan struct{}, 4), release: make(chan struct{})}
	e := newEnv(t, withAI(stub))
	h := e.createWorkshop()
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.request(http.MethodPost, draftsPath, draftBody(classDescription))
		}()
	}
	defer wg.Wait()
	defer close(stub.release)
	<-stub.entered
	<-stub.entered

	var got draftResponseJSON
	e.call(http.MethodPost, draftsPath, draftBody(classDescription), http.StatusOK, &got)
	if got.Drafted {
		t.Error("a third draft ran while both drafting slots were busy")
	}
	e.joinAndSay(h, "Ana", "Saturday works for me")
}
