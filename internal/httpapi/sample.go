package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"math"
	"net/http"
	"strconv"
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
	maxSampleMessage     = 160
	maxSampleQuestions   = 3
	maxSampleQuestion    = 200
	sampleVisitorEvery   = 20 * time.Second
	sampleVisitorBurst   = 3
	sampleVisitorHourly  = 10
	sampleHourlyCap      = 60
	sampleDailyCap       = 150
	sampleSlots          = 2
	sampleBusyWait       = 5 * time.Second
	minSampleReserve     = 20
	sampleReserveDivisor = 10
	readByClaude         = "claude"
	readByRules          = "rules"
	questionSigContext   = "commitpool sample question\n"
)

var sampleTimeout = 12 * time.Second

var sampleKeys = []string{"sat", "sun"}

type sampleQuota struct {
	mu        sync.Mutex
	visitors  *rateLimiter
	perHour   map[string]*slidingCap
	lastSweep time.Time
	hour      slidingCap
	day       slidingCap
	slots     chan struct{}
}

func newSampleQuota(perVisitor bool) *sampleQuota {
	q := &sampleQuota{
		hour:  slidingCap{limit: sampleHourlyCap, within: time.Hour},
		day:   slidingCap{limit: sampleDailyCap, within: 24 * time.Hour},
		slots: make(chan struct{}, sampleSlots),
	}
	if perVisitor {
		q.visitors = newIntervalLimiter(sampleVisitorEvery, sampleVisitorBurst)
		q.perHour = map[string]*slidingCap{}
	}
	return q
}

func later(wait time.Duration) string {
	if wait <= time.Hour {
		return "in " + minutes(wait)
	}
	if n := int(math.Ceil(wait.Hours())); n > 1 {
		return "in about " + strconv.Itoa(n) + " hours"
	}
	return "in about an hour"
}

func (q *sampleQuota) visitorHour(addr string, now time.Time) *slidingCap {
	if now.Sub(q.lastSweep) > sweepEvery {
		for a, c := range q.perHour {
			if c.wait(now) == 0 && len(c.times) == 0 {
				delete(q.perHour, a)
			}
		}
		q.lastSweep = now
	}
	c, ok := q.perHour[addr]
	if !ok {
		c = &slidingCap{limit: sampleVisitorHourly, within: time.Hour}
		q.perHour[addr] = c
	}
	return c
}

func (q *sampleQuota) take(addr string, now time.Time) (time.Duration, *apiError) {
	q.mu.Lock()
	defer q.mu.Unlock()
	var mine *slidingCap
	if q.visitors != nil {
		if wait := q.visitors.wait(addr, now); wait > 0 {
			return wait, newError(http.StatusTooManyRequests, "sample_rate_limited",
				"You sent a few answers just now. Wait a few seconds and send another.")
		}
		mine = q.visitorHour(addr, now)
		if wait := mine.wait(now); wait > 0 {
			return wait, newError(http.StatusTooManyRequests, "sample_rate_limited",
				fmt.Sprintf("You sent many answers this hour. Try again %s, or try it as Maya.", later(wait)))
		}
	}
	if wait := q.hour.wait(now); wait > 0 {
		return wait, newError(http.StatusTooManyRequests, "sample_capped",
			fmt.Sprintf("Claude has read many sample answers this hour. Try again %s, or try it as Maya.", later(wait)))
	}
	if wait := q.day.wait(now); wait > 0 {
		return wait, newError(http.StatusTooManyRequests, "sample_capped",
			fmt.Sprintf("Claude has read many sample answers today. Try again %s, or try it as Maya.", later(wait)))
	}
	if mine != nil {
		mine.add(now)
	}
	q.hour.add(now)
	q.day.add(now)
	return 0, nil
}

func (q *sampleQuota) refund(addr string, at time.Time) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if c, ok := q.perHour[addr]; ok {
		c.remove(at)
	}
	q.hour.remove(at)
	q.day.remove(at)
}

func sampleReserve(dailyCap int) int {
	return max(minSampleReserve, dailyCap/sampleReserveDivisor)
}

func newQuestionKey(secret []byte) []byte {
	if len(secret) > 0 {
		return secret
	}
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	return key
}

func (s *server) questionSig(question string) string {
	m := hmac.New(sha256.New, s.questionKey)
	m.Write([]byte(questionSigContext + question))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func (s *server) askedByUs(questions, sigs []string) bool {
	if len(questions) == 0 || len(questions) != len(sigs) {
		return false
	}
	for i, q := range questions {
		if !hmac.Equal([]byte(sigs[i]), []byte(s.questionSig(q))) {
			return false
		}
	}
	return true
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

type sampleEarlier struct {
	Message      string   `json:"message"`
	Questions    []string `json:"questions"`
	QuestionSigs []string `json:"question_sigs"`
}

type sampleRequest struct {
	Message string         `json:"message"`
	Earlier *sampleEarlier `json:"earlier"`
}

func (s *server) sampleContext(e *sampleEarlier) (*ai.EarlierAnswer, error) {
	if e == nil {
		return nil, nil
	}
	invalid := badRequest("invalid_earlier", "Send the earlier answer in 1 to %d characters, with at most %d questions of up to %d characters each.",
		maxSampleMessage, maxSampleQuestions, maxSampleQuestion)
	message := strings.TrimSpace(e.Message)
	if n := utf8.RuneCountInString(message); n == 0 || n > maxSampleMessage || len(e.Questions) > maxSampleQuestions || len(e.QuestionSigs) > maxSampleQuestions {
		return nil, invalid
	}
	questions := make([]string, 0, len(e.Questions))
	for _, q := range e.Questions {
		q = strings.TrimSpace(q)
		if n := utf8.RuneCountInString(q); n == 0 || n > maxSampleQuestion {
			return nil, invalid
		}
		questions = append(questions, q)
	}
	out := &ai.EarlierAnswer{Message: message, Questions: []string{}}
	if s.askedByUs(questions, e.QuestionSigs) {
		out.Questions = questions
	}
	return out, nil
}

func sampleInput(message string, earlier *ai.EarlierAnswer, days []sampleDay, loc *time.Location) ai.InterpretInput {
	dates := make([]ai.DateOption, len(days))
	for i, d := range days {
		dates[i] = ai.DateOption{ID: d.key, Label: present.DateLabel(d.start, d.end, loc), StartsAt: d.start}
	}
	return ai.InterpretInput{WorkshopTitle: demo.Maya.Title, Timezone: demoTimezone, Dates: dates, Message: message, Earlier: earlier}
}

type sampleDateDTO struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Hours string `json:"hours"`
}

type sampleVerdictDTO struct {
	Verdict   domain.Verdict   `json:"verdict"`
	Condition domain.Condition `json:"condition"`
	Counted   bool             `json:"counted"`
}

type sampleReadingDTO struct {
	ReadBy      string                      `json:"read_by"`
	Dates       []sampleDateDTO             `json:"dates"`
	PerDate     map[string]sampleVerdictDTO `json:"per_date"`
	Question    string                      `json:"question"`
	QuestionSig string                      `json:"question_sig"`
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
	earlier, err := s.sampleContext(req.Earlier)
	if err != nil {
		return err
	}
	loc, err := time.LoadLocation(demoTimezone)
	if err != nil {
		return err
	}
	now := s.Clock.Now()
	addr := clientAddr(r)
	if wait, refused := s.samples.take(addr, now); refused != nil {
		retryAfter(w, wait)
		return refused
	}
	release, ok := tryAcquire(s.samples.slots)
	if !ok {
		s.samples.refund(addr, now)
		s.Log.Warn("sample reading: every sample slot is busy", "length", length)
		retryAfter(w, sampleBusyWait)
		return newError(http.StatusTooManyRequests, "sample_busy", "Claude is reading other visitors' answers right now. Try again in a few seconds.")
	}
	defer release()
	if c, ok := s.AI.(ai.Capacity); ok {
		if wait := c.Room(sampleReserve(s.Cfg.AIDailyCap)); wait > 0 {
			s.samples.refund(addr, now)
			s.Log.Warn("sample reading: keeping Claude's last readings for participants", "length", length, "retry_after", wait)
			return modelCapped(w, wait, now, loc)
		}
	}
	days := sampleDays(now, loc)
	ctx, cancel := context.WithTimeout(r.Context(), sampleTimeout)
	defer cancel()
	in, meta, err := s.AI.Interpret(ctx, sampleInput(message, earlier, days, loc))
	if err != nil {
		if wait, capped := ai.BusyWait(err); capped {
			s.samples.refund(addr, now)
			s.Log.Warn("sample reading: Claude is capped", "length", length, "retry_after", wait)
			return modelCapped(w, wait, now, loc)
		}
		s.Log.Error("sample reading", "length", length, "error", err, "provider", meta.Provider, "model", meta.Model)
		return newError(http.StatusServiceUnavailable, "ai_unavailable", "Claude could not read this answer right now.")
	}
	out := s.sampleAnswer(in, days, loc)
	asked := 0
	if earlier != nil {
		asked = len(earlier.Questions)
	}
	s.Log.Info("sample reading", "length", length, "follows_up", earlier != nil, "signed_questions", asked, "read_by", out.ReadBy, "asked", out.Question != "",
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
		out.PerDate[d.key] = sampleVerdictDTO{Verdict: v.Verdict, Condition: v.Condition, Counted: counted}
	}
	if clean.NeedsClarification {
		for _, a := range clean.Ambiguities {
			if q := strings.TrimSpace(a.Question); q != "" {
				out.Question = clip(q, maxSampleQuestion)
				out.QuestionSig = s.questionSig(out.Question)
				break
			}
		}
	}
	return out
}
