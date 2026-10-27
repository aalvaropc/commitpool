package worker

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/aalvaropc/commitpool/internal/paypal"
	"github.com/aalvaropc/commitpool/internal/store"
)

// reconcile reads the attempt's resources back from PayPal and records what
// they say, then resolves the attempt's unknown jobs: one whose resource now
// exists is completed, and one whose resource does not is sent back to pending
// under its own key, so PayPal either replays its earlier answer or performs
// the action once. The reconcile of generation maxReconcileGenerations stops
// sending: it leaves such a job unknown for an operator and flags the
// enrollment. Reads are safe to repeat, so a failed or ambiguous one retries
// this job like any other failure.
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
	return nil, r.resolveUnknown(ctx, s.log, *a, n >= maxReconcileGenerations)
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
	case kindCreateOrder:
		return a.OrderID != nil, true
	case kindAuthorize:
		return a.AuthorizationID != nil, true
	case kindCapture:
		return a.CaptureID != nil || is(a.AuthorizationStatus, ppCaptured), true
	case kindVoid:
		return is(a.AuthorizationStatus, ppVoided), true
	case kindRefund:
		return a.RefundID != nil || is(a.CaptureStatus, ppRefunded), true
	}
	return false, false
}

// orderGone is PayPal's answer for an order that no longer exists.
func orderGone(pe *paypal.Error) bool {
	return pe.HTTPStatus == http.StatusNotFound && pe.Issue == issueInvalidResourceID
}

func (r *Runner) expireOrder(ctx context.Context, attemptID string) error {
	_, err := r.st.UpdateAttemptFunc(ctx, attemptID, func(a *store.AttemptRow) error {
		expired := ppExpired
		a.OrderStatus = &expired
		return nil
	})
	return err
}
