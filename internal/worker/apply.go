package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/paypal"
	"github.com/aalvaropc/commitpool/internal/store"
)

type move struct {
	from []domain.EnrollmentStatus
	to   domain.EnrollmentStatus
}

func statuses(s ...domain.EnrollmentStatus) []domain.EnrollmentStatus { return s }

var (
	activeStatuses = statuses(domain.EnrollmentSeatReserved, domain.EnrollmentAuthorized, domain.EnrollmentCapturing,
		domain.EnrollmentCapturePending, domain.EnrollmentPaid, domain.EnrollmentVoiding, domain.EnrollmentRefunding,
		domain.EnrollmentRefundPending)

	unpaidActiveStatuses = statuses(domain.EnrollmentSeatReserved, domain.EnrollmentAuthorized, domain.EnrollmentCapturing,
		domain.EnrollmentCapturePending, domain.EnrollmentVoiding, domain.EnrollmentRefunding, domain.EnrollmentRefundPending)

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
	errIDConflict = errors.New("worker: PayPal reports a different resource than the attempt holds")

	errCaptureRecorded = errors.New("worker: the attempt already holds a capture; no capture failure recorded")
)

func (r *Runner) ApplyOrder(ctx context.Context, attemptID string, o paypal.Order) error {
	_, err := r.applyOrder(ctx, attemptID, o)
	return err
}

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

func (r *Runner) ApplyRefund(ctx context.Context, attemptID string, rf paypal.Refund) error {
	_, err := r.record(ctx, attemptID, func(a *store.AttemptRow) error {
		if slices.Contains(a.FailedRefundIDs, rf.ID) {
			return errSetAside
		}
		if err := setID(&a.RefundID, rf.ID); err != nil {
			return err
		}
		setStatus(&a.RefundStatus, rf.Status)
		a.RefundReason = reason(rf.StatusDetails)
		return nil
	}, lookup(refundMoves, rf.Status), true)
	if errors.Is(err, errSetAside) {
		r.log.Info("ignoring a refund an operator retry set aside", "attempt_id", attemptID, "refund_id", rf.ID, "refund_status", rf.Status)
		return nil
	}
	return err
}

var errSetAside = errors.New("refund set aside by an operator retry")

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

func (r *Runner) enqueueVoid(ctx context.Context, a store.AttemptRow) error {
	en, err := r.st.EnrollmentByID(ctx, a.EnrollmentID)
	if err != nil {
		return err
	}
	_, err = r.EnqueueOrRevive(ctx, store.KindVoid, store.JobKey(store.KindVoid, a.ID), &en.WorkshopID, &a.ID, nil, r.clock.Now())
	return err
}

type recorded struct {
	attempt store.AttemptRow
	ok      bool
	driving bool
	moved   bool
}

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
		return recorded{attempt: *a}, r.flag(ctx, log, a.EnrollmentID, activeStatuses, "PayPal reports a second resource for one payment attempt")
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
		return res, r.flag(ctx, log, saved.EnrollmentID, unpaidActiveStatuses, "PayPal moved money on an older attempt")
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

func (r *Runner) driving(ctx context.Context, a store.AttemptRow) (bool, error) {
	latest, err := r.st.LatestAttempt(ctx, a.EnrollmentID)
	if err != nil {
		return false, err
	}
	return latest != nil && latest.ID == a.ID, nil
}

func (r *Runner) transition(ctx context.Context, log *slog.Logger, enrollmentID string, m move) (bool, error) {
	if m.to == domain.EnrollmentNeedsAttention {
		return false, r.flag(ctx, log, enrollmentID, m.from, "PayPal refused or reported something only an operator can finish")
	}
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

func (r *Runner) flag(ctx context.Context, log *slog.Logger, enrollmentID string, from []domain.EnrollmentStatus, reason string) error {
	en, err := r.st.EnrollmentByID(ctx, enrollmentID)
	if err != nil {
		return err
	}
	if !slices.Contains(from, en.Status) {
		log.Debug("enrollment not flagged: it is paid or finished", "status", en.Status)
		return nil
	}
	flagged, err := r.st.FlagEnrollment(ctx, enrollmentID, reason)
	if err != nil || !flagged {
		return err
	}
	log.Error("enrollment flagged for an operator", "status", en.Status, "reason", reason)
	return errors.Join(r.flagIfConfirmed(ctx, en.WorkshopID), r.SettleIfResolved(ctx, en.WorkshopID))
}

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
