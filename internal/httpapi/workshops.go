package httpapi

import (
	"context"
	"net/http"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/store"
)

const (
	maxTitle         = 120
	maxHostName      = 80
	maxLocation      = 160
	maxDescription   = 2000
	maxDurationMin   = 24 * 60
	maxCapacity      = 500
	maxWindowMin     = int(domain.MaxPaymentWindow / time.Minute)
	dateLengthSlack  = time.Hour
	maxDates         = 10
	maxDisplayName   = 80
	maxMessageLength = 2000
)

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
	HostName         string `json:"host_name"`
	Location         string `json:"location"`
	VenueCost        string `json:"venue_cost"`
	Demo             bool   `json:"demo"`
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
	hostName := strings.TrimSpace(req.HostName)
	if utf8.RuneCountInString(hostName) > maxHostName {
		return none, badRequest("invalid_host_name", "Your name can have at most %d characters.", maxHostName)
	}
	location := strings.TrimSpace(req.Location)
	if utf8.RuneCountInString(location) > maxLocation {
		return none, badRequest("invalid_location", "The place can have at most %d characters.", maxLocation)
	}
	if req.DurationMin < 1 || req.DurationMin > maxDurationMin {
		return none, badRequest("invalid_duration", "The duration must be between 1 and %d minutes.", maxDurationMin)
	}
	if _, err := timeZone(req.Timezone); err != nil {
		return none, err
	}
	if req.Minimum < 1 {
		return none, badRequest("invalid_minimum", "At least 1 person must be needed.")
	}
	if req.Capacity > maxCapacity {
		return none, badRequest("invalid_capacity", "A workshop can have at most %d spots.", maxCapacity)
	}
	if req.Minimum > req.Capacity {
		return none, badRequest("minimum_above_capacity", "The people needed cannot be more than the spots.")
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
	venueCost, err := optionalVenueCost(req.VenueCost, price.Currency)
	if err != nil {
		return none, err
	}
	window := store.DefaultPaymentWindowMin
	if req.PaymentWindowMin != nil {
		window = *req.PaymentWindowMin
		if window < 1 || window > maxWindowMin {
			return none, badRequest("invalid_payment_window", "Reservations can stay open between 1 and %d minutes: PayPal honours a hold for three days.", maxWindowMin)
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
		if diff := d.EndsAt.Sub(d.StartsAt) - time.Duration(req.DurationMin)*time.Minute; diff > dateLengthSlack || diff < -dateLengthSlack {
			return none, badRequest("invalid_dates", "Each candidate date must last the workshop's duration.")
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
		HostName:         hostName,
		Location:         location,
		VenueCostMinor:   venueCost,
		IsDemo:           req.Demo && s.Cfg.DemoMode,
		Dates:            dates,
	}, nil
}

func timeZone(name string) (*time.Location, error) {
	invalid := badRequest("invalid_timezone", "The time zone must be an IANA name such as America/New_York.")
	if name == "" || name == "Local" {
		return nil, invalid
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, invalid
	}
	return loc, nil
}

func optionalVenueCost(value, currency string) (*int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	cost, err := domain.ParseMoney(value, currency)
	if err != nil {
		return nil, badRequest("invalid_venue_cost", "The studio cost must be an amount with at most two decimals, such as 120 or 120.00.")
	}
	return &cost.Minor, nil
}

type publicView struct {
	Workshop workshopDTO  `json:"workshop"`
	Dates    []dateDTO    `json:"dates"`
	Proposal *proposalDTO `json:"proposal"`
	Tally    tallyDTO     `json:"tally"`
	Group    groupDTO     `json:"group"`
}

func (s *server) publicWorkshop(w http.ResponseWriter, r *http.Request) error {
	ws, err := s.sharedWorkshop(r)
	if err != nil {
		return err
	}
	st, err := s.loadState(r.Context(), ws)
	if err != nil {
		return err
	}
	g, err := s.group(r.Context(), st)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, publicView{
		Workshop: publicWorkshopView(ws),
		Dates:    dateViews(st.dates, st.loc),
		Proposal: proposalView(st.proposal, ws, s.Clock.Now()),
		Tally:    st.tally(),
		Group:    g,
	})
	return nil
}

func (s *server) accepting(ctx context.Context, ws store.WorkshopRow) (bool, error) {
	if ws.Status == domain.WorkshopCollecting {
		return true, nil
	}
	p, err := s.Store.ActiveProposal(ctx, ws.ID)
	if err != nil {
		return false, err
	}
	return paymentsOpen(ws, p, s.Clock.Now()), nil
}

func (s *server) open(ctx context.Context, ws store.WorkshopRow) error {
	ok, err := s.accepting(ctx, ws)
	if err != nil {
		return err
	}
	if !ok {
		return errWorkshopClosed
	}
	return nil
}

var errWorkshopClosed = conflict("workshop_closed", "This workshop no longer takes new participants or changes.")

func (s *server) joinWorkshop(w http.ResponseWriter, r *http.Request) error {
	ws, err := s.sharedWorkshop(r)
	if err != nil {
		return err
	}
	var req struct {
		DisplayName string `json:"display_name"`
		Email       string `json:"email"`
	}
	if err := decode(r, &req); err != nil {
		return err
	}
	name := strings.TrimSpace(req.DisplayName)
	if n := utf8.RuneCountInString(name); n == 0 || n > maxDisplayName {
		return badRequest("invalid_display_name", "The name must have between 1 and %d characters.", maxDisplayName)
	}
	email, err := optionalEmail(req.Email)
	if err != nil {
		return err
	}
	if err := s.open(r.Context(), ws); err != nil {
		return err
	}
	joined, err := s.Store.CountParticipants(r.Context(), ws.ID)
	if err != nil {
		return err
	}
	if joined >= maxParticipantsPerWorkshop {
		return conflict("too_many_participants", "This workshop already has as many participants as it can take.")
	}
	p, token, err := s.Store.CreateParticipant(r.Context(), ws.ID, name)
	if err != nil {
		return err
	}
	link := s.Cfg.BaseURL + "/p/" + token
	if email != "" {
		if err := s.Store.SetParticipantEmail(r.Context(), p.ID, email); err != nil {
			return err
		}
		if err := s.Worker.NotifyJoined(r.Context(), ws.ID, p.ID, link); err != nil {
			s.Log.Error("join: could not queue the welcome message", "workshop_id", ws.ID, "error", err)
		}
	}
	writeJSON(w, http.StatusCreated, map[string]string{"participant_url": link})
	return nil
}

func optionalEmail(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	addr, err := mail.ParseAddress(raw)
	if err != nil || addr.Address != raw || len(raw) > 254 {
		return "", badRequest("invalid_email", "The email must be one plain address, such as ana@example.com.")
	}
	return raw, nil
}
