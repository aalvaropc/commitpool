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
// enrollment, or the workshop when the enrollment is finished or paid. An
// order the buyer approved without coming back from PayPal is authorized from
// here (authorizeApproved). Last, the workshop gets the captures it is ready
// for and is healed (HealWorkshop), which also settles it. Reads are safe to
// repeat, so a failed or ambiguous one retries this job like any other
// failure.
func (r *Runner) reconcile(ctx context.Context, job *store.Job) (*int, error) {
	s, err := r.load(ctx, job)
	if err != nil {
		return nil, err
	}
	order, status, err := r.refresh(ctx, s)
	if err != nil {
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
	if err := r.authorizeApproved(ctx, s.log, *a, order); err != nil {
		return nil, err
	}
	workshopID := s.enrollment.WorkshopID
	return nil, errors.Join(r.StartCaptureRoundIfReady(ctx, workshopID), r.HealWorkshop(ctx, workshopID))
}

// authorizeApproved authorizes an order the buyer approved at PayPal without
// coming back to the return page. It acts when PayPal shows the latest
// attempt's order APPROVED with no authorization, the order is the checkout
// the attempt opened (OrderMismatch, as on the return page), payments are
// still open and the enrollment is SEAT_RESERVED or RELEASED. o is the order
// as the reconcile just read it. Expired seats are released first; a released
// enrollment takes a seat again when one is free and otherwise stays
// RELEASED, with nothing authorized. With a seat held it enqueues
// authorize:{attempt}, or revives it: a skipped one automatically, and one
// PayPal refused as ORDER_NOT_APPROVED because the order is approved now.
func (r *Runner) authorizeApproved(ctx context.Context, log *slog.Logger, a store.AttemptRow, o *paypal.Order) error {
	if !is(a.OrderStatus, paypal.StatusApproved) || a.AuthorizationID != nil || o == nil {
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
	p, err := r.st.ProposalByID(ctx, en.ProposalID)
	if err != nil {
		return err
	}
	if p == nil {
		return fmt.Errorf("proposal %s: %w", en.ProposalID, store.ErrNotFound)
	}
	if field := OrderMismatch(*o, a, en.ID, *p); field != "" {
		log.Warn("PayPal reports the order for another checkout; not authorizing it", "field", field)
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
	if err != nil || j == nil || j.Status != store.JobFailed || !is(j.ProviderIssue, issueOrderNotApproved) {
		return err
	}
	log.Info("order approved since PayPal refused to authorize it; reviving the authorize job")
	_, err = r.Revive(ctx, key)
	return err
}

// OrderMismatch names the first thing in which PayPal's order differs from
// the checkout the attempt opened, or "" when it is that checkout: the
// attempt's order, one purchase unit, the enrollment as custom_id, the
// attempt as reference_id and the proposal's price and currency. An approved
// order is authorized only when it matches, from the return page and from a
// reconcile alike.
func OrderMismatch(o paypal.Order, a store.AttemptRow, enrollmentID string, p store.ProposalRow) string {
	if a.OrderID == nil || o.ID != *a.OrderID {
		return "order_id"
	}
	if len(o.PurchaseUnits) != 1 {
		return "purchase_units"
	}
	u := o.PurchaseUnits[0]
	switch {
	case u.CustomID != enrollmentID:
		return "custom_id"
	case u.ReferenceID != a.ID:
		return "reference_id"
	case u.Amount.CurrencyCode != p.Currency:
		return "currency_code"
	case u.Amount.Value != (domain.Money{Minor: p.PriceMinor, Currency: p.Currency}).String():
		return "amount"
	}
	return ""
}

// paymentsOpen reports whether the enrollment's workshop still takes
// authorizations (domain.PaymentsOpen). status is the workshop's status.
func (r *Runner) paymentsOpen(ctx context.Context, en store.EnrollmentRow, now time.Time) (open bool, status domain.WorkshopStatus, err error) {
	w, err := r.st.WorkshopByID(ctx, en.WorkshopID)
	if err != nil {
		return false, "", err
	}
	p, err := r.st.ProposalByID(ctx, en.ProposalID)
	if err != nil {
		return false, w.Status, err
	}
	if p == nil {
		return false, w.Status, fmt.Errorf("proposal %s: %w", en.ProposalID, store.ErrNotFound)
	}
	return domain.PaymentsOpen(domain.Workshop{Status: w.Status}, domain.Proposal{ClosesAt: p.ClosesAt}, now), w.Status, nil
}

// refresh applies the order, with every payment it lists, and then the
// authorization, capture and refund the attempt knows of. A 404 on the order
// of an attempt that was never authorized means PayPal dropped the order
// unapproved: EXPIRED. It returns the order as PayPal showed it, or nil when
// the attempt has none or PayPal no longer has it.
func (r *Runner) refresh(ctx context.Context, s subject) (*paypal.Order, *int, error) {
	id := s.attempt.ID
	var order *paypal.Order
	if s.attempt.OrderID != nil {
		o, err := r.pp.GetOrder(ctx, *s.attempt.OrderID)
		switch pe := paypal.Definitive(err); {
		case err == nil:
			if err := r.ApplyOrder(ctx, id, o); err != nil {
				return nil, nil, err
			}
			order = &o
		case pe != nil && orderGone(pe) && s.attempt.AuthorizationID == nil:
			s.log.Info("order expired at PayPal before it was approved")
			if err := r.expireOrder(ctx, id); err != nil {
				return nil, nil, err
			}
		default:
			return nil, statusOf(err), err
		}
	}
	a, err := r.st.AttemptByID(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if a == nil {
		return nil, nil, fmt.Errorf("payment attempt %s: %w", id, store.ErrNotFound)
	}
	if a.AuthorizationID != nil {
		au, err := r.pp.GetAuthorization(ctx, *a.AuthorizationID)
		if err != nil {
			return nil, statusOf(err), err
		}
		if err := r.ApplyAuthorization(ctx, id, au); err != nil {
			return nil, nil, err
		}
	}
	if a.CaptureID != nil {
		c, err := r.pp.GetCapture(ctx, *a.CaptureID)
		if err != nil {
			return nil, statusOf(err), err
		}
		if err := r.ApplyCapture(ctx, id, c); err != nil {
			return nil, nil, err
		}
	}
	if a.RefundID != nil {
		rf, err := r.pp.GetRefund(ctx, *a.RefundID)
		if err != nil {
			return nil, statusOf(err), err
		}
		if err := r.ApplyRefund(ctx, id, rf); err != nil {
			return nil, nil, err
		}
	}
	return order, nil, nil
}

func (r *Runner) resolveUnknown(ctx context.Context, log *slog.Logger, a store.AttemptRow, last bool) error {
	jobs, err := r.st.JobsByAttempt(ctx, a.ID)
	if err != nil {
		return err
	}
	for _, j := range jobs {
		if j.Status != store.JobUnknown {
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
			if err := r.flagForOperator(ctx, jl, a.EnrollmentID); err != nil {
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

// flagForOperator flags the enrollment or, when it is finished or paid and so
// is not flagged, such as a released seat whose authorize may still have
// placed a hold, its workshop.
func (r *Runner) flagForOperator(ctx context.Context, log *slog.Logger, enrollmentID string) error {
	if err := r.flag(ctx, log, enrollmentID, unpaidActiveStatuses, "PayPal shows no trace of an action after the last reconcile"); err != nil {
		return err
	}
	en, err := r.st.EnrollmentByID(ctx, enrollmentID)
	if err != nil || en.NeedsAttentionAt != nil {
		return err
	}
	return r.flagWorkshop(ctx, log, en.WorkshopID)
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

// expireOrder records the attempt's order as EXPIRED unless the attempt holds
// an authorization by then (store.MarkOrderExpired).
func (r *Runner) expireOrder(ctx context.Context, attemptID string) error {
	_, err := r.st.MarkOrderExpired(ctx, attemptID)
	return err
}
