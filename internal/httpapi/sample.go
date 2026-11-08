package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/aalvaropc/commitpool/internal/ai"
	"github.com/aalvaropc/commitpool/internal/config"
	"github.com/aalvaropc/commitpool/internal/demo"
	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/present"
	"github.com/aalvaropc/commitpool/internal/store"
)

const (
	maxSampleMessage   = 160
	sampleVisitorEvery = 20 * time.Second
	sampleVisitorBurst = 3
	sampleHourlyCap    = 60
	sampleSlots        = 2
	sampleBusyWait     = 5 * time.Second
	sampleReserve      = 20
	readByClaude       = "claude"
	readByRules        = "rules"
)

var sampleTimeout = 12 * time.Second

var sampleKeys = []string{"sat", "sun"}

type sampleQuota struct {
	mu       sync.Mutex
	visitors *rateLimiter
	hour     slidingCap
	slots    chan struct{}
}

func newSampleQuota(perVisitor bool) *sampleQuota {
	q := &sampleQuota{hour: slidingCap{limit: sampleHourlyCap, within: time.Hour}, slots: make(chan struct{}, sampleSlots)}
	if perVisitor {
		q.visitors = newIntervalLimiter(sampleVisitorEvery, sampleVisitorBurst)
	}
	return q
}

func (q *sampleQuota) take(addr string, now time.Time) (time.Duration, *apiError) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.visitors != nil {
		if wait := q.visitors.wait(addr, now); wait > 0 {
			return wait, newError(http.StatusTooManyRequests, "sample_rate_limited",
				"You sent a few answers just now. Wait a few seconds and send another.")
		}
	}
	if wait := q.hour.wait(now); wait > 0 {
		return wait, newError(http.StatusTooManyRequests, "sample_capped",
			fmt.Sprintf("Claude has read many sample answers this hour. Try again in %s, or try it as Maya.", minutes(wait)))
	}
	q.hour.add(now)
	return 0, nil
}

func (q *sampleQuota) refund(at time.Time) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.hour.remove(at)
}

type sampleDay struct {
	key        string
	start, end time.Time
}

func sampleDays(now time.Time, loc *time.Location) []sampleDay {
	saturday := demo.FirstSaturday(now, demoWindowMin, demo.Maya.StartHour, loc)
	length := time.Duration(demo.Maya.DurationMin) * time.Minute
	out := make([]sampleDay, len(sampleKeys))
	for i, key := range sampleKeys {
		start := saturday.AddDate(0, 0, i)
		out[i] = sampleDay{key: key, start: start, end: start.Add(length)}
	}
	return out
}

type sampleRequest struct {
	Message string `json:"message"`
}

func sampleInput(message string, days []sampleDay, loc *time.Location) ai.InterpretInput {
	dates := make([]ai.DateOption, len(days))
	for i, d := range days {
		dates[i] = ai.DateOption{ID: d.key, Label: present.DateLabel(d.start, d.end, loc), StartsAt: d.start}
	}
	return ai.InterpretInput{WorkshopTitle: demo.Maya.Title, Timezone: demoTimezone, Dates: dates, Message: message}
}

type sampleDateDTO struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Hours string `json:"hours"`
}

type sampleVerdictDTO struct {
	Verdict   domain.Verdict   `json:"verdict"`
	Condition domain.Condition `json:"condition"`
	Evidence  string           `json:"evidence"`
	Counted   bool             `json:"counted"`
}

type sampleReadingDTO struct {
	ReadBy   string                      `json:"read_by"`
	Dates    []sampleDateDTO             `json:"dates"`
	PerDate  map[string]sampleVerdictDTO `json:"per_date"`
	Question string                      `json:"question"`
}

func (s *server) sampleReading(w http.ResponseWriter, r *http.Request) error {
	var req sampleRequest
	if err := decode(r, &req); err != nil {
		return err
	}
	message := strings.TrimSpace(req.Message)
	length := utf8.RuneCountInString(message)
	if length == 0 || length > maxSampleMessage {
		return badRequest("invalid_message", "Write your answer in 1 to %d characters.", maxSampleMessage)
	}
	loc, err := time.LoadLocation(demoTimezone)
	if err != nil {
		return err
	}
	now := s.Clock.Now()
	if wait, refused := s.samples.take(clientAddr(r), now); refused != nil {
		retryAfter(w, wait)
		return refused
	}
	release, ok := tryAcquire(s.samples.slots)
	if !ok {
		s.samples.refund(now)
		s.Log.Warn("sample reading: every sample slot is busy", "length", length)
		retryAfter(w, sampleBusyWait)
		return newError(http.StatusTooManyRequests, "sample_busy", "Claude is reading other visitors' answers right now. Try again in a few seconds.")
	}
	defer release()
	if c, ok := s.AI.(ai.Capacity); ok {
		if wait := c.Room(sampleReserve); wait > 0 {
			s.samples.refund(now)
			s.Log.Warn("sample reading: keeping Claude's last readings for participants", "length", length, "retry_after", wait)
			return modelCapped(w, wait, now, loc)
		}
	}
	days := sampleDays(now, loc)
	ctx, cancel := context.WithTimeout(r.Context(), sampleTimeout)
	defer cancel()
	in, meta, err := s.AI.Interpret(ctx, sampleInput(message, days, loc))
	if err != nil {
		if wait, capped := ai.BusyWait(err); capped {
			s.samples.refund(now)
			s.Log.Warn("sample reading: Claude is capped", "length", length, "retry_after", wait)
			return modelCapped(w, wait, now, loc)
		}
		s.Log.Error("sample reading", "length", length, "error", err, "provider", meta.Provider, "model", meta.Model)
		return newError(http.StatusServiceUnavailable, "ai_unavailable", "Claude could not read this answer right now.")
	}
	out := s.sampleAnswer(in, days, loc)
	s.Log.Info("sample reading", "length", length, "read_by", out.ReadBy, "asked", out.Question != "",
		"counted_sat", out.PerDate["sat"].Counted, "counted_sun", out.PerDate["sun"].Counted,
		"provider", meta.Provider, "model", meta.Model, "input_tokens", meta.InputTokens, "output_tokens", meta.OutputTokens)
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (s *server) sampleAnswer(in domain.Interpretation, days []sampleDay, loc *time.Location) sampleReadingDTO {
	rows := make([]store.CandidateDateRow, len(days))
	for i, d := range days {
		rows[i] = store.CandidateDateRow{ID: d.key, StartsAt: d.start, EndsAt: d.end}
	}
	clean, _ := cleanInterpretation(in, rows)
	out := sampleReadingDTO{ReadBy: readByRules, Dates: make([]sampleDateDTO, len(days)), PerDate: make(map[string]sampleVerdictDTO, len(days))}
	if s.Cfg.AIProvider == config.AIProviderBedrock {
		out.ReadBy = readByClaude
	}
	for i, d := range days {
		v := clean.PerDate[i]
		counted, _ := domain.Compatible(clean, domain.CandidateDate{ID: d.key, StartsAt: d.start, EndsAt: d.end}, loc)
		out.Dates[i] = sampleDateDTO{Key: d.key, Label: present.Day(d.start, loc), Hours: present.Clock(d.start.In(loc)) + "–" + present.Clock(d.end.In(loc))}
		out.PerDate[d.key] = sampleVerdictDTO{Verdict: v.Verdict, Condition: v.Condition, Evidence: clip(v.Evidence, maxSampleMessage), Counted: counted}
	}
	if clean.NeedsClarification {
		for _, a := range clean.Ambiguities {
			if q := strings.TrimSpace(a.Question); q != "" {
				out.Question = clip(q, maxSampleMessage*2)
				break
			}
		}
	}
	return out
}
