package httpapi

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aalvaropc/commitpool/internal/ai"
	"github.com/aalvaropc/commitpool/internal/domain"
)

const (
	maxDraftText  = 2000
	draftTimeout  = 30 * time.Second
	draftsPerMin  = 4
	draftDayClock = "2006-01-02 15:04"
)

var errDraftLimited = newError(http.StatusTooManyRequests, "draft_rate_limited",
	"Claude has drafted a few workshops from this address already. Wait a minute, or fill in the form yourself.")

var draftFields = []string{"title", "description", "host_name", "location", "duration_min", "price", "venue_cost", "minimum", "capacity", "dates"}

type draftDateDTO struct {
	Date  string `json:"date"`
	Start string `json:"start"`
}

type draftDTO struct {
	Title       string         `json:"title"`
	Description string         `json:"description"`
	HostName    string         `json:"host_name"`
	Location    string         `json:"location"`
	Timezone    string         `json:"timezone"`
	DurationMin int            `json:"duration_min"`
	Price       string         `json:"price"`
	VenueCost   string         `json:"venue_cost"`
	Minimum     int            `json:"minimum"`
	Capacity    int            `json:"capacity"`
	Dates       []draftDateDTO `json:"dates"`
	Missing     []string       `json:"missing"`
}

type draftResponse struct {
	Draft   draftDTO `json:"draft"`
	Drafted bool     `json:"drafted"`
}

func (s *server) draftWorkshop(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Text     string `json:"text"`
		Timezone string `json:"timezone"`
	}
	if err := decode(r, &req); err != nil {
		return err
	}
	text := strings.TrimSpace(req.Text)
	if n := utf8.RuneCountInString(text); n == 0 || n > maxDraftText {
		return badRequest("invalid_text", "Describe your workshop in 1 to %d characters.", maxDraftText)
	}
	loc, err := timeZone(req.Timezone)
	if err != nil {
		return err
	}
	zone := req.Timezone
	now := s.Clock.Now()
	if s.drafts != nil && !s.drafts.allow(clientAddr(r), now) {
		w.Header().Set("Retry-After", "60")
		return errDraftLimited
	}
	draft, ok := s.draft(r, ai.DraftInput{Text: text, Timezone: req.Timezone, Today: now.In(loc)})
	if place, err := timeZone(draft.Timezone); err == nil {
		loc, zone = place, draft.Timezone
	}
	out := cleanDraft(draft, loc, now)
	out.Timezone = zone
	writeJSON(w, http.StatusOK, draftResponse{Draft: out, Drafted: ok})
	return nil
}

func (s *server) draft(r *http.Request, in ai.DraftInput) (ai.Draft, bool) {
	release, err := s.acquireAI(r.Context())
	if err != nil {
		s.Log.Warn("draft workshop: no reader free", "error", err)
		return ai.Draft{}, false
	}
	defer release()
	ctx, cancel := context.WithTimeout(r.Context(), draftTimeout)
	defer cancel()
	d, meta, err := s.AI.Draft(ctx, in)
	if err != nil {
		s.Log.Error("draft workshop", "error", err, "provider", meta.Provider, "model", meta.Model)
		return ai.Draft{}, false
	}
	s.Log.Info("draft workshop", "provider", meta.Provider, "model", meta.Model, "input_tokens", meta.InputTokens, "output_tokens", meta.OutputTokens)
	return d, true
}

func cleanDraft(d ai.Draft, loc *time.Location, now time.Time) draftDTO {
	out := draftDTO{
		Title:       clip(d.Title, maxTitle),
		Description: clip(d.Description, maxDescription),
		HostName:    clip(d.HostName, maxHostName),
		Location:    clip(d.Location, maxLocation),
		Price:       amount(d.Price, false),
		VenueCost:   amount(d.VenueCost, true),
		Dates:       draftDates(d.Dates, loc, now),
	}
	if d.DurationMin >= 1 && d.DurationMin <= maxDurationMin {
		out.DurationMin = d.DurationMin
	}
	if d.Capacity >= 1 && d.Capacity <= maxCapacity {
		out.Capacity = d.Capacity
	}
	if d.Minimum >= 1 && (out.Capacity == 0 || d.Minimum <= out.Capacity) {
		out.Minimum = d.Minimum
	}
	out.Missing = missingFields(out)
	return out
}

func clip(s string, limit int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	return strings.TrimSpace(string([]rune(s)[:limit]))
}

func amount(value string, zeroAllowed bool) string {
	m, err := domain.ParseMoney(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "$")), "USD")
	if err != nil || m.Minor < 0 || (m.Minor == 0 && !zeroAllowed) {
		return ""
	}
	return m.String()
}

func draftDates(in []ai.DraftDate, loc *time.Location, now time.Time) []draftDateDTO {
	var starts []time.Time
	for _, d := range in {
		t, err := time.ParseInLocation(draftDayClock, d.Date+" "+d.Start, loc)
		if err != nil || !t.After(now) || slices.ContainsFunc(starts, t.Equal) {
			continue
		}
		starts = append(starts, t)
	}
	slices.SortFunc(starts, time.Time.Compare)
	out := make([]draftDateDTO, 0, min(len(starts), maxDates))
	for _, t := range starts[:min(len(starts), maxDates)] {
		out = append(out, draftDateDTO{Date: t.Format(time.DateOnly), Start: t.Format("15:04")})
	}
	return out
}

func missingFields(d draftDTO) []string {
	empty := map[string]bool{
		"title": d.Title == "", "description": d.Description == "", "host_name": d.HostName == "", "location": d.Location == "",
		"duration_min": d.DurationMin == 0, "price": d.Price == "", "venue_cost": d.VenueCost == "",
		"minimum": d.Minimum == 0, "capacity": d.Capacity == 0, "dates": len(d.Dates) == 0,
	}
	missing := []string{}
	for _, f := range draftFields {
		if empty[f] {
			missing = append(missing, f)
		}
	}
	return missing
}
