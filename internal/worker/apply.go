package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/paypal"
	"github.com/aalvaropc/commitpool/internal/store"
)

// move is the enrollment transition a PayPal status implies: to `to`, but only
// from one of `from`, so an enrollment that is already there or past it, or
// finished, is never moved back.
type move struct {
	from []domain.EnrollmentStatus
	to   domain.EnrollmentStatus
}

func statuses(s ...domain.EnrollmentStatus) []domain.EnrollmentStatus { return s }

var (
	// activeStatuses are the enrollment statuses that are not terminal.
	activeStatuses = statuses(domain.EnrollmentSeatReserved, domain.EnrollmentAuthorized, domain.EnrollmentCapturing,
		domain.EnrollmentCapturePending, domain.EnrollmentPaid, domain.EnrollmentVoiding, domain.EnrollmentRefunding,
		domain.EnrollmentRefundPending)

	// unpaidActiveStatuses are the active statuses a stuck job may flag: a
	// completed payment is never taken out of PAID because a job gave up.
	unpaidActiveStatuses = statuses(domain.EnrollmentSeatReserved, domain.EnrollmentAuthorized, domain.EnrollmentCapturing,
		domain.EnrollmentCapturePending, domain.EnrollmentVoiding, domain.EnrollmentRefunding, domain.EnrollmentRefundPending)

	// A status missing from a table leaves the enrollment as it is. A capture
	// REFUNDED moves nothing on its own: the refund's status decides.
	authorizationMoves = map[string]move{
		paypal.StatusCreated: {statuses(domain.EnrollmentSeatReserved), domain.EnrollmentAuthorized},
		paypal.StatusVoided:  {statuses(domain.EnrollmentAuthorized, domain.EnrollmentVoiding), domain.EnrollmentVoided},
		paypal.StatusDenied:  {statuses(domain.EnrollmentSeatReserved, domain.EnrollmentAuthorized, domain.EnrollmentCapturing), domain.EnrollmentCaptureFailed},
	}
	captureMoves = map[string]move{
		paypal.StatusCompleted: {statuses(domain.EnrollmentAuthorized, domain.EnrollmentCapturing, domain.EnrollmentCapturePending), domain.EnrollmentPaid},
		paypal.StatusPending:   {statuses(domain.EnrollmentAuthorized, domain.EnrollmentCapturing), domain.EnrollmentCapturePending},
		paypal.StatusDeclined:  {statuses(domain.EnrollmentCapturing, domain.EnrollmentCapturePending), domain.EnrollmentCaptureFailed},
		paypal.StatusFailed:    {statuses(domain.EnrollmentCapturing, domain.EnrollmentCapturePending), domain.EnrollmentCaptureFailed},
		paypal.StatusPartiallyRefunded: {statuses(domain.EnrollmentCapturing, domain.EnrollmentCapturePending, domain.EnrollmentPaid,
			domain.EnrollmentRefunding, domain.EnrollmentRefundPending), domain.EnrollmentNeedsAttention},
	}
	refundMoves = map[string]move{
		paypal.StatusCompleted: {statuses(domain.EnrollmentPaid, domain.EnrollmentRefunding, domain.EnrollmentRefundPending), domain.EnrollmentRefunded},
		paypal.StatusPending:   {statuses(domain.EnrollmentPaid, domain.EnrollmentRefunding), domain.EnrollmentRefundPending},
		paypal.StatusFailed:    {statuses(domain.EnrollmentRefunding, domain.EnrollmentRefundPending), domain.EnrollmentRefundFailed},
		paypal.StatusCancelled: {statuses(domain.EnrollmentRefunding, domain.EnrollmentRefundPending), domain.EnrollmentRefundFailed},
	}
)

func lookup(table map[string]move, status string) *move {
	if m, ok := table[status]; ok {
		return &m
	}
	return nil
}

var (
	// errIDConflict stops an update that would replace a PayPal id the
	// attempt already holds with a different one.
	errIDConflict = errors.New("worker: PayPal reports a different resource than the attempt holds")

	// errCaptureRecorded stops a capture failure from being recorded on an
	// attempt that already holds a capture.
	errCaptureRecorded = errors.New("worker: the attempt already holds a capture; no capture failure recorded")
)

// ApplyOrder records what PayPal reports about the attempt's order, its id
// and status, and then applies every authorization, capture and refund the
// order lists, in that order. Like every Apply function it can be called any
// number of times with the same answer, fills an id only when the attempt has
// none or the same one, never moves an enrollment back from a later or
// terminal status, and moves an enrollment only for its latest attempt: an
// enrollment follows the checkout it opened last.
func (r *Runner) ApplyOrder(ctx context.Context, attemptID string, o paypal.Order) error {
	_, err := r.applyOrder(ctx, attemptID, o)
	return err
}

// applyOrder is ApplyOrder reporting whether the order was recorded; it is
// false when PayPal named a different order than the attempt holds.
func (r *Runner) applyOrder(ctx context.Context, attemptID string, o paypal.Order) (bool, error) {
	res, err := r.record(ctx, attemptID, func(a *store.AttemptRow) error {
		if err := setID(&a.OrderID, o.ID); err != nil {
			return err
		}
		setStatus(&a.OrderStatus, o.Status)
		return nil
	}, nil, false)
	if err != nil || !res.ok || len(o.PurchaseUnits) == 0 {
		return res.ok, err
	}
	payments := o.PurchaseUnits[0].Payments
	for _, au := range payments.Authorizations {
		if err := r.ApplyAuthorization(ctx, attemptID, au); err != nil {
			return true, err
		}
	}
	for _, c := range payments.Captures {
		if err := r.ApplyCapture(ctx, attemptID, c); err != nil {
			return true, err
		}
	}
	for _, rf := range payments.Refunds {
		if err := r.ApplyRefund(ctx, attemptID, rf); err != nil {
			return true, err
		}
	}
	return true, nil
}

// ApplyAuthorization records the authorization's id, status and expiry and
// moves the enrollment: CREATED → AUTHORIZED, VOIDED → VOIDED, DENIED →
// CAPTURE_FAILED. A CREATED authorization the enrollment cannot use, because
// the attempt is not its latest or its seat was released, is a hold nobody
// will capture, so it enqueues the attempt's void.
func (r *Runner) ApplyAuthorization(ctx context.Context, attemptID string, au paypal.Authorization) error {
	res, err := r.record(ctx, attemptID, func(a *store.AttemptRow) error {
		if err := setID(&a.AuthorizationID, au.ID); err != nil {
			return err
		}
		setStatus(&a.AuthorizationStatus, au.Status)
		if !au.ExpirationTime.IsZero() {
			expires := au.ExpirationTime
			a.AuthExpiresAt = &expires
		}
		return nil
	}, lookup(authorizationMoves, au.Status), false)
	if err != nil || !res.ok || au.Status != paypal.StatusCreated || res.moved {
		return err
	}
	unused := !res.driving
	if !unused {
		en, err := r.st.EnrollmentByID(ctx, res.attempt.EnrollmentID)
		if err != nil {
			return err
		}
		unused = en.Status == domain.EnrollmentReleased
	}
	if !unused {
		return nil
	}
	r.log.Warn("authorization the enrollment cannot use; voiding it",
		"attempt_id", attemptID, "enrollment_id", res.attempt.EnrollmentID, "latest_attempt", res.driving)
	return r.enqueueVoid(ctx, res.attempt)
}

// ApplyCapture records the capture's id, status and reason and moves the
// enrollment: COMPLETED → PAID, PENDING → CAPTURE_PENDING, DECLINED or FAILED
// → CAPTURE_FAILED, PARTIALLY_REFUNDED → NEEDS_ATTENTION. A declined or
// failed capture leaves the authorization open, so it also enqueues the
// attempt's void, even when what the move set off failed.
func (r *Runner) ApplyCapture(ctx context.Context, attemptID string, c paypal.Capture) error {
	res, err := r.record(ctx, attemptID, func(a *store.AttemptRow) error {
		if err := setID(&a.CaptureID, c.ID); err != nil {
			return err
		}
		setStatus(&a.CaptureStatus, c.Status)
		a.CaptureReason = reason(c.StatusDetails)
		return nil
	}, lookup(captureMoves, c.Status), c.Status == paypal.StatusCompleted || c.Status == paypal.StatusPending)
	if !res.ok {
		return err
	}
	if c.Status == paypal.StatusDeclined || c.Status == paypal.StatusFailed {
		return errors.Join(err, r.enqueueVoid(ctx, res.attempt))
	}
	return err
}

// ApplyRefund records the refund's id, status and reason and moves the
// enrollment: COMPLETED → REFUNDED, PENDING → REFUND_PENDING, FAILED or
// CANCELLED → REFUND_FAILED.
func (r *Runner) ApplyRefund(ctx context.Context, attemptID string, rf paypal.Refund) error {
	_, err := r.record(ctx, attemptID, func(a *store.AttemptRow) error {
		if err := setID(&a.RefundID, rf.ID); err != nil {
			return err
		}
		setStatus(&a.RefundStatus, rf.Status)
		a.RefundReason = reason(rf.StatusDetails)
		return nil
	}, lookup(refundMoves, rf.Status), true)
	return err
}

// ApplyCaptureFailure records a capture PayPal refused for good: its issue and
// debug id, capture status FAILED and the enrollment CAPTURING →
// CAPTURE_FAILED. The authorization is still open, so it also enqueues (or
// revives) void:{attempt} to release the buyer's hold, even when what the
// move set off failed. An attempt that already holds a capture is left alone
// and errCaptureRecorded is returned.
func (r *Runner) ApplyCaptureFailure(ctx context.Context, attemptID string, e *paypal.Error) error {
	issue := e.Issue
	if issue == "" {
		issue = e.Name
	}
	return r.failCapture(ctx, attemptID, issue, e.DebugID)
}

func (r *Runner) failCapture(ctx context.Context, attemptID, issue, debugID string) error {
	res, err := r.record(ctx, attemptID, func(a *store.AttemptRow) error {
		if a.CaptureID != nil {
			return errCaptureRecorded
		}
		failed := paypal.StatusFailed
		a.CaptureStatus = &failed
		a.CaptureIssue = nonEmpty(issue)
		a.CaptureDebugID = nonEmpty(debugID)
		return nil
	}, &move{statuses(domain.EnrollmentCapturing), domain.EnrollmentCaptureFailed}, false)
	if !res.ok {
		return err
	}
	return errors.Join(err, r.enqueueVoid(ctx, res.attempt))
}

// enqueueVoid enqueues, or revives, void:{attempt} due now.
func (r *Runner) enqueueVoid(ctx context.Context, a store.AttemptRow) error {
	en, err := r.st.EnrollmentByID(ctx, a.EnrollmentID)
	if err != nil {
		return err
	}
	_, err = r.EnqueueOrRevive(ctx, store.KindVoid, store.JobKey(store.KindVoid, a.ID), &en.WorkshopID, &a.ID, nil, r.clock.Now())
	return err
}

// recorded is what record did.
type recorded struct {
	attempt store.AttemptRow
	ok      bool // false: a conflicting PayPal id; nothing was recorded and the enrollment was flagged
	driving bool // the attempt is its enrollment's latest, the one the enrollment follows
	moved   bool
}

// record updates the attempt under its row lock with fn and then, when the
// attempt is its enrollment's latest, moves the enrollment as m says (nil: no
// move). money marks money that moved (a completed or pending capture, any
// refund): recorded on an older attempt it should not exist, since a new
// attempt opens only while SEAT_RESERVED, so the enrollment is flagged
// NEEDS_ATTENTION instead (unless it is paid or finished). When fn finds a
// conflicting PayPal id the attempt is left untouched and the enrollment is
// flagged too.
func (r *Runner) record(ctx context.Context, attemptID string, fn func(a *store.AttemptRow) error, m *move, money bool) (recorded, error) {
	log := r.log.With("attempt_id", attemptID)
	saved, err := r.st.UpdateAttemptFunc(ctx, attemptID, fn)
	if errors.Is(err, errIDConflict) {
		a, lerr := r.st.AttemptByID(ctx, attemptID)
		if lerr != nil {
			return recorded{}, lerr
		}
		if a == nil {
			return recorded{}, fmt.Errorf("payment attempt %s: %w", attemptID, store.ErrNotFound)
		}
		log = log.With("enrollment_id", a.EnrollmentID)
		log.Error("PayPal reports a second resource for one payment attempt; flagging the enrollment")
		return recorded{attempt: *a}, r.flag(ctx, log, a.EnrollmentID, activeStatuses)
	}
	if err != nil {
		return recorded{}, err
	}
	res := recorded{attempt: saved, ok: true}
	if m == nil && !money {
		return res, nil
	}
	log = log.With("enrollment_id", saved.EnrollmentID)
	if res.driving, err = r.driving(ctx, saved); err != nil {
		return res, err
	}
	if !res.driving && money {
		log.Error("PayPal moved money on an older attempt of the enrollment; flagging it")
		return res, r.flag(ctx, log, saved.EnrollmentID, unpaidActiveStatuses)
	}
	if !res.driving {
		log.Debug("enrollment left as it is: it follows its latest attempt, not this one")
		return res, nil
	}
	if m == nil {
		return res, nil
	}
	res.moved, err = r.transition(ctx, log, saved.EnrollmentID, *m)
	return res, err
}

// driving reports whether the attempt is its enrollment's latest attempt.
func (r *Runner) driving(ctx context.Context, a store.AttemptRow) (bool, error) {
	latest, err := r.st.LatestAttempt(ctx, a.EnrollmentID)
	if err != nil {
		return false, err
	}
	return latest != nil && latest.ID == a.ID, nil
}

// transition moves the enrollment as m says; log already names it. Not moving
// is not an error: the enrollment is already there, past it or finished. A
// move then runs what it sets off (afterMove), so it must never be called
// inside an UpdateAttemptFunc fn. Every move the worker makes on one
// enrollment goes through here; only seat releases, done by the store for a
// whole workshop, do not, and their callers settle the workshop themselves.
func (r *Runner) transition(ctx context.Context, log *slog.Logger, enrollmentID string, m move) (bool, error) {
	moved, err := r.st.TransitionEnrollment(ctx, enrollmentID, m.from, m.to)
	if err != nil {
		return false, err
	}
	if !moved {
		log.Debug("enrollment left as it is", "not_to", m.to)
		return false, nil
	}
	log.Info("enrollment moved", "to", m.to)
	return true, r.afterMove(ctx, enrollmentID, m.to)
}

// flag marks the enrollment NEEDS_ATTENTION when it is in one of from.
func (r *Runner) flag(ctx context.Context, log *slog.Logger, enrollmentID string, from []domain.EnrollmentStatus) error {
	_, err := r.transition(ctx, log, enrollmentID, move{from, domain.EnrollmentNeedsAttention})
	return err
}

// setID fills an id column. An empty id (PayPal did not say which resource)
// and the id already stored are accepted; a different id is a conflict.
func setID(dst **string, id string) error {
	switch {
	case id == "":
	case *dst == nil:
		*dst = &id
	case **dst != id:
		return errIDConflict
	}
	return nil
}

func setStatus(dst **string, status string) {
	if status != "" {
		*dst = &status
	}
}

func reason(d *paypal.StatusDetails) *string {
	if d == nil {
		return nil
	}
	return nonEmpty(d.Reason)
}

func is(s *string, want string) bool {
	return s != nil && *s == want
}
