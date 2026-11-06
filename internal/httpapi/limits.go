package httpapi

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

var (
	maxParticipantsPerWorkshop   = 200
	maxSubmissionsPerParticipant = 20
	aiSlots                      = 4
	aiWait                       = 10 * time.Second
)

const (
	sweepEvery = 5 * time.Minute
	idleAfter  = 10 * time.Minute
)

type rateLimiter struct {
	mu        sync.Mutex
	perMin    int
	buckets   map[string]*bucket
	lastSweep time.Time
}

type bucket struct {
	lim  *rate.Limiter
	seen time.Time
}

func newRateLimiter(perMin int) *rateLimiter {
	return &rateLimiter{perMin: perMin, buckets: map[string]*bucket{}}
}

func (l *rateLimiter) allow(addr string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.lastSweep) > sweepEvery {
		for a, b := range l.buckets {
			if now.Sub(b.seen) > idleAfter {
				delete(l.buckets, a)
			}
		}
		l.lastSweep = now
	}
	b, ok := l.buckets[addr]
	if !ok {
		b = &bucket{lim: rate.NewLimiter(rate.Every(time.Minute/time.Duration(l.perMin)), max(l.perMin/2, 1))}
		l.buckets[addr] = b
	}
	b.seen = now
	return b.lim.AllowN(now, 1)
}

func limited(r *http.Request) bool {
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

func clientAddr(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsPrivate() || ip.IsLoopback()) {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			hops := strings.Split(fwd, ",")
			return strings.TrimSpace(hops[len(hops)-1])
		}
	}
	return host
}

func (s *server) acquireAI(ctx context.Context) (release func(), err error) {
	t := time.NewTimer(aiWait)
	defer t.Stop()
	select {
	case s.aiSem <- struct{}{}:
		return func() { <-s.aiSem }, nil
	case <-t.C:
		return nil, errAIBusy
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
