package worker

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/store"
)

// RetryRefund sends a refund PayPal failed again, under a new key
// (store.RetryRefund), and reopens the workshop's settlement when nothing
// else needs an operator. It returns the refund job's key, for a caller that
// runs it inline.
func (r *Runner) RetryRefund(ctx context.Context, enrollmentID string) (string, error) {
	key, err := r.st.RetryRefund(ctx, enrollmentID, r.clock.Now())
	if err != nil {
		return "", err
	}
	en, err := r.st.EnrollmentByID(ctx, enrollmentID)
	if err != nil {
		return key, err
	}
	r.log.Warn("operator: retrying a failed refund", "workshop_id", en.WorkshopID, "enrollment_id", en.ID, "key", key)
	if err := r.ClearWorkshopFlag(ctx, en.WorkshopID); err != nil && !errors.Is(err, domain.ErrStillUnresolved) {
		return key, err
	}
	return key, nil
}

// OperatorReconcile enqueues a read of the enrollment's latest attempt back
// from PayPal, due now, under reconcile:{attempt}:operator:{minute}: it only
// reads and applies what PayPal shows, so it is safe at any time. It returns
// the key.
func (r *Runner) OperatorReconcile(ctx context.Context, enrollmentID string) (string, error) {
	en, err := r.st.EnrollmentByID(ctx, enrollmentID)
	if err != nil {
		return "", err
	}
	a, err := r.st.LatestAttempt(ctx, enrollmentID)
	if err != nil {
		return "", err
	}
	if a == nil {
		return "", fmt.Errorf("enrollment %s has no payment attempt: %w", enrollmentID, store.ErrNotFound)
	}
	now := r.clock.Now()
	key := reconcilePrefix(a.ID) + "operator:" + store.Minute(now)
	if err := r.Enqueue(ctx, store.KindReconcile, key, &en.WorkshopID, &a.ID, nil, now); err != nil {
		return "", err
	}
	return key, nil
}

// ClearEnrollmentFlag removes an enrollment's operator flag once nothing of
// its latest attempt is still unknown, running or parked
// (domain.ErrStillUnresolved otherwise), and settles its workshop again.
func (r *Runner) ClearEnrollmentFlag(ctx context.Context, enrollmentID string) error {
	en, err := r.st.EnrollmentByID(ctx, enrollmentID)
	if err != nil {
		return err
	}
	a, err := r.st.LatestAttempt(ctx, enrollmentID)
	if err != nil {
		return err
	}
	if a != nil {
		jobs, err := r.st.JobsByAttempt(ctx, a.ID)
		if err != nil {
			return err
		}
		if slices.ContainsFunc(jobs, func(j store.Job) bool {
			return j.Status == store.JobUnknown || j.Status == store.JobRunning || j.Status == store.JobNeedsAttention
		}) {
			return domain.ErrStillUnresolved
		}
	}
	if _, err := r.st.ClearEnrollmentFlag(ctx, enrollmentID); err != nil {
		return err
	}
	r.log.Warn("operator: enrollment flag cleared", "workshop_id", en.WorkshopID, "enrollment_id", en.ID, "status", en.Status)
	return r.SettleIfResolved(ctx, en.WorkshopID)
}

// ClearWorkshopFlag takes a workshop out of NEEDS_ATTENTION once no
// enrollment needs an operator (domain.ErrStillUnresolved otherwise): a
// cancelled workshop goes back to RESOLVING and is settled again, any other
// to OPEN. A workshop not flagged is left alone.
func (r *Runner) ClearWorkshopFlag(ctx context.Context, workshopID string) error {
	for range casRetries {
		w, err := r.st.WorkshopByID(ctx, workshopID)
		if err != nil || w.Resolution != domain.ResolutionNeedsAttention {
			return err
		}
		v, err := r.workshopView(ctx, workshopID)
		if err != nil {
			return err
		}
		if slices.ContainsFunc(v.es, domain.Enrollment.NeedsOperator) {
			return domain.ErrStillUnresolved
		}
		to := domain.ResolutionOpen
		if v.w.Status == domain.WorkshopCancelled {
			to = domain.ResolutionResolving
		}
		ok, err := r.st.TransitionWorkshop(ctx, workshopID, []domain.WorkshopStatus{v.w.Status},
			[]domain.Resolution{domain.ResolutionNeedsAttention}, v.w.Status, to)
		if err != nil {
			return err
		}
		if ok {
			r.log.Warn("operator: workshop flag cleared", "workshop_id", workshopID, "status", v.w.Status, "resolution", to)
			return r.SettleIfResolved(ctx, workshopID)
		}
	}
	return errWorkshopMoving
}
