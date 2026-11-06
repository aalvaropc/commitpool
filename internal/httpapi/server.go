package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/aalvaropc/commitpool/internal/ai"
	"github.com/aalvaropc/commitpool/internal/clock"
	"github.com/aalvaropc/commitpool/internal/config"
	"github.com/aalvaropc/commitpool/internal/inproc"
	"github.com/aalvaropc/commitpool/internal/paypal"
	"github.com/aalvaropc/commitpool/internal/store"
	"github.com/aalvaropc/commitpool/internal/worker"
)

const (
	maxBodyBytes    = 64 << 10
	healthTimeout   = 2 * time.Second
	requestIDHeader = "X-Request-Id"
)

type Deps struct {
	Store  *store.Store
	Worker *worker.Runner
	PayPal paypal.API
	AI     ai.Interpreter
	Clock  clock.Clock
	Cfg    config.Config
	Log    *slog.Logger
	Dist   fs.FS
	MCP    MCPServer

	Background *sync.WaitGroup
}

type MCPServer interface {
	Attach(api http.Handler)
	HTTPHandler() http.Handler
}

type server struct {
	Deps
	explanations *textCache
	nudges       *textCache
	limiter      *rateLimiter
	demoQuota    *demoQuota
	images       imageCache
	previews     previewCache
	self         http.Handler
	drafts       *draftQuota
	aiSem        chan struct{}
}

func New(d Deps) http.Handler {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	if d.Clock == nil {
		d.Clock = clock.Real{}
	}
	if d.Background == nil {
		d.Background = &sync.WaitGroup{}
	}
	s := &server{Deps: d, explanations: newTextCache(), nudges: newTextCache(), aiSem: make(chan struct{}, aiSlots)}
	if d.Cfg.RateLimitPerMin > 0 {
		s.limiter = newRateLimiter(d.Cfg.RateLimitPerMin)
	}
	s.demoQuota = newDemoQuota(s.limiter != nil)
	s.drafts = newDraftQuota(s.limiter != nil)
	mux := http.NewServeMux()
	s.routes(mux)
	h := s.observe(mux)
	s.self = h
	if d.MCP != nil {
		d.MCP.Attach(h)
		mux.Handle("/mcp", d.MCP.HTTPHandler())
	}
	return h
}

func (s *server) routes(mux *http.ServeMux) {
	api := func(pattern string, h handlerFunc) { mux.Handle(pattern, s.handle(h)) }

	api("POST /api/v1/workshops", s.createWorkshop)
	api("GET /api/v1/demo", s.demoInfo)
	api("POST /api/v1/demo/workshops", s.createDemoWorkshop)
	api("POST /api/v1/workshop-drafts", s.draftWorkshop)
	api("GET /api/v1/w/{token}", s.publicWorkshop)
	api("GET /api/v1/w/{token}/og.png", s.inviteImage)
	api("GET /api/v1/w/{token}/calendar.ics", s.inviteCalendar)
	api("GET /api/v1/p/{token}/calendar.ics", s.participantCalendar)
	api("POST /api/v1/w/{token}/participants", s.joinWorkshop)

	api("GET /api/v1/owner/{token}", s.ownerView)
	api("POST /api/v1/owner/{token}/proposal", s.openProposal)
	api("POST /api/v1/owner/{token}/cancel", s.cancelWorkshop)
	api("POST /api/v1/owner/{token}/operator/reconcile", s.operatorReconcile)
	api("POST /api/v1/owner/{token}/operator/retry-refund", s.operatorRetryRefund)
	api("POST /api/v1/owner/{token}/operator/clear-flag", s.operatorClearFlag)
	api("POST /api/v1/owner/{token}/demo/close-now", s.demoCloseNow)
	api("POST /api/v1/owner/{token}/demo/fail-next-capture", s.demoFailNextCapture)

	api("GET /api/v1/p/{token}", s.participantView)
	api("POST /api/v1/p/{token}/availability", s.submitAvailability)
	api("PUT /api/v1/p/{token}/availability/{id}", s.correctAvailability)
	api("POST /api/v1/p/{token}/enrollment", s.enroll)
	api("POST /api/v1/p/{token}/withdraw", s.withdraw)

	mux.HandleFunc("GET /paypal/return", s.paypalReturn)
	mux.HandleFunc("GET /paypal/cancel", s.paypalCancel)
	api("POST "+webhookPath, s.paypalWebhook)

	api("/api/", func(http.ResponseWriter, *http.Request) error { return errNotFound })
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.Handle("/", spaHandler(s.Dist, s.pagePreview))
}

type handlerFunc func(w http.ResponseWriter, r *http.Request) error

func (s *server) handle(h handlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := h(w, r)
		if err == nil {
			return
		}
		var ae *apiError
		if !errors.As(err, &ae) {
			s.Log.Error("request failed", "request_id", requestID(r.Context()), "method", r.Method, "route", r.Pattern, "error", err)
			ae = errInternal
		}
		writeError(w, ae)
	})
}

func (s *server) healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), healthTimeout)
	defer cancel()
	if err := s.Store.Ping(ctx); err != nil {
		s.Log.Error("health check: database unreachable", "error", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]bool{"ok": false})
		return
	}
	body := map[string]any{"ok": true}
	if q, err := s.Store.Health(ctx); err == nil {
		body["jobs"] = map[string]int{"unknown": q.Unknown, "parked": q.Parked, "stuck_pending": q.StuckPending}
		body["workshops_flagged"] = q.WorkshopsFlagged
	}
	if s.Worker != nil {
		h := s.Worker.Health()
		age := func(t time.Time) any {
			if t.IsZero() {
				return nil
			}
			return time.Since(t).Round(time.Second).Seconds()
		}
		body["worker"] = map[string]any{"drain_age_s": age(h.Drain), "tick_age_s": age(h.Tick), "periodic_age_s": age(h.Periodic)}
	}
	writeJSON(w, http.StatusOK, body)
}

func (s *server) observe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := newRequestID()
		w.Header().Set(requestIDHeader, id)
		r = r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id))
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Content-Security-Policy", "frame-ancestors 'none'")
		h.Set("Referrer-Policy", "no-referrer")
		if strings.HasPrefix(s.Cfg.BaseURL, "https://") {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		sw := &statusWriter{ResponseWriter: w}
		limit := int64(maxBodyBytes)
		if r.URL.Path == webhookPath {
			limit = maxWebhookBytes
		}
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		defer func() {
			if p := recover(); p != nil {
				if p == http.ErrAbortHandler {
					panic(p)
				}
				s.Log.Error("handler panicked", "request_id", id, "method", r.Method, "route", route(r),
					"panic", fmt.Sprint(p), "stack", string(debug.Stack()))
				if !sw.wrote {
					writeError(sw, errInternal)
				}
			}
			if r.Pattern == "GET /healthz" {
				return
			}
			s.Log.Info("request", "request_id", id, "method", r.Method, "route", route(r), "status", sw.code(),
				"duration_ms", time.Since(start).Milliseconds())
		}()
		if s.limiter != nil && limited(r) && !inproc.Is(r.Context()) && !s.limiter.allow(clientAddr(r), time.Now()) {
			sw.Header().Set("Retry-After", "60")
			writeError(sw, errRateLimited)
			return
		}
		next.ServeHTTP(sw, r)
	})
}

type requestIDKey struct{}

func newRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func requestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

func route(r *http.Request) string {
	if r.Pattern == "" {
		return "unmatched"
	}
	return r.Pattern
}

type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *statusWriter) WriteHeader(status int) {
	if !w.wrote {
		w.status, w.wrote = status, true
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *statusWriter) code() int {
	if !w.wrote {
		return http.StatusOK
	}
	return w.status
}

const tokenLength = 43

func pathToken(r *http.Request) (string, error) {
	t := r.PathValue("token")
	if !validToken(t) {
		return "", errNotFound
	}
	return t, nil
}

func validToken(t string) bool {
	if len(t) != tokenLength {
		return false
	}
	for i := 0; i < len(t); i++ {
		c := t[i]
		if !('A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' || '0' <= c && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func notFoundIfMissing(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return errNotFound
	}
	return err
}

func (s *server) ownerWorkshop(r *http.Request) (store.WorkshopRow, error) {
	token, err := pathToken(r)
	if err != nil {
		return store.WorkshopRow{}, err
	}
	w, err := s.Store.WorkshopByOwnerToken(r.Context(), token)
	return w, notFoundIfMissing(err)
}

func (s *server) sharedWorkshop(r *http.Request) (store.WorkshopRow, error) {
	token, err := pathToken(r)
	if err != nil {
		return store.WorkshopRow{}, err
	}
	w, err := s.Store.WorkshopByShareToken(r.Context(), token)
	return w, notFoundIfMissing(err)
}

func (s *server) participant(r *http.Request) (store.ParticipantRow, store.WorkshopRow, error) {
	token, err := pathToken(r)
	if err != nil {
		return store.ParticipantRow{}, store.WorkshopRow{}, err
	}
	p, err := s.Store.ParticipantByToken(r.Context(), token)
	if err != nil {
		return store.ParticipantRow{}, store.WorkshopRow{}, notFoundIfMissing(err)
	}
	w, err := s.Store.WorkshopByID(r.Context(), p.WorkshopID)
	if err != nil {
		return store.ParticipantRow{}, store.WorkshopRow{}, err
	}
	return p, w, nil
}
