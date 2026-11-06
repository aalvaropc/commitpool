package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aalvaropc/commitpool/internal/ai"
	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/store"
)

const interpretTimeout = 30 * time.Second

type participantViewDTO struct {
	Participant  participantDTO   `json:"participant"`
	Workshop     workshopDTO      `json:"workshop"`
	Dates        []dateDTO        `json:"dates"`
	Proposal     *proposalDTO     `json:"proposal"`
	Availability *availabilityDTO `json:"availability"`
	Enrollment   *enrollmentDTO   `json:"enrollment"`
	Tally        tallyDTO         `json:"tally"`
	Consent      consentDTO       `json:"consent"`
}

func (s *server) participantView(w http.ResponseWriter, r *http.Request) error {
	p, ws, err := s.participant(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	st, err := s.loadState(ctx, ws)
	if err != nil {
		return err
	}
	now := s.Clock.Now()
	view := participantViewDTO{
		Participant: participantDTO{ID: p.ID, DisplayName: p.DisplayName},
		Workshop:    publicWorkshopView(ws),
		Dates:       dateViews(st.dates, st.loc),
		Proposal:    proposalView(st.proposal, ws, now),
		Tally:       st.tally(),
		Consent:     consentDTO{Version: ConsentVersion, Text: ConsentText},
	}
	a, err := s.Store.LatestAvailabilityByParticipant(ctx, p.ID)
	if err != nil {
		return err
	}
	if a != nil {
		in, err := inForce(*a)
		if err != nil {
			return err
		}
		av := availabilityView(*a, in)
		view.Availability = &av
	}
	if en := ownEnrollment(st.enrollments, p.ID); en != nil {
		latest, err := s.Store.LatestAttempt(ctx, en.ID)
		if err != nil {
			return err
		}
		ev := enrollmentView(*en, ws, st.proposal, latest, now)
		view.Enrollment = &ev
	}
	writeJSON(w, http.StatusOK, view)
	return nil
}

func ownEnrollment(es []store.EnrollmentRow, participantID string) *store.EnrollmentRow {
	var last *store.EnrollmentRow
	for i := range es {
		if es[i].ParticipantID != participantID {
			continue
		}
		if !es[i].Status.Terminal() {
			return &es[i]
		}
		last = &es[i]
	}
	return last
}

func (s *server) submitAvailability(w http.ResponseWriter, r *http.Request) error {
	p, ws, err := s.participant(r)
	if err != nil {
		return err
	}
	var req struct {
		Message string `json:"message"`
	}
	if err := decode(r, &req); err != nil {
		return err
	}
	message := strings.TrimSpace(req.Message)
	if n := utf8.RuneCountInString(message); n == 0 || n > maxMessageLength {
		return badRequest("invalid_message", "Describe your availability in 1 to %d characters.", maxMessageLength)
	}
	ctx := r.Context()
	if err := s.open(ctx, ws); err != nil {
		return err
	}
	st, err := s.loadState(ctx, ws)
	if err != nil {
		return err
	}
	sent, err := s.Store.CountAvailabilities(ctx, p.ID)
	if err != nil {
		return err
	}
	if sent >= maxSubmissionsPerParticipant {
		return conflict("too_many_submissions", "You have sent your availability many times already; correct the reading instead.")
	}
	release, err := s.acquireAI(ctx)
	if err != nil {
		return err
	}
	defer release()
	actx, cancel := context.WithTimeout(ctx, interpretTimeout)
	defer cancel()
	in, meta, err := s.AI.Interpret(actx, ai.InterpretInput{
		WorkshopTitle: ws.Title, Timezone: ws.Timezone, Dates: st.dateOptions(), Message: message,
	})
	if err != nil {
		s.Log.Error("interpret availability", "workshop_id", ws.ID, "participant_id", p.ID, "error", err)
		return newError(http.StatusBadGateway, "ai_unavailable", "We could not read your message right now. Please try again in a moment.")
	}
	clean, problems := cleanInterpretation(in, st.dates)
	if len(problems) > 0 {
		s.Log.Warn("interpreter reading adjusted to the workshop's dates", "workshop_id", ws.ID, "participant_id", p.ID, "problems", len(problems))
	}
	reading, err := json.Marshal(clean)
	if err != nil {
		return err
	}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	saved, err := s.Store.SaveAvailability(ctx, store.AvailabilityRow{
		ParticipantID:      p.ID,
		RawMessage:         message,
		Interpretation:     reading,
		NeedsClarification: clean.NeedsClarification,
		ModelMeta:          metaJSON,
	})
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, map[string]availabilityDTO{"availability": availabilityView(saved, clean)})
	return nil
}

func (s *server) correctAvailability(w http.ResponseWriter, r *http.Request) error {
	p, ws, err := s.participant(r)
	if err != nil {
		return err
	}
	var req struct {
		Corrected *domain.Interpretation `json:"corrected"`
	}
	if err := decode(r, &req); err != nil {
		return err
	}
	if req.Corrected == nil {
		return badRequest("invalid_correction", "Send the corrected reading as corrected.")
	}
	ctx := r.Context()
	if err := s.open(ctx, ws); err != nil {
		return err
	}
	dates, err := s.Store.CandidateDates(ctx, ws.ID)
	if err != nil {
		return err
	}
	clean, problems := cleanInterpretation(*req.Corrected, dates)
	if len(problems) > 0 {
		return badRequest("invalid_correction", "The correction is not valid: %s.", strings.Join(problems, "; "))
	}
	raw, err := json.Marshal(clean)
	if err != nil {
		return err
	}
	id := r.PathValue("id")
	if err := s.Store.CorrectAvailability(ctx, id, p.ID, raw); err != nil {
		return notFoundIfMissing(err)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"availability": map[string]any{"id": id, "corrected": clean, "needs_clarification": clean.NeedsClarification},
	})
	return nil
}

func cleanInterpretation(in domain.Interpretation, dates []store.CandidateDateRow) (domain.Interpretation, []string) {
	var problems []string
	known := make(map[string]bool, len(dates))
	for _, d := range dates {
		known[d.ID] = true
	}
	byDate := make(map[string]domain.DateVerdict, len(in.PerDate))
	for _, v := range in.PerDate {
		switch {
		case !known[v.CandidateDateID]:
			problems = append(problems, fmt.Sprintf("%q is not a candidate date of this workshop", v.CandidateDateID))
			continue
		case byDate[v.CandidateDateID].CandidateDateID != "":
			problems = append(problems, fmt.Sprintf("date %s has more than one verdict", v.CandidateDateID))
			continue
		}
		switch v.Verdict {
		case domain.VerdictAvailable, domain.VerdictUnavailable, domain.VerdictUnknown:
			v.Condition = domain.Condition{Type: domain.ConditionNone}
		case domain.VerdictConditional:
			if !validCondition(v.Condition) {
				problems = append(problems, fmt.Sprintf("date %s needs a condition end_before or start_after with a time as HH:MM", v.CandidateDateID))
				v = domain.DateVerdict{CandidateDateID: v.CandidateDateID, Verdict: domain.VerdictUnknown, Condition: domain.Condition{Type: domain.ConditionNone}, Evidence: v.Evidence}
			}
		default:
			problems = append(problems, fmt.Sprintf("verdict %q for date %s is not available, unavailable, conditional or unknown", v.Verdict, v.CandidateDateID))
			v = domain.DateVerdict{CandidateDateID: v.CandidateDateID, Verdict: domain.VerdictUnknown, Condition: domain.Condition{Type: domain.ConditionNone}, Evidence: v.Evidence}
		}
		byDate[v.CandidateDateID] = v
	}
	out := domain.Interpretation{
		PerDate:            make([]domain.DateVerdict, len(dates)),
		HardConstraints:    nonNil(in.HardConstraints),
		Preferences:        nonNil(in.Preferences),
		Ambiguities:        in.Ambiguities,
		NeedsClarification: in.NeedsClarification,
	}
	if out.Ambiguities == nil {
		out.Ambiguities = []domain.Ambiguity{}
	}
	for i, d := range dates {
		v, ok := byDate[d.ID]
		if !ok {
			v = domain.DateVerdict{CandidateDateID: d.ID, Verdict: domain.VerdictUnknown, Condition: domain.Condition{Type: domain.ConditionNone}}
		}
		out.PerDate[i] = v
	}
	return out, problems
}

func validCondition(c domain.Condition) bool {
	if c.Type != domain.ConditionEndBefore && c.Type != domain.ConditionStartAfter {
		return false
	}
	t, err := time.Parse("15:04", c.Time)
	return err == nil && t.Format("15:04") == c.Time
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
