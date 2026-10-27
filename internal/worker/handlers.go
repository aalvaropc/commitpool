package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/paypal"
	"github.com/aalvaropc/commitpool/internal/store"
)

// PayPal issues the handlers branch on, plus the one the worker records itself.
const (
	issueInvalidResourceID    = "INVALID_RESOURCE_ID"
	issueOrderNotApproved     = "ORDER_NOT_APPROVED"
	issuePreviouslyVoided     = "PREVIOUSLY_VOIDED"
	issueAuthorizationExpired = "AUTHORIZATION_EXPIRED"
	issueCaptureFullyRefunded = "CAPTURE_FULLY_REFUNDED"
	issueDeadlinePassed       = "CAPTURE_DEADLINE_PASSED"
)

// subject is what a payment job acts on, as it stood when the job started.
type subject struct {
	attempt    store.AttemptRow
	enrollment store.EnrollmentRow
	log        *slog.Logger
}

func (r *Runner) load(ctx context.Context, job *store.Job) (subject, error) {
	if job.AttemptID == nil {
		return subject{}, errors.New("job has no payment attempt")
	}
	a, err := r.st.AttemptByID(ctx, *job.AttemptID)
	if err != nil {
		return subject{}, err
	}
	if a == nil {
		return subject{}, fmt.Errorf("payment attempt %s: %w", *job.AttemptID, store.ErrNotFound)
	}
	en, err := r.st.EnrollmentByID(ctx, a.EnrollmentID)
	if err != nil {
		return subject{}, err
	}
	return subject{attempt: *a, enrollment: en, log: r.jobLog(job).With("enrollment_id", en.ID)}, nil
}

// skip refuses a job whose action does not apply to the enrollment as it
// stands. The job is failed, not completed, so it can be revived once the
// action applies again.
func (s subject) skip() (*int, error) {
	return s.refuse("skipped: enrollment " + string(s.enrollment.Status))
}

func (s subject) skipOlderAttempt() (*int, error) {
	return s.refuse("skipped: not the enrollment's latest attempt")
}

func (s subject) refuse(reason string) (*int, error) {
	s.log.Info("job " + reason)
	return nil, &refusal{reason: reason}
}

// failed returns a PayPal error that is not a final answer to settle as is.
func failed(err error) (*int, error) {
	return statusOf(err), err
}

func (r *Runner) mockCode(a store.AttemptRow) string {
	if r.cfg.SandboxMocks && a.MockCode != nil {
		return *a.MockCode
	}
	return ""
}

type orderPayload struct {
	ApproveURL string `json:"approve_url"`
	OrderID    string `json:"order_id"`
}

// createOrder creates the attempt's PayPal order for the attempt's amount,
// stores the order id and status on the attempt and {approve_url, order_id}
// in the job's own payload, where the HTTP layer reads the approval link. The
// enrollment stays SEAT_RESERVED. An order other than the one the attempt
// holds is flagged and its link is not stored.
func (r *Runner) createOrder(ctx context.Context, job *store.Job) (*int, error) {
	s, err := r.load(ctx, job)
	if err != nil {
		return nil, err
	}
	a := s.attempt
	returnURL, cancelURL := r.ReturnURLs(s.enrollment.ID)
	o, err := r.pp.CreateOrder(ctx, paypal.CreateOrderParams{
		RequestID:   job.IdempotencyKey,
		ReferenceID: a.ID,
		CustomID:    s.enrollment.ID,
		Amount: paypal.Money{
			CurrencyCode: a.Currency,
			Value:        domain.Money{Minor: a.AmountMinor, Currency: a.Currency}.String(),
		},
		ReturnURL: returnURL,
		CancelURL: cancelURL,
	})
	if err != nil {
		if pe := definitiveError(err); pe != nil {
			return refused(pe)
		}
		return failed(err)
	}
	ok, err := r.applyOrder(ctx, a.ID, o)
	if err != nil {
		return nil, err
	}
	if !ok {
		return s.refuse("PayPal returned a different order than the attempt holds")
	}
	return nil, r.st.UpdateJobPayload(ctx, job.ID, orderPayload{ApproveURL: o.ApproveURL(), OrderID: o.ID})
}

// authorize authorizes the approved order of the enrollment's latest attempt
// while the enrollment holds its seat and payments are open, recording the
// authorization and its expiry and moving the enrollment SEAT_RESERVED →
// AUTHORIZED. Once payments have closed, or the workshop was cancelled, the
// job is skipped without asking PayPal and the seat waits for its expiry.
// PayPal answers an order the buyer has not approved with 422
// ORDER_NOT_APPROVED, and one that expired unapproved with 404
// INVALID_RESOURCE_ID, recorded as order status EXPIRED. Both refusals leave
// the enrollment SEAT_RESERVED.
func (r *Runner) authorize(ctx context.Context, job *store.Job) (*int, error) {
	s, err := r.load(ctx, job)
	if err != nil {
		return nil, err
	}
	a := s.attempt
	if s.enrollment.Status != domain.EnrollmentSeatReserved {
		return s.skip()
	}
	if driving, err := r.driving(ctx, a); err != nil || !driving {
		if err != nil {
			return nil, err
		}
		return s.skipOlderAttempt()
	}
	if open, _, err := r.paymentsOpen(ctx, s.enrollment, r.clock.Now()); err != nil || !open {
		if err != nil {
			return nil, err
		}
		return s.refuse("skipped: payments closed")
	}
	if a.OrderID == nil {
		return nil, errors.New("payment attempt has no order to authorize")
	}
	o, err := r.pp.AuthorizeOrder(ctx, *a.OrderID, job.IdempotencyKey)
	if err == nil {
		return nil, r.ApplyOrder(ctx, a.ID, o)
	}
	pe := definitiveError(err)
	if pe == nil {
		return failed(err)
	}
	if orderGone(pe) && a.AuthorizationID == nil {
		s.log.Info("order expired at PayPal before it was approved")
		if err := r.expireOrder(ctx, a.ID); err != nil {
			return nil, err
		}
	}
	return refused(pe)
}

// capture captures the authorization of the enrollment's latest attempt once,
// in full and as the final capture. The enrollment moves AUTHORIZED →
// CAPTURING before the request leaves. A CAPTURING enrollment is resumed,
// sending again under the same key: a new attempt can only be opened while
// SEAT_RESERVED, so CAPTURING belongs to the latest attempt, whose only
// capture job is this one, and an earlier run of it may have reached PayPal.
//
// Past the capture deadline (the earlier of the payment close and the
// authorization's expiry) the capture is failed without sending it. A resumed
// run first reads the attempt back from PayPal and fails only when no capture
// exists. A failed capture leaves the authorization open, so both that and a
// final refusal from PayPal mark the enrollment CAPTURE_FAILED and enqueue its
// void.
func (r *Runner) capture(ctx context.Context, job *store.Job) (*int, error) {
	s, err := r.load(ctx, job)
	if err != nil {
		return nil, err
	}
	a := s.attempt
	from := statuses(domain.EnrollmentAuthorized, domain.EnrollmentCapturing)
	if !slices.Contains(from, s.enrollment.Status) {
		return s.skip()
	}
	resumed := s.enrollment.Status == domain.EnrollmentCapturing
	if driving, err := r.driving(ctx, a); err != nil || !driving {
		if err != nil {
			return nil, err
		}
		return s.skipOlderAttempt()
	}
	if a.AuthorizationID == nil {
		return nil, errors.New("payment attempt has no authorization to capture")
	}
	p, err := r.st.ProposalByID(ctx, s.enrollment.ProposalID)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, fmt.Errorf("proposal %s: %w", s.enrollment.ProposalID, store.ErrNotFound)
	}
	moved, err := r.transition(ctx, s.log, s.enrollment.ID, move{from, domain.EnrollmentCapturing})
	if err != nil {
		return nil, err
	}
	if !moved {
		return s.skip()
	}

	var authExpires time.Time
	if a.AuthExpiresAt != nil {
		authExpires = *a.AuthExpiresAt
	}
	if r.clock.Now().After(domain.CaptureDeadline(p.ClosesAt, authExpires)) {
		return r.captureTooLate(ctx, s, resumed)
	}

	c, err := r.pp.CaptureAuthorization(ctx, paypal.CaptureParams{
		AuthorizationID: *a.AuthorizationID,
		RequestID:       job.IdempotencyKey,
		MockCode:        r.mockCode(a),
	})
	if err == nil {
		return nil, r.ApplyCapture(ctx, a.ID, c)
	}
	pe := definitiveError(err)
	if pe == nil {
		return failed(err)
	}
	if err := r.ApplyCaptureFailure(ctx, a.ID, pe); err != nil {
		if errors.Is(err, errCaptureRecorded) {
			s.log.Warn("PayPal refused a capture the attempt already holds; keeping the capture", "issue", pe.Issue)
			return statusOf(pe), nil
		}
		return nil, err
	}
	return refused(pe)
}

// captureTooLate fails a capture past its deadline without sending it. A
// resumed run may already have captured, so it reads the attempt back from
// PayPal first and keeps a capture it finds.
func (r *Runner) captureTooLate(ctx context.Context, s subject, resumed bool) (*int, error) {
	if resumed {
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
		if done, _ := performed(kindCapture, *a); done {
			s.log.Info("capture deadline passed, but PayPal shows the earlier run captured")
			return nil, nil
		}
	}
	s.log.Warn("capture deadline passed; failing the capture without sending it")
	err := r.failCapture(ctx, s.attempt.ID, issueDeadlinePassed, "")
	if errors.Is(err, errCaptureRecorded) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return nil, &refusal{reason: "capture deadline passed", issue: issueDeadlinePassed}
}

// void releases the attempt's authorization. For the enrollment's latest
// attempt, AUTHORIZED goes VOIDING and then VOIDED; after a failed capture the
// enrollment stays CAPTURE_FAILED and only the authorization is recorded as
// VOIDED. An authorization the enrollment does not use, on an older attempt or
// a released seat, is voided without touching the enrollment.
// PREVIOUSLY_VOIDED and AUTHORIZATION_EXPIRED mean the hold is already gone
// and count as voided; any other final refusal flags the enrollment
// NEEDS_ATTENTION when the authorization was its own.
func (r *Runner) void(ctx context.Context, job *store.Job) (*int, error) {
	s, err := r.load(ctx, job)
	if err != nil {
		return nil, err
	}
	a := s.attempt
	st := s.enrollment.Status
	driving, err := r.driving(ctx, a)
	if err != nil {
		return nil, err
	}
	unused := !driving || st == domain.EnrollmentReleased
	own := !unused && (st == domain.EnrollmentAuthorized || st == domain.EnrollmentVoiding)
	if !unused && !own && st != domain.EnrollmentCaptureFailed {
		return s.skip()
	}
	if a.AuthorizationID == nil {
		return nil, errors.New("payment attempt has no authorization to void")
	}
	var stuck *move
	switch {
	case own:
		moved, err := r.transition(ctx, s.log, s.enrollment.ID, move{statuses(domain.EnrollmentAuthorized, domain.EnrollmentVoiding), domain.EnrollmentVoiding})
		if err != nil {
			return nil, err
		}
		if !moved {
			return s.skip()
		}
		stuck = &move{statuses(domain.EnrollmentVoiding), domain.EnrollmentNeedsAttention}
	case !unused:
		stuck = &move{statuses(domain.EnrollmentCaptureFailed), domain.EnrollmentNeedsAttention}
	default:
		s.log.Warn("voiding an authorization the enrollment does not use", "latest_attempt", driving)
	}

	voided := paypal.Authorization{ID: *a.AuthorizationID, Status: ppVoided}
	err = r.pp.VoidAuthorization(ctx, *a.AuthorizationID, job.IdempotencyKey)
	if err == nil {
		return nil, r.ApplyAuthorization(ctx, a.ID, voided)
	}
	pe := definitiveError(err)
	if pe == nil {
		return failed(err)
	}
	if pe.Issue == issuePreviouslyVoided || pe.Issue == issueAuthorizationExpired {
		s.log.Info("authorization already gone at PayPal; counting it as voided", "issue", pe.Issue)
		return statusOf(pe), r.ApplyAuthorization(ctx, a.ID, voided)
	}
	s.log.Error("PayPal refused the void", "http_status", pe.HTTPStatus, "issue", pe.Issue, "debug_id", pe.DebugID)
	if stuck != nil {
		if _, err := r.transition(ctx, s.log, s.enrollment.ID, *stuck); err != nil {
			return nil, err
		}
	}
	return refused(pe)
}

// refund refunds the capture of the enrollment's latest attempt in full. The
// enrollment goes PAID → REFUNDING before the request leaves and then follows
// the refund's status; a job of an older attempt is skipped.
// CAPTURE_FULLY_REFUNDED means the money is already back and counts as
// refunded, the refund id being filled in by a later reconcile; any other
// final refusal flags the enrollment NEEDS_ATTENTION.
func (r *Runner) refund(ctx context.Context, job *store.Job) (*int, error) {
	s, err := r.load(ctx, job)
	if err != nil {
		return nil, err
	}
	a := s.attempt
	if st := s.enrollment.Status; st != domain.EnrollmentPaid && st != domain.EnrollmentRefunding {
		return s.skip()
	}
	if driving, err := r.driving(ctx, a); err != nil || !driving {
		if err != nil {
			return nil, err
		}
		return s.skipOlderAttempt()
	}
	if a.CaptureID == nil {
		return nil, errors.New("payment attempt has no capture to refund")
	}
	moved, err := r.transition(ctx, s.log, s.enrollment.ID, move{statuses(domain.EnrollmentPaid, domain.EnrollmentRefunding), domain.EnrollmentRefunding})
	if err != nil {
		return nil, err
	}
	if !moved {
		return s.skip()
	}

	rf, err := r.pp.RefundCapture(ctx, paypal.RefundParams{
		CaptureID: *a.CaptureID,
		RequestID: job.IdempotencyKey,
		MockCode:  r.mockCode(a),
	})
	if err == nil {
		return nil, r.ApplyRefund(ctx, a.ID, rf)
	}
	pe := definitiveError(err)
	if pe == nil {
		return failed(err)
	}
	if pe.Issue == issueCaptureFullyRefunded {
		s.log.Info("capture already refunded at PayPal; counting it as refunded")
		if err := r.ApplyCapture(ctx, a.ID, paypal.Capture{Status: ppRefunded}); err != nil {
			return nil, err
		}
		return statusOf(pe), r.ApplyRefund(ctx, a.ID, paypal.Refund{Status: ppCompleted})
	}
	s.log.Error("PayPal refused the refund", "http_status", pe.HTTPStatus, "issue", pe.Issue, "debug_id", pe.DebugID)
	if _, err := r.transition(ctx, s.log, s.enrollment.ID, move{statuses(domain.EnrollmentRefunding), domain.EnrollmentNeedsAttention}); err != nil {
		return nil, err
	}
	return refused(pe)
}
