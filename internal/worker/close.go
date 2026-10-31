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

// minuteLayout is the minute a release_seats key names, in UTC.
const minuteLayout = "2006-01-02T15:04"

// closePayments runs close:{proposal_id} when the payment window closes. It
// first heals what missed hooks left undone, then records the outcome of
// domain.CloseDecision on the workshop (decideClose) and only after that
// enqueues what the outcome owes, each for the enrollment's latest attempt:
// void:{attempt} for each authorization to release, refund:{attempt} for each
// completed payment to give back and reconcile:{attempt}:close for each
// capture still in flight. Every key is fixed by its attempt, so running the
// job again enqueues nothing new.
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

// decideClose records the close's outcome and returns it with the view it
// was taken from. A FUNDING workshop moves to domain.CloseDecision's status,
// with the resolution fundingResolution gives, by compare-and-set from the
// status and resolution it read; when that loses, because a capture
// confirmed the workshop, another close cancelled it or it was flagged
// meanwhile, the decision is taken again from the new state. Once the
// cancellation is written, what it owes is re-derived from a fresh view, so a
// payment that completed between the first read and the write is refunded
// by this close. For a workshop that is already cancelled it re-derives what
// the cancellation owes, so a close cut short after writing it enqueues the
// same keys when it runs again. A confirmed workshop gets CloseDecision's
// voids and reconciles and never a refund.
func (r *Runner) decideClose(ctx context.Context, workshopID string) (view, domain.Close, error) {
	now := r.clock.Now()
	for range casRetries {
		v, err := r.workshopView(ctx, workshopID)
		if err != nil {
			return view{}, domain.Close{}, err
		}
		c := domain.CloseDecision(v.w, v.p, v.es, now)
		switch v.w.Status {
		case domain.WorkshopFunding:
			c.Resolution = fundingResolution(v, c.NewStatus, c.Resolution)
			ok, err := r.st.TransitionWorkshop(ctx, workshopID,
				[]domain.WorkshopStatus{domain.WorkshopFunding}, []domain.Resolution{v.w.Resolution}, c.NewStatus, c.Resolution)
			if err != nil {
				return view{}, domain.Close{}, err
			}
			if ok && c.NewStatus != domain.WorkshopCancelled {
				return v, c, nil
			}
			if ok {
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

// cancellationOwed is what cancelling the workshop owes as things stand: the
// lists domain.CloseDecision gives for a FUNDING workshop that misses its
// minimum, whatever the payments now add up to, with the cancelled
// workshop's own status and resolution.
func cancellationOwed(v view, now time.Time) domain.Close {
	w, p := v.w, v.p
	w.Status = domain.WorkshopFunding
	p.Minimum = math.MaxInt
	c := domain.CloseDecision(w, p, v.es, now)
	c.NewStatus, c.Resolution = v.w.Status, v.w.Resolution
	return c
}

// fundingResolution is the resolution a FUNDING workshop takes as it moves to
// status, where the domain rule decided resolution: a workshop already
// flagged for an operator stays flagged, and one confirmed while an
// enrollment needs an operator is flagged, since a confirmed workshop is
// never settled and nothing else would show it.
func fundingResolution(v view, status domain.WorkshopStatus, resolution domain.Resolution) domain.Resolution {
	if v.w.Resolution == domain.ResolutionNeedsAttention ||
		status == domain.WorkshopConfirmed && slices.ContainsFunc(v.es, needsOperator) {
		return domain.ResolutionNeedsAttention
	}
	return resolution
}

// needsOperator reports an enrollment only an operator can finish: a refund
// PayPal failed, or one flagged NEEDS_ATTENTION.
func needsOperator(e domain.Enrollment) bool {
	return e.Status == domain.EnrollmentRefundFailed || e.Status == domain.EnrollmentNeedsAttention
}

// SettleIfResolved records how a resolving workshop ended: SETTLED once every
// enrollment is finished (or paid, in a confirmed workshop) and no failed
// capture or released seat may still hold the buyer's money (mayHold),
// NEEDS_ATTENTION as
// soon as a refund failed or an enrollment needs an operator, which also
// reopens a SETTLED workshop. Otherwise it changes nothing, so it can be
// called any number of times. Each write is a compare-and-set from the state
// it was decided on.
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
		case needsOperator(e):
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

// mayHold reports whether enrollment i's latest attempt may still hold the
// buyer's money: an authorization not yet released, or an authorize that is
// running or whose outcome is unknown, which a seat release does not stop.
func (v view) mayHold(i int) bool {
	j, ok := v.job(i, store.KindAuthorize)
	return holdsAuthorization(v.att[i]) || ok && (j.Status == store.JobRunning || j.Status == store.JobUnknown)
}

// holdsAuthorization reports whether the attempt has an authorization PayPal
// has not shown voided, denied or expired: a hold that may still block the
// buyer's money.
func holdsAuthorization(a *store.AttemptRow) bool {
	return a != nil && a.AuthorizationID != nil &&
		!is(a.AuthorizationStatus, paypal.StatusVoided) && !is(a.AuthorizationStatus, paypal.StatusDenied) && !is(a.AuthorizationStatus, paypal.StatusExpired)
}

// HealWorkshop redoes, idempotently by key, the follow-ups a hook that failed
// after its move would have left undone, from a fresh view of the workshop:
//   - a failed capture or a released seat whose authorization still holds is
//     voided: void:{attempt} is enqueued, or revived when it was skipped
//     (EnqueueOrRevive never sends again a void PayPal refused);
//   - a FUNDING workshop whose payments reach the minimum is confirmed, and
//     one whose authorized and paid enrollments reach it while payments are
//     open gets its capture round (StartCaptureRoundIfReady), which the
//     hook of the authorization that reached it may have missed;
//   - in a cancelled workshop a payment with no refund job is refunded, and an
//     authorization with no void job is voided;
//   - in a confirmed workshop an authorization with neither a capture nor a
//     void job is captured while payments are open and voided after, and an
//     enrollment that needs an operator flags the workshop (flagIfConfirmed);
//   - a resolving or settled workshop is settled again.
//
// A payment kept in a confirmed workshop past its start is left alone:
// nothing tells a late payment from one that confirmed the workshop.
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
		_, err := r.confirmIfFunded(ctx, workshopID)
		errs = append(errs, err)
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
	if v.w.Status == domain.WorkshopConfirmed && slices.ContainsFunc(v.es, needsOperator) {
		errs = append(errs, r.flagWorkshop(ctx, r.log, workshopID))
	}
	errs = append(errs, r.SettleIfResolved(ctx, workshopID))
	return errors.Join(errs...)
}

// flagIfConfirmed flags a CONFIRMED workshop for an operator. A confirmed
// workshop keeps resolution OPEN and is never settled, so nothing else would
// show an enrollment there that needs one; a cancelled workshop is flagged
// by its settlement, and a funding one when it is confirmed
// (fundingResolution).
func (r *Runner) flagIfConfirmed(ctx context.Context, workshopID string) error {
	w, err := r.st.WorkshopByID(ctx, workshopID)
	if err != nil || w.Status != domain.WorkshopConfirmed {
		return err
	}
	return r.flagWorkshop(ctx, r.log, workshopID)
}

// flagWorkshop marks the workshop's resolution NEEDS_ATTENTION and keeps its
// status, by compare-and-set from the state it read.
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
		if err := r.Enqueue(ctx, store.KindReleaseSeats, store.JobKey(store.KindReleaseSeats, id+":"+minute), &id, nil, nil, now); err != nil {
			return err
		}
	}
	return nil
}
