package httpapi

import (
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/store"
)

// Limits on what a workshop and a participant may carry.
const (
	maxTitle         = 120
	maxDescription   = 2000
	maxDurationMin   = 24 * 60
	maxCapacity      = 500
	maxWindowMin     = 30 * 24 * 60
	maxDates         = 10
	maxDisplayName   = 80
	maxMessageLength = 2000
)

// supportedCurrencies are the currencies a price may be set in: ParseMoney
// reads two decimals, and the MVP charges in US dollars.
var supportedCurrencies = map[string]bool{"USD": true}

type createWorkshopRequest struct {
	Title            string `json:"title"`
	Description      string `json:"description"`
	DurationMin      int    `json:"duration_min"`
	Timezone         string `json:"timezone"`
	Minimum          int    `json:"minimum"`
	Capacity         int    `json:"capacity"`
	Price            string `json:"price"`
	Currency         string `json:"currency"`
	PaymentWindowMin *int   `json:"payment_window_min"`
	Dates            []struct {
		StartsAt time.Time `json:"starts_at"`
		EndsAt   time.Time `json:"ends_at"`
	} `json:"dates"`
}

type createWorkshopResponse struct {
	Workshop workshopDTO `json:"workshop"`
	Dates    []dateDTO   `json:"dates"`
	OwnerURL string      `json:"owner_url"`
	ShareURL string      `json:"share_url"`
}

// createWorkshop stores a workshop with its terms and candidate dates. The
// owner and share tokens appear only in this answer's URLs: the terms cannot
// be edited afterwards, and they freeze into the proposal when payments open.
func (s *server) createWorkshop(w http.ResponseWriter, r *http.Request) error {
	var req createWorkshopRequest
	if err := decode(r, &req); err != nil {
		return err
	}
	params, err := s.workshopParams(req)
	if err != nil {
		return err
	}
	ws, ownerToken, shareToken, err := s.Store.CreateWorkshop(r.Context(), params)
	if err != nil {
		return err
	}
	dates, err := s.Store.CandidateDates(r.Context(), ws.ID)
	if err != nil {
		return err
	}
	loc, err := time.LoadLocation(ws.Timezone)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, createWorkshopResponse{
		Workshop: ownerWorkshopView(ws),
		Dates:    dateViews(dates, loc),
		OwnerURL: s.Cfg.BaseURL + "/owner/" + ownerToken,
		ShareURL: s.Cfg.BaseURL + "/w/" + shareToken,
	})
	return nil
}

func (s *server) workshopParams(req createWorkshopRequest) (store.CreateWorkshopParams, error) {
	var none store.CreateWorkshopParams
	title := strings.TrimSpace(req.Title)
	if n := utf8.RuneCountInString(title); n == 0 || n > maxTitle {
		return none, badRequest("invalid_title", "The title must have between 1 and %d characters.", maxTitle)
	}
	description := strings.TrimSpace(req.Description)
	if utf8.RuneCountInString(description) > maxDescription {
		return none, badRequest("invalid_description", "The description can have at most %d characters.", maxDescription)
	}
	if req.DurationMin < 1 || req.DurationMin > maxDurationMin {
		return none, badRequest("invalid_duration", "The duration must be between 1 and %d minutes.", maxDurationMin)
	}
	if req.Timezone == "" || req.Timezone == "Local" {
		return none, badRequest("invalid_timezone", "The time zone must be an IANA name such as America/Lima.")
	}
	if _, err := time.LoadLocation(req.Timezone); err != nil {
		return none, badRequest("invalid_timezone", "The time zone must be an IANA name such as America/Lima.")
	}
	if req.Minimum < 1 {
		return none, badRequest("invalid_minimum", "The minimum must be at least 1 participant.")
	}
	if req.Capacity > maxCapacity {
		return none, badRequest("invalid_capacity", "The capacity can be at most %d participants.", maxCapacity)
	}
	if req.Minimum > req.Capacity {
		return none, badRequest("minimum_above_capacity", "The minimum cannot be greater than the capacity.")
	}
	price, err := domain.ParseMoney(req.Price, req.Currency)
	if err != nil {
		return none, badRequest("invalid_price", "The price must be an amount with at most two decimals, such as 10.00, with a three-letter currency code.")
	}
	if price.Minor <= 0 {
		return none, badRequest("invalid_price", "The price must be greater than zero.")
	}
	if !supportedCurrencies[price.Currency] {
		return none, badRequest("unsupported_currency", "Prices are in USD for now.")
	}
	window := store.DefaultPaymentWindowMin
	if req.PaymentWindowMin != nil {
		window = *req.PaymentWindowMin
		if window < 1 || window > maxWindowMin {
			return none, badRequest("invalid_payment_window", "The payment window must be between 1 and %d minutes.", maxWindowMin)
		}
	}
	if len(req.Dates) == 0 || len(req.Dates) > maxDates {
		return none, badRequest("invalid_dates", "Propose between 1 and %d candidate dates.", maxDates)
	}
	now := s.Clock.Now()
	dates := make([]struct{ StartsAt, EndsAt time.Time }, len(req.Dates))
	for i, d := range req.Dates {
		if !d.EndsAt.After(d.StartsAt) {
			return none, badRequest("invalid_dates", "Each candidate date must end after it starts.")
		}
		if !d.StartsAt.After(now) {
			return none, badRequest("invalid_dates", "Each candidate date must be in the future.")
		}
		dates[i] = struct{ StartsAt, EndsAt time.Time }{d.StartsAt.UTC(), d.EndsAt.UTC()}
	}
	return store.CreateWorkshopParams{
		Title:            title,
		Description:      description,
		DurationMin:      req.DurationMin,
		Timezone:         req.Timezone,
		Minimum:          req.Minimum,
		Capacity:         req.Capacity,
		PriceMinor:       price.Minor,
		Currency:         price.Currency,
		PaymentWindowMin: window,
		Dates:            dates,
	}, nil
}

type publicView struct {
	Workshop workshopDTO  `json:"workshop"`
	Dates    []dateDTO    `json:"dates"`
	Proposal *proposalDTO `json:"proposal"`
	Tally    tallyDTO     `json:"tally"`
}

// publicWorkshop is what the share link shows before joining: the terms, the
// dates, the proposal once payments open and the aggregate tally.
func (s *server) publicWorkshop(w http.ResponseWriter, r *http.Request) error {
	ws, err := s.sharedWorkshop(r)
	if err != nil {
		return err
	}
	st, err := s.loadState(r.Context(), ws)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, publicView{
		Workshop: publicWorkshopView(ws),
		Dates:    dateViews(st.dates, st.loc),
		Proposal: proposalView(st.proposal, ws, s.Clock.Now()),
		Tally:    st.tally(),
	})
	return nil
}

// accepting reports whether the workshop still takes participants and their
// availability: until it is confirmed or cancelled.
func accepting(ws store.WorkshopRow) bool {
	return ws.Status == domain.WorkshopCollecting || ws.Status == domain.WorkshopFunding
}

var errWorkshopClosed = conflict("workshop_closed", "This workshop is already decided and no longer takes changes.")

// joinWorkshop adds a participant through the share link and returns the
// participant's private URL, the only time its token is shown.
func (s *server) joinWorkshop(w http.ResponseWriter, r *http.Request) error {
	ws, err := s.sharedWorkshop(r)
	if err != nil {
		return err
	}
	var req struct {
		DisplayName string `json:"display_name"`
	}
	if err := decode(r, &req); err != nil {
		return err
	}
	name := strings.TrimSpace(req.DisplayName)
	if n := utf8.RuneCountInString(name); n == 0 || n > maxDisplayName {
		return badRequest("invalid_display_name", "The name must have between 1 and %d characters.", maxDisplayName)
	}
	if !accepting(ws) {
		return errWorkshopClosed
	}
	_, token, err := s.Store.CreateParticipant(r.Context(), ws.ID, name)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, map[string]string{"participant_url": s.Cfg.BaseURL + "/p/" + token})
	return nil
}
