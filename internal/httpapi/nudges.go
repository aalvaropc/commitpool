package httpapi

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"github.com/aalvaropc/commitpool/internal/ai"
	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/present"
	"github.com/aalvaropc/commitpool/internal/store"
)

type suggestionDTO struct {
	Kind          string `json:"kind"`
	ParticipantID string `json:"participant_id,omitempty"`
	Name          string `json:"name,omitempty"`
	Reason        string `json:"reason"`
	Message       string `json:"message"`
	Pending       bool   `json:"pending"`
}

func (s *server) suggestions(ctx context.Context, st workshopState, rec domain.Recommendation, readings map[string]domain.Interpretation, names map[string]string, shareURL string) []suggestionDTO {
	now := s.Clock.Now()
	dateID, minimum := rec.DateID, st.w.Minimum
	var closes time.Time
	if st.proposal != nil {
		dateID, minimum, closes = st.proposal.ChosenDateID, st.proposal.Minimum, st.proposal.ClosesAt
	}
	var date *store.CandidateDateRow
	for i := range st.dates {
		if st.dates[i].ID == dateID {
			date = &st.dates[i]
		}
	}
	if date == nil {
		return []suggestionDTO{}
	}
	var summary domain.DateSummary
	for _, d := range rec.PerDate {
		if d.DateID == dateID {
			summary = d
		}
	}
	var unclear []string
	for _, p := range st.participants {
		if r, ok := readings[p.ID]; ok && r.NeedsClarification {
			unclear = append(unclear, p.ID)
		}
	}
	reserved := map[string]bool{}
	for _, en := range st.enrollments {
		if en.Status.Occupies() {
			reserved[en.ParticipantID] = true
		}
	}
	c := st.counts()
	committed := c.Paid + c.Authorized + c.Capturing + c.Pending
	list := domain.Suggestions(domain.SuggestInput{
		Status: st.w.Status, PaymentsOpen: paymentsOpen(st.w, st.proposal, now), ClosesAt: closes, Now: now,
		Minimum: minimum, Committed: committed, Date: summary, Unclear: unclear, Reserved: reserved,
	})
	day := present.Day(date.StartsAt, st.loc)
	hours := present.Clock(date.StartsAt.In(st.loc)) + "–" + present.Clock(date.EndsAt.In(st.loc))
	deadline := ""
	if !closes.IsZero() {
		deadline = present.Moment(closes, st.loc)
	}
	out := make([]suggestionDTO, 0, len(list))
	for _, sg := range list {
		name := names[sg.ParticipantID]
		in := ai.SuggestInput{
			WorkshopTitle: st.w.Title, Kind: string(sg.Kind), Name: name, Date: day, Hours: hours,
			Deadline: deadline, Needed: minimum, Missing: max(minimum-committed, 0),
		}
		var reason string
		switch sg.Kind {
		case domain.SuggestClarify:
			if r := readings[sg.ParticipantID]; len(r.Ambiguities) > 0 {
				in.Question = r.Ambiguities[0].Question
			}
			reason = name + "’s message needs clarifying."
		case domain.SuggestCondition:
			in.Condition = conditionText(sg.Blocker)
			reason = fmt.Sprintf("%s said %s; %s runs %s.", name, in.Condition, day, hours)
		case domain.SuggestReserve:
			reason = fmt.Sprintf("%s can come %s and hasn’t reserved yet.", name, day)
		case domain.SuggestShare:
			reason = fmt.Sprintf("Reservations close %s, and %s more %s needed.", deadline, fmt.Sprint(in.Missing), plural(in.Missing, "is", "are"))
		}
		message, pending := s.nudgeMessage(ctx, st, in)
		if sg.Kind == domain.SuggestShare && shareURL != "" && message != "" {
			message += "\n" + shareURL
		}
		out = append(out, suggestionDTO{Kind: string(sg.Kind), ParticipantID: sg.ParticipantID, Name: name, Reason: reason, Message: message, Pending: pending})
	}
	return out
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func conditionText(blocker string) string {
	if t, ok := strings.CutPrefix(blocker, "ends after "); ok {
		return "only if it ends by " + clockOf(t)
	}
	if t, ok := strings.CutPrefix(blocker, "starts before "); ok {
		return "only if it starts at " + clockOf(t) + " or later"
	}
	return blocker
}

func clockOf(hhmm string) string {
	t, err := time.Parse("15:04", hhmm)
	if err != nil {
		return hhmm
	}
	return present.Clock(t)
}

func (s *server) nudgeMessage(ctx context.Context, st workshopState, in ai.SuggestInput) (string, bool) {
	key := sha256.Sum256([]byte(st.w.ID + "\nnudge\n" + ai.SuggestFacts(in)))
	text, fresh, start := s.nudges.claim(key, s.Clock.Now())
	if fresh {
		return text, false
	}
	if start {
		bctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), explainTimeout)
		go func() {
			defer cancel()
			text, retryAt := s.writeNudge(bctx, st, in)
			s.nudges.finish(key, text, retryAt)
			if f := afterExplanation.Load(); f != nil {
				(*f)()
			}
		}()
	}
	return text, true
}

func (s *server) writeNudge(ctx context.Context, st workshopState, in ai.SuggestInput) (text string, retryAt time.Time) {
	plain := ai.PlainSuggestion(in)
	defer func() {
		if p := recover(); p != nil {
			s.Log.Error("suggest a nudge panicked", "workshop_id", st.w.ID, "panic", fmt.Sprint(p))
			text, retryAt = plain, s.Clock.Now().Add(explanationRetryAfter)
		}
	}()
	release, err := s.acquireAI(ctx)
	if err != nil {
		return plain, s.Clock.Now().Add(explanationRetryAfter)
	}
	defer release()
	text, meta, err := s.AI.Suggest(ctx, in)
	if err != nil {
		s.Log.Warn("suggest a nudge; using the plain message", "workshop_id", st.w.ID, "error", err)
		return plain, s.Clock.Now().Add(explanationRetryAfter)
	}
	for _, d := range st.dates {
		if present.Day(d.StartsAt, st.loc) == in.Date {
			if namesAnotherDate(text, d.ID, st.dates, st.loc) {
				s.Log.Warn("nudge named another date; using the plain message", "workshop_id", st.w.ID, "model", meta.Model)
				return plain, time.Time{}
			}
		}
	}
	return text, time.Time{}
}
