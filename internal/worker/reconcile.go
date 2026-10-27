package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/paypal"
	"github.com/aalvaropc/commitpool/internal/store"
)

// reconcile reads the attempt's resources back from PayPal and records what
// they say, then resolves the attempt's unknown jobs: one whose resource now
// exists is completed, and one whose resource does not is sent back to pending
// under its own key, so PayPal either replays its earlier answer or performs
// the action once. The reconcile of generation maxReconcileGenerations stops
// sending: it leaves such a job unknown for an operator and flags the
// enrollment. An order the buyer approved without coming back from PayPal is
// authorized from here (authorizeApproved). Last, the workshop gets the
// captures it is ready for and is healed (HealWorkshop), which also settles
// it. Reads are safe to repeat, so a failed or ambiguous one retries this job
// like any other failure.
func (r *Runner) reconcile(ctx context.Context, job *store.Job) (*int, error) {
	s, err := r.load(ctx, job)
	if err != nil {
		return nil, err
	}
	if status, err := r.refresh(ctx, s); err != nil {
		return status, err
	}
	a, err := r.st.AttemptByID(ctx, s.attempt.ID)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, fmt.Errorf("payment attempt %s: %w", s.attempt.ID, store.ErrNotFound)
	}
	n, _ := generation(*job)
	if err := r.resolveUnknown(ctx, s.log, *a, n >= maxReconcileGenerations); err != nil {
		return nil, err
	}
	if err := r.authorizeApproved(ctx, s.log, *a); err != nil {
		return nil, err
	}
	workshopID := s.enrollment.WorkshopID
	return nil, errors.Join(r.StartCaptureRoundIfReady(ctx, workshopID), r.HealWorkshop(ctx, workshopID))
}

// authorizeApproved authorizes an order the buyer approved at PayPal without
// coming back to the return page. It acts when PayPal shows the latest
// attempt's order APPROVED with no authorization, payments are still open and
// the enrollment is SEAT_RESERVED or RELEASED. Expired seats are released
// first; a released enrollment takes a seat again when one is free and
// otherwise stays RELEASED, with nothing authorized. With a seat held it
// enqueues authorize:{attempt}, or revives it: a skipped one automatically,
// and one PayPal refused as ORDER_NOT_APPROVED because the order is approved
// now.
func (r *Runner) authorizeApproved(ctx context.Context, log *slog.Logger, a store.AttemptRow) error {
	if !is(a.OrderStatus, paypal.StatusApproved) || a.AuthorizationID != nil {
		return nil
	}
	if driving, err := r.driving(ctx, a); err != nil || !driving {
		return err
	}
	en, err := r.st.EnrollmentByID(ctx, a.EnrollmentID)
	if err != nil {
		return err
	}
	if en.Status != domain.EnrollmentSeatReserved && en.Status != domain.EnrollmentReleased {
		return nil
	}
	now := r.clock.Now()
	open, status, err := r.paymentsOpen(ctx, en, now)
	if err != nil || !open {
		if err == nil {
			log.Info("order approved after payments closed; not authorizing it", "workshop_status", status)
		}
		return err
	}
	if _, err := r.st.ReleaseExpiredSeats(ctx, en.WorkshopID, now); err != nil {
		return err
	}
	if en, err = r.st.EnrollmentByID(ctx, en.ID); err != nil {
		return err
	}
	switch en.Status {
	case domain.EnrollmentSeatReserved:
	case domain.EnrollmentReleased:
		reserved, err := r.st.ReReserveSeat(ctx, en.ID, now, r.cfg.SeatTTL)
		if err != nil {
			return err
		}
		if !reserved {
			log.Info("order approved after the seat expired; seat not re-reserved, not authorizing it", "workshop_status", status)
			return nil
		}
		log.Info("order approved after the seat expired; seat taken again")
	default:
		return nil
	}
	key := store.JobKey(store.KindAuthorize, a.ID)
	created, err := r.EnqueueOrRevive(ctx, store.KindAuthorize, key, &en.WorkshopID, &a.ID, nil, now)
	if err != nil || created {
		if created {
			log.Info("order approved without the buyer coming back; authorizing it")
		}
		return err
	}
	j, err := r.st.JobByKey(ctx, key)
	if err != nil || j == nil || j.Status != jobFailed || !is(j.ProviderIssue, issueOrderNotApproved) {
		return err
	}
	log.Info("order approved since PayPal refused to authorize it; reviving the authorize job")
	_, err = r.Revive(ctx, key)
	return err
}

// paymentsOpen reports whether the enrollment's workshop still takes
// authorizations: funding or confirmed, before its payments close. status is
// the workshop's status.
func (r *Runner) paymentsOpen(ctx context.Context, en store.EnrollmentRow, now time.Time) (open bool, status domain.WorkshopStatus, err error) {
	w, err := r.st.WorkshopByID(ctx, en.WorkshopID)
	if err != nil {
		return false, "", err
	}
	if w.Status != domain.WorkshopFunding && w.Status != domain.WorkshopConfirmed {
		return false, w.Status, nil
	}
	p, err := r.st.ProposalByID(ctx, en.ProposalID)
	if err != nil {
		return false, w.Status, err
	}
	if p == nil {
		return false, w.Status, fmt.Errorf("proposal %s: %w", en.ProposalID, store.ErrNotFound)
	}
	return now.Before(p.ClosesAt), w.Status, nil
}

// refresh applies the order, with every payment it lists, and then the
// authorization, capture and refund the attempt knows of. A 404 on the order
// of an attempt that was never authorized means PayPal dropped the order
// unapproved: EXPIRED.
func (r *Runner) refresh(ctx context.Context, s subject) (*int, error) {
	id := s.attempt.ID
	if s.attempt.OrderID != nil {
		o, err := r.pp.GetOrder(ctx, *s.attempt.OrderID)
		switch pe := definitiveError(err); {
		case err == nil:
			if err := r.ApplyOrder(ctx, id, o); err != nil {
				return nil, err
			}
		case pe != nil && orderGone(pe) && s.attempt.AuthorizationID == nil:
			s.log.Info("order expired at PayPal before it was approved")
			if err := r.expireOrder(ctx, id); err != nil {
				return nil, err
			}
		default:
			return failed(err)
		}
	}
	a, err := r.st.AttemptByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, fmt.Errorf("payment attempt %s: %w", id, store.ErrNotFound)
	}
	if a.AuthorizationID != nil {
		au, err := r.pp.GetAuthorization(ctx, *a.AuthorizationID)
		if err != nil {
			return failed(err)
		}
		if err := r.ApplyAuthorization(ctx, id, au); err != nil {
			return nil, err
		}
	}
	if a.CaptureID != nil {
		c, err := r.pp.GetCapture(ctx, *a.CaptureID)
		if err != nil {
			return failed(err)
		}
		if err := r.ApplyCapture(ctx, id, c); err != nil {
			return nil, err
		}
	}
	if a.RefundID != nil {
		rf, err := r.pp.GetRefund(ctx, *a.RefundID)
		if err != nil {
			return failed(err)
		}
		if err := r.ApplyRefund(ctx, id, rf); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func (r *Runner) resolveUnknown(ctx context.Context, log *slog.Logger, a store.AttemptRow, last bool) error {
	jobs, err := r.st.JobsByAttempt(ctx, a.ID)
	if err != nil {
		return err
	}
	for _, j := range jobs {
		if j.Status != jobUnknown {
			continue
		}
		done, known := performed(j.Kind, a)
		if !known {
			continue
		}
		jl := log.With("unknown_job_id", j.ID, "unknown_key", j.IdempotencyKey)
		if done {
			jl.Info("PayPal shows the action happened; completing its job")
			if err := r.tolerate(jl, r.st.CompleteJob(ctx, j.ID, j.HTTPStatus)); err != nil {
				return err
			}
			continue
		}
		if last {
			jl.Error("PayPal still shows no trace of the action after the last reconcile; leaving the job unknown for an operator")
			if err := r.flag(ctx, jl, a.EnrollmentID, unpaidActiveStatuses); err != nil {
				return err
			}
			continue
		}
		jl.Info("PayPal shows no trace of the action; sending it again under the same key")
		if err := r.st.ReenqueueJob(ctx, j.IdempotencyKey, r.clock.Now()); err != nil {
			return err
		}
	}
	return nil
}

// performed reports whether PayPal shows the action of a job of this kind as
// done for the attempt; known is false for kinds a reconcile does not resolve.
func performed(kind string, a store.AttemptRow) (done, known bool) {
	switch kind {
	case store.KindCreateOrder:
		return a.OrderID != nil, true
	case store.KindAuthorize:
		return a.AuthorizationID != nil, true
	case store.KindCapture:
		return a.CaptureID != nil || is(a.AuthorizationStatus, paypal.StatusCaptured), true
	case store.KindVoid:
		return is(a.AuthorizationStatus, paypal.StatusVoided), true
	case store.KindRefund:
		return a.RefundID != nil || is(a.CaptureStatus, paypal.StatusRefunded), true
	}
	return false, false
}

// orderGone is PayPal's answer for an order that no longer exists.
func orderGone(pe *paypal.Error) bool {
	return pe.HTTPStatus == http.StatusNotFound && pe.Issue == issueInvalidResourceID
}

func (r *Runner) expireOrder(ctx context.Context, attemptID string) error {
	_, err := r.st.UpdateAttemptFunc(ctx, attemptID, func(a *store.AttemptRow) error {
		expired := paypal.StatusExpired
		a.OrderStatus = &expired
		return nil
	})
	return err
}
