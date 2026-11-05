// Package worker runs the payment jobs in process. It claims due jobs from the
// store, calls PayPal with each job's idempotency key as the PayPal-Request-Id
// and maps PayPal's answers onto the payment attempt and its enrollment. An
// ambiguous answer parks the job as unknown until a reconcile job has read
// the outcome back from PayPal; no path ever retries under a new key.
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
	"time"

	"github.com/aalvaropc/commitpool/internal/clock"
	"github.com/aalvaropc/commitpool/internal/config"
	"github.com/aalvaropc/commitpool/internal/paypal"
	"github.com/aalvaropc/commitpool/internal/store"
)

const (
	pollInterval = 2 * time.Second
	tickInterval = time.Minute
	staleAfter   = 5 * time.Minute

	settleTimeout = 10 * time.Second
	// shutdownGrace is how long a job Run is running may go on once Run's
	// context is done.
	shutdownGrace = 10 * time.Second
)

// reconcileGaps is how long after an ambiguous result each reconcile
// generation runs, by generation: they spread over seven hours, so a PayPal
// outage of a few minutes never uses them up.
var reconcileGaps = [...]time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute, time.Hour, 6 * time.Hour}

// maxReconcileGenerations is how many reconciles an ambiguous action gets: the
// one with this generation no longer sends the action again when PayPal still
// shows no trace of it, and flags the enrollment for an operator instead.
const maxReconcileGenerations = 5

var errUnknownKind = errors.New("unknown kind")

type Deps struct {
	Store  *store.Store
	PayPal paypal.API
	Clock  clock.Clock
	Cfg    config.Config
	Log    *slog.Logger
}

// Handler runs one job. A nil error completes the job. Otherwise the error
// decides what happens next: an ambiguous PayPal error parks the job as
// unknown and schedules a reconcile; a refusal the handler has already
// recorded (see refusal) fails the job, which keeps its key: a skip is
// revived by EnqueueOrRevive once the action applies again, and a PayPal
// refusal only through Revive; anything else, a 429, a store failure or a
// panic included, retries it later under the same key. httpStatus is PayPal's
// HTTP status when the handler knows it.
type Handler func(ctx context.Context, job *store.Job) (httpStatus *int, err error)

type Runner struct {
	st       *store.Store
	pp       paypal.API
	clock    clock.Clock
	cfg      config.Config
	log      *slog.Logger
	workerID string
	handlers map[string]Handler
}

// New returns a runner with the payment handlers, close and release_seats
// registered. A nil Log uses slog.Default and a nil Clock the real clock.
func New(d Deps) *Runner {
	log := d.Log
	if log == nil {
		log = slog.Default()
	}
	clk := d.Clock
	if clk == nil {
		clk = clock.Real{}
	}
	r := &Runner{st: d.Store, pp: d.PayPal, clock: clk, cfg: d.Cfg, log: log, workerID: newWorkerID()}
	r.handlers = map[string]Handler{
		store.KindCreateOrder:  r.createOrder,
		store.KindAuthorize:    r.authorize,
		store.KindCapture:      r.capture,
		store.KindVoid:         r.void,
		store.KindRefund:       r.refund,
		store.KindReconcile:    r.reconcile,
		store.KindClose:        r.closePayments,
		store.KindReleaseSeats: r.releaseSeats,
	}
	return r
}

// Register adds or replaces the handler for a job kind. Call it before Run.
func (r *Runner) Register(kind string, h Handler) {
	r.handlers[kind] = h
}

// Run claims and runs due jobs until ctx is done, then returns ctx.Err(). When
// nothing is due it waits two seconds before looking again. Once a minute it
// takes back jobs claimed more than five minutes ago and never finished, and
// enqueues the minute's seat release for every workshop with a reserved seat;
// every two minutes it reconciles what PayPal may still change and heals the
// workshops still funding or resolving (RunPeriodic). Once ctx is done it
// claims nothing new and returns when the job in flight has finished, which
// RunOnce lets go on for up to shutdownGrace.
func (r *Runner) Run(ctx context.Context) error {
	var lastTick, lastPeriodic time.Time
	for {
		if lastTick.IsZero() || time.Since(lastTick) >= tickInterval {
			r.requeueStale(ctx)
			if err := r.EnqueueSeatReleases(ctx); err != nil && ctx.Err() == nil {
				r.log.Error("worker: enqueue seat releases", "error", err)
			}
			lastTick = time.Now()
		}
		if lastPeriodic.IsZero() || time.Since(lastPeriodic) >= periodicInterval {
			if err := r.RunPeriodic(ctx); err != nil && ctx.Err() == nil {
				r.log.Error("worker: periodic reconcile and heal", "error", err)
			}
			lastPeriodic = time.Now()
		}
		r.drain(ctx)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
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

// RunOnce claims the next due job, runs it and records its outcome. It
// reports whether a job ran; the error is a failure to claim or to record.
// A job claimed before ctx is done runs on for up to shutdownGrace after
// that, so a shutdown lets a PayPal call already sent finish instead of
// turning its answer into an unknown result.
func (r *Runner) RunOnce(ctx context.Context) (bool, error) {
	job, err := r.st.ClaimJob(ctx, r.workerID, r.clock.Now())
	if err != nil || job == nil {
		return false, err
	}
	jobCtx, cancel := withShutdownGrace(ctx)
	defer cancel()
	return true, r.run(jobCtx, job)
}

// withShutdownGrace returns a context with ctx's values that is done
// shutdownGrace after ctx is, or as soon as cancel is called.
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

// RunKey runs the job with this key now, for a caller that needs its outcome
// before answering, such as an HTTP handler. It returns nil once the outcome
// is recorded on the job, whatever it was: read it back with store.JobByKey.
// A job that exists but cannot be claimed (another worker is running it, it
// is finished or it is not due yet) is left alone and RunKey returns nil; when
// no job has the key it returns store.ErrNotFound.
func (r *Runner) RunKey(ctx context.Context, key string) error {
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

// Enqueue adds a pending job due at runAt. A job with the same key that
// already exists is left as it is.
func (r *Runner) Enqueue(ctx context.Context, kind, key string, workshopID, attemptID *string, payload any, runAt time.Time) error {
	_, err := r.st.EnqueueJob(ctx, kind, key, workshopID, attemptID, payload, runAt)
	return err
}

// EnqueueOrRevive adds a pending job due at runAt or, when a job with the key
// already exists and failed without a provider answer (a skip), sends that
// job back to pending at runAt under its own key, keeping its payload. A job
// PayPal refused, one that gave up, and one that is pending, running, unknown
// or succeeded are left as they are, so an automatic path never re-sends an
// action PayPal already refused. created reports whether a new job was
// written. Callers with new information about a refused job use Revive.
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

// Revive sends the job with this key back to pending now, under its own key,
// when it failed or gave up, whatever the reason: it is for callers that know
// something changed since, such as the HTTP layer once the buyer is back from
// PayPal with an approved order, or an operator. It reports whether a job was
// revived.
func (r *Runner) Revive(ctx context.Context, key string) (bool, error) {
	revived, err := r.st.ReviveJob(ctx, key, r.clock.Now(), true)
	if revived {
		r.log.Info("worker: revived a job under its own key", "key", key)
	}
	return revived, err
}

// ReturnURLs are where PayPal sends the buyer back after approving or
// cancelling the enrollment's checkout.
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

// call runs the handler and turns a panic into an error, so the job is retried
// like any other failure instead of taking the process down with it.
func (r *Runner) call(ctx context.Context, h Handler, job *store.Job) (status *int, err error) {
	defer func() {
		if p := recover(); p != nil {
			r.jobLog(job).Error("job handler panicked", "panic", fmt.Sprint(p), "stack", string(debug.Stack()))
			status, err = nil, fmt.Errorf("panic: %v", p)
		}
	}()
	return h(ctx, job)
}

// settle records the outcome of a run on the job, in a context that outlives
// a shutdown so a result PayPal already produced is not lost.
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

// tolerate drops ErrJobNotActive: the job was taken back or settled elsewhere
// while this run was in flight, and this late result must not overwrite that.
func (r *Runner) tolerate(log *slog.Logger, err error) error {
	if errors.Is(err, store.ErrJobNotActive) {
		log.Warn("job changed hands while it ran; this run's result is dropped")
		return nil
	}
	return err
}

// scheduleReconcile enqueues the attempt's next reconcile generation,
// reconcile:{attempt}:{n} with n one above the highest generation the attempt
// has, reconcileGaps after now. A reconcile of the attempt that is pending and
// not due yet is enough instead: it has not started, so it will resolve this
// job together with every other unknown job of the attempt.
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

// reconcilable reports whether a reconcile can resolve an ambiguous result of
// a job of this kind; any other kind, a reconcile included, is retried.
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

// generation is n in a reconcile job's key reconcile:{attempt}:{n}; ok is
// false for a key of another shape.
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

// escalate flags the enrollment behind a job whose retries are used up,
// unless it is paid or finished. A close or release_seats job that gives up
// flags its workshop's resolution instead, whatever its status.
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
	return r.flag(ctx, log.With("enrollment_id", a.EnrollmentID), a.EnrollmentID, unpaidActiveStatuses, "a "+job.Kind+" job gave up after its retries")
}

func (r *Runner) jobLog(job *store.Job) *slog.Logger {
	return r.log.With("job_id", job.ID, "kind", job.Kind, "key", job.IdempotencyKey, "attempt_id", deref(job.AttemptID))
}

// refusal ends a run whose action did not happen and would not happen by
// retrying it as is: PayPal refused it for good (pe), the worker refused it
// itself (issue, such as a passed capture deadline), or the enrollment was not
// in a state the action applies to. The handler has already recorded any
// outcome on the attempt and the enrollment. The job is failed with the
// reason and keeps its key: EnqueueOrRevive sends a skip again once the
// action applies, and only Revive sends again an action PayPal refused.
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

// newWorkerID names this process in the jobs it locks: host, pid and four
// random bytes, so two runners in one process are told apart.
func newWorkerID() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "worker"
	}
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s-%d-%s", host, os.Getpid(), hex.EncodeToString(b))
}
