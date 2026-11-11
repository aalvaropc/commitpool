package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/inproc"
	"github.com/aalvaropc/commitpool/internal/store"
)

const mockAuthorizationExpired = "AUTHORIZATION_EXPIRED"

type participantDTO struct {
	ID          string  `json:"id"`
	DisplayName string  `json:"display_name"`
	Source      string  `json:"source"`
	ChatMessage *string `json:"chat_message"`
}

type ownerParticipantDTO struct {
	participantDTO
	Unread         bool    `json:"unread"`
	ParticipantURL *string `json:"participant_url"`
}

type readingDTO struct {
	Pending int  `json:"pending"`
	Total   int  `json:"total"`
	Active  bool `json:"active"`
}

type demoDTO struct {
	DemoMode     bool `json:"demo_mode"`
	SandboxMocks bool `json:"sandbox_mocks"`
}

type actAsDTO struct {
	Name           string `json:"name"`
	ParticipantURL string `json:"participant_url"`
}

type ownerViewDTO struct {
	ShareURL       string                `json:"share_url"`
	Workshop       workshopDTO           `json:"workshop"`
	Dates          []dateDTO             `json:"dates"`
	Proposal       *proposalDTO          `json:"proposal"`
	Extension      *extensionDTO         `json:"extension"`
	Tally          ownerTallyDTO         `json:"tally"`
	Participants   []ownerParticipantDTO `json:"participants"`
	Reading        readingDTO            `json:"reading"`
	DemoChat       *string               `json:"demo_chat"`
	ChatImported   bool                  `json:"chat_imported"`
	Availabilities []availabilityDTO     `json:"availabilities"`
	Recommendation recommendationDTO     `json:"recommendation"`
	Enrollments    []enrollmentDTO       `json:"enrollments"`
	Suggestions    []suggestionDTO       `json:"suggestions"`
	Payout         *payoutDTO            `json:"payout"`
	Demo           demoDTO               `json:"demo"`
	ActAs          *[]actAsDTO           `json:"act_as,omitempty"`
}

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
	participants, reading := s.ownerParticipants(ws.ID, st.participants, avails)
	for _, p := range st.participants {
		names[p.ID] = p.DisplayName
	}
	availabilities := make([]availabilityDTO, len(avails))
	readings := make(map[string]domain.Interpretation, len(avails))
	for i, a := range avails {
		in, err := inForce(a)
		if err != nil {
			return err
		}
		readings[a.ParticipantID] = in
		availabilities[i] = availabilityView(a, in)
		availabilities[i].ParticipantID = a.ParticipantID
		availabilities[i].DisplayName = names[a.ParticipantID]
	}
	now := s.Clock.Now()
	if st.extension, err = s.extensionOption(ctx, st, now); err != nil {
		return err
	}
	extension := s.extensionView(st.extension, st, r.PathValue("token"))
	proposal, err := s.withLatest(ctx, st, ownerProposalView(st.proposal, ws, now), now)
	if err != nil {
		return err
	}
	latest, err := s.Store.LatestAttemptsByWorkshop(ctx, ws.ID)
	if err != nil {
		return err
	}
	enrollments := make([]enrollmentDTO, len(st.enrollments))
	for i, en := range st.enrollments {
		enrollments[i] = enrollmentView(en, ws, st.proposal, latest[en.ID], now)
		ownerPayment(&enrollments[i], latest[en.ID])
		enrollments[i].ParticipantID = en.ParticipantID
		enrollments[i].DisplayName = names[en.ParticipantID]
	}

	payout, err := s.payoutView(ctx, st, latest, now)
	if err != nil {
		return err
	}

	recommendation := recommendationView(rec)
	recommendation.Explanation, recommendation.ExplanationPending = s.explanation(ctx, st, rec, names, len(avails))

	var actAs *[]actAsDTO
	if ws.IsDemo {
		links, err := s.Store.DemoLinks(ctx, ws.ID)
		if err != nil {
			return err
		}
		people := make([]actAsDTO, len(links))
		for i, l := range links {
			people[i] = actAsDTO{Name: l.DisplayName, ParticipantURL: s.Cfg.BaseURL + "/p/" + l.Token}
		}
		actAs = &people
	}

	shareURL := ""
	if ws.ShareToken != "" {
		shareURL = s.Cfg.BaseURL + "/w/" + ws.ShareToken
	}
	writeJSON(w, http.StatusOK, ownerViewDTO{
		ShareURL:       shareURL,
		Workshop:       ownerWorkshopView(ws),
		Dates:          dateViews(st.dates, st.loc),
		Proposal:       proposal,
		Extension:      extension,
		Tally:          st.ownerTally(),
		Participants:   participants,
		Reading:        reading,
		DemoChat:       demoChat(ws, st.loc),
		ChatImported:   ws.ChatImports > 0,
		Availabilities: availabilities,
		Recommendation: recommendation,
		Enrollments:    enrollments,
		Suggestions:    s.suggestions(ctx, st, rec, readings, names, shareURL),
		Payout:         payout,
		Demo:           demoDTO{DemoMode: s.Cfg.DemoMode, SandboxMocks: s.Cfg.SandboxMocks},
		ActAs:          actAs,
	})
	return nil
}

var (
	errProposalAlreadyOpen = conflict("proposal_already_open", "Reservations are already open for this workshop; its terms are locked.")
	errCaptureStarted      = conflict("capture_already_started", "This enrollment's capture has already started or it is finished.")
)

var proposalRefusals = []struct {
	err             error
	reason, message string
}{
	{domain.ErrWindowMismatch, "window_mismatch", "The time to reserve does not match the workshop's setting."},
	{domain.ErrWindowTooLong, "window_too_long", "Reservations can stay open at most 72 hours: PayPal honours a hold for three days."},
	{domain.ErrStartsAtMismatch, "starts_at_mismatch", "The proposal does not start when the chosen date does."},
	{domain.ErrStartsTooSoon, "starts_too_soon", "The workshop must start at least 24 hours after reservations close."},
	{domain.ErrMinimumAboveCapacity, "minimum_above_capacity", "The people needed cannot be more than the spots."},
	{domain.ErrNotEnoughCompatible, "not_enough_compatible", "Fewer people can come on this date than the workshop needs."},
}

var errOpenNeedsApproval = newError(http.StatusForbidden, "approval_required",
	"An assistant opens reservations only after the organizer approves the terms on their own page: preview them, then confirm with the approval.")

func (s *server) openProposal(w http.ResponseWriter, r *http.Request) error {
	if inproc.Is(r.Context()) {
		return errOpenNeedsApproval
	}
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
	row, err := s.proposalOn(ctx, ws, req.DateID)
	if err != nil {
		return err
	}
	opened, err := s.Store.OpenProposal(ctx, row)
	return s.writeOpened(ctx, w, ws, opened, err)
}

func (s *server) proposalOn(ctx context.Context, ws store.WorkshopRow, dateID string) (store.ProposalRow, error) {
	st, err := s.loadState(ctx, ws)
	if err != nil {
		return store.ProposalRow{}, err
	}
	var date *store.CandidateDateRow
	for i := range st.dates {
		if st.dates[i].ID == dateID {
			date = &st.dates[i]
		}
	}
	if date == nil {
		return store.ProposalRow{}, badRequest("invalid_date", "Choose one of this workshop's candidate dates.")
	}
	if ws.Status != domain.WorkshopCollecting {
		return store.ProposalRow{}, s.alreadyOpen(ctx, st.proposal)
	}

	avails, err := s.Store.AvailabilitiesByWorkshop(ctx, ws.ID)
	if err != nil {
		return store.ProposalRow{}, err
	}
	rec, err := st.recommend(avails)
	if err != nil {
		return store.ProposalRow{}, err
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
	if err := domain.ValidateProposal(dw, p, domain.CandidateDate{ID: date.ID, StartsAt: date.StartsAt, EndsAt: date.EndsAt}, compatible); err != nil {
		if errors.Is(err, domain.ErrWorkshopNotCollecting) {
			return store.ProposalRow{}, s.alreadyOpen(ctx, st.proposal)
		}
		for _, refusal := range proposalRefusals {
			if errors.Is(err, refusal.err) {
				e := conflict("proposal_invalid", refusal.message)
				e.reason = refusal.reason
				return store.ProposalRow{}, e
			}
		}
		return store.ProposalRow{}, err
	}
	return store.ProposalRow{
		WorkshopID:   ws.ID,
		ChosenDateID: date.ID,
		PriceMinor:   p.Price.Minor,
		Currency:     p.Price.Currency,
		Minimum:      p.Minimum,
		Capacity:     p.Capacity,
		OpensAt:      p.OpensAt,
		ClosesAt:     p.ClosesAt,
		StartsAt:     p.StartsAt,
	}, nil
}

func (s *server) writeOpened(ctx context.Context, w http.ResponseWriter, ws store.WorkshopRow, opened store.ProposalRow, err error) error {
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
	writeJSON(w, http.StatusCreated, map[string]*proposalDTO{"proposal": ownerProposalView(&opened, ws, opened.OpensAt)})
	return nil
}

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
	writeJSON(w, http.StatusAccepted, map[string]*proposalDTO{"proposal": ownerProposalView(&closed, ws, now)})
	return nil
}

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

const maxCancelReason = 280

func (s *server) cancelWorkshop(w http.ResponseWriter, r *http.Request) error {
	ws, err := s.ownerWorkshop(r)
	if err != nil {
		return err
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if r.ContentLength != 0 {
		if err := decode(r, &req); err != nil {
			return err
		}
	}
	reason := strings.TrimSpace(req.Reason)
	if len([]rune(reason)) > maxCancelReason {
		return badRequest("invalid_reason", "The reason can be at most %d characters.", maxCancelReason)
	}
	ctx := r.Context()
	switch err := s.Worker.CancelWorkshop(ctx, ws.ID, reason); {
	case errors.Is(err, domain.ErrPayoutLocked):
		return conflict("workshop_started", "The class has started and its organizer is being paid, so it can no longer be cancelled.")
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
