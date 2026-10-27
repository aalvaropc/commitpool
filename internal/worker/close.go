package worker

import (
	"context"
	"errors"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/store"
)

// minuteLayout is the minute a release_seats key names, in UTC.
const minuteLayout = "2006-01-02T15:04"

// closePayments runs close:{proposal_id} when the payment window closes and
// applies domain.CloseDecision: a FUNDING workshop below its minimum is
// cancelled and starts resolving, a CONFIRMED one stays confirmed. It
// enqueues void:{attempt} for each authorization to release,
// refund:{attempt} for each completed payment to give back and
// reconcile:{attempt}:close for each capture still in flight, each for the
// enrollment's latest attempt, and only then records the workshop's new
// status, so a run cut short is redone in full. A workshop that has already
// been closed has nothing left to close, so running the job again enqueues
// nothing new.
func (r *Runner) closePayments(ctx context.Context, job *store.Job) (*int, error) {
	if job.WorkshopID == nil {
		return nil, errors.New("close job has no workshop")
	}
	workshopID := *job.WorkshopID
	w, p, es, rows, err := r.workshopView(ctx, workshopID)
	if err != nil {
		return nil, err
	}
	c := domain.CloseDecision(w, p, es, r.clock.Now())
	byID := make(map[string]store.EnrollmentRow, len(rows))
	for _, en := range rows {
		byID[en.ID] = en
	}
	steps := []struct {
		kind, suffix string
		ids          []string
	}{
		{kindVoid, "", c.Voids},
		{kindRefund, "", c.Refunds},
		{kindReconcile, ":close", c.Reconciles},
	}
	for _, step := range steps {
		for _, id := range step.ids {
			if _, err := r.enqueueLatest(ctx, step.kind, byID[id], step.suffix); err != nil {
				return nil, err
			}
		}
	}
	if c.NewStatus != w.Status || c.Resolution != w.Resolution {
		if err := r.st.SetWorkshopStatus(ctx, workshopID, c.NewStatus, c.Resolution); err != nil {
			return nil, err
		}
	}
	r.jobLog(job).Info("payments closed", "workshop_id", workshopID, "status", c.NewStatus, "resolution", c.Resolution,
		"voids", len(c.Voids), "refunds", len(c.Refunds), "reconciles", len(c.Reconciles))
	return nil, r.SettleIfResolved(ctx, workshopID)
}

// SettleIfResolved records how a resolving workshop ended: SETTLED once every
// enrollment is finished (or paid, in a confirmed workshop), NEEDS_ATTENTION
// as soon as a refund failed or an enrollment needs an operator. Otherwise,
// and for a workshop that is not resolving, it changes nothing, so it can be
// called any number of times.
func (r *Runner) SettleIfResolved(ctx context.Context, workshopID string) error {
	wr, err := r.st.WorkshopByID(ctx, workshopID)
	if err != nil || wr.Resolution != domain.ResolutionResolving {
		return err
	}
	w, _, es, _, err := r.workshopView(ctx, workshopID)
	if err != nil {
		return err
	}
	next := settlement(w, es)
	if next == w.Resolution {
		return nil
	}
	r.log.Info("workshop resolved", "workshop_id", workshopID, "status", w.Status, "resolution", next)
	return r.st.SetWorkshopStatus(ctx, workshopID, w.Status, next)
}

func settlement(w domain.Workshop, es []domain.Enrollment) domain.Resolution {
	settled := true
	for _, e := range es {
		switch {
		case e.Status == domain.EnrollmentRefundFailed || e.Status == domain.EnrollmentNeedsAttention:
			return domain.ResolutionNeedsAttention
		case e.Status.Terminal(), e.Status == domain.EnrollmentPaid && w.Status == domain.WorkshopConfirmed:
		default:
			settled = false
		}
	}
	if settled {
		return domain.ResolutionSettled
	}
	return domain.ResolutionResolving
}

// releaseSeats runs release_seats:{workshop}:{minute}: the workshop's
// reserved seats whose window has passed are released, which may be what a
// resolving workshop was waiting for.
func (r *Runner) releaseSeats(ctx context.Context, job *store.Job) (*int, error) {
	if job.WorkshopID == nil {
		return nil, errors.New("release_seats job has no workshop")
	}
	n, err := r.st.ReleaseExpiredSeats(ctx, *job.WorkshopID, r.clock.Now())
	if err != nil {
		return nil, err
	}
	if n > 0 {
		r.jobLog(job).Info("released expired seats", "workshop_id", *job.WorkshopID, "count", n)
	}
	return nil, r.SettleIfResolved(ctx, *job.WorkshopID)
}

// EnqueueSeatReleases enqueues release_seats:{workshop}:{minute}, due now,
// for every workshop with a reserved seat; minute is now's minute in UTC, so
// a second call within the same minute adds nothing. Run calls it once a
// minute.
func (r *Runner) EnqueueSeatReleases(ctx context.Context) error {
	ids, err := r.st.WorkshopIDsWithReservedSeats(ctx)
	if err != nil {
		return err
	}
	now := r.clock.Now()
	minute := now.UTC().Format(minuteLayout)
	for _, id := range ids {
		if err := r.Enqueue(ctx, kindReleaseSeats, kindReleaseSeats+":"+id+":"+minute, &id, nil, nil, now); err != nil {
			return err
		}
	}
	return nil
}
