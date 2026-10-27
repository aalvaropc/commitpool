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
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/aalvaropc/commitpool/internal/clock"
	"github.com/aalvaropc/commitpool/internal/config"
	"github.com/aalvaropc/commitpool/internal/paypal"
	"github.com/aalvaropc/commitpool/internal/store"
)

const (
	pollInterval    = 2 * time.Second
	requeueInterval = time.Minute
	staleAfter      = 5 * time.Minute
	reconcileDelay  = 30 * time.Second
	settleTimeout   = 10 * time.Second
)

// Job kinds this package handles.
const (
	kindCreateOrder = "create_order"
	kindAuthorize   = "authorize"
	kindCapture     = "capture"
	kindVoid        = "void"
	kindRefund      = "refund"
	kindReconcile   = "reconcile"
)

// Job statuses as the store writes them.
const (
	jobPending = "pending"
	jobUnknown = "unknown"
)

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
// unknown and schedules a reconcile; a final PayPal refusal the handler has
// already recorded (see definitive) completes it; anything else, a 429 or a
// store failure included, retries it later under the same key. httpStatus is
// PayPal's HTTP status when the handler knows it.
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

// New returns a runner with the payment handlers registered. A nil Log uses
// slog.Default and a nil Clock the real clock.
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
		kindCreateOrder: r.createOrder,
		kindAuthorize:   r.authorize,
		kindCapture:     r.capture,
		kindVoid:        r.void,
		kindRefund:      r.refund,
		kindReconcile:   r.reconcile,
	}
	return r
}

// Register adds or replaces the handler for a job kind. Call it before Run.
func (r *Runner) Register(kind string, h Handler) {
	r.handlers[kind] = h
}

// Run claims and runs due jobs until ctx is done, then returns ctx.Err(). When
// nothing is due it waits two seconds before looking again, and once a minute
// it takes back jobs claimed more than five minutes ago and never finished.
func (r *Runner) Run(ctx context.Context) error {
	var lastRequeue time.Time
	for {
		if lastRequeue.IsZero() || time.Since(lastRequeue) >= requeueInterval {
			r.requeueStale(ctx)
			lastRequeue = time.Now()
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
func (r *Runner) RunOnce(ctx context.Context) (bool, error) {
	job, err := r.st.ClaimJob(ctx, r.workerID, r.clock.Now())
	if err != nil || job == nil {
		return false, err
	}
	return true, r.run(ctx, job)
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
	status, err := h(ctx, job)
	return r.settle(ctx, job, status, err)
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

	var def *definitive
	switch {
	case err == nil:
		log.Info("job succeeded")
		return r.tolerate(log, r.st.CompleteJob(ctx, job.ID, status))
	case errors.As(err, &def):
		log.Warn("job finished with a final refusal from PayPal", "error", err)
		return r.tolerate(log, r.st.CompleteJob(ctx, job.ID, status))
	case paypal.IsAmbiguous(err) && job.AttemptID != nil:
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
		log.Error("job failed too many times and needs attention", "error", err)
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

// scheduleReconcile enqueues reconcile:{attempt}:{n} thirty seconds from now,
// n being the job's attempt count. When a reconcile that already ran holds
// that key the next free n is used; a reconcile of the attempt that is still
// pending is enough, since it resolves every unknown job of the attempt.
func (r *Runner) scheduleReconcile(ctx context.Context, job *store.Job) error {
	runAt := r.clock.Now().Add(reconcileDelay)
	for n := job.Attempts; ; n++ {
		key := kindReconcile + ":" + *job.AttemptID + ":" + strconv.Itoa(n)
		created, err := r.st.EnqueueJob(ctx, kindReconcile, key, job.WorkshopID, job.AttemptID, nil, runAt)
		if err != nil {
			return err
		}
		if created {
			return nil
		}
		existing, err := r.st.JobByKey(ctx, key)
		if err != nil {
			return err
		}
		if existing != nil && existing.Status == jobPending {
			return nil
		}
	}
}

// escalate flags the enrollment behind a job whose retries are used up.
func (r *Runner) escalate(ctx context.Context, log *slog.Logger, job *store.Job) error {
	if job.AttemptID == nil {
		return nil
	}
	a, err := r.st.AttemptByID(ctx, *job.AttemptID)
	if err != nil || a == nil {
		return err
	}
	return r.flag(ctx, log.With("enrollment_id", a.EnrollmentID), a.EnrollmentID)
}

func (r *Runner) jobLog(job *store.Job) *slog.Logger {
	return r.log.With("job_id", job.ID, "kind", job.Kind, "key", job.IdempotencyKey, "attempt_id", deref(job.AttemptID))
}

// definitive wraps a final PayPal refusal whose outcome the handler has
// already recorded on the attempt and the enrollment, so the job is finished
// rather than failed.
type definitive struct{ err *paypal.Error }

func (d *definitive) Error() string { return d.err.Error() }
func (d *definitive) Unwrap() error { return d.err }

func refused(pe *paypal.Error) (*int, error) {
	return statusOf(pe), &definitive{pe}
}

// definitiveError returns PayPal's error when it is a final answer about the
// resource: any 4xx except 401 and 403 (credentials, not the payment), 409
// (ambiguous) and 429 (rejected before processing).
func definitiveError(err error) *paypal.Error {
	var pe *paypal.Error
	if !errors.As(err, &pe) || pe.HTTPStatus < 400 || pe.HTTPStatus >= 500 {
		return nil
	}
	switch pe.HTTPStatus {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusConflict, http.StatusTooManyRequests:
		return nil
	}
	return pe
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
