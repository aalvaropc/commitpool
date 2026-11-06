package httpapi

import (
	"context"
	"net"
	"net/http"
	"slices"
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
	every     time.Duration
	burst     int
	buckets   map[string]*bucket
	lastSweep time.Time
}

type bucket struct {
	lim  *rate.Limiter
	seen time.Time
}

func newRateLimiter(perMin int) *rateLimiter {
	return newIntervalLimiter(time.Minute/time.Duration(perMin), max(perMin/2, 1))
}

func newIntervalLimiter(every time.Duration, burst int) *rateLimiter {
	return &rateLimiter{every: every, burst: burst, buckets: map[string]*bucket{}}
}

func (l *rateLimiter) allow(addr string, now time.Time) bool {
	return l.wait(addr, now) == 0
}

func (l *rateLimiter) wait(addr string, now time.Time) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.lastSweep) > sweepEvery {
		idle := max(idleAfter, l.every*time.Duration(l.burst))
		for a, b := range l.buckets {
			if now.Sub(b.seen) > idle {
				delete(l.buckets, a)
			}
		}
		l.lastSweep = now
	}
	b, ok := l.buckets[addr]
	if !ok {
		b = &bucket{lim: rate.NewLimiter(rate.Every(l.every), l.burst)}
		l.buckets[addr] = b
	}
	b.seen = now
	r := b.lim.ReserveN(now, 1)
	if d := r.DelayFrom(now); d > 0 {
		r.CancelAt(now)
		return d
	}
	return 0
}

func limited(r *http.Request) bool {
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return strings.HasSuffix(r.URL.Path, "/og.png")
}

func clientAddr(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsPrivate() || ip.IsLoopback()) {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			hops := strings.Split(fwd, ",")
			host = strings.TrimSpace(hops[len(hops)-1])
		}
	}
	if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
		return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
	}
	return host
}

type slidingCap struct {
	limit  int
	within time.Duration
	times  []time.Time
}

func (c *slidingCap) wait(now time.Time) time.Duration {
	c.times = slices.DeleteFunc(c.times, func(t time.Time) bool { return !now.Before(t.Add(c.within)) })
	if len(c.times) < c.limit {
		return 0
	}
	return c.times[0].Add(c.within).Sub(now)
}

func (c *slidingCap) add(t time.Time) {
	c.times = append(c.times, t)
}

func (c *slidingCap) remove(t time.Time) {
	if i := slices.IndexFunc(c.times, t.Equal); i >= 0 {
		c.times = slices.Delete(c.times, i, i+1)
	}
}

func tryAcquire(slots chan struct{}) (release func(), ok bool) {
	select {
	case slots <- struct{}{}:
		return func() { <-slots }, true
	default:
		return nil, false
	}
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
