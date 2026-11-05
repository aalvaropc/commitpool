package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/store"
)

// mockAuthorizationExpired is the only PayPal-Mock-Response code the demo
// control sets: it makes the next capture of the attempt fail.
const mockAuthorizationExpired = "AUTHORIZATION_EXPIRED"

type participantDTO struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

type demoDTO struct {
	DemoMode     bool `json:"demo_mode"`
	SandboxMocks bool `json:"sandbox_mocks"`
}

type ownerViewDTO struct {
	ShareURL       string            `json:"share_url"`
	Workshop       workshopDTO       `json:"workshop"`
	Dates          []dateDTO         `json:"dates"`
	Proposal       *proposalDTO      `json:"proposal"`
	Tally          ownerTallyDTO     `json:"tally"`
	Participants   []participantDTO  `json:"participants"`
	Availabilities []availabilityDTO `json:"availabilities"`
	Recommendation recommendationDTO `json:"recommendation"`
	Enrollments    []enrollmentDTO   `json:"enrollments"`
	Demo           demoDTO           `json:"demo"`
}

// ownerView is the owner's dashboard: the share link to pass around, every
// participant with their latest availability (message, reading and
// correction), the date recommendation with its explanation, the separated
// counts and each enrollment's financial status. Only the owner sees the
// share link here.
func (s *server) ownerView(w http.ResponseWriter, r *http.Request) error {
	ws, err := s.ownerWorkshop(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	st, err := s.loadState(ctx, ws)
	if err != nil {
		return err
	}
	avails, err := s.Store.AvailabilitiesByWorkshop(ctx, ws.ID)
	if err != nil {
		return err
	}
	rec, err := st.recommend(avails)
	if err != nil {
		return err
	}

	names := make(map[string]string, len(st.participants))
	participants := make([]participantDTO, len(st.participants))
	for i, p := range st.participants {
		names[p.ID] = p.DisplayName
		participants[i] = participantDTO{ID: p.ID, DisplayName: p.DisplayName}
	}
	availabilities := make([]availabilityDTO, len(avails))
	for i, a := range avails {
		in, err := inForce(a)
		if err != nil {
			return err
		}
		availabilities[i] = availabilityView(a, in)
		availabilities[i].ParticipantID = a.ParticipantID
		availabilities[i].DisplayName = names[a.ParticipantID]
	}
	now := s.Clock.Now()
	enrollments := make([]enrollmentDTO, len(st.enrollments))
	for i, en := range st.enrollments {
		latest, err := s.Store.LatestAttempt(ctx, en.ID)
		if err != nil {
			return err
		}
		enrollments[i] = enrollmentView(en, ws, st.proposal, latest, now)
		enrollments[i].ParticipantID = en.ParticipantID
		enrollments[i].DisplayName = names[en.ParticipantID]
	}

	recommendation := recommendationView(rec)
	recommendation.Explanation, recommendation.ExplanationPending = s.explanation(ctx, st, rec, names, len(avails))

	shareURL := ""
	if ws.ShareToken != "" {
		shareURL = s.Cfg.BaseURL + "/w/" + ws.ShareToken
	}
	writeJSON(w, http.StatusOK, ownerViewDTO{
		ShareURL:       shareURL,
		Workshop:       ownerWorkshopView(ws),
		Dates:          dateViews(st.dates, st.loc),
		Proposal:       proposalView(st.proposal, ws, now),
		Tally:          st.ownerTally(),
		Participants:   participants,
		Availabilities: availabilities,
		Recommendation: recommendation,
		Enrollments:    enrollments,
		Demo:           demoDTO{DemoMode: s.Cfg.DemoMode, SandboxMocks: s.Cfg.SandboxMocks},
	})
	return nil
}

var (
	errProposalAlreadyOpen = conflict("proposal_already_open", "Payments are already open for this workshop; its terms are locked.")
	errCaptureStarted      = conflict("capture_already_started", "This enrollment's capture has already started or it is finished.")
)

// proposalRefusals turns each domain.ValidateProposal rule into the reason a
// refused proposal reports.
var proposalRefusals = []struct {
	err             error
	reason, message string
}{
	{domain.ErrWindowMismatch, "window_mismatch", "The payment window does not match the workshop's setting."},
	{domain.ErrWindowTooLong, "window_too_long", "The payment window can be at most 72 hours: PayPal honours a hold for three days."},
	{domain.ErrStartsAtMismatch, "starts_at_mismatch", "The proposal does not start when the chosen date does."},
	{domain.ErrStartsTooSoon, "starts_too_soon", "The workshop must start at least 24 hours after payments close."},
	{domain.ErrMinimumAboveCapacity, "minimum_above_capacity", "The minimum cannot be greater than the capacity."},
	{domain.ErrNotEnoughCompatible, "not_enough_compatible", "Fewer participants can attend this date than the minimum."},
}

// openProposal opens payments on one candidate date. The proposal is built
// from the workshop's own terms and the chosen date, never from the request,
// and domain.ValidateProposal decides whether it may open. store.OpenProposal
// moves the workshop COLLECTING → FUNDING by compare-and-set, which locks its
// terms, and schedules close:{proposal} for when the window ends, in one
// transaction.
func (s *server) openProposal(w http.ResponseWriter, r *http.Request) error {
	ws, err := s.ownerWorkshop(r)
	if err != nil {
		return err
	}
	var req struct {
		DateID string `json:"date_id"`
	}
	if err := decode(r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	st, err := s.loadState(ctx, ws)
	if err != nil {
		return err
	}
	var date *store.CandidateDateRow
	for i := range st.dates {
		if st.dates[i].ID == req.DateID {
			date = &st.dates[i]
		}
	}
	if date == nil {
		return badRequest("invalid_date", "Choose one of this workshop's candidate dates.")
	}
	if ws.Status != domain.WorkshopCollecting {
		return s.alreadyOpen(ctx, st.proposal)
	}

	avails, err := s.Store.AvailabilitiesByWorkshop(ctx, ws.ID)
	if err != nil {
		return err
	}
	rec, err := st.recommend(avails)
	if err != nil {
		return err
	}
	compatible := 0
	for _, d := range rec.PerDate {
		if d.DateID == date.ID {
			compatible = len(d.Compatible)
		}
	}
	now := s.Clock.Now()
	dw := domainWorkshop(ws)
	p := domain.Proposal{
		DateID:   date.ID,
		OpensAt:  now,
		ClosesAt: now.Add(dw.PaymentWindow),
		StartsAt: date.StartsAt,
		Price:    dw.Price,
		Minimum:  ws.Minimum,
		Capacity: ws.Capacity,
	}
	if err := domain.ValidateProposal(dw, p, domain.CandidateDate{ID: date.ID, StartsAt: date.StartsAt, EndsAt: date.EndsAt}, compatible, now); err != nil {
		if errors.Is(err, domain.ErrWorkshopNotCollecting) {
			return s.alreadyOpen(ctx, st.proposal)
		}
		for _, refusal := range proposalRefusals {
			if errors.Is(err, refusal.err) {
				e := conflict("proposal_invalid", refusal.message)
				e.reason = refusal.reason
				return e
			}
		}
		return err
	}

	opened, err := s.Store.OpenProposal(ctx, store.ProposalRow{
		WorkshopID:   ws.ID,
		ChosenDateID: date.ID,
		PriceMinor:   p.Price.Minor,
		Currency:     p.Price.Currency,
		Minimum:      p.Minimum,
		Capacity:     p.Capacity,
		OpensAt:      p.OpensAt,
		ClosesAt:     p.ClosesAt,
		StartsAt:     p.StartsAt,
	})
	if errors.Is(err, store.ErrProposalExists) {
		current, err := s.Store.ActiveProposal(ctx, ws.ID)
		if err != nil {
			return err
		}
		return s.alreadyOpen(ctx, current)
	}
	if err != nil {
		return err
	}
	ws.Status = domain.WorkshopFunding
	writeJSON(w, http.StatusCreated, map[string]*proposalDTO{"proposal": proposalView(&opened, ws, now)})
	return nil
}

// alreadyOpen refuses a second proposal. It first makes sure the open one has
// its close job, a repair for proposals opened before the close was enqueued
// with them; the enqueue is a no-op otherwise.
func (s *server) alreadyOpen(ctx context.Context, open *store.ProposalRow) error {
	if open != nil {
		if err := s.scheduleClose(ctx, *open); err != nil {
			return err
		}
	}
	return errProposalAlreadyOpen
}

func (s *server) scheduleClose(ctx context.Context, p store.ProposalRow) error {
	return s.Worker.Enqueue(ctx, store.KindClose, store.JobKey(store.KindClose, p.ID), &p.WorkshopID, nil, nil, p.ClosesAt)
}

// demoCloseNow ends the payment window now, for demos: the proposal's close
// moves to now first, so no capture round or new seat can start, and then
// close:{proposal} is made due now for the worker, created, revived or
// brought forward as it stands.
func (s *server) demoCloseNow(w http.ResponseWriter, r *http.Request) error {
	if !s.Cfg.DemoMode {
		return errNotFound
	}
	ws, err := s.ownerWorkshop(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	p, err := s.Store.ActiveProposal(ctx, ws.ID)
	if err != nil {
		return err
	}
	if p == nil {
		return errProposalNotOpen
	}
	now := s.Clock.Now()
	closed, err := s.Store.ShortenPaymentWindow(ctx, p.ID, now)
	if err != nil {
		return err
	}
	// Each step is a no-op unless it applies: a missing job is created due
	// now, a failed or parked one is revived now, a pending one is brought
	// forward, so 202 means a close will run.
	key := store.JobKey(store.KindClose, p.ID)
	if err := s.Worker.Enqueue(ctx, store.KindClose, key, &ws.ID, nil, nil, now); err != nil {
		return err
	}
	if _, err := s.Worker.Revive(ctx, key); err != nil {
		return err
	}
	if _, err := s.Store.AdvanceJob(ctx, key, now); err != nil {
		return err
	}
	s.Log.Info("demo: payment window closed now", "workshop_id", ws.ID, "proposal_id", p.ID)
	writeJSON(w, http.StatusAccepted, map[string]*proposalDTO{"proposal": proposalView(&closed, ws, now)})
	return nil
}

// demoFailNextCapture makes the enrollment's next capture fail at PayPal's
// sandbox, by storing the AUTHORIZATION_EXPIRED mock on its latest attempt;
// the capture job sends it as PayPal-Mock-Response. It applies until the
// capture starts: once the enrollment is CAPTURING, or the attempt records a
// capture, it is refused with 409 (store.SetCaptureMock).
func (s *server) demoFailNextCapture(w http.ResponseWriter, r *http.Request) error {
	if !s.Cfg.SandboxMocks {
		return errNotFound
	}
	ws, err := s.ownerWorkshop(r)
	if err != nil {
		return err
	}
	var req struct {
		EnrollmentID string `json:"enrollment_id"`
		MockCode     string `json:"mock_code"`
	}
	if err := decode(r, &req); err != nil {
		return err
	}
	if req.MockCode != "" && req.MockCode != mockAuthorizationExpired {
		return badRequest("unsupported_mock_code", "Only %s can be simulated.", mockAuthorizationExpired)
	}
	ctx := r.Context()
	en, err := s.Store.EnrollmentByID(ctx, req.EnrollmentID)
	if err != nil {
		return notFoundIfMissing(err)
	}
	if en.WorkshopID != ws.ID {
		return errNotFound
	}
	if en.Status != domain.EnrollmentSeatReserved && en.Status != domain.EnrollmentAuthorized {
		return errCaptureStarted
	}
	a, err := s.Store.LatestAttempt(ctx, en.ID)
	if err != nil {
		return err
	}
	if a == nil {
		return conflict("no_payment_attempt", "This enrollment has no payment yet.")
	}
	code := mockAuthorizationExpired
	// Checked again under the enrollment's row lock, so a capture that started
	// while this request ran is seen: the mock must never reach a refund.
	stored, err := s.Store.SetCaptureMock(ctx, a.ID, code)
	if err != nil {
		return err
	}
	if !stored {
		return errCaptureStarted
	}
	s.Log.Info("demo: next capture will fail", "workshop_id", ws.ID, "enrollment_id", en.ID, "attempt_id", a.ID)
	writeJSON(w, http.StatusAccepted, map[string]string{"enrollment_id": en.ID, "attempt_id": a.ID, "mock_code": code})
	return nil
}

// maxCancelReason caps the reason an organizer gives for cancelling.
const maxCancelReason = 280

// cancelWorkshop is the organizer's cancel: any time before the workshop
// starts (worker.CancelWorkshop). Holds are released and payments refunded
// by the worker right after; the answer is the cancelled workshop.
func (s *server) cancelWorkshop(w http.ResponseWriter, r *http.Request) error {
	ws, err := s.ownerWorkshop(r)
	if err != nil {
		return err
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if err := decode(r, &req); err != nil {
		return err
	}
	reason := strings.TrimSpace(req.Reason)
	if len([]rune(reason)) > maxCancelReason {
		return badRequest("invalid_reason", "The reason can be at most %d characters.", maxCancelReason)
	}
	ctx := r.Context()
	switch err := s.Worker.CancelWorkshop(ctx, ws.ID, reason); {
	case errors.Is(err, domain.ErrAlreadyCancelled):
		return conflict("already_cancelled", "This workshop is already cancelled.")
	case errors.Is(err, domain.ErrWorkshopStarted):
		return conflict("workshop_started", "The workshop has already started and can no longer be cancelled.")
	case err != nil:
		return err
	}
	cancelled, err := s.Store.WorkshopByID(ctx, ws.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]workshopDTO{"workshop": ownerWorkshopView(cancelled)})
	return nil
}
