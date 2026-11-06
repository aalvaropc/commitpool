package httpapi

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/aalvaropc/commitpool/internal/demo"
	"github.com/aalvaropc/commitpool/internal/inproc"
)

const (
	demoTimezone      = "America/New_York"
	demoWindowMin     = 1440
	demoAnswerTimeout = 3 * time.Minute
	demoHourlyCap     = 30
)

type demoQuota struct {
	mu      sync.Mutex
	visitor *rateLimiter
	hour    time.Time
	used    int
}

func newDemoQuota() *demoQuota {
	return &demoQuota{visitor: newRateLimiter(1)}
}

func (q *demoQuota) allow(addr string, now time.Time) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if hour := now.Truncate(time.Hour); !hour.Equal(q.hour) {
		q.hour, q.used = hour, 0
	}
	if q.used >= demoHourlyCap || !q.visitor.allow(addr, now) {
		return false
	}
	q.used++
	return true
}

type demoInfoDTO struct {
	Workshops bool `json:"workshops"`
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
	writeJSON(w, http.StatusOK, demoInfoDTO{Workshops: true})
	return nil
}

func (s *server) createDemoWorkshop(w http.ResponseWriter, r *http.Request) error {
	if !s.Cfg.DemoMode {
		return errNotFound
	}
	if s.demoQuota != nil && !s.demoQuota.allow(clientAddr(r), s.Clock.Now()) {
		return errRateLimited
	}
	api := inproc.Client{Handler: s.self}
	ctx := r.Context()
	opts := demo.Options{Now: s.Clock.Now(), Timezone: demoTimezone, WindowMin: demoWindowMin, Demo: true}
	res, err := demo.Create(ctx, api, demo.Maya, opts)
	if err != nil {
		return err
	}
	people, err := demo.Join(ctx, api, res, demo.Maya.People)
	if err != nil {
		return err
	}
	s.answerInBackground(ctx, api, people)
	out := demoWorkshopDTO{OwnerURL: res.OwnerURL, ShareURL: res.ShareURL}
	for _, p := range people {
		out.Participants = append(out.Participants, demoParticipantDTO{Name: p.Name, ParticipantURL: p.URL})
	}
	writeJSON(w, http.StatusCreated, out)
	return nil
}

func (s *server) answerInBackground(ctx context.Context, api inproc.Client, people []demo.Participant) {
	bctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), demoAnswerTimeout)
	pace := s.Cfg.DemoPace
	go func() {
		defer cancel()
		err := demo.Answer(bctx, api, people, func(ctx context.Context) error {
			if pace <= 0 {
				return nil
			}
			select {
			case <-time.After(pace):
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		if err != nil {
			s.Log.Warn("demo: answers stopped", "error", err)
		}
	}()
}
