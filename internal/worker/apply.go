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

// PayPal statuses the worker reads or writes.
const (
	ppCreated           = "CREATED"
	ppCompleted         = "COMPLETED"
	ppPending           = "PENDING"
	ppDeclined          = "DECLINED"
	ppFailed            = "FAILED"
	ppDenied            = "DENIED"
	ppVoided            = "VOIDED"
	ppCaptured          = "CAPTURED"
	ppRefunded          = "REFUNDED"
	ppPartiallyRefunded = "PARTIALLY_REFUNDED"
	ppCancelled         = "CANCELLED"
	ppExpired           = "EXPIRED"
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

	// A status missing from a table leaves the enrollment as it is. A capture
	// REFUNDED moves nothing on its own: the refund's status decides.
	authorizationMoves = map[string]move{
		ppCreated: {statuses(domain.EnrollmentSeatReserved), domain.EnrollmentAuthorized},
		ppVoided:  {statuses(domain.EnrollmentAuthorized, domain.EnrollmentVoiding), domain.EnrollmentVoided},
		ppDenied:  {statuses(domain.EnrollmentSeatReserved, domain.EnrollmentAuthorized, domain.EnrollmentCapturing), domain.EnrollmentCaptureFailed},
	}
	captureMoves = map[string]move{
		ppCompleted: {statuses(domain.EnrollmentAuthorized, domain.EnrollmentCapturing, domain.EnrollmentCapturePending), domain.EnrollmentPaid},
		ppPending:   {statuses(domain.EnrollmentAuthorized, domain.EnrollmentCapturing), domain.EnrollmentCapturePending},
		ppDeclined:  {statuses(domain.EnrollmentCapturing, domain.EnrollmentCapturePending), domain.EnrollmentCaptureFailed},
		ppFailed:    {statuses(domain.EnrollmentCapturing, domain.EnrollmentCapturePending), domain.EnrollmentCaptureFailed},
		ppPartiallyRefunded: {statuses(domain.EnrollmentCapturing, domain.EnrollmentCapturePending, domain.EnrollmentPaid,
			domain.EnrollmentRefunding, domain.EnrollmentRefundPending), domain.EnrollmentNeedsAttention},
	}
	refundMoves = map[string]move{
		ppCompleted: {statuses(domain.EnrollmentPaid, domain.EnrollmentRefunding, domain.EnrollmentRefundPending), domain.EnrollmentRefunded},
		ppPending:   {statuses(domain.EnrollmentPaid, domain.EnrollmentRefunding), domain.EnrollmentRefundPending},
		ppFailed:    {statuses(domain.EnrollmentRefunding, domain.EnrollmentRefundPending), domain.EnrollmentRefundFailed},
		ppCancelled: {statuses(domain.EnrollmentRefunding, domain.EnrollmentRefundPending), domain.EnrollmentRefundFailed},
	}
)

func lookup(table map[string]move, status string) *move {
	if m, ok := table[status]; ok {
		return &m
	}
	return nil
}

// errIDConflict stops an update that would replace a PayPal id the attempt
// already holds with a different one.
var errIDConflict = errors.New("worker: PayPal reports a different resource than the attempt holds")

// ApplyOrder records what PayPal reports about the attempt's order, its id
// and status, and then applies every authorization, capture and refund the
// order lists, in that order. Like every Apply function it can be called any
// number of times with the same answer, fills an id only when the attempt has
// none or the same one, and never moves an enrollment back from a later or
// terminal status.
func (r *Runner) ApplyOrder(ctx context.Context, attemptID string, o paypal.Order) error {
	_, ok, err := r.record(ctx, attemptID, func(a *store.AttemptRow) error {
		if err := setID(&a.OrderID, o.ID); err != nil {
			return err
		}
		setStatus(&a.OrderStatus, o.Status)
		return nil
	}, nil)
	if err != nil || !ok || len(o.PurchaseUnits) == 0 {
		return err
	}
	payments := o.PurchaseUnits[0].Payments
	for _, au := range payments.Authorizations {
		if err := r.ApplyAuthorization(ctx, attemptID, au); err != nil {
			return err
		}
	}
	for _, c := range payments.Captures {
		if err := r.ApplyCapture(ctx, attemptID, c); err != nil {
			return err
		}
	}
	for _, rf := range payments.Refunds {
		if err := r.ApplyRefund(ctx, attemptID, rf); err != nil {
			return err
		}
	}
	return nil
}

// ApplyAuthorization records the authorization's id, status and expiry and
// moves the enrollment: CREATED → AUTHORIZED, VOIDED → VOIDED, DENIED →
// CAPTURE_FAILED.
func (r *Runner) ApplyAuthorization(ctx context.Context, attemptID string, au paypal.Authorization) error {
	_, _, err := r.record(ctx, attemptID, func(a *store.AttemptRow) error {
		if err := setID(&a.AuthorizationID, au.ID); err != nil {
			return err
		}
		setStatus(&a.AuthorizationStatus, au.Status)
		if !au.ExpirationTime.IsZero() {
			expires := au.ExpirationTime
			a.AuthExpiresAt = &expires
		}
		return nil
	}, lookup(authorizationMoves, au.Status))
	return err
}

// ApplyCapture records the capture's id, status and reason and moves the
// enrollment: COMPLETED → PAID, PENDING → CAPTURE_PENDING, DECLINED or FAILED
// → CAPTURE_FAILED, PARTIALLY_REFUNDED → NEEDS_ATTENTION.
func (r *Runner) ApplyCapture(ctx context.Context, attemptID string, c paypal.Capture) error {
	_, _, err := r.record(ctx, attemptID, func(a *store.AttemptRow) error {
		if err := setID(&a.CaptureID, c.ID); err != nil {
			return err
		}
		setStatus(&a.CaptureStatus, c.Status)
		a.CaptureReason = reason(c.StatusDetails)
		return nil
	}, lookup(captureMoves, c.Status))
	return err
}

// ApplyRefund records the refund's id, status and reason and moves the
// enrollment: COMPLETED → REFUNDED, PENDING → REFUND_PENDING, FAILED or
// CANCELLED → REFUND_FAILED.
func (r *Runner) ApplyRefund(ctx context.Context, attemptID string, rf paypal.Refund) error {
	_, _, err := r.record(ctx, attemptID, func(a *store.AttemptRow) error {
		if err := setID(&a.RefundID, rf.ID); err != nil {
			return err
		}
		setStatus(&a.RefundStatus, rf.Status)
		a.RefundReason = reason(rf.StatusDetails)
		return nil
	}, lookup(refundMoves, rf.Status))
	return err
}

// ApplyCaptureFailure records a capture PayPal refused for good: its issue and
// debug id, capture status FAILED and the enrollment CAPTURING →
// CAPTURE_FAILED. The authorization is still open, so it also enqueues
// void:{attempt} to release the buyer's hold.
func (r *Runner) ApplyCaptureFailure(ctx context.Context, attemptID string, e *paypal.Error) error {
	issue := e.Issue
	if issue == "" {
		issue = e.Name
	}
	return r.failCapture(ctx, attemptID, issue, e.DebugID)
}

func (r *Runner) failCapture(ctx context.Context, attemptID, issue, debugID string) error {
	saved, ok, err := r.record(ctx, attemptID, func(a *store.AttemptRow) error {
		failed := ppFailed
		a.CaptureStatus = &failed
		a.CaptureIssue = nonEmpty(issue)
		a.CaptureDebugID = nonEmpty(debugID)
		return nil
	}, &move{statuses(domain.EnrollmentCapturing), domain.EnrollmentCaptureFailed})
	if err != nil || !ok {
		return err
	}
	en, err := r.st.EnrollmentByID(ctx, saved.EnrollmentID)
	if err != nil {
		return err
	}
	if en.Status != domain.EnrollmentCaptureFailed {
		return nil
	}
	_, err = r.st.EnqueueJob(ctx, kindVoid, kindVoid+":"+attemptID, &en.WorkshopID, &attemptID, nil, r.clock.Now())
	return err
}

// record updates the attempt under its row lock with fn and then moves its
// enrollment as m says (nil: no move). When fn finds a conflicting PayPal id
// the attempt is left untouched, the enrollment is flagged NEEDS_ATTENTION
// and ok is false.
func (r *Runner) record(ctx context.Context, attemptID string, fn func(a *store.AttemptRow) error, m *move) (saved store.AttemptRow, ok bool, err error) {
	log := r.log.With("attempt_id", attemptID)
	saved, err = r.st.UpdateAttemptFunc(ctx, attemptID, fn)
	if errors.Is(err, errIDConflict) {
		a, lerr := r.st.AttemptByID(ctx, attemptID)
		if lerr != nil {
			return store.AttemptRow{}, false, lerr
		}
		if a == nil {
			return store.AttemptRow{}, false, fmt.Errorf("payment attempt %s: %w", attemptID, store.ErrNotFound)
		}
		log = log.With("enrollment_id", a.EnrollmentID)
		log.Error("PayPal reports a second resource for one payment attempt; flagging the enrollment")
		return *a, false, r.flag(ctx, log, a.EnrollmentID)
	}
	if err != nil {
		return store.AttemptRow{}, false, err
	}
	if m != nil {
		if _, err := r.transition(ctx, log.With("enrollment_id", saved.EnrollmentID), saved.EnrollmentID, *m); err != nil {
			return saved, true, err
		}
	}
	return saved, true, nil
}

// transition moves the enrollment as m says; log already names it. Not moving
// is not an error: the enrollment is already there, past it or finished.
func (r *Runner) transition(ctx context.Context, log *slog.Logger, enrollmentID string, m move) (bool, error) {
	moved, err := r.st.TransitionEnrollment(ctx, enrollmentID, m.from, m.to)
	if err != nil {
		return false, err
	}
	if moved {
		log.Info("enrollment moved", "to", m.to)
	} else {
		log.Debug("enrollment left as it is", "not_to", m.to)
	}
	return moved, nil
}

// flag marks an enrollment that is not finished yet NEEDS_ATTENTION.
func (r *Runner) flag(ctx context.Context, log *slog.Logger, enrollmentID string) error {
	_, err := r.transition(ctx, log, enrollmentID, move{activeStatuses, domain.EnrollmentNeedsAttention})
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
