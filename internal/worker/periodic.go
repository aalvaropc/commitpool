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
	periodicInterval   = 2 * time.Minute
	unknownAfter       = 5 * time.Minute
	hourLayout         = "2006-01-02T15"
	succeededRetention = 30 * 24 * time.Hour
	closeOverdueAfter  = 5 * time.Minute
)

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

func (r *Runner) leftToOperator(ctx context.Context, attemptID string) (bool, error) {
	j, err := r.st.JobByKey(ctx, reconcilePrefix(attemptID)+strconv.Itoa(maxReconcileGenerations))
	return j != nil && j.Status == store.JobSucceeded, err
}
