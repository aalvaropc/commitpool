package httpapi

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/aalvaropc/commitpool/internal/ai"
	"github.com/aalvaropc/commitpool/internal/domain"
)

const (
	maxDraftText      = 2000
	draftTimeout      = 30 * time.Second
	draftVisitorEvery = 15 * time.Second
	draftVisitorBurst = 2
	draftHourlyCap    = 120
	draftSlots        = 2
	draftDayClock     = "2006-01-02 15:04"
	maxDraftAmount    = 999999999
)

type draftQuota struct {
	mu       sync.Mutex
	visitors *rateLimiter
	hour     slidingCap
	slots    chan struct{}
}

func newDraftQuota(perVisitor bool) *draftQuota {
	q := &draftQuota{hour: slidingCap{limit: draftHourlyCap, within: time.Hour}, slots: make(chan struct{}, draftSlots)}
	if perVisitor {
		q.visitors = newIntervalLimiter(draftVisitorEvery, draftVisitorBurst)
	}
	return q
}

func (q *draftQuota) take(addr string, now time.Time) (time.Duration, *apiError) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.visitors != nil {
		if wait := q.visitors.wait(addr, now); wait > 0 {
			return wait, newError(http.StatusTooManyRequests, "draft_rate_limited",
				"Claude just drafted twice for you. Wait a moment, or fill in the form yourself.")
		}
	}
	if wait := q.hour.wait(now); wait > 0 {
		return wait, newError(http.StatusTooManyRequests, "draft_busy",
			fmt.Sprintf("Claude has drafted many workshops this hour. Try again in %s, or fill in the form yourself.", minutes(wait)))
	}
	q.hour.add(now)
	return 0, nil
}

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
	if wait, refused := s.drafts.take(clientAddr(r), now); refused != nil {
		w.Header().Set("Retry-After", strconv.Itoa(max(1, int(math.Ceil(wait.Seconds())))))
		return refused
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
	release, ok := tryAcquire(s.drafts.slots)
	if !ok {
		s.Log.Warn("draft workshop: every drafting slot is busy")
		return ai.Draft{}, false
	}
	defer release()
	ctx, cancel := context.WithTimeout(r.Context(), draftTimeout)
	defer cancel()
	d, meta, err := s.AI.Draft(ctx, in)
	if wait, capped := ai.BusyWait(err); capped {
		s.Log.Warn("draft workshop: Claude is capped; answering with an empty draft", "retry_after", wait)
		return ai.Draft{}, false
	}
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
	if d.Minimum >= 1 && d.Minimum <= maxCapacity && (out.Capacity == 0 || d.Minimum <= out.Capacity) {
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
	if err != nil || m.Minor < 0 || m.Minor > maxDraftAmount || (m.Minor == 0 && !zeroAllowed) {
		return ""
	}
	return m.String()
}

func draftDates(in []ai.DraftDate, loc *time.Location, now time.Time) []draftDateDTO {
	var starts []time.Time
	for _, d := range in {
		t, err := time.ParseInLocation(draftDayClock, d.Date+" "+d.Start, loc)
		if err != nil || t.Format(draftDayClock) != d.Date+" "+d.Start || !t.After(now) || slices.ContainsFunc(starts, t.Equal) {
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
