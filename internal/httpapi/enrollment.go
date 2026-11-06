package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/store"
	"github.com/aalvaropc/commitpool/internal/worker"
)

var (
	errProposalNotOpen   = conflict("proposal_not_open", "Reservations are not open for this workshop.")
	errConsentMismatch   = conflict("consent_version_mismatch", "Please review and accept the current terms before paying.")
	errCapacityFull      = conflict("capacity_full", "Every spot is taken.")
	errPayPalUnavailable = newError(http.StatusBadGateway, "paypal_unavailable",
		"PayPal is not answering right now. Your spot is kept; please try again in a moment.")
	errCheckoutInProgress = conflict("checkout_in_progress",
		"Your checkout is still being prepared in another window. Please try again in a moment.")
)

var (
	checkoutWait  = 3 * time.Second
	checkoutPoll  = 100 * time.Millisecond
	checkoutPause = pause
)

func pause(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

type enrollResponse struct {
	EnrollmentID string `json:"enrollment_id"`
	ApproveURL   string `json:"approve_url"`
	Status       string `json:"status"`
	StatusLabel  string `json:"status_label"`
}

func (s *server) enroll(w http.ResponseWriter, r *http.Request) error {
	p, ws, err := s.participant(r)
	if err != nil {
		return err
	}
	var req struct {
		ConsentVersion string `json:"consent_version"`
		ProposalID     string `json:"proposal_id"`
	}
	if err := decode(r, &req); err != nil {
		return err
	}
	if req.ConsentVersion != ConsentVersion {
		return errConsentMismatch
	}
	ctx := r.Context()
	now := s.Clock.Now()
	proposal, err := s.Store.ActiveProposal(ctx, ws.ID)
	if err != nil {
		return err
	}
	if !paymentsOpen(ws, proposal, now) || !strings.EqualFold(req.ProposalID, proposal.ID) {
		return errProposalNotOpen
	}
	if err := s.compatibleWithProposal(ctx, p.ID, ws, *proposal); err != nil {
		return err
	}

	en, created, err := s.Store.ReserveSeatAndCreateEnrollment(ctx, store.ReserveParams{
		WorkshopID:     p.WorkshopID,
		ParticipantID:  p.ID,
		ProposalID:     proposal.ID,
		ConsentVersion: ConsentVersion,
		Now:            now,
		SeatTTL:        s.Cfg.SeatTTL,
	})
	switch {
	case errors.Is(err, store.ErrProposalNotOpen):
		return errProposalNotOpen
	case errors.Is(err, domain.ErrCapacityFull):
		return errCapacityFull
	case err != nil:
		return err
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	if en, err = s.renewLapsedSeat(ctx, en, now); err != nil {
		return err
	}

	attempt, _, err := s.Store.CheckoutAttempt(ctx, en.ID, proposal.PriceMinor, proposal.Currency)
	if errors.Is(err, store.ErrAttemptNotAllowed) {
		return s.pastCheckout(ctx, w, ws, proposal, en.ID)
	}
	if err != nil {
		return err
	}
	link, err := s.approvalLink(ctx, ws.ID, attempt)
	if err != nil {
		return err
	}
	writeJSON(w, status, enrollResponse{
		EnrollmentID: en.ID,
		ApproveURL:   link,
		Status:       string(en.Status),
		StatusLabel:  enrollmentLabel(en, ws, proposal, &attempt, now),
	})
	return nil
}

func (s *server) renewLapsedSeat(ctx context.Context, en store.EnrollmentRow, now time.Time) (store.EnrollmentRow, error) {
	if en.Status != domain.EnrollmentSeatReserved || !en.SeatExpiresAt.Before(now) {
		return en, nil
	}
	seat, err := s.holdSeat(ctx, en, now)
	switch {
	case err != nil:
		return en, err
	case seat == seatClosed:
		s.Log.Info("enrollment: payments closed before the expired seat could be renewed", "enrollment_id", en.ID)
		return en, errProposalNotOpen
	case seat == seatGone:
		s.Log.Info("enrollment: the seat expired and could not be renewed", "enrollment_id", en.ID)
		return en, errSeatExpired
	}
	s.Log.Info("enrollment: the expired seat is held again", "enrollment_id", en.ID)
	return s.Store.EnrollmentByID(ctx, en.ID)
}

func (s *server) pastCheckout(ctx context.Context, w http.ResponseWriter, ws store.WorkshopRow, p *store.ProposalRow, enrollmentID string) error {
	en, err := s.Store.EnrollmentByID(ctx, enrollmentID)
	if err != nil {
		return err
	}
	latest, err := s.Store.LatestAttempt(ctx, en.ID)
	if err != nil {
		return err
	}
	link := ""
	if en.Status == domain.EnrollmentSeatReserved {
		if link, err = s.attemptApprovalLink(ctx, latest); err != nil {
			return err
		}
	}
	writeJSON(w, http.StatusOK, enrollResponse{
		EnrollmentID: en.ID,
		ApproveURL:   link,
		Status:       string(en.Status),
		StatusLabel:  enrollmentLabel(en, ws, p, latest, s.Clock.Now()),
	})
	return nil
}

func (s *server) attemptApprovalLink(ctx context.Context, a *store.AttemptRow) (string, error) {
	if a == nil {
		return "", nil
	}
	job, err := s.Store.JobByKey(ctx, store.JobKey(store.KindCreateOrder, a.ID))
	if err != nil || job == nil {
		return "", err
	}
	return worker.ApproveURL(*job), nil
}

func (s *server) approvalLink(ctx context.Context, workshopID string, a store.AttemptRow) (string, error) {
	key := store.JobKey(store.KindCreateOrder, a.ID)
	if err := s.Worker.Enqueue(ctx, store.KindCreateOrder, key, &workshopID, &a.ID, nil, s.Clock.Now()); err != nil {
		return "", err
	}
	if err := s.Worker.RunKey(ctx, key); err != nil {
		return "", fmt.Errorf("run %s: %w", key, err)
	}
	for waited := time.Duration(0); ; waited += checkoutPoll {
		job, err := s.Store.JobByKey(ctx, key)
		if err != nil {
			return "", err
		}
		if job == nil {
			return "", fmt.Errorf("job %s vanished", key)
		}
		if link := worker.ApproveURL(*job); link != "" {
			return link, nil
		}
		if job.Status != store.JobRunning {
			s.Log.Error("enrollment: no approval link from PayPal", "workshop_id", workshopID, "attempt_id", a.ID,
				"job_status", job.Status, "http_status", job.HTTPStatus, "provider_issue", job.ProviderIssue, "debug_id", job.DebugID)
			return "", errPayPalUnavailable
		}
		if waited >= checkoutWait {
			return "", errCheckoutInProgress
		}
		if err := checkoutPause(ctx, checkoutPoll); err != nil {
			return "", err
		}
	}
}

func (s *server) compatibleWithProposal(ctx context.Context, participantID string, ws store.WorkshopRow, proposal store.ProposalRow) error {
	a, err := s.Store.LatestAvailabilityByParticipant(ctx, participantID)
	if err != nil || a == nil {
		return err
	}
	in, err := inForce(*a)
	if err != nil {
		return err
	}
	dates, err := s.Store.CandidateDates(ctx, ws.ID)
	if err != nil {
		return err
	}
	loc, err := time.LoadLocation(ws.Timezone)
	if err != nil {
		return err
	}
	for _, d := range dates {
		if d.ID != proposal.ChosenDateID {
			continue
		}
		if ok, blocker := domain.Compatible(in, domain.CandidateDate{ID: d.ID, StartsAt: d.StartsAt, EndsAt: d.EndsAt}, loc); !ok {
			return conflict("date_incompatible", fmt.Sprintf("Your availability says you cannot come on %s (%s). Update it first if that changed.", dateLabel(d, loc), blocker))
		}
	}
	return nil
}

func (s *server) withdraw(w http.ResponseWriter, r *http.Request) error {
	p, ws, err := s.participant(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	en, err := s.Store.ActiveEnrollmentByParticipant(ctx, p.ID)
	if err != nil {
		return err
	}
	if en == nil {
		return conflict("nothing_to_withdraw", "You have no spot to give up.")
	}
	_, voidKey, err := s.Worker.Withdraw(ctx, en.ID)
	switch {
	case errors.Is(err, domain.ErrNothingToWithdraw):
		return conflict("nothing_to_withdraw", "You have no spot to give up.")
	case errors.Is(err, domain.ErrCaptureInProgress):
		return conflict("capture_in_progress", "Your payment is being charged right now; check again in a minute.")
	case errors.Is(err, domain.ErrPaidNoSelfRefund):
		return conflict("paid_no_self_refund", "You have paid. Payments are refunded only if the workshop is cancelled; ask the organizer.")
	case errors.Is(err, domain.ErrHoldUnderReview):
		return conflict("hold_under_review", "PayPal is still reviewing your hold; you can give up your spot once it decides.")
	case errors.Is(err, domain.ErrNeedsOperator):
		return conflict("needs_attention", "Your payment needs a check by the organizer before anything else can change.")
	case err != nil:
		return err
	}
	if voidKey != "" {
		if err := s.Worker.RunKey(ctx, voidKey); err != nil {
			s.Log.Warn("withdraw: the void will run on the worker", "enrollment_id", en.ID, "error", err)
		}
	}
	after, err := s.Store.EnrollmentByID(ctx, en.ID)
	if err != nil {
		return err
	}
	p2, err := s.Store.ActiveProposal(ctx, ws.ID)
	if err != nil {
		return err
	}
	latest, err := s.Store.LatestAttempt(ctx, en.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, enrollmentView(after, ws, p2, latest, s.Clock.Now()))
	return nil
}
