package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/store"
)

var (
	errProposalNotOpen   = conflict("proposal_not_open", "Payments are not open for this workshop.")
	errConsentMismatch   = conflict("consent_version_mismatch", "Please review and accept the current terms before paying.")
	errCapacityFull      = conflict("capacity_full", "Every seat is taken.")
	errPayPalUnavailable = newError(http.StatusBadGateway, "paypal_unavailable",
		"PayPal is not answering right now. Your seat is reserved; please try again in a moment.")
	errCheckoutInProgress = conflict("checkout_in_progress",
		"Your checkout is still being prepared in another window. Please try again in a moment.")
)

// A request that finds its checkout's create_order running in another
// request waits for it, looking every checkoutPoll for up to checkoutWait;
// checkoutPause is how it waits between looks.
var (
	checkoutWait  = 3 * time.Second
	checkoutPoll  = 100 * time.Millisecond
	checkoutPause = pause
)

// pause waits d or until ctx is done.
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

// enroll takes the participant's consent to the open proposal, reserves a
// seat and returns where to approve the hold at PayPal. The workshop comes
// from the participant's row and the amount from the proposal, never from
// the request.
//
// It is idempotent per participant: while a checkout can still be approved
// a second request, from another tab say, gets the same enrollment and the
// same approval link (201 the first time, 200 after), and PayPal is asked
// for one order only. A seat that expired before the seat tick released it
// is renewed first, or the answer is seat_expired. An enrollment past
// checkout, with its hold authorized or being charged, answers 200 with its
// enrollment and status, no link.
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
	if !paymentsOpen(ws, proposal, now) || req.ProposalID != proposal.ID {
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
		StatusLabel:  enrollmentLabel(en, ws, proposal, now),
	})
	return nil
}

// renewLapsedSeat gives a participant who asks again for a checkout whose
// seat has expired, before the seat tick released it, the seat back under
// the capacity rule (holdSeat) and returns the enrollment as it is then; a
// seat another request or a reconcile renewed first counts as renewed. When
// it cannot be renewed the enrollment is left RELEASED and the answer has no
// link: proposal_not_open once payments have closed, seat_expired otherwise.
// Any other enrollment is returned as is.
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

// pastCheckout answers for an enrollment that can no longer open a checkout,
// with its current status; the approval link of its latest attempt comes
// with it only while the enrollment is SEAT_RESERVED, as when its
// authorization is being recorded.
func (s *server) pastCheckout(ctx context.Context, w http.ResponseWriter, ws store.WorkshopRow, p *store.ProposalRow, enrollmentID string) error {
	en, err := s.Store.EnrollmentByID(ctx, enrollmentID)
	if err != nil {
		return err
	}
	link := ""
	if en.Status == domain.EnrollmentSeatReserved {
		if link, err = s.lastApprovalLink(ctx, en.ID); err != nil {
			return err
		}
	}
	writeJSON(w, http.StatusOK, enrollResponse{
		EnrollmentID: en.ID,
		ApproveURL:   link,
		Status:       string(en.Status),
		StatusLabel:  enrollmentLabel(en, ws, p, s.Clock.Now()),
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
// job's payload. When another request is running that job, as on a double
// click, it waits for the link (checkoutWait) and then answers
// checkout_in_progress. A PayPal failure, which leaves the job to retry under
// the same key, is paypal_unavailable; a database failure is returned as is
// and answered as a 500.
func (s *server) approvalLink(ctx context.Context, workshopID string, a store.AttemptRow) (string, error) {
	key := store.JobKey(store.KindCreateOrder, a.ID)
	if err := s.Worker.Enqueue(ctx, store.KindCreateOrder, key, &workshopID, &a.ID, nil, s.Clock.Now()); err != nil {
		return "", err
	}
	if err := s.Worker.RunKey(ctx, key); err != nil {
		s.Log.Error("enrollment: create_order did not run", "workshop_id", workshopID, "attempt_id", a.ID, "error", err)
		return "", errPayPalUnavailable
	}
	for waited := time.Duration(0); ; waited += checkoutPoll {
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

func approveURL(payload json.RawMessage) string {
	var p struct {
		ApproveURL string `json:"approve_url"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return ""
	}
	return p.ApproveURL
}
