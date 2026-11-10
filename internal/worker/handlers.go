package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/paypal"
	"github.com/aalvaropc/commitpool/internal/store"
)

const (
	issueInvalidResourceID    = paypal.IssueInvalidResourceID
	issueOrderNotApproved     = paypal.IssueOrderNotApproved
	issuePreviouslyVoided     = "PREVIOUSLY_VOIDED"
	issueAuthorizationExpired = "AUTHORIZATION_EXPIRED"
	issueCaptureFullyRefunded = "CAPTURE_FULLY_REFUNDED"
	issueDeadlinePassed       = "CAPTURE_DEADLINE_PASSED"
	issueExpiredLocally       = "AUTHORIZATION_EXPIRED_LOCAL"
	issueWorkshopCancelled    = "WORKSHOP_CANCELLED"
)

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

func ApproveURL(j store.Job) string {
	var p orderPayload
	if len(j.Payload) == 0 || json.Unmarshal(j.Payload, &p) != nil {
		return ""
	}
	return p.ApproveURL
}

func (r *Runner) createOrder(ctx context.Context, job *store.Job) (*int, error) {
	s, err := r.load(ctx, job)
	if err != nil {
		return nil, err
	}
	a := s.attempt
	if driving, err := r.driving(ctx, a); err != nil || !driving {
		if err != nil {
			return nil, err
		}
		return s.skipOlderAttempt()
	}
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
		if pe := paypal.Definitive(err); pe != nil {
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
	pe := paypal.Definitive(err)
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
	cancelled, err := r.workshopCancelled(ctx, s.enrollment.WorkshopID)
	if err != nil {
		return nil, err
	}
	if cancelled && !resumed {
		s.log.Info("workshop cancelled; the capture is skipped and the hold voided")
		return s.skip()
	}
	moved, err := r.transition(ctx, s.log, s.enrollment.ID, move{from, domain.EnrollmentCapturing})
	if err != nil {
		return nil, err
	}
	if !moved {
		return s.skip()
	}

	if !cancelled {
		if cancelled, err = r.workshopCancelled(ctx, s.enrollment.WorkshopID); err != nil {
			return nil, err
		}
	}
	if cancelled {
		return r.captureTooLate(ctx, s, true, issueWorkshopCancelled)
	}
	if deadline, missed := captureDeadline(p.ClosesAt, a.AuthExpiresAt); r.clock.Now().After(deadline) {
		return r.captureTooLate(ctx, s, resumed, missed)
	}

	c, err := r.pp.CaptureAuthorization(ctx, paypal.CaptureParams{
		AuthorizationID: *a.AuthorizationID,
		RequestID:       job.IdempotencyKey,
		MockCode:        r.mockCode(a),
	})
	if err == nil {
		return nil, r.ApplyCapture(ctx, a.ID, c)
	}
	pe := paypal.Definitive(err)
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

func captureDeadline(closesAt time.Time, authExpiresAt *time.Time) (deadline time.Time, issue string) {
	var expires time.Time
	if authExpiresAt != nil {
		expires = *authExpiresAt
	}
	deadline = domain.CaptureDeadline(closesAt, expires)
	if deadline.Before(closesAt) {
		return deadline, issueExpiredLocally
	}
	return deadline, issueDeadlinePassed
}

func (r *Runner) captureTooLate(ctx context.Context, s subject, resumed bool, issue string) (*int, error) {
	if resumed {
		if _, status, err := r.refresh(ctx, s); err != nil {
			return status, err
		}
		a, err := r.st.AttemptByID(ctx, s.attempt.ID)
		if err != nil {
			return nil, err
		}
		if a == nil {
			return nil, fmt.Errorf("payment attempt %s: %w", s.attempt.ID, store.ErrNotFound)
		}
		if done, _ := performed(store.KindCapture, *a); done {
			s.log.Info("capture deadline passed, but PayPal shows the earlier run captured")
			return nil, nil
		}
	}
	reason := "capture deadline passed"
	switch issue {
	case issueExpiredLocally:
		reason = "authorization expired before the capture"
	case issueWorkshopCancelled:
		reason = "workshop cancelled before the capture"
	}
	s.log.Warn(reason+"; failing the capture without sending it", "issue", issue)
	err := r.failCapture(ctx, s.attempt.ID, issue, "")
	if errors.Is(err, errCaptureRecorded) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return nil, &refusal{reason: reason, issue: issue}
}

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

	voided := paypal.Authorization{ID: *a.AuthorizationID, Status: paypal.StatusVoided}
	err = r.pp.VoidAuthorization(ctx, *a.AuthorizationID, job.IdempotencyKey)
	if err == nil {
		return nil, r.recordVoided(ctx, s, voided)
	}
	pe := paypal.Definitive(err)
	if pe == nil {
		return failed(err)
	}
	if pe.Issue == issuePreviouslyVoided || pe.Issue == issueAuthorizationExpired {
		s.log.Info("authorization already gone at PayPal; counting it as voided", "issue", pe.Issue)
		return statusOf(pe), r.recordVoided(ctx, s, voided)
	}
	s.log.Error("PayPal refused the void", "http_status", pe.HTTPStatus, "issue", pe.Issue, "debug_id", pe.DebugID)
	if stuck != nil {
		if _, err := r.transition(ctx, s.log, s.enrollment.ID, *stuck); err != nil {
			return nil, err
		}
	} else if err := r.flagWorkshop(ctx, s.log, s.enrollment.WorkshopID); err != nil {
		return nil, err
	}
	return refused(pe)
}

func (r *Runner) recordVoided(ctx context.Context, s subject, voided paypal.Authorization) error {
	if err := r.ApplyAuthorization(ctx, s.attempt.ID, voided); err != nil {
		return err
	}
	return r.SettleIfResolved(ctx, s.enrollment.WorkshopID)
}

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
	moved, paidOut, err := r.st.StartRefund(ctx, s.enrollment.ID, a.ID, statuses(domain.EnrollmentPaid, domain.EnrollmentRefunding))
	if err != nil {
		return nil, err
	}
	if paidOut {
		s.log.Warn("payment was paid out to the organizer; it keeps its seat and is not refunded", "payout_id", deref(a.PayoutID))
		return s.refuse("skipped: paid out to the organizer")
	}
	if !moved {
		return s.skip()
	}
	s.log.Info("enrollment moved", "to", domain.EnrollmentRefunding)

	rf, err := r.pp.RefundCapture(ctx, paypal.RefundParams{
		CaptureID: *a.CaptureID,
		RequestID: job.IdempotencyKey,
		MockCode:  r.mockCode(a),
	})
	if err == nil {
		return nil, r.ApplyRefund(ctx, a.ID, rf)
	}
	pe := paypal.Definitive(err)
	if pe == nil {
		return failed(err)
	}
	if pe.Issue == issueCaptureFullyRefunded {
		s.log.Info("capture already refunded at PayPal; counting it as refunded")
		if err := r.ApplyCapture(ctx, a.ID, paypal.Capture{Status: paypal.StatusRefunded}); err != nil {
			return nil, err
		}
		return statusOf(pe), r.ApplyRefund(ctx, a.ID, paypal.Refund{Status: paypal.StatusCompleted})
	}
	s.log.Error("PayPal refused the refund", "http_status", pe.HTTPStatus, "issue", pe.Issue, "debug_id", pe.DebugID)
	moved, err = r.transition(ctx, s.log, s.enrollment.ID, move{statuses(domain.EnrollmentRefunding), domain.EnrollmentRefundFailed})
	if err != nil {
		return nil, err
	}
	if moved {
		if err := r.flag(ctx, s.log, s.enrollment.ID, statuses(domain.EnrollmentRefundFailed), "PayPal refused the refund: "+pe.Issue); err != nil {
			return nil, err
		}
	}
	return refused(pe)
}

func (r *Runner) workshopCancelled(ctx context.Context, workshopID string) (bool, error) {
	w, err := r.st.WorkshopByID(ctx, workshopID)
	if err != nil {
		return false, err
	}
	return w.Status == domain.WorkshopCancelled, nil
}
