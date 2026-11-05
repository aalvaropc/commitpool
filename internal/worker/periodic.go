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
	// closeOverdueAfter is how long past its proposal's close a funding
	// workshop may wait before RunPeriodic runs its close again.
	closeOverdueAfter = 5 * time.Minute
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
			enqueue(*j.AttemptID, j.WorkshopID, store.Minute(now))
		}
	}

	errs = append(errs, r.reviveOverdueCloses(ctx, now))

	events, err := r.st.PurgeProviderEvents(ctx, succeededRetention)
	errs = append(errs, err)
	if events > 0 {
		r.log.Info("purged old webhook deliveries", "count", events)
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

// reviveOverdueCloses enqueues close:{proposal} due now for every workshop
// still FUNDING more than closeOverdueAfter past its close, or revives the
// job when it failed or was parked: a close only reads and decides, so it is
// safe to run again, and nothing else would ever close that workshop.
func (r *Runner) reviveOverdueCloses(ctx context.Context, now time.Time) error {
	overdue, err := r.st.OverdueCloses(ctx, now.Add(-closeOverdueAfter))
	if err != nil {
		return err
	}
	var errs []error
	for _, o := range overdue {
		key := store.JobKey(store.KindClose, o.ProposalID)
		created, err := r.st.EnqueueJob(ctx, store.KindClose, key, &o.WorkshopID, nil, nil, now)
		if err == nil && !created {
			created, err = r.st.ReviveJob(ctx, key, now, true)
		}
		if created {
			r.log.Warn("workshop still funding after its close; running its close again", "workshop_id", o.WorkshopID, "key", key)
		}
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// leftToOperator reports whether the attempt's last reconcile generation,
// reconcile:{attempt}:{maxReconcileGenerations}, has run: it sends nothing
// again and leaves the attempt's unknown jobs to an operator. While it waits
// its six hours, the periodic reconciles keep reading PayPal and sending the
// action again under its own key, so a PayPal that recovers in the meantime
// still gets the money back without an operator.
func (r *Runner) leftToOperator(ctx context.Context, attemptID string) (bool, error) {
	j, err := r.st.JobByKey(ctx, reconcilePrefix(attemptID)+strconv.Itoa(maxReconcileGenerations))
	return j != nil && j.Status == store.JobSucceeded, err
}
