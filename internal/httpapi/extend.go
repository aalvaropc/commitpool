package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/inproc"
	"github.com/aalvaropc/commitpool/internal/present"
	"github.com/aalvaropc/commitpool/internal/store"
)

const demoLastHours = 2 * time.Hour

type extensionOption struct {
	from, to time.Time
	refusal  error
}

func (o *extensionOption) available() bool { return o != nil && o.refusal == nil }

type extensionDTO struct {
	Available  bool       `json:"available"`
	From       time.Time  `json:"from"`
	To         *time.Time `json:"to,omitempty"`
	Refusal    string     `json:"refusal,omitempty"`
	Reason     string     `json:"reason"`
	ApproveURL string     `json:"approve_url,omitempty"`
}

var extensionRefusals = []struct {
	err             error
	code, statement string
}{
	{domain.ErrNotFunding, "not_funding", "Reservations are not open for a class still short of its minimum, so there is no deadline to extend."},
	{domain.ErrTooLateToExtend, "too_late_to_extend", "Reservations close in less than 10 minutes, too soon to move the deadline."},
	{domain.ErrAlreadyExtended, "already_extended", "The deadline was already extended once, and it can move only once."},
	{domain.ErrAlreadyFunded, "already_funded", "Enough people have paid, so the class is on and the deadline stays."},
	{domain.ErrChargingStarted, "charging_started", "Charging has started, so not everyone could still leave; the deadline stays."},
	{domain.ErrConsentPredatesExtension, "consent_predates_extension", "Someone reserved on terms that did not mention an extension, so the deadline stays."},
	{domain.ErrExtensionTooSmall, "extension_too_small", "The deadline is already about as late as it can be: within three days of opening, while PayPal honours the holds, and a day before the class."},
}

var (
	errExtendNeedsOrganizer = newError(http.StatusForbidden, "approval_required",
		"An assistant cannot extend the deadline. Give the organizer the approve_url from preview_extend_reservations; they confirm on their own page.")
	errExtensionChanged = conflict("extension_changed", "The deadline or the extension on offer changed. Check the new option and try again.")
	errCloseInProgress  = conflict("close_in_progress", "Reservations are closing right now, so the deadline can no longer move.")
)

func extensionRefusal(err error) (code, statement string, ok bool) {
	for _, r := range extensionRefusals {
		if errors.Is(err, r.err) {
			return r.code, r.statement, true
		}
	}
	return "", "", false
}

func (s *server) extensionOption(ctx context.Context, st workshopState, now time.Time) (*extensionOption, error) {
	p := st.proposal
	if p == nil {
		return nil, nil
	}
	check := domain.ExtensionCheck{Status: st.w.Status, Proposal: p.Domain(), Extended: p.ExtendedAt != nil, Now: now}
	if st.w.Status == domain.WorkshopFunding {
		var err error
		if check, err = s.Store.ExtensionCheck(ctx, st.w.Status, *p, now); err != nil {
			return nil, err
		}
	}
	to, refusal := domain.CanExtend(check)
	return &extensionOption{from: p.ClosesAt, to: to, refusal: refusal}, nil
}

func (s *server) extensionView(o *extensionOption, st workshopState, ownerToken string) *extensionDTO {
	if o == nil {
		return nil
	}
	out := &extensionDTO{From: o.from.UTC()}
	if o.refusal != nil {
		code, statement, ok := extensionRefusal(o.refusal)
		if !ok {
			s.Log.Error("deadline extension refused for an unmapped reason", "workshop_id", st.w.ID, "error", o.refusal)
			code, statement = "unavailable", "The deadline cannot be extended right now."
		}
		out.Refusal, out.Reason = code, statement
		return out
	}
	to := o.to.UTC()
	out.Available, out.To = true, &to
	out.Reason = fmt.Sprintf("You can move the deadline once, to %s. Holds stay in place, everyone with an email is told, and anyone can still leave before charging starts.", present.Moment(o.to, st.loc))
	out.ApproveURL = s.Cfg.BaseURL + "/owner/" + ownerToken + "?extend=" + strconv.FormatInt(o.to.Unix(), 10)
	return out
}

func extendedFrom(p *store.ProposalRow) *time.Time {
	if p.ExtendedFrom == nil {
		return nil
	}
	from := p.ExtendedFrom.UTC()
	return &from
}

func latestClosesAt(p *store.ProposalRow, status domain.WorkshopStatus) time.Time {
	latest := p.ClosesAt
	if p.ExtendedAt == nil && status == domain.WorkshopFunding {
		if bound := domain.LatestDeadline(p.Domain()); bound.After(latest) {
			latest = bound
		}
	}
	return latest.UTC()
}

func rescueNudge(st workshopState, deadline string, missing int) (reason, newDeadline string) {
	newDeadline = present.Moment(st.extension.to, st.loc)
	return fmt.Sprintf("Reservations close %s and %d more %s needed. Extending the deadline once, to %s, gives people more time; Claude drafted what to tell the group once you do.",
		deadline, missing, plural(missing, "is", "are"), newDeadline), newDeadline
}

func (s *server) extendDeadline(w http.ResponseWriter, r *http.Request) error {
	if inproc.Is(r.Context()) {
		return errExtendNeedsOrganizer
	}
	ws, err := s.ownerWorkshop(r)
	if err != nil {
		return err
	}
	var req struct {
		From time.Time `json:"from"`
		To   time.Time `json:"to"`
	}
	if err := decode(r, &req); err != nil {
		return err
	}
	if req.From.IsZero() || req.To.IsZero() {
		return badRequest("invalid_extension", "Send the current deadline as from and the deadline on offer as to.")
	}
	ctx := r.Context()
	p, err := s.Store.ActiveProposal(ctx, ws.ID)
	if err != nil {
		return err
	}
	if p == nil {
		return errProposalNotOpen
	}
	extended, err := s.Worker.ExtendDeadline(ctx, p.ID, req.From, req.To)
	if err != nil {
		return extendError(err)
	}
	writeJSON(w, http.StatusOK, map[string]*proposalDTO{"proposal": ownerProposalView(&extended, ws, s.Clock.Now())})
	return nil
}

func extendError(err error) error {
	switch {
	case errors.Is(err, store.ErrExtensionChanged):
		return errExtensionChanged
	case errors.Is(err, store.ErrCloseNotPending):
		return errCloseInProgress
	}
	if code, statement, ok := extensionRefusal(err); ok {
		return conflict(code, statement)
	}
	return err
}

func (s *server) demoSkipToLastHours(w http.ResponseWriter, r *http.Request) error {
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
	now := s.Clock.Now()
	if ws.Status != domain.WorkshopFunding || !paymentsOpen(ws, p, now) {
		return errProposalNotOpen
	}
	if p.ExtendedAt != nil {
		return conflict("already_extended", "The deadline was already extended, so the demo clock no longer moves it.")
	}
	shortened, err := s.Store.ShortenPaymentWindow(ctx, p.ID, now.Add(demoLastHours))
	if err != nil {
		return err
	}
	if _, err := s.Store.AdvanceJob(ctx, store.JobKey(store.KindClose, p.ID), shortened.ClosesAt); err != nil {
		return err
	}
	s.Log.Info("demo: skipped to the last hours of reservations", "workshop_id", ws.ID, "proposal_id", p.ID)
	writeJSON(w, http.StatusAccepted, map[string]*proposalDTO{"proposal": ownerProposalView(&shortened, ws, now)})
	return nil
}
