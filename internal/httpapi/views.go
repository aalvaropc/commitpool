package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aalvaropc/commitpool/internal/ai"
	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/present"
	"github.com/aalvaropc/commitpool/internal/store"
)

const ConsentVersion = "consent-v5"

const ConsentText = "Reserving your spot places a temporary hold for the price on your PayPal account. Nothing is charged yet. " +
	"Holds are charged once enough people have reserved, and the workshop is on when the minimum number of payments has gone through. " +
	"If the minimum is not reached by the deadline, the workshop is cancelled: every hold is released and any charge already made is refunded. " +
	"You can leave and have your hold released until charging starts; after that, your payment is refunded only if the workshop is cancelled, including when the organizer cancels it. " +
	"Refunds can take a few days, and you can follow yours on your page. " +
	"The organizer can extend the deadline once, to at most three days after reservations opened. " +
	"Your page always shows the current deadline and the latest possible one, and you can leave and release your hold until charging starts."

type labelState struct {
	workshop     domain.WorkshopStatus
	paymentsOpen bool
	seatLive     bool
	authPending  bool
}

func statusLabel(s domain.EnrollmentStatus, st labelState) string {
	switch s {
	case domain.EnrollmentSeatReserved:
		if st.authPending {
			return "PayPal is reviewing the hold"
		}
		if st.paymentsOpen && st.seatLive {
			return "At checkout"
		}
		return "Reservation expired"
	case domain.EnrollmentAuthorized:
		if st.workshop == domain.WorkshopCancelled || !st.paymentsOpen {
			return "Not charged — releasing the hold"
		}
		return "Spot reserved — not charged yet"
	case domain.EnrollmentCapturing:
		return "Charging…"
	case domain.EnrollmentCapturePending:
		return "PayPal is verifying the payment"
	case domain.EnrollmentPaid:
		if st.workshop == domain.WorkshopCancelled {
			return "Refund on its way"
		}
		return "Paid"
	case domain.EnrollmentCaptureFailed:
		if st.paymentsOpen {
			return "Payment didn't go through — try again"
		}
		return "Not charged"
	case domain.EnrollmentVoiding:
		return "Releasing the hold…"
	case domain.EnrollmentVoided:
		return "Hold released — never charged"
	case domain.EnrollmentRefunding, domain.EnrollmentRefundPending:
		return "Refund on its way"
	case domain.EnrollmentRefunded:
		return "Refunded"
	case domain.EnrollmentRefundFailed:
		return "Refund needs a check"
	case domain.EnrollmentReleased:
		return "Reservation expired"
	}
	return "Needs a check"
}

type moneyDTO struct {
	Minor    int64  `json:"minor"`
	Currency string `json:"currency"`
	Value    string `json:"value"`
}

func money(minor int64, currency string) moneyDTO {
	return moneyDTO{Minor: minor, Currency: currency, Value: domain.Money{Minor: minor, Currency: currency}.String()}
}

type workshopDTO struct {
	ID               string    `json:"id,omitempty"`
	Title            string    `json:"title"`
	Description      string    `json:"description"`
	DurationMin      int       `json:"duration_min"`
	Timezone         string    `json:"timezone"`
	Minimum          int       `json:"minimum"`
	Capacity         int       `json:"capacity"`
	Price            moneyDTO  `json:"price"`
	Currency         string    `json:"currency"`
	PaymentWindowMin int       `json:"payment_window_min"`
	Status           string    `json:"status"`
	Resolution       string    `json:"resolution"`
	HostName         string    `json:"host_name"`
	Location         string    `json:"location"`
	Demo             bool      `json:"demo,omitempty"`
	VenueCost        *moneyDTO `json:"venue_cost,omitempty"`
	BreakEvenSeats   int       `json:"break_even_seats,omitempty"`
	PayoutEmail      string    `json:"payout_email,omitempty"`
}

func publicWorkshopView(w store.WorkshopRow) workshopDTO {
	return workshopDTO{
		Title:            w.Title,
		Description:      w.Description,
		DurationMin:      w.DurationMin,
		Timezone:         w.Timezone,
		Minimum:          w.Minimum,
		Capacity:         w.Capacity,
		Price:            money(w.PriceMinor, w.Currency),
		Currency:         w.Currency,
		PaymentWindowMin: w.PaymentWindowMin,
		Status:           string(w.Status),
		Resolution:       string(w.Resolution),
		HostName:         w.HostName,
		Location:         w.Location,
		Demo:             w.IsDemo,
	}
}

func ownerWorkshopView(w store.WorkshopRow) workshopDTO {
	d := publicWorkshopView(w)
	d.ID = w.ID
	d.PayoutEmail = w.PayoutEmail
	if w.VenueCostMinor != nil {
		cost := money(*w.VenueCostMinor, w.Currency)
		d.VenueCost = &cost
		seats, err := domain.BreakEvenSeats(
			domain.Money{Minor: *w.VenueCostMinor, Currency: w.Currency},
			domain.Money{Minor: w.PriceMinor, Currency: w.Currency},
		)
		if err == nil {
			d.BreakEvenSeats = seats
		}
	}
	return d
}

type dateDTO struct {
	ID       string    `json:"id"`
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
	Label    string    `json:"label"`
}

func dateViews(ds []store.CandidateDateRow, loc *time.Location) []dateDTO {
	out := make([]dateDTO, len(ds))
	for i, d := range ds {
		out[i] = dateDTO{ID: d.ID, StartsAt: d.StartsAt.UTC(), EndsAt: d.EndsAt.UTC(), Label: dateLabel(d, loc)}
	}
	return out
}

func dateLabel(d store.CandidateDateRow, loc *time.Location) string {
	return present.DateLabel(d.StartsAt, d.EndsAt, loc)
}

type proposalDTO struct {
	ID           string     `json:"id"`
	Version      int        `json:"version"`
	DateID       string     `json:"date_id"`
	Price        moneyDTO   `json:"price"`
	Minimum      int        `json:"minimum"`
	Capacity     int        `json:"capacity"`
	OpensAt      time.Time  `json:"opens_at"`
	ClosesAt     time.Time  `json:"closes_at"`
	StartsAt     time.Time  `json:"starts_at"`
	PaymentsOpen bool       `json:"payments_open"`
	CalendarPath string     `json:"calendar_path,omitempty"`
	ExtendedFrom *time.Time `json:"extended_from"`
	LatestCloses time.Time  `json:"latest_closes_at"`
}

func paymentsOpen(ws store.WorkshopRow, p *store.ProposalRow, now time.Time) bool {
	return p != nil && domain.PaymentsOpen(domain.Workshop{Status: ws.Status}, domain.Proposal{ClosesAt: p.ClosesAt}, now)
}

func enrollmentLabel(en store.EnrollmentRow, ws store.WorkshopRow, p *store.ProposalRow, latest *store.AttemptRow, now time.Time) string {
	if en.NeedsAttentionAt != nil {
		return statusLabel(domain.EnrollmentNeedsAttention, labelState{})
	}
	if en.WithdrawnAt != nil {
		switch en.Status {
		case domain.EnrollmentReleased:
			return "Gave up the spot"
		case domain.EnrollmentVoiding:
			return "Gave up the spot — releasing the hold"
		case domain.EnrollmentVoided:
			return "Gave up the spot — hold released"
		}
	}
	return statusLabel(en.Status, labelState{
		workshop:     ws.Status,
		paymentsOpen: paymentsOpen(ws, p, now),
		seatLive:     !domain.SeatExpired(en.SeatExpiresAt, now),
		authPending:  authorizationPending(latest),
	})
}

func authorizationPending(a *store.AttemptRow) bool {
	return a != nil && a.AuthorizationStatus != nil && *a.AuthorizationStatus == store.AuthorizationPending
}

func ownerProposalView(p *store.ProposalRow, w store.WorkshopRow, now time.Time) *proposalDTO {
	out := proposalView(p, w, now)
	if out != nil && w.ShareToken != "" {
		out.CalendarPath = "/api/v1/w/" + w.ShareToken + "/calendar.ics"
	}
	return out
}

func proposalView(p *store.ProposalRow, w store.WorkshopRow, now time.Time) *proposalDTO {
	if p == nil {
		return nil
	}
	return &proposalDTO{
		ID:           p.ID,
		Version:      p.Version,
		DateID:       p.ChosenDateID,
		Price:        money(p.PriceMinor, p.Currency),
		Minimum:      p.Minimum,
		Capacity:     p.Capacity,
		OpensAt:      p.OpensAt.UTC(),
		ClosesAt:     p.ClosesAt.UTC(),
		StartsAt:     p.StartsAt.UTC(),
		PaymentsOpen: paymentsOpen(w, p, now),
		ExtendedFrom: extendedFrom(p),
		LatestCloses: p.ClosesAt.UTC(),
	}
}

type tallyDTO struct {
	Interested int `json:"interested"`
	Authorized int `json:"authorized"`
	Paid       int `json:"paid"`
	Charging   int `json:"charging"`
	Minimum    int `json:"minimum"`
	Capacity   int `json:"capacity"`
}

type ownerTallyDTO struct {
	tallyDTO
	Reserved   int `json:"reserved"`
	Capturing  int `json:"capturing"`
	Pending    int `json:"pending"`
	Failed     int `json:"failed"`
	SeatsTaken int `json:"seats_taken"`
}

type paymentDTO struct {
	AttemptID           string    `json:"attempt_id"`
	AttemptNo           int       `json:"attempt_no"`
	OrderID             *string   `json:"order_id"`
	AuthorizationID     *string   `json:"authorization_id"`
	CaptureID           *string   `json:"capture_id"`
	RefundID            *string   `json:"refund_id"`
	OrderStatus         *string   `json:"order_status"`
	AuthorizationStatus *string   `json:"authorization_status"`
	CaptureStatus       *string   `json:"capture_status"`
	CaptureIssue        *string   `json:"capture_issue"`
	RefundStatus        *string   `json:"refund_status"`
	Fee                 *moneyDTO `json:"fee,omitempty"`
	Net                 *moneyDTO `json:"net,omitempty"`
}

func paymentView(a *store.AttemptRow) *paymentDTO {
	if a == nil {
		return nil
	}
	return &paymentDTO{
		AttemptID:           a.ID,
		AttemptNo:           a.AttemptNo,
		OrderID:             a.OrderID,
		AuthorizationID:     a.AuthorizationID,
		CaptureID:           a.CaptureID,
		RefundID:            a.RefundID,
		OrderStatus:         a.OrderStatus,
		AuthorizationStatus: a.AuthorizationStatus,
		CaptureStatus:       a.CaptureStatus,
		CaptureIssue:        a.CaptureIssue,
		RefundStatus:        a.RefundStatus,
	}
}

func ownerPayment(d *enrollmentDTO, a *store.AttemptRow) {
	if d.Payment == nil || a == nil || a.CaptureFeeMinor == nil || a.CaptureNetMinor == nil {
		return
	}
	fee, net := money(*a.CaptureFeeMinor, a.Currency), money(*a.CaptureNetMinor, a.Currency)
	d.Payment.Fee, d.Payment.Net = &fee, &net
}

type enrollmentDTO struct {
	ID                  string      `json:"id"`
	ParticipantID       string      `json:"participant_id,omitempty"`
	DisplayName         string      `json:"display_name,omitempty"`
	Status              string      `json:"status"`
	MoneyStatus         string      `json:"money_status"`
	NeedsAttention      bool        `json:"needs_attention"`
	StatusLabel         string      `json:"status_label"`
	SeatExpiresAt       time.Time   `json:"seat_expires_at"`
	ApprovalCancelledAt *time.Time  `json:"approval_cancelled_at"`
	Payment             *paymentDTO `json:"payment"`
}

func enrollmentView(en store.EnrollmentRow, w store.WorkshopRow, p *store.ProposalRow, latest *store.AttemptRow, now time.Time) enrollmentDTO {
	var cancelled *time.Time
	if en.ApprovalCancelledAt != nil {
		at := en.ApprovalCancelledAt.UTC()
		cancelled = &at
	}
	status := en.Status
	if en.NeedsAttentionAt != nil {
		status = domain.EnrollmentNeedsAttention
	}
	return enrollmentDTO{
		ID:                  en.ID,
		Status:              string(status),
		MoneyStatus:         string(en.Status),
		NeedsAttention:      en.NeedsAttentionAt != nil,
		StatusLabel:         enrollmentLabel(en, w, p, latest, now),
		SeatExpiresAt:       en.SeatExpiresAt.UTC(),
		ApprovalCancelledAt: cancelled,
		Payment:             paymentView(latest),
	}
}

type availabilityDTO struct {
	ID                 string          `json:"id"`
	ParticipantID      string          `json:"participant_id,omitempty"`
	DisplayName        string          `json:"display_name,omitempty"`
	Message            string          `json:"message"`
	Interpretation     json.RawMessage `json:"interpretation"`
	Corrected          json.RawMessage `json:"corrected"`
	NeedsClarification bool            `json:"needs_clarification"`
	CreatedAt          time.Time       `json:"created_at"`
}

func availabilityView(a store.AvailabilityRow, effective domain.Interpretation) availabilityDTO {
	return availabilityDTO{
		ID:                 a.ID,
		Message:            a.RawMessage,
		Interpretation:     a.Interpretation,
		Corrected:          a.Corrected,
		NeedsClarification: effective.NeedsClarification,
		CreatedAt:          a.CreatedAt.UTC(),
	}
}

type consentDTO struct {
	Version string `json:"version"`
	Text    string `json:"text"`
}

type recommendationDTO struct {
	DateID             string           `json:"date_id"`
	MeetsMinimum       bool             `json:"meets_minimum"`
	Tied               bool             `json:"tied"`
	PerDate            []dateSummaryDTO `json:"per_date"`
	Explanation        string           `json:"explanation"`
	ExplanationPending bool             `json:"explanation_pending"`
}

type dateSummaryDTO struct {
	DateID     string            `json:"date_id"`
	Compatible []string          `json:"compatible"`
	Blockers   map[string]string `json:"blockers"`
}

func recommendationView(rec domain.Recommendation) recommendationDTO {
	out := recommendationDTO{DateID: rec.DateID, MeetsMinimum: rec.MeetsMinimum, Tied: rec.Tied, PerDate: make([]dateSummaryDTO, len(rec.PerDate))}
	for i, d := range rec.PerDate {
		out.PerDate[i] = dateSummaryDTO{DateID: d.DateID, Compatible: d.Compatible, Blockers: d.Blockers}
	}
	return out
}

type workshopState struct {
	w            store.WorkshopRow
	loc          *time.Location
	dates        []store.CandidateDateRow
	proposal     *store.ProposalRow
	participants []store.ParticipantRow
	enrollments  []store.EnrollmentRow
	extension    *extensionOption
}

func (s *server) loadState(ctx context.Context, w store.WorkshopRow) (workshopState, error) {
	st := workshopState{w: w}
	var err error
	if st.loc, err = time.LoadLocation(w.Timezone); err != nil {
		return st, fmt.Errorf("workshop %s time zone: %w", w.ID, err)
	}
	if st.dates, err = s.Store.CandidateDates(ctx, w.ID); err != nil {
		return st, err
	}
	if st.proposal, err = s.Store.ActiveProposal(ctx, w.ID); err != nil {
		return st, err
	}
	if st.participants, err = s.Store.ParticipantsByWorkshop(ctx, w.ID); err != nil {
		return st, err
	}
	if st.enrollments, err = s.Store.EnrollmentsByWorkshop(ctx, w.ID); err != nil {
		return st, err
	}
	return st, nil
}

func (st workshopState) counts() domain.Tally {
	es := make([]domain.Enrollment, len(st.enrollments))
	for i, en := range st.enrollments {
		es[i] = en.Domain()
	}
	return domain.Counts(es)
}

func (st workshopState) tally() tallyDTO {
	t := st.counts()
	out := tallyDTO{Interested: len(st.participants), Authorized: t.Authorized, Paid: t.Paid, Charging: t.Capturing + t.Pending, Minimum: st.w.Minimum, Capacity: st.w.Capacity}
	if st.proposal != nil {
		out.Minimum, out.Capacity = st.proposal.Minimum, st.proposal.Capacity
	}
	return out
}

type groupDTO struct {
	Answered int            `json:"answered"`
	CanCome  map[string]int `json:"can_come"`
	Tied     bool           `json:"tied"`
}

func (s *server) group(ctx context.Context, st workshopState) (groupDTO, error) {
	if st.proposal != nil {
		return groupDTO{CanCome: map[string]int{}}, nil
	}
	avails, err := s.Store.AvailabilitiesByWorkshop(ctx, st.w.ID)
	if err != nil {
		return groupDTO{}, err
	}
	rec, err := st.recommend(avails)
	if err != nil {
		return groupDTO{}, err
	}
	answered := make(map[string]bool, len(avails))
	for _, a := range avails {
		answered[a.ParticipantID] = true
	}
	g := groupDTO{Answered: len(answered), Tied: rec.Tied, CanCome: make(map[string]int, len(st.dates))}
	for _, d := range st.dates {
		g.CanCome[d.ID] = 0
	}
	for _, d := range rec.PerDate {
		g.CanCome[d.DateID] = len(d.Compatible)
	}
	return g, nil
}

func (st workshopState) ownerTally() ownerTallyDTO {
	t := st.counts()
	return ownerTallyDTO{
		tallyDTO:   st.tally(),
		Reserved:   t.Reserved,
		Capturing:  t.Capturing,
		Pending:    t.Pending,
		Failed:     t.Failed,
		SeatsTaken: t.Occupying,
	}
}

func domainWorkshop(w store.WorkshopRow) domain.Workshop {
	return domain.Workshop{
		ID: w.ID, Title: w.Title, Timezone: w.Timezone,
		Minimum: w.Minimum, Capacity: w.Capacity,
		Price:         domain.Money{Minor: w.PriceMinor, Currency: w.Currency},
		Status:        w.Status,
		Resolution:    w.Resolution,
		PaymentWindow: time.Duration(w.PaymentWindowMin) * time.Minute,
	}
}

func domainDates(ds []store.CandidateDateRow) []domain.CandidateDate {
	out := make([]domain.CandidateDate, len(ds))
	for i, d := range ds {
		out[i] = domain.CandidateDate{ID: d.ID, StartsAt: d.StartsAt, EndsAt: d.EndsAt}
	}
	return out
}

func isCorrected(a store.AvailabilityRow) bool {
	return len(a.Corrected) > 0 && string(a.Corrected) != "null"
}

func inForce(a store.AvailabilityRow) (domain.Interpretation, error) {
	raw := a.Interpretation
	if isCorrected(a) {
		raw = a.Corrected
	}
	var in domain.Interpretation
	if err := json.Unmarshal(raw, &in); err != nil {
		return domain.Interpretation{}, fmt.Errorf("availability %s: %w", a.ID, err)
	}
	return in, nil
}

func (st workshopState) dateOptions() []ai.DateOption {
	out := make([]ai.DateOption, len(st.dates))
	for i, d := range st.dates {
		out[i] = ai.DateOption{ID: d.ID, Label: dateLabel(d, st.loc), StartsAt: d.StartsAt.In(st.loc)}
	}
	return out
}

func (st workshopState) recommend(avails []store.AvailabilityRow) (domain.Recommendation, error) {
	ins := make(map[string]domain.Interpretation, len(avails))
	for _, a := range avails {
		in, err := inForce(a)
		if err != nil {
			return domain.Recommendation{}, err
		}
		ins[a.ParticipantID] = in
	}
	return domain.Recommend(domainWorkshop(st.w), domainDates(st.dates), ins, st.loc), nil
}
