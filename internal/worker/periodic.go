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
	// hourLayout is the hour a pending attempt's periodic reconcile key
	// names, in UTC.
	hourLayout = "2006-01-02T15"
	// succeededRetention is how long RunPeriodic keeps a succeeded seat
	// release or read-only reconcile (store.PurgeSucceededJobs).
	succeededRetention = 30 * 24 * time.Hour
)

// RunPeriodic reconciles, with a reconcile job due now, every attempt whose
// outcome PayPal may still change without telling us. The latest attempt of
// a CAPTURE_PENDING or REFUND_PENDING enrollment, and of a SEAT_RESERVED one
// whose authorization PayPal reports PENDING, gets
// reconcile:{attempt}:periodic:{hour}, so an authorization, capture or
// refund that stays pending for days is read back once an hour, the first
// time within two minutes of it turning pending. An attempt with a job
// unknown for more than five minutes gets reconcile:{attempt}:periodic:{minute},
// unless the attempt's last reconcile generation exists, which leaves that
// job to an operator. Hours and minutes are UTC, so another call within the
// same one enqueues nothing new. It deletes the succeeded seat releases and
// read-only reconciles older than 30 days (store.PurgeSucceededJobs), and
// then heals every workshop still funding or with money not settled
// (HealWorkshop), so a step a failed hook left undone does not wait for an
// unrelated event. Run calls it every two minutes; a failure on one attempt
// or workshop does not stop the others.
func (r *Runner) RunPeriodic(ctx context.Context) error {
	var errs []error
	now := r.clock.Now()
	enqueue := func(attemptID string, workshopID *string, bucket string) {
		key := store.JobKey(store.KindReconcile, attemptID+":periodic:"+bucket)
		if err := r.Enqueue(ctx, store.KindReconcile, key, workshopID, &attemptID, nil, now); err != nil {
			errs = append(errs, err)
		}
	}
	pending, err := r.st.LatestAttemptsInStatus(ctx, domain.EnrollmentCapturePending, domain.EnrollmentRefundPending)
	errs = append(errs, err)
	held, err := r.st.LatestAttemptsWithPendingAuthorization(ctx)
	errs = append(errs, err)
	for _, ref := range append(pending, held...) {
		enqueue(ref.AttemptID, nonEmpty(ref.WorkshopID), now.UTC().Format(hourLayout))
	}
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
			enqueue(*j.AttemptID, j.WorkshopID, now.UTC().Format(minuteLayout))
		}
	}

	purged, err := r.st.PurgeSucceededJobs(ctx, now.Add(-succeededRetention))
	errs = append(errs, err)
	if purged > 0 {
		r.log.Info("purged old succeeded jobs", "count", purged)
	}

	ids, err := r.st.WorkshopIDsFundingOrUnsettled(ctx)
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
