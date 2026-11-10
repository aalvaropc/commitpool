package worker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aalvaropc/commitpool/internal/clock"
	"github.com/aalvaropc/commitpool/internal/config"
	"github.com/aalvaropc/commitpool/internal/mail"
	"github.com/aalvaropc/commitpool/internal/paypal"
	"github.com/aalvaropc/commitpool/internal/store"
)

const (
	pollInterval = 2 * time.Second
	tickInterval = time.Minute
	staleAfter   = 5 * time.Minute

	settleTimeout    = 10 * time.Second
	shutdownGrace    = 10 * time.Second
	inlineJobTimeout = 60 * time.Second
)

var reconcileGaps = [...]time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute, time.Hour, 6 * time.Hour}

const maxReconcileGenerations = 5

var errUnknownKind = errors.New("unknown kind")

type Deps struct {
	Store  *store.Store
	PayPal paypal.API
	Clock  clock.Clock
	Cfg    config.Config
	Log    *slog.Logger
	Mailer mail.Mailer
}

type Handler func(ctx context.Context, job *store.Job) (httpStatus *int, err error)

type Runner struct {
	st       *store.Store
	pp       paypal.API
	clock    clock.Clock
	cfg      config.Config
	log      *slog.Logger
	workerID string
	handlers map[string]Handler
	mailer   mail.Mailer
	beats    struct{ drain, tick, periodic atomic.Int64 }
	inline   sync.WaitGroup
	retries  feeRetries
}

func New(d Deps) *Runner {
	log := d.Log
	if log == nil {
		log = slog.Default()
	}
	clk := d.Clock
	if clk == nil {
		clk = clock.Real{}
	}
	mailer := d.Mailer
	if mailer == nil {
		mailer = mail.Log{Log: log}
	}
	r := &Runner{st: d.Store, pp: d.PayPal, clock: clk, cfg: d.Cfg, log: log, workerID: newWorkerID(), mailer: mailer}
	r.handlers = map[string]Handler{
		store.KindCreateOrder:  r.createOrder,
		store.KindAuthorize:    r.authorize,
		store.KindCapture:      r.capture,
		store.KindVoid:         r.void,
		store.KindRefund:       r.refund,
		store.KindReconcile:    r.reconcile,
		store.KindClose:        r.closePayments,
		store.KindReleaseSeats: r.releaseSeats,
		store.KindNotify:       r.notify,
	}
	return r
}

func (r *Runner) Register(kind string, h Handler) {
	r.handlers[kind] = h
}

func (r *Runner) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	for _, l := range []struct {
		name     string
		interval time.Duration
		beat     *atomic.Int64
		fn       func(context.Context)
	}{
		{"drain", pollInterval, &r.beats.drain, r.drain},
		{"tick", tickInterval, &r.beats.tick, func(ctx context.Context) {
			r.requeueStale(ctx)
			if err := r.EnqueueSeatReleases(ctx); err != nil && ctx.Err() == nil {
				r.log.Error("worker: enqueue seat releases", "error", err)
			}
		}},
		{"periodic", periodicInterval, &r.beats.periodic, func(ctx context.Context) {
			if err := r.RunPeriodic(ctx); err != nil && ctx.Err() == nil {
				r.log.Error("worker: periodic reconcile and heal", "error", err)
			}
		}},
	} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.loop(ctx, l.name, l.interval, l.beat, l.fn)
		}()
	}
	wg.Wait()
	return ctx.Err()
}

func (r *Runner) loop(ctx context.Context, name string, interval time.Duration, beat *atomic.Int64, fn func(context.Context)) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		func() {
			defer func() {
				if p := recover(); p != nil {
					r.log.Error("worker: loop panicked; it goes on", "loop", name, "panic", fmt.Sprint(p), "stack", string(debug.Stack()))
				}
			}()
			fn(ctx)
			beat.Store(time.Now().UnixNano())
		}()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

type Health struct {
	Drain, Tick, Periodic time.Time
}

func (r *Runner) Health() Health {
	at := func(v *atomic.Int64) time.Time {
		if n := v.Load(); n != 0 {
			return time.Unix(0, n)
		}
		return time.Time{}
	}
	return Health{Drain: at(&r.beats.drain), Tick: at(&r.beats.tick), Periodic: at(&r.beats.periodic)}
}

func (r *Runner) WaitInline(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		r.inline.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Runner) drain(ctx context.Context) {
	for ctx.Err() == nil {
		ran, err := r.RunOnce(ctx)
		if err != nil {
			if ctx.Err() == nil {
				r.log.Error("worker: run job", "error", err)
			}
			return
		}
		if !ran {
			return
		}
	}
}

func (r *Runner) requeueStale(ctx context.Context) {
	n, err := r.st.RequeueStale(ctx, r.clock.Now(), staleAfter)
	switch {
	case err != nil:
		if ctx.Err() == nil {
			r.log.Error("worker: requeue stale jobs", "error", err)
		}
	case n > 0:
		r.log.Warn("worker: took back stale jobs", "count", n)
	}
}

func (r *Runner) RunOnce(ctx context.Context) (bool, error) {
	job, err := r.st.ClaimJob(ctx, r.workerID, r.clock.Now())
	if err != nil || job == nil {
		return false, err
	}
	jobCtx, cancel := withShutdownGrace(ctx)
	defer cancel()
	return true, r.run(jobCtx, job)
}

func withShutdownGrace(ctx context.Context) (context.Context, context.CancelFunc) {
	jobCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	stop := context.AfterFunc(ctx, func() {
		grace := time.NewTimer(shutdownGrace)
		defer grace.Stop()
		select {
		case <-grace.C:
			cancel()
		case <-jobCtx.Done():
		}
	})
	return jobCtx, func() {
		stop()
		cancel()
	}
}

func (r *Runner) RunKey(ctx context.Context, key string) error {
	r.inline.Add(1)
	defer r.inline.Done()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), inlineJobTimeout)
	defer cancel()
	job, err := r.st.ClaimJobByKey(ctx, key, r.workerID, r.clock.Now())
	if err != nil {
		return err
	}
	if job != nil {
		return r.run(ctx, job)
	}
	existing, err := r.st.JobByKey(ctx, key)
	if err != nil {
		return err
	}
	if existing == nil {
		return store.ErrNotFound
	}
	return nil
}

func (r *Runner) Enqueue(ctx context.Context, kind, key string, workshopID, attemptID *string, payload any, runAt time.Time) error {
	_, err := r.st.EnqueueJob(ctx, kind, key, workshopID, attemptID, payload, runAt)
	return err
}

func (r *Runner) EnqueueOrRevive(ctx context.Context, kind, key string, workshopID, attemptID *string, payload any, runAt time.Time) (created bool, err error) {
	created, err = r.st.EnqueueJob(ctx, kind, key, workshopID, attemptID, payload, runAt)
	if err != nil || created {
		return created, err
	}
	revived, err := r.st.ReviveJob(ctx, key, runAt, false)
	if revived {
		r.log.Info("worker: revived a skipped job under its own key", "kind", kind, "key", key)
	}
	return false, err
}

func (r *Runner) Revive(ctx context.Context, key string) (bool, error) {
	revived, err := r.st.ReviveJob(ctx, key, r.clock.Now(), true)
	if revived {
		r.log.Info("worker: revived a job under its own key", "key", key)
	}
	return revived, err
}

func (r *Runner) ReturnURLs(enrollmentID string) (returnURL, cancelURL string) {
	return r.cfg.BaseURL + "/paypal/return?e=" + enrollmentID, r.cfg.BaseURL + "/paypal/cancel?e=" + enrollmentID
}

func (r *Runner) run(ctx context.Context, job *store.Job) error {
	h, ok := r.handlers[job.Kind]
	if !ok {
		return r.settle(ctx, job, nil, errUnknownKind)
	}
	status, err := r.call(ctx, h, job)
	return r.settle(ctx, job, status, err)
}

func (r *Runner) call(ctx context.Context, h Handler, job *store.Job) (status *int, err error) {
	defer func() {
		if p := recover(); p != nil {
			r.jobLog(job).Error("job handler panicked", "panic", fmt.Sprint(p), "stack", string(debug.Stack()))
			status, err = nil, fmt.Errorf("panic: %v", p)
		}
	}()
	return h(ctx, job)
}

func (r *Runner) settle(ctx context.Context, job *store.Job, status *int, err error) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), settleTimeout)
	defer cancel()
	log := r.jobLog(job)
	if status != nil {
		log = log.With("http_status", *status)
	}
	var pe *paypal.Error
	if errors.As(err, &pe) {
		log = log.With("issue", pe.Issue, "debug_id", pe.DebugID)
	}

	var ref *refusal
	switch {
	case err == nil:
		log.Info("job succeeded")
		return r.tolerate(log, r.st.CompleteJob(ctx, job.ID, status))
	case errors.As(err, &ref):
		issue, debugID := ref.details()
		log.Warn("job refused; it keeps its key and can be revived", "error", err)
		return r.tolerate(log, r.st.FailJob(ctx, job.ID, err.Error(), status, issue, debugID))
	case paypal.IsAmbiguous(err) && job.AttemptID != nil && reconcilable(job.Kind):
		log.Warn("PayPal result is ambiguous; reconciling before acting again", "error", err)
		if rerr := r.scheduleReconcile(ctx, job); rerr != nil {
			return rerr
		}
		return r.tolerate(log, r.st.MarkJobUnknown(ctx, job.ID, err.Error(), status))
	default:
		issue, debugID := providerDetails(err)
		needsAttention, rerr := r.st.RetryJob(ctx, job.ID, r.clock.Now(), err.Error(), status, issue, debugID)
		if rerr != nil {
			return r.tolerate(log, rerr)
		}
		if !needsAttention {
			log.Warn("job failed; it will run again under the same key", "error", err)
			return nil
		}
		if store.RetriesForever(job.Kind) {
			log.Error("job keeps failing; flagged for an operator and still retrying under the same key", "error", err)
		} else {
			log.Error("job failed too many times and needs attention", "error", err)
		}
		return r.escalate(ctx, log, job)
	}
}

func (r *Runner) tolerate(log *slog.Logger, err error) error {
	if errors.Is(err, store.ErrJobNotActive) {
		log.Warn("job changed hands while it ran; this run's result is dropped")
		return nil
	}
	return err
}

func (r *Runner) scheduleReconcile(ctx context.Context, job *store.Job) error {
	now := r.clock.Now()
	jobs, err := r.st.JobsByAttempt(ctx, *job.AttemptID)
	if err != nil {
		return err
	}
	next := 1
	for _, j := range jobs {
		if j.Kind != store.KindReconcile {
			continue
		}
		if j.Status == store.JobPending && j.RunAt.After(now) {
			return nil
		}
		if n, ok := generation(j); ok && n >= next {
			next = n + 1
		}
	}
	key := reconcilePrefix(*job.AttemptID) + strconv.Itoa(next)
	gap := reconcileGaps[min(next, len(reconcileGaps))-1]
	_, err = r.st.EnqueueJob(ctx, store.KindReconcile, key, job.WorkshopID, job.AttemptID, nil, now.Add(gap))
	return err
}

func reconcilable(kind string) bool {
	switch kind {
	case store.KindCreateOrder, store.KindAuthorize, store.KindCapture, store.KindVoid, store.KindRefund:
		return true
	}
	return false
}

func reconcilePrefix(attemptID string) string {
	return store.JobKey(store.KindReconcile, attemptID) + ":"
}

func generation(j store.Job) (n int, ok bool) {
	if j.AttemptID == nil {
		return 0, false
	}
	rest, found := strings.CutPrefix(j.IdempotencyKey, reconcilePrefix(*j.AttemptID))
	if !found {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	return n, err == nil && n > 0
}

func (r *Runner) escalate(ctx context.Context, log *slog.Logger, job *store.Job) error {
	if job.AttemptID == nil {
		if job.WorkshopID != nil && (job.Kind == store.KindClose || job.Kind == store.KindReleaseSeats) {
			return r.flagWorkshop(ctx, log, *job.WorkshopID)
		}
		return nil
	}
	a, err := r.st.AttemptByID(ctx, *job.AttemptID)
	if err != nil || a == nil {
		return err
	}
	log = log.With("enrollment_id", a.EnrollmentID)
	if err := r.flag(ctx, log, a.EnrollmentID, unpaidActiveStatuses, "a "+job.Kind+" job keeps failing"); err != nil {
		return err
	}
	if job.Kind != store.KindVoid && job.Kind != store.KindRefund {
		return nil
	}
	en, err := r.st.EnrollmentByID(ctx, a.EnrollmentID)
	if err != nil || en.NeedsAttentionAt != nil {
		return err
	}
	return r.flagWorkshop(ctx, log, en.WorkshopID)
}

func (r *Runner) jobLog(job *store.Job) *slog.Logger {
	return r.log.With("job_id", job.ID, "kind", job.Kind, "key", job.IdempotencyKey, "attempt_id", deref(job.AttemptID))
}

type refusal struct {
	reason string
	issue  string
	pe     *paypal.Error
}

func (e *refusal) Error() string {
	if e.pe != nil {
		return e.pe.Error()
	}
	return e.reason
}

func (e *refusal) Unwrap() error {
	if e.pe == nil {
		return nil
	}
	return e.pe
}

func (e *refusal) details() (issue, debugID *string) {
	if e.pe != nil {
		return nonEmpty(e.pe.Issue), nonEmpty(e.pe.DebugID)
	}
	return nonEmpty(e.issue), nil
}

func refused(pe *paypal.Error) (*int, error) {
	return statusOf(pe), &refusal{pe: pe}
}

func statusOf(err error) *int {
	var pe *paypal.Error
	if errors.As(err, &pe) {
		status := pe.HTTPStatus
		return &status
	}
	return nil
}

func providerDetails(err error) (issue, debugID *string) {
	var pe *paypal.Error
	if !errors.As(err, &pe) {
		return nil, nil
	}
	return nonEmpty(pe.Issue), nonEmpty(pe.DebugID)
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func newWorkerID() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "worker"
	}
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s-%d-%s", host, os.Getpid(), hex.EncodeToString(b))
}
