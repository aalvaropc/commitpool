package httpapi

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/aalvaropc/commitpool/internal/ai"
	"github.com/aalvaropc/commitpool/internal/demo"
	"github.com/aalvaropc/commitpool/internal/inproc"
	"github.com/aalvaropc/commitpool/internal/store"
)

const (
	demoTimezone     = "America/New_York"
	demoWindowMin    = 1440
	demoHourlyCap    = 30
	demoRunningCap   = 6
	demoVisitorEvery = 20 * time.Minute
	demoVisitorBurst = 5
	demoModelCalls   = 10
)

type demoRefusal struct {
	err  *apiError
	wait time.Duration
}

type demoQuota struct {
	mu       sync.Mutex
	visitors *rateLimiter
	started  slidingCap
	running  int
}

type demoTicket struct {
	q       *demoQuota
	started time.Time
	once    sync.Once
}

func newDemoQuota(perVisitor bool) *demoQuota {
	q := &demoQuota{started: slidingCap{limit: demoHourlyCap, within: time.Hour}}
	if perVisitor {
		q.visitors = newIntervalLimiter(demoVisitorEvery, demoVisitorBurst)
	}
	return q
}

func minutes(d time.Duration) string {
	n := int(math.Ceil(d.Minutes()))
	if n <= 1 {
		return "a minute"
	}
	return strconv.Itoa(n) + " minutes"
}

func demoResting(wait time.Duration) string {
	if wait < time.Hour {
		return "Claude has read many messages this hour. Try the demo again in " + minutes(wait) + ", or create your own workshop."
	}
	hours := "an hour"
	if n := int(math.Ceil(wait.Hours())); n > 1 {
		hours = strconv.Itoa(n) + " hours"
	}
	return "Claude has read many messages today. Try the demo again in about " + hours + ", or create your own workshop."
}

func (q *demoQuota) start(addr string, now time.Time) (*demoTicket, *demoRefusal) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if wait := q.started.wait(now); wait > 0 {
		return nil, &demoRefusal{wait: wait, err: newError(http.StatusTooManyRequests, "demo_busy",
			fmt.Sprintf("Many people tried the demo this hour. Try again in %s, or create your own workshop.", minutes(wait)))}
	}
	if q.running >= demoRunningCap {
		return nil, &demoRefusal{wait: time.Minute, err: newError(http.StatusTooManyRequests, "demo_busy",
			"Several demos are filling in right now. Try again in a minute.")}
	}
	if q.visitors != nil {
		if wait := q.visitors.wait(addr, now); wait > 0 {
			return nil, &demoRefusal{wait: wait, err: newError(http.StatusTooManyRequests, "demo_rate_limited",
				fmt.Sprintf("You started a demo a few minutes ago. Use the links from that one, or try again in %s.", minutes(wait)))}
		}
	}
	q.started.add(now)
	q.running++
	return &demoTicket{q: q, started: now}, nil
}

func (t *demoTicket) done() {
	t.once.Do(func() {
		t.q.mu.Lock()
		defer t.q.mu.Unlock()
		t.q.running--
	})
}

func (t *demoTicket) refund() {
	t.q.mu.Lock()
	t.q.started.remove(t.started)
	t.q.mu.Unlock()
	t.done()
}

type demoInfoDTO struct {
	Workshops  bool           `json:"workshops"`
	VideoURL   string         `json:"video_url,omitempty"`
	DevpostURL string         `json:"devpost_url,omitempty"`
	JudgeLogin *judgeLoginDTO `json:"judge_login,omitempty"`
}

type judgeLoginDTO struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type demoParticipantDTO struct {
	Name           string `json:"name"`
	ParticipantURL string `json:"participant_url"`
}

type demoWorkshopDTO struct {
	OwnerURL     string               `json:"owner_url"`
	ShareURL     string               `json:"share_url"`
	Participants []demoParticipantDTO `json:"participants"`
}

func (s *server) demoInfo(w http.ResponseWriter, _ *http.Request) error {
	if !s.Cfg.DemoMode {
		return errNotFound
	}
	info := demoInfoDTO{Workshops: true, VideoURL: s.Cfg.VideoEmbedURL, DevpostURL: s.Cfg.DevpostURL}
	if s.Cfg.JudgeBuyerEmail != "" && s.Cfg.JudgeBuyerPassword != "" {
		info.JudgeLogin = &judgeLoginDTO{Email: s.Cfg.JudgeBuyerEmail, Password: s.Cfg.JudgeBuyerPassword}
	}
	writeJSON(w, http.StatusOK, info)
	return nil
}

func (s *server) createDemoWorkshop(w http.ResponseWriter, r *http.Request) error {
	if !s.Cfg.DemoMode {
		return errNotFound
	}
	if c, ok := s.AI.(ai.Capacity); ok {
		if wait := c.RoomLeaving(demoModelCalls, windowReserve); wait > 0 {
			retryAfter(w, wait)
			return newError(http.StatusTooManyRequests, "demo_resting", demoResting(wait))
		}
	}
	ticket, refused := s.demoQuota.start(clientAddr(r), s.Clock.Now())
	if refused != nil {
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(refused.wait.Seconds()))))
		return refused.err
	}
	api := inproc.Client{Handler: s.self}
	opts := demo.Options{Now: s.Clock.Now(), Timezone: demoTimezone, WindowMin: demoWindowMin, Demo: true}
	res, err := demo.Create(r.Context(), api, demo.LifeDrawing, opts)
	if err != nil {
		ticket.refund()
		return err
	}
	ticket.done()
	writeJSON(w, http.StatusCreated, demoWorkshopDTO{OwnerURL: res.OwnerURL, ShareURL: res.ShareURL, Participants: []demoParticipantDTO{}})
	return nil
}

func demoChat(ws store.WorkshopRow, loc *time.Location) *string {
	if !ws.IsDemo {
		return nil
	}
	chat := demo.LifeDrawing.Chat(ws.CreatedAt, loc)
	return &chat
}

func (s *server) pace() demo.Pace {
	pace := s.Cfg.DemoPace
	return func(ctx context.Context) error {
		if pace <= 0 {
			return nil
		}
		t := time.NewTimer(pace)
		defer t.Stop()
		select {
		case <-t.C:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
