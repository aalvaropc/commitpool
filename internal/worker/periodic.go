package worker

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/store"
)

const (
	// periodicInterval is how often Run calls RunPeriodic.
	periodicInterval = 2 * time.Minute
	// unknownAfter is how long a job may stay unknown before RunPeriodic
	// reconciles its attempt, on top of the reconciles the job scheduled.
	unknownAfter = 5 * time.Minute
)

// RunPeriodic enqueues reconcile:{attempt}:periodic:{minute}, due now, for
// every attempt whose outcome PayPal may still change without telling us: the
// latest attempt of a CAPTURE_PENDING or REFUND_PENDING enrollment, and an
// attempt with a job unknown for more than five minutes, unless the attempt's
// last reconcile generation exists, which leaves that job to an operator.
// minute is now's minute in UTC, so another call within the same minute
// enqueues nothing new. It then heals every FUNDING workshop and every
// workshop whose money is still resolving (HealWorkshop), so a step a failed
// hook left undone does not wait for an unrelated event. Run calls it every
// two minutes; a failure on one attempt or workshop does not stop the others.
func (r *Runner) RunPeriodic(ctx context.Context) error {
	var errs []error
	refs, err := r.st.LatestAttemptsInStatus(ctx, domain.EnrollmentCapturePending, domain.EnrollmentRefundPending)
	errs = append(errs, err)
	unknown, err := r.st.UnknownJobs(ctx, unknownAfter)
	errs = append(errs, err)
	for _, j := range unknown {
		if j.AttemptID == nil {
			continue
		}
		left, err := r.leftToOperator(ctx, *j.AttemptID)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !left {
			refs = append(refs, store.AttemptRef{AttemptID: *j.AttemptID, WorkshopID: deref(j.WorkshopID)})
		}
	}
	now := r.clock.Now()
	suffix := ":periodic:" + now.UTC().Format(minuteLayout)
	for _, ref := range refs {
		key := store.JobKey(store.KindReconcile, ref.AttemptID+suffix)
		if err := r.Enqueue(ctx, store.KindReconcile, key, nonEmpty(ref.WorkshopID), &ref.AttemptID, nil, now); err != nil {
			errs = append(errs, err)
		}
	}

	ids, err := r.st.WorkshopIDsFundingOrResolving(ctx)
	errs = append(errs, err)
	for _, id := range ids {
		if err := r.HealWorkshop(ctx, id); err != nil {
			errs = append(errs, fmt.Errorf("heal workshop %s: %w", id, err))
		}
	}
	return errors.Join(errs...)
}

// leftToOperator reports whether the attempt's last reconcile generation,
// reconcile:{attempt}:{maxReconcileGenerations}, exists: once it has run it
// sends nothing again and leaves the attempt's unknown jobs to an operator,
// and until then it is the reconcile already scheduled.
func (r *Runner) leftToOperator(ctx context.Context, attemptID string) (bool, error) {
	j, err := r.st.JobByKey(ctx, reconcilePrefix(attemptID)+strconv.Itoa(maxReconcileGenerations))
	return j != nil, err
}
