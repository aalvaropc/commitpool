package httpapi

import (
	"context"
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/aalvaropc/commitpool/internal/ai"
	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/present"
	"github.com/aalvaropc/commitpool/internal/store"
)

const (
	nudgeSlots      = 1
	groupReplySlots = 1
	nudgeBusyRetry  = 30 * time.Second
)

type suggestionDTO struct {
	Kind          string `json:"kind"`
	ParticipantID string `json:"participant_id,omitempty"`
	Name          string `json:"name,omitempty"`
	Reason        string `json:"reason"`
	Message       string `json:"message"`
	Pending       bool   `json:"pending"`
}

func (s *server) suggestions(ctx context.Context, st workshopState, rec domain.Recommendation, readings map[string]domain.Interpretation, names map[string]string, answered []time.Time, shareURL string) []suggestionDTO {
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
		if en.Status.Occupies() || en.WithdrawnAt != nil {
			reserved[en.ParticipantID] = true
		}
	}
	c := st.counts()
	committed := c.Paid + c.Authorized + c.Capturing + c.Pending
	capacity := st.w.Capacity
	if st.proposal != nil {
		capacity = st.proposal.Capacity
	}
	list := domain.Suggestions(domain.SuggestInput{
		Status: st.w.Status, PaymentsOpen: paymentsOpen(st.w, st.proposal, now), ClosesAt: closes, Now: now,
		Minimum: minimum, Committed: committed, Capacity: capacity, SeatsTaken: c.Occupying,
		Date: summary, Unclear: unclear, Reserved: reserved,
		CanExtend: st.extension.available(), Extended: st.proposal != nil && st.proposal.ExtendedAt != nil,
		GroupReply: domain.GroupReplyDue(answered, now),
	})
	day := present.Day(date.StartsAt, st.loc)
	hours := slotHours(*date, st.loc)
	deadline := ""
	if !closes.IsZero() {
		deadline = present.Moment(closes, st.loc)
	}
	out := make([]suggestionDTO, 0, len(list))
	for _, sg := range list {
		name := names[sg.ParticipantID]
		missing := max(minimum-committed, 0)
		in := ai.SuggestInput{WorkshopTitle: st.w.Title, Kind: string(sg.Kind), Name: name, Date: day, Hours: hours}
		var reason string
		switch sg.Kind {
		case domain.SuggestGroupReply:
			in.CanCome, in.Needed = len(summary.Compatible), minimum
			in.Missing = max(minimum-in.CanCome, 0)
			if rec.Tied {
				in.Tied, in.Hours = leadingLabels(st, rec)
			}
			in.Questions = groupQuestions(st, unclear, readings, names)
			reason = fmt.Sprintf("%d people have answered. Tell the group where things stand.", len(answered))
		case domain.SuggestClarify:
			in.Question = openQuestion(readings[sg.ParticipantID])
			in.Options = classOptions(st.dates, st.loc)
			reason = name + "’s message needs clarifying."
		case domain.SuggestCondition:
			in.Condition = conditionText(sg.Blocker)
			reason = fmt.Sprintf("%s said %s; %s runs %s.", name, in.Condition, day, hours)
		case domain.SuggestReserve:
			in.Deadline, in.Needed = deadline, minimum
			reason = fmt.Sprintf("%s can come %s and hasn’t reserved yet.", name, day)
		case domain.SuggestShare:
			in.Deadline, in.Missing = deadline, missing
			reason = fmt.Sprintf("Reservations close %s, and %d more %s needed.", deadline, missing, plural(missing, "is", "are"))
		case domain.SuggestRescue:
			in.Missing, in.Earlier = missing, deadline
			reason, in.Deadline = rescueNudge(st, deadline, missing)
		}
		message, pending := s.nudgeMessage(ctx, st, in, closes)
		if (sg.Kind == domain.SuggestShare || sg.Kind == domain.SuggestRescue || sg.Kind == domain.SuggestGroupReply) && shareURL != "" && message != "" {
			message += "\n" + shareURL
		}
		out = append(out, suggestionDTO{Kind: string(sg.Kind), ParticipantID: sg.ParticipantID, Name: name, Reason: reason, Message: message, Pending: pending})
	}
	return out
}

func openQuestion(r domain.Interpretation) string {
	if len(r.Ambiguities) > 0 {
		return r.Ambiguities[0].Question
	}
	return ""
}

func groupQuestions(st workshopState, unclear []string, readings map[string]domain.Interpretation, names map[string]string) []ai.NamedQuestion {
	candidates := datesStarting(st.dates, func(store.CandidateDateRow) bool { return true })
	var out []ai.NamedQuestion
	for _, id := range unclear {
		q, ok := ai.Quotable(openQuestion(readings[id]))
		name, okName := ai.Quotable(names[id])
		if !ok || !okName || namesAnotherDate(q, candidates, st.loc) {
			continue
		}
		if out = append(out, ai.NamedQuestion{Name: name, Question: q}); len(out) == domain.MaxGroupReplyQuestions {
			break
		}
	}
	return out
}

func slotHours(d store.CandidateDateRow, loc *time.Location) string {
	return present.Clock(d.StartsAt.In(loc)) + "–" + present.Clock(d.EndsAt.In(loc))
}

func slotLabel(d store.CandidateDateRow, loc *time.Location) string {
	return present.Day(d.StartsAt, loc) + ", " + slotHours(d, loc)
}

func leadingLabels(st workshopState, rec domain.Recommendation) (labels []string, sharedHours string) {
	var lead []store.CandidateDateRow
	days := map[string]bool{}
	for _, d := range st.dates {
		if slices.Contains(rec.LeadingDateIDs(), d.ID) {
			lead = append(lead, d)
			days[present.Day(d.StartsAt, st.loc)] = true
		}
	}
	if len(lead) == 0 {
		return nil, ""
	}
	same := len(days) == len(lead)
	for _, d := range lead {
		same = same && slotHours(d, st.loc) == slotHours(lead[0], st.loc)
	}
	for _, d := range lead {
		if same {
			labels = append(labels, present.Day(d.StartsAt, st.loc))
		} else {
			labels = append(labels, slotLabel(d, st.loc))
		}
	}
	if same {
		sharedHours = slotHours(lead[0], st.loc)
	}
	return labels, sharedHours
}

func classOptions(dates []store.CandidateDateRow, loc *time.Location) string {
	if len(dates) == 0 {
		return ""
	}
	same := true
	for _, d := range dates[1:] {
		same = same && slotHours(d, loc) == slotHours(dates[0], loc)
	}
	days := make([]string, len(dates))
	for i, d := range dates {
		days[i] = present.Day(d.StartsAt, loc)
		if !same {
			days[i] = slotLabel(d, loc)
		}
	}
	list := days[len(days)-1]
	if len(days) > 1 {
		list = strings.Join(days[:len(days)-1], "; ") + " or " + list
	}
	if same {
		list += ", " + slotHours(dates[0], loc)
	}
	return list
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

func (s *server) nudgeMessage(ctx context.Context, st workshopState, in ai.SuggestInput, closes time.Time) (string, bool) {
	key := sha256.Sum256([]byte(st.w.ID + "\nnudge\n" + ai.SuggestFacts(in)))
	text, fresh, start := s.nudges.claim(key, s.Clock.Now())
	if fresh {
		return text, false
	}
	if start {
		bctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), explainTimeout)
		go func() {
			defer cancel()
			text, retryAt := s.writeNudge(bctx, st, in, closes)
			s.nudges.finish(key, text, retryAt)
			if f := afterExplanation.Load(); f != nil {
				(*f)()
			}
		}()
	}
	if text == "" {
		text = ai.PlainSuggestion(in)
	}
	return text, true
}

func (s *server) writeNudge(ctx context.Context, st workshopState, in ai.SuggestInput, closes time.Time) (text string, retryAt time.Time) {
	plain := ai.PlainSuggestion(in)
	defer func() {
		if p := recover(); p != nil {
			s.Log.Error("suggest a nudge panicked", "workshop_id", st.w.ID, "panic", fmt.Sprint(p))
			text, retryAt = plain, s.Clock.Now().Add(explanationRetryAfter)
		}
	}()
	slots := s.nudgeSlots
	if in.Kind == string(domain.SuggestGroupReply) {
		slots = s.replySlots
	}
	release, ok := tryAcquire(slots)
	if !ok {
		return plain, s.Clock.Now().Add(nudgeBusyRetry)
	}
	defer release()
	text, meta, err := s.AI.Suggest(ctx, in)
	if wait, capped := ai.BusyWait(err); capped {
		s.Log.Info("suggest a nudge: Claude is capped; using the plain message", "workshop_id", st.w.ID, "retry_after", wait)
		return plain, s.Clock.Now().Add(max(explanationRetryAfter, wait))
	}
	if err != nil {
		s.Log.Warn("suggest a nudge; using the plain message", "workshop_id", st.w.ID, "error", err)
		return plain, s.Clock.Now().Add(explanationRetryAfter)
	}
	allowed := datesStarting(st.dates, func(d store.CandidateDateRow) bool {
		day := present.Day(d.StartsAt, st.loc)
		return in.Kind == string(domain.SuggestClarify) || day == in.Date || slices.Contains(in.Tied, day) || slices.Contains(in.Tied, slotLabel(d, st.loc))
	})
	if len(allowed) > 0 && !closes.IsZero() {
		allowed = append(allowed, closes)
	}
	if len(allowed) > 0 && in.Kind == string(domain.SuggestRescue) && st.extension != nil {
		allowed = append(allowed, st.extension.to)
	}
	if namesAnotherDate(ai.WithoutQuestions(text, in), allowed, st.loc) {
		s.Log.Warn("nudge named another date; using the plain message", "workshop_id", st.w.ID, "model", meta.Model)
		return plain, time.Time{}
	}
	return text, time.Time{}
}
