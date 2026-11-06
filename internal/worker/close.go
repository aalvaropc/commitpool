package worker

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"slices"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/paypal"
	"github.com/aalvaropc/commitpool/internal/store"
)

func (r *Runner) closePayments(ctx context.Context, job *store.Job) (*int, error) {
	if job.WorkshopID == nil {
		return nil, errors.New("close job has no workshop")
	}
	workshopID := *job.WorkshopID
	if err := r.HealWorkshop(ctx, workshopID); err != nil {
		return nil, err
	}
	v, c, err := r.decideClose(ctx, workshopID)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]store.EnrollmentRow, len(v.rows))
	for _, en := range v.rows {
		byID[en.ID] = en
	}
	steps := []struct {
		kind, suffix string
		ids          []string
	}{
		{store.KindVoid, "", c.Voids},
		{store.KindRefund, "", c.Refunds},
		{store.KindReconcile, ":close", c.Reconciles},
	}
	for _, step := range steps {
		for _, id := range step.ids {
			if _, err := r.enqueueLatest(ctx, step.kind, byID[id], step.suffix); err != nil {
				return nil, err
			}
		}
	}
	r.jobLog(job).Info("payments closed", "workshop_id", workshopID, "status", c.NewStatus, "resolution", c.Resolution,
		"voids", len(c.Voids), "refunds", len(c.Refunds), "reconciles", len(c.Reconciles))
	return nil, r.SettleIfResolved(ctx, workshopID)
}

func (r *Runner) decideClose(ctx context.Context, workshopID string) (view, domain.Close, error) {
	now := r.clock.Now()
	for range casRetries {
		v, err := r.workshopView(ctx, workshopID)
		if err != nil {
			return view{}, domain.Close{}, err
		}
		c := domain.CloseDecision(v.w, v.p, v.es)
		switch v.w.Status {
		case domain.WorkshopFunding:
			c.Resolution = fundingResolution(v, c.NewStatus, c.Resolution)
			ok, err := r.st.TransitionWorkshop(ctx, workshopID,
				[]domain.WorkshopStatus{domain.WorkshopFunding}, []domain.Resolution{v.w.Resolution}, c.NewStatus, c.Resolution)
			if err != nil {
				return view{}, domain.Close{}, err
			}
			if ok && c.NewStatus != domain.WorkshopCancelled {
				if c.NewStatus == domain.WorkshopConfirmed {
					r.onConfirmed(ctx, workshopID)
				}
				return v, c, nil
			}
			if ok {
				r.notifyCancelled(ctx, workshopID)
				if v, err = r.workshopView(ctx, workshopID); err != nil {
					return view{}, domain.Close{}, err
				}
				return v, cancellationOwed(v, now), nil
			}
			r.log.Info("workshop changed while payments were closing; deciding again", "workshop_id", workshopID)
		case domain.WorkshopCancelled:
			return v, cancellationOwed(v, now), nil
		default:
			return v, c, nil
		}
	}
	return view{}, domain.Close{}, errWorkshopMoving
}

func cancellationOwed(v view, now time.Time) domain.Close {
	w, p := v.w, v.p
	w.Status = domain.WorkshopFunding
	p.Minimum = math.MaxInt
	c := domain.CloseDecision(w, p, v.es)
	c.NewStatus, c.Resolution = v.w.Status, v.w.Resolution
	return c
}

func fundingResolution(v view, status domain.WorkshopStatus, resolution domain.Resolution) domain.Resolution {
	if v.w.Resolution == domain.ResolutionNeedsAttention ||
		status == domain.WorkshopConfirmed && slices.ContainsFunc(v.es, domain.Enrollment.NeedsOperator) {
		return domain.ResolutionNeedsAttention
	}
	return resolution
}

func (r *Runner) SettleIfResolved(ctx context.Context, workshopID string) error {
	for range casRetries {
		wr, err := r.st.WorkshopByID(ctx, workshopID)
		if err != nil {
			return err
		}
		if wr.Resolution != domain.ResolutionResolving && wr.Resolution != domain.ResolutionSettled {
			return nil
		}
		v, err := r.workshopView(ctx, workshopID)
		if err != nil {
			return err
		}
		from, next := v.w.Resolution, settlement(v)
		if next == from || from == domain.ResolutionSettled && next != domain.ResolutionNeedsAttention {
			return nil
		}
		ok, err := r.st.TransitionWorkshop(ctx, workshopID,
			[]domain.WorkshopStatus{v.w.Status}, []domain.Resolution{from}, v.w.Status, next)
		if err != nil {
			return err
		}
		if ok {
			r.log.Info("workshop resolved", "workshop_id", workshopID, "status", v.w.Status, "from", from, "resolution", next)
			return nil
		}
	}
	return errWorkshopMoving
}

func settlement(v view) domain.Resolution {
	settled := true
	for i, e := range v.es {
		switch {
		case e.NeedsOperator():
			return domain.ResolutionNeedsAttention
		case (e.Status == domain.EnrollmentCaptureFailed || e.Status == domain.EnrollmentReleased) && v.mayHold(i):
			settled = false
		case e.Status.Terminal(), e.Status == domain.EnrollmentPaid && v.w.Status == domain.WorkshopConfirmed:
		default:
			settled = false
		}
	}
	if settled {
		return domain.ResolutionSettled
	}
	return domain.ResolutionResolving
}

func (v view) mayHold(i int) bool {
	j, ok := v.job(i, store.KindAuthorize)
	return holdsAuthorization(v.att[i]) || ok && (j.Status == store.JobRunning || j.Status == store.JobUnknown)
}

func holdsAuthorization(a *store.AttemptRow) bool {
	return a != nil && a.AuthorizationID != nil &&
		!is(a.AuthorizationStatus, paypal.StatusVoided) && !is(a.AuthorizationStatus, paypal.StatusDenied) && !is(a.AuthorizationStatus, paypal.StatusExpired)
}

func (r *Runner) HealWorkshop(ctx context.Context, workshopID string) error {
	v, err := r.workshopView(ctx, workshopID)
	if err != nil {
		return err
	}
	var errs []error
	heal := func(kind string, en store.EnrollmentRow, reason string) {
		created, err := r.enqueueLatest(ctx, kind, en, "")
		if created {
			r.log.Warn("healing a missed step", "workshop_id", workshopID, "enrollment_id", en.ID, "kind", kind, "reason", reason)
		}
		errs = append(errs, err)
	}
	if v.w.Status == domain.WorkshopFunding && domain.Counts(v.es).Paid >= v.p.Minimum {
		confirmed, err := r.confirmIfFunded(ctx, workshopID)
		errs = append(errs, err)
		if confirmed {
			if v, err = r.workshopView(ctx, workshopID); err != nil {
				return errors.Join(append(errs, err)...)
			}
		}
	}
	now := r.clock.Now()
	open := now.Before(v.p.ClosesAt)
	round := domain.ShouldStartCaptureRound(v.w, v.p, domain.Counts(v.es), now)
	for i, en := range v.rows {
		switch {
		case (en.Status == domain.EnrollmentCaptureFailed || en.Status == domain.EnrollmentReleased) && holdsAuthorization(v.att[i]):
			heal(store.KindVoid, en, "authorization left holding after the enrollment ended")
		case v.w.Status == domain.WorkshopCancelled && en.Status == domain.EnrollmentPaid && !v.has(i, store.KindRefund):
			heal(store.KindRefund, en, "payment kept in a cancelled workshop")
		case v.w.Status == domain.WorkshopCancelled && en.Status == domain.EnrollmentAuthorized && !v.has(i, store.KindVoid):
			heal(store.KindVoid, en, "authorization kept in a cancelled workshop")
		case en.Status == domain.EnrollmentCapturing && (v.w.Status == domain.WorkshopCancelled || !open):
			if j, ok := v.job(i, store.KindCapture); ok && j.Status == store.JobNeedsAttention {
				revived, err := r.st.ReviveJob(ctx, j.IdempotencyKey, now, true)
				if revived {
					r.log.Warn("healing a parked capture after payments closed", "workshop_id", workshopID, "enrollment_id", en.ID)
				}
				errs = append(errs, err)
			}
		case v.w.Status == domain.WorkshopConfirmed && en.Status == domain.EnrollmentAuthorized &&
			!v.has(i, store.KindCapture) && !v.has(i, store.KindVoid):
			if open {
				round = true
			} else {
				heal(store.KindVoid, en, "authorization left uncaptured after payments closed")
			}
		}
	}
	if round {
		errs = append(errs, r.StartCaptureRoundIfReady(ctx, workshopID))
	}
	if v.w.Status == domain.WorkshopConfirmed && slices.ContainsFunc(v.es, domain.Enrollment.NeedsOperator) {
		errs = append(errs, r.flagWorkshop(ctx, r.log, workshopID))
	}
	errs = append(errs, r.SettleIfResolved(ctx, workshopID))
	return errors.Join(errs...)
}

func (r *Runner) flagIfConfirmed(ctx context.Context, workshopID string) error {
	w, err := r.st.WorkshopByID(ctx, workshopID)
	if err != nil || w.Status != domain.WorkshopConfirmed {
		return err
	}
	return r.flagWorkshop(ctx, r.log, workshopID)
}

func (r *Runner) flagWorkshop(ctx context.Context, log *slog.Logger, workshopID string) error {
	for range casRetries {
		w, err := r.st.WorkshopByID(ctx, workshopID)
		if err != nil || w.Resolution == domain.ResolutionNeedsAttention {
			return err
		}
		ok, err := r.st.TransitionWorkshop(ctx, workshopID,
			[]domain.WorkshopStatus{w.Status}, []domain.Resolution{w.Resolution}, w.Status, domain.ResolutionNeedsAttention)
		if err != nil {
			return err
		}
		if ok {
			log.Error("workshop flagged for an operator", "workshop_id", workshopID, "status", w.Status, "from", w.Resolution)
			return nil
		}
	}
	return errWorkshopMoving
}

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

func (r *Runner) EnqueueSeatReleases(ctx context.Context) error {
	now := r.clock.Now()
	ids, err := r.st.WorkshopIDsWithExpiredSeats(ctx, now)
	if err != nil {
		return err
	}
	minute := store.Minute(now)
	for _, id := range ids {
		if err := r.Enqueue(ctx, store.KindReleaseSeats, store.JobKey(store.KindReleaseSeats, id+":"+minute), &id, nil, nil, now); err != nil {
			return err
		}
	}
	return nil
}
