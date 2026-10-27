package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/store"
)

// ConsentVersion names the consent text a participant accepts before paying;
// an enrollment must quote it.
const ConsentVersion = "consent-v1"

// ConsentText is the fixed meaning of ConsentVersion. Changing a word of it
// needs a new version.
const ConsentText = "You authorize a temporary hold. We will attempt to charge it once enough people have authorized. " +
	"The workshop is confirmed when the minimum number of completed payments is reached. " +
	"If the minimum is not reached by the deadline, the workshop is cancelled and we request a refund of any charge already made. " +
	"Refunds can take time and you will be able to follow their status."

// statusLabel is the participant-facing copy for an enrollment status.
// seatHeld matters for SEAT_RESERVED only: whether the seat still counts,
// because payments are open (domain.PaymentsOpen) and the seat has not
// expired. The workshop status refines the two other cases where the next
// step depends on it: a failed capture can only be retried while the workshop
// is funding, and a payment kept in a cancelled workshop is being refunded.
func statusLabel(s domain.EnrollmentStatus, w domain.WorkshopStatus, seatHeld bool) string {
	switch s {
	case domain.EnrollmentSeatReserved:
		if seatHeld {
			return "Seat reserved"
		}
		return "Seat expired"
	case domain.EnrollmentAuthorized:
		return "Hold authorized"
	case domain.EnrollmentCapturing:
		return "Charging…"
	case domain.EnrollmentCapturePending:
		return "Payment pending verification"
	case domain.EnrollmentPaid:
		if w == domain.WorkshopCancelled {
			return "Refund in progress"
		}
		return "Seat paid"
	case domain.EnrollmentCaptureFailed:
		if w == domain.WorkshopFunding {
			return "Payment failed — you can try again"
		}
		return "Hold released"
	case domain.EnrollmentVoiding, domain.EnrollmentVoided:
		return "Hold released"
	case domain.EnrollmentRefunding, domain.EnrollmentRefundPending:
		return "Refund in progress"
	case domain.EnrollmentRefunded:
		return "Refund completed"
	case domain.EnrollmentRefundFailed:
		return "Refund needs attention"
	case domain.EnrollmentReleased:
		return "Seat expired"
	}
	return "Needs attention"
}

type moneyDTO struct {
	Minor    int64  `json:"minor"`
	Currency string `json:"currency"`
	Value    string `json:"value"`
}

func money(minor int64, currency string) moneyDTO {
	return moneyDTO{Minor: minor, Currency: currency, Value: domain.Money{Minor: minor, Currency: currency}.String()}
}

// workshopDTO is the workshop's public face; only the owner sees its ID.
type workshopDTO struct {
	ID               string   `json:"id,omitempty"`
	Title            string   `json:"title"`
	Description      string   `json:"description"`
	DurationMin      int      `json:"duration_min"`
	Timezone         string   `json:"timezone"`
	Minimum          int      `json:"minimum"`
	Capacity         int      `json:"capacity"`
	Price            moneyDTO `json:"price"`
	Currency         string   `json:"currency"`
	PaymentWindowMin int      `json:"payment_window_min"`
	Status           string   `json:"status"`
	Resolution       string   `json:"resolution"`
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
	}
}

func ownerWorkshopView(w store.WorkshopRow) workshopDTO {
	d := publicWorkshopView(w)
	d.ID = w.ID
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

// dateLabel reads a candidate date on the workshop's clock, as in
// "Sat 14 Nov 2026, 15:00–18:00 America/Lima".
func dateLabel(d store.CandidateDateRow, loc *time.Location) string {
	start, end := d.StartsAt.In(loc), d.EndsAt.In(loc)
	sy, sm, sd := start.Date()
	ey, em, ed := end.Date()
	if sy == ey && sm == em && sd == ed {
		return fmt.Sprintf("%s, %s–%s %s", start.Format("Mon 2 Jan 2006"), start.Format("15:04"), end.Format("15:04"), loc)
	}
	return fmt.Sprintf("%s – %s %s", start.Format("Mon 2 Jan 2006, 15:04"), end.Format("Mon 2 Jan 2006, 15:04"), loc)
}

type proposalDTO struct {
	ID           string    `json:"id"`
	Version      int       `json:"version"`
	DateID       string    `json:"date_id"`
	Price        moneyDTO  `json:"price"`
	Minimum      int       `json:"minimum"`
	Capacity     int       `json:"capacity"`
	OpensAt      time.Time `json:"opens_at"`
	ClosesAt     time.Time `json:"closes_at"`
	StartsAt     time.Time `json:"starts_at"`
	PaymentsOpen bool      `json:"payments_open"`
}

// paymentsOpen is domain.PaymentsOpen for the stored rows; false while there
// is no proposal.
func paymentsOpen(ws store.WorkshopRow, p *store.ProposalRow, now time.Time) bool {
	return p != nil && domain.PaymentsOpen(domain.Workshop{Status: ws.Status}, domain.Proposal{ClosesAt: p.ClosesAt}, now)
}

// enrollmentLabel is statusLabel for a stored enrollment at now.
func enrollmentLabel(en store.EnrollmentRow, ws store.WorkshopRow, p *store.ProposalRow, now time.Time) string {
	held := paymentsOpen(ws, p, now) && now.Before(en.SeatExpiresAt)
	return statusLabel(en.Status, ws.Status, held)
}

// proposalView is nil while payments have not been opened.
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
	}
}

// tallyDTO is what anyone with a link sees: how many people are interested
// (joined), how many holds are authorized and how many payments completed,
// against the minimum and the capacity.
type tallyDTO struct {
	Interested int `json:"interested"`
	Authorized int `json:"authorized"`
	Paid       int `json:"paid"`
	Minimum    int `json:"minimum"`
	Capacity   int `json:"capacity"`
}

// ownerTallyDTO adds the owner's detail: seats reserved without a hold yet,
// captures in flight, failed captures and the seats taken in all.
type ownerTallyDTO struct {
	tallyDTO
	Reserved   int `json:"reserved"`
	Capturing  int `json:"capturing"`
	Pending    int `json:"pending"`
	Failed     int `json:"failed"`
	SeatsTaken int `json:"seats_taken"`
}

type paymentDTO struct {
	AttemptID           string  `json:"attempt_id"`
	AttemptNo           int     `json:"attempt_no"`
	OrderID             *string `json:"order_id"`
	AuthorizationID     *string `json:"authorization_id"`
	CaptureID           *string `json:"capture_id"`
	RefundID            *string `json:"refund_id"`
	OrderStatus         *string `json:"order_status"`
	AuthorizationStatus *string `json:"authorization_status"`
	CaptureStatus       *string `json:"capture_status"`
	CaptureIssue        *string `json:"capture_issue"`
	RefundStatus        *string `json:"refund_status"`
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

type enrollmentDTO struct {
	ID            string      `json:"id"`
	ParticipantID string      `json:"participant_id,omitempty"`
	DisplayName   string      `json:"display_name,omitempty"`
	Status        string      `json:"status"`
	StatusLabel   string      `json:"status_label"`
	SeatExpiresAt time.Time   `json:"seat_expires_at"`
	Payment       *paymentDTO `json:"payment"`
}

func enrollmentView(en store.EnrollmentRow, w store.WorkshopRow, p *store.ProposalRow, latest *store.AttemptRow, now time.Time) enrollmentDTO {
	return enrollmentDTO{
		ID:            en.ID,
		Status:        string(en.Status),
		StatusLabel:   enrollmentLabel(en, w, p, now),
		SeatExpiresAt: en.SeatExpiresAt.UTC(),
		Payment:       paymentView(latest),
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

// availabilityView shows the raw message, the interpreter's reading and the
// participant's correction; needs_clarification follows the reading in force.
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
	DateID       string           `json:"date_id"`
	MeetsMinimum bool             `json:"meets_minimum"`
	PerDate      []dateSummaryDTO `json:"per_date"`
	Explanation  string           `json:"explanation"`
}

type dateSummaryDTO struct {
	DateID     string            `json:"date_id"`
	Compatible []string          `json:"compatible"`
	Blockers   map[string]string `json:"blockers"`
}

func recommendationView(rec domain.Recommendation) recommendationDTO {
	out := recommendationDTO{DateID: rec.DateID, MeetsMinimum: rec.MeetsMinimum, PerDate: make([]dateSummaryDTO, len(rec.PerDate))}
	for i, d := range rec.PerDate {
		out.PerDate[i] = dateSummaryDTO{DateID: d.DateID, Compatible: d.Compatible, Blockers: d.Blockers}
	}
	return out
}

// workshopState is what every view of a workshop starts from.
type workshopState struct {
	w            store.WorkshopRow
	loc          *time.Location
	dates        []store.CandidateDateRow
	proposal     *store.ProposalRow
	participants []store.ParticipantRow
	enrollments  []store.EnrollmentRow
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
		es[i] = domain.Enrollment{ID: en.ID, ParticipantID: en.ParticipantID, Status: en.Status, SeatExpiresAt: en.SeatExpiresAt}
	}
	return domain.Counts(es)
}

// tally counts against the proposal's terms once payments are open; they
// are the workshop's own, frozen.
func (st workshopState) tally() tallyDTO {
	t := st.counts()
	out := tallyDTO{Interested: len(st.participants), Authorized: t.Authorized, Paid: t.Paid, Minimum: st.w.Minimum, Capacity: st.w.Capacity}
	if st.proposal != nil {
		out.Minimum, out.Capacity = st.proposal.Minimum, st.proposal.Capacity
	}
	return out
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

// domainWorkshop is the workshop as the domain rules see it.
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

// inForce is the reading of an availability the server decides with: the
// participant's correction when there is one, the interpreter's otherwise.
func inForce(a store.AvailabilityRow) (domain.Interpretation, error) {
	raw := a.Interpretation
	if len(a.Corrected) > 0 && string(a.Corrected) != "null" {
		raw = a.Corrected
	}
	var in domain.Interpretation
	if err := json.Unmarshal(raw, &in); err != nil {
		return domain.Interpretation{}, fmt.Errorf("availability %s: %w", a.ID, err)
	}
	return in, nil
}

// recommend runs domain.Recommend over the latest availability of each
// participant, corrected or as interpreted.
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
