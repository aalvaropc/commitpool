package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/paypal"
	"github.com/aalvaropc/commitpool/internal/store"
)

// PayPal issues the handlers branch on, plus the one the worker records itself.
const (
	issueInvalidResourceID    = "INVALID_RESOURCE_ID"
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

// skip completes a job that has nothing left to do for its enrollment.
func (s subject) skip(why string) (*int, error) {
	s.log.Info("job skipped: "+why, "status", s.enrollment.Status)
	return nil, nil
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
// enrollment stays SEAT_RESERVED.
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
	if err := r.ApplyOrder(ctx, a.ID, o); err != nil {
		return nil, err
	}
	return nil, r.st.UpdateJobPayload(ctx, job.ID, orderPayload{ApproveURL: o.ApproveURL(), OrderID: o.ID})
}

// authorize authorizes the approved order while the enrollment holds its seat,
// recording the authorization and its expiry and moving the enrollment
// SEAT_RESERVED → AUTHORIZED. PayPal answers an order the buyer has not
// approved with 422 ORDER_NOT_APPROVED, and one that expired unapproved with
// 404 INVALID_RESOURCE_ID, recorded as order status EXPIRED. Both answers are
// final and leave the enrollment SEAT_RESERVED.
func (r *Runner) authorize(ctx context.Context, job *store.Job) (*int, error) {
	s, err := r.load(ctx, job)
	if err != nil {
		return nil, err
	}
	a := s.attempt
	if s.enrollment.Status != domain.EnrollmentSeatReserved {
		return s.skip("the enrollment no longer waits for an authorization")
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

// capture captures the authorization once, in full and as the final capture.
// It starts only from an AUTHORIZED enrollment, which moves to CAPTURING
// before the request leaves; a retry of the same job finds it CAPTURING and
// sends again under the same key. Past the capture deadline (the earlier of
// the payment close and the authorization's expiry) the capture fails without
// calling PayPal. A failed capture leaves the authorization open, so both
// that and a final refusal from PayPal mark the enrollment CAPTURE_FAILED and
// enqueue its void.
func (r *Runner) capture(ctx context.Context, job *store.Job) (*int, error) {
	s, err := r.load(ctx, job)
	if err != nil {
		return nil, err
	}
	a := s.attempt
	if st := s.enrollment.Status; st != domain.EnrollmentAuthorized && st != domain.EnrollmentCapturing {
		return s.skip("the enrollment is not authorized")
	}
	if a.AuthorizationID == nil {
		return nil, errors.New("payment attempt has no authorization to capture")
	}
	p, err := r.st.ActiveProposal(ctx, s.enrollment.WorkshopID)
	if err != nil {
		return nil, err
	}
	if p == nil || p.ID != s.enrollment.ProposalID {
		return nil, fmt.Errorf("proposal %s of the enrollment is not the workshop's active proposal", s.enrollment.ProposalID)
	}
	moved, err := r.transition(ctx, s.log, s.enrollment.ID, move{statuses(domain.EnrollmentAuthorized, domain.EnrollmentCapturing), domain.EnrollmentCapturing})
	if err != nil {
		return nil, err
	}
	if !moved {
		return s.skip("the enrollment moved on before the capture started")
	}
	var authExpires time.Time
	if a.AuthExpiresAt != nil {
		authExpires = *a.AuthExpiresAt
	}
	if r.clock.Now().After(domain.CaptureDeadline(p.ClosesAt, authExpires)) {
		s.log.Warn("capture deadline passed; failing the capture without calling PayPal")
		return nil, r.failCapture(ctx, a.ID, issueDeadlinePassed, "")
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
		return nil, err
	}
	return refused(pe)
}

// void releases the authorization. From AUTHORIZED the enrollment goes
// VOIDING and then VOIDED; after a failed capture it stays CAPTURE_FAILED and
// only the authorization is recorded as VOIDED. PREVIOUSLY_VOIDED and
// AUTHORIZATION_EXPIRED mean the hold is already gone and count as voided;
// any other final refusal flags the enrollment NEEDS_ATTENTION.
func (r *Runner) void(ctx context.Context, job *store.Job) (*int, error) {
	s, err := r.load(ctx, job)
	if err != nil {
		return nil, err
	}
	a := s.attempt
	switch s.enrollment.Status {
	case domain.EnrollmentAuthorized, domain.EnrollmentVoiding, domain.EnrollmentCaptureFailed:
	default:
		return s.skip("the enrollment holds no authorization to void")
	}
	if a.AuthorizationID == nil {
		return nil, errors.New("payment attempt has no authorization to void")
	}
	if s.enrollment.Status != domain.EnrollmentCaptureFailed {
		moved, err := r.transition(ctx, s.log, s.enrollment.ID, move{statuses(domain.EnrollmentAuthorized, domain.EnrollmentVoiding), domain.EnrollmentVoiding})
		if err != nil {
			return nil, err
		}
		if !moved {
			return s.skip("the enrollment moved on before the void started")
		}
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
	switch pe.Issue {
	case issuePreviouslyVoided, issueAuthorizationExpired:
		if err := r.ApplyAuthorization(ctx, a.ID, voided); err != nil {
			return nil, err
		}
	default:
		s.log.Error("PayPal refused the void", "http_status", pe.HTTPStatus, "issue", pe.Issue, "debug_id", pe.DebugID)
		stuck := move{statuses(domain.EnrollmentVoiding, domain.EnrollmentCaptureFailed), domain.EnrollmentNeedsAttention}
		if _, err := r.transition(ctx, s.log, s.enrollment.ID, stuck); err != nil {
			return nil, err
		}
	}
	return refused(pe)
}

// refund refunds the capture in full. The enrollment goes PAID → REFUNDING
// before the request leaves and then follows the refund's status.
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
		return s.skip("the enrollment holds no completed payment to refund")
	}
	if a.CaptureID == nil {
		return nil, errors.New("payment attempt has no capture to refund")
	}
	moved, err := r.transition(ctx, s.log, s.enrollment.ID, move{statuses(domain.EnrollmentPaid, domain.EnrollmentRefunding), domain.EnrollmentRefunding})
	if err != nil {
		return nil, err
	}
	if !moved {
		return s.skip("the enrollment moved on before the refund started")
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
		if err := r.ApplyCapture(ctx, a.ID, paypal.Capture{Status: ppRefunded}); err != nil {
			return nil, err
		}
		if err := r.ApplyRefund(ctx, a.ID, paypal.Refund{Status: ppCompleted}); err != nil {
			return nil, err
		}
		return refused(pe)
	}
	s.log.Error("PayPal refused the refund", "http_status", pe.HTTPStatus, "issue", pe.Issue, "debug_id", pe.DebugID)
	if _, err := r.transition(ctx, s.log, s.enrollment.ID, move{statuses(domain.EnrollmentRefunding), domain.EnrollmentNeedsAttention}); err != nil {
		return nil, err
	}
	return refused(pe)
}
