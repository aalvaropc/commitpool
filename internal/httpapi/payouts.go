package httpapi

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/store"
	"github.com/aalvaropc/commitpool/internal/worker"
)

const (
	payoutNoEmail   = "no_email"
	payoutOff       = "off"
	payoutScheduled = "scheduled"
	payoutWaiting   = "waiting"
	payoutCreated   = "created"
	payoutParked    = "parked"
	payoutSending   = "sending"
	payoutPaid      = "paid"
	payoutUnclaimed = "unclaimed"
	payoutFailed    = "failed"
	payoutUnknown   = "unknown"
)

type payoutDTO struct {
	State         string     `json:"state"`
	Locked        bool       `json:"locked"`
	Receiver      string     `json:"receiver,omitempty"`
	ScheduledAt   *time.Time `json:"scheduled_at,omitempty"`
	Waiting       string     `json:"waiting,omitempty"`
	Estimated     bool       `json:"estimated"`
	Amount        *moneyDTO  `json:"amount,omitempty"`
	Gross         *moneyDTO  `json:"gross,omitempty"`
	Fees          *moneyDTO  `json:"fees,omitempty"`
	Seats         int        `json:"seats"`
	Early         bool       `json:"early,omitempty"`
	SenderBatchID string     `json:"sender_batch_id,omitempty"`
	PayoutBatchID string     `json:"payout_batch_id,omitempty"`
	PayoutItemID  string     `json:"payout_item_id,omitempty"`
	TransactionID string     `json:"transaction_id,omitempty"`
	BatchStatus   string     `json:"batch_status,omitempty"`
	ItemStatus    string     `json:"item_status,omitempty"`
	Issue         string     `json:"issue,omitempty"`
	DebugID       string     `json:"debug_id,omitempty"`
	SentAt        *time.Time `json:"sent_at,omitempty"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
}

var payoutStates = map[domain.PayoutStatus]string{
	domain.PayoutCreated:   payoutCreated,
	domain.PayoutSent:      payoutSending,
	domain.PayoutSucceeded: payoutPaid,
	domain.PayoutUnclaimed: payoutUnclaimed,
	domain.PayoutFailed:    payoutFailed,
	domain.PayoutUnknown:   payoutUnknown,
}

func (s *server) frozenPayoutView(ctx context.Context, po store.PayoutRow) (*payoutDTO, error) {
	d := frozenPayoutView(po)
	if po.Status != domain.PayoutCreated {
		return d, nil
	}
	j, err := s.Store.JobByKey(ctx, worker.PayoutKey(po.WorkshopID))
	if err != nil {
		return nil, err
	}
	if j != nil && j.Status == store.JobNeedsAttention {
		d.State = payoutParked
	}
	return d, nil
}

func frozenPayoutView(po store.PayoutRow) *payoutDTO {
	amount, gross, fees := money(po.AmountMinor, po.Currency), money(po.GrossMinor, po.Currency), money(po.FeeMinor, po.Currency)
	d := &payoutDTO{
		State: payoutStates[po.Status], Locked: true, Receiver: po.Receiver, Amount: &amount, Gross: &gross, Fees: &fees,
		Seats: po.Seats, Early: po.Early, SenderBatchID: po.SenderBatchID,
		PayoutBatchID: orEmpty(po.PayoutBatchID), PayoutItemID: orEmpty(po.PayoutItemID), TransactionID: orEmpty(po.TransactionID),
		BatchStatus: orEmpty(po.BatchStatus), ItemStatus: orEmpty(po.ItemStatus), Issue: orEmpty(po.Issue), DebugID: orEmpty(po.DebugID),
		SentAt: utc(po.SentAt), CompletedAt: utc(po.CompletedAt),
	}
	if d.State == "" {
		d.State = payoutUnknown
	}
	return d
}

func (s *server) payoutView(ctx context.Context, st workshopState, latest map[string]*store.AttemptRow, now time.Time) (*payoutDTO, error) {
	po, err := s.Store.PayoutByWorkshop(ctx, st.w.ID)
	if err != nil {
		return nil, err
	}
	if po != nil {
		return s.frozenPayoutView(ctx, *po)
	}
	if st.w.Status != domain.WorkshopConfirmed || st.proposal == nil {
		return nil, nil
	}
	p := st.proposal
	f := domain.PayoutFacts{
		Status: st.w.Status, Resolution: st.w.Resolution, PayoutEmail: st.w.PayoutEmail,
		ClosesAt: p.ClosesAt, StartsAt: p.StartsAt,
	}
	var gross, fees int64
	for _, en := range st.enrollments {
		if slices.Contains(domain.PayoutHoldStatuses, en.Status) {
			f.InFlight++
		}
		if en.NeedsAttentionAt != nil {
			f.Flagged++
		}
		if en.Status != domain.EnrollmentPaid {
			continue
		}
		f.Paid++
		a := latest[en.ID]
		if a == nil || a.CaptureNetMinor == nil || a.CaptureFeeMinor == nil {
			f.MissingNet++
			gross += p.PriceMinor
			continue
		}
		gross += *a.CaptureNetMinor + *a.CaptureFeeMinor
		fees += *a.CaptureFeeMinor
		f.Net += *a.CaptureNetMinor
	}
	starts := p.StartsAt.UTC()
	d := &payoutDTO{Receiver: st.w.PayoutEmail, Estimated: true, Seats: f.Paid, ScheduledAt: &starts}
	grossMoney := money(gross, p.Currency)
	d.Gross = &grossMoney
	if f.MissingNet == 0 {
		amount, fee := money(f.Net, p.Currency), money(fees, p.Currency)
		d.Amount, d.Fees = &amount, &fee
	}
	switch reason := domain.PayoutBlocker(f, now, false); {
	case reason == domain.PayoutNoEmail:
		d.State = payoutNoEmail
	case !s.Cfg.Payouts:
		d.State = payoutOff
	case reason == domain.PayoutPaymentsOpen || reason == domain.PayoutNotStarted || reason == "":
		d.State = payoutScheduled
	default:
		d.State, d.Waiting = payoutWaiting, reason
	}
	return d, nil
}

func orEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

var payoutRefusals = map[string]string{
	domain.PayoutNotConfirmed:   "The class is not on: only a confirmed class pays its organizer.",
	domain.PayoutNeedsAttention: "A payment needs a check first; clear it from the Needs your check panel.",
	domain.PayoutNoEmail:        "This workshop has no PayPal email to pay out to.",
	domain.PayoutPaymentsOpen:   "Reservations are still open. Close them first.",
	domain.PayoutNotStarted:     "The class has not started yet.",
	domain.PayoutInFlight:       "A payment is still moving at PayPal. Try again once it settles.",
	domain.PayoutFlagged:        "A payment needs a check first; clear it from the Needs your check panel.",
	domain.PayoutFeesMissing:    "PayPal has not reported the fees of every payment yet. Try again in a minute.",
	domain.PayoutNothingToPay:   "Nothing was paid, so there is nothing to pay out.",
}

func payoutRefusal(reason string) error {
	msg, ok := payoutRefusals[reason]
	if !ok {
		msg = "The payout cannot start yet."
	}
	e := conflict("payout_not_ready", msg)
	e.reason = reason
	return e
}

func (s *server) demoStartClass(w http.ResponseWriter, r *http.Request) error {
	if !s.Cfg.DemoMode {
		return errNotFound
	}
	ws, err := s.ownerWorkshop(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	po, err := s.Worker.StartPayoutNow(context.WithoutCancel(ctx), ws.ID)
	var notReady *store.ErrPayoutNotReady
	switch {
	case errors.As(err, &notReady):
		return payoutRefusal(notReady.Reason)
	case errors.Is(err, worker.ErrPayoutsOff):
		e := conflict("payout_not_ready", "Payouts are switched off on this server.")
		e.reason = "payouts_off"
		return e
	case err != nil:
		return err
	}
	s.Log.Info("demo: class started now", "workshop_id", ws.ID, "payout_id", po.ID, "payout_status", po.Status)
	view, err := s.frozenPayoutView(ctx, po)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusAccepted, map[string]*payoutDTO{"payout": view})
	return nil
}

func (s *server) operatorCheckPayout(w http.ResponseWriter, r *http.Request) error {
	ws, err := s.ownerWorkshop(r)
	if err != nil {
		return err
	}
	po, err := s.Worker.CheckPayoutNow(r.Context(), ws.ID)
	if errors.Is(err, worker.ErrNoPayout) {
		return conflict("no_payout", "This workshop has not paid out yet.")
	}
	if err != nil {
		return err
	}
	view, err := s.frozenPayoutView(r.Context(), po)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]*payoutDTO{"payout": view})
	return nil
}

func payoutEvent(ev webhookEvent) (worker.PayoutEvent, bool) {
	if !strings.HasPrefix(ev.EventType, "PAYMENT.PAYOUTSBATCH.") && !strings.HasPrefix(ev.EventType, "PAYMENT.PAYOUTS-ITEM.") {
		return worker.PayoutEvent{}, false
	}
	res := ev.Resource
	out := worker.PayoutEvent{ID: ev.ID, PayoutItemID: res.PayoutItemID, PayoutBatchID: res.PayoutBatchID, SenderBatchID: res.SenderBatchID}
	if res.BatchHeader != nil {
		out.PayoutBatchID = cmp.Or(out.PayoutBatchID, res.BatchHeader.PayoutBatchID)
		out.SenderBatchID = cmp.Or(out.SenderBatchID, res.BatchHeader.SenderBatchHeader.SenderBatchID)
	}
	return out, true
}
