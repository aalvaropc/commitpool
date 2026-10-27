package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/store"
)

var (
	errProposalNotOpen   = conflict("proposal_not_open", "Payments are not open for this workshop.")
	errConsentMismatch   = conflict("consent_version_mismatch", "Please review and accept the current terms before paying.")
	errCapacityFull      = conflict("capacity_full", "Every seat is taken.")
	errPayPalUnavailable = newError(http.StatusBadGateway, "paypal_unavailable",
		"PayPal is not answering right now. Your seat is reserved; please try again in a moment.")
)

type enrollResponse struct {
	EnrollmentID string `json:"enrollment_id"`
	ApproveURL   string `json:"approve_url"`
	Status       string `json:"status"`
	StatusLabel  string `json:"status_label"`
}

// enroll takes the participant's consent to the open proposal, reserves a
// seat and returns where to approve the hold at PayPal. The workshop comes
// from the participant's row and the amount from the proposal, never from
// the request.
//
// It is idempotent per participant: while a checkout can still be approved
// a second request, from another tab say, gets the same enrollment and the
// same approval link (201 the first time, 200 after), and PayPal is asked
// for one order only. An enrollment past checkout, with its hold authorized
// or being charged, answers with its enrollment and its last link, 200.
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
	if !payWindowOpen(ws, proposal, now) || req.ProposalID != proposal.ID {
		return errProposalNotOpen
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

	attempt, _, err := s.Store.CheckoutAttempt(ctx, en.ID, proposal.PriceMinor, proposal.Currency)
	if errors.Is(err, store.ErrAttemptNotAllowed) {
		return s.pastCheckout(ctx, w, ws, en.ID)
	}
	if err != nil {
		return err
	}
	link, err := s.approvalLink(ctx, ws.ID, attempt)
	if err != nil {
		s.Log.Error("enrollment: no approval link from PayPal", "workshop_id", ws.ID, "enrollment_id", en.ID,
			"attempt_id", attempt.ID, "error", err)
		return errPayPalUnavailable
	}
	writeJSON(w, status, enrollResponse{
		EnrollmentID: en.ID,
		ApproveURL:   link,
		Status:       string(en.Status),
		StatusLabel:  statusLabel(en.Status, ws.Status),
	})
	return nil
}

// pastCheckout answers for an enrollment that can no longer open a checkout,
// with its current status and the approval link of its latest attempt.
func (s *server) pastCheckout(ctx context.Context, w http.ResponseWriter, ws store.WorkshopRow, enrollmentID string) error {
	en, err := s.Store.EnrollmentByID(ctx, enrollmentID)
	if err != nil {
		return err
	}
	link, err := s.lastApprovalLink(ctx, en.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, enrollResponse{
		EnrollmentID: en.ID,
		ApproveURL:   link,
		Status:       string(en.Status),
		StatusLabel:  statusLabel(en.Status, ws.Status),
	})
	return nil
}

// lastApprovalLink is the approval link create_order stored for the
// enrollment's latest attempt, or "" when there is none.
func (s *server) lastApprovalLink(ctx context.Context, enrollmentID string) (string, error) {
	a, err := s.Store.LatestAttempt(ctx, enrollmentID)
	if err != nil || a == nil {
		return "", err
	}
	job, err := s.Store.JobByKey(ctx, store.JobKey(store.KindCreateOrder, a.ID))
	if err != nil || job == nil {
		return "", err
	}
	return approveURL(job.Payload), nil
}

// approvalLink returns the attempt's PayPal approval link, creating the
// order now when it does not exist yet: create_order:{attempt} is enqueued
// (a no-op when it already is) and run inline, and the link is read from the
// job's payload. A failure leaves the job to retry under the same key, so a
// later request reuses it; until then there is no link.
func (s *server) approvalLink(ctx context.Context, workshopID string, a store.AttemptRow) (string, error) {
	key := store.JobKey(store.KindCreateOrder, a.ID)
	if err := s.Worker.Enqueue(ctx, store.KindCreateOrder, key, &workshopID, &a.ID, nil, s.Clock.Now()); err != nil {
		return "", err
	}
	if err := s.Worker.RunKey(ctx, key); err != nil {
		return "", err
	}
	job, err := s.Store.JobByKey(ctx, key)
	if err != nil {
		return "", err
	}
	if job == nil {
		return "", fmt.Errorf("job %s vanished", key)
	}
	if link := approveURL(job.Payload); link != "" {
		return link, nil
	}
	return "", fmt.Errorf("job %s is %s without an approval link", key, job.Status)
}

func approveURL(payload json.RawMessage) string {
	var p struct {
		ApproveURL string `json:"approve_url"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return ""
	}
	return p.ApproveURL
}
