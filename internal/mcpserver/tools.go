package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aalvaropc/commitpool/internal/inproc"
)

func (s *Server) addTools() {
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true}
	mcp.AddTool(s.srv, &mcp.Tool{Name: "create_workshop", Annotations: &mcp.ToolAnnotations{},
		Description: "Create a paid workshop with one or more candidate dates, the organizer's name and the place. An optional studio cost stays private and lets get_status say how many paid seats cover it before fees. Returns the private owner link (keep it secret: it controls the workshop) and the share link to send to participants."},
		logged(s, "create_workshop", s.createWorkshop))
	mcp.AddTool(s.srv, &mcp.Tool{Name: "get_workshop", Annotations: readOnly,
		Description: "Read a workshop from its share link: terms, candidate dates, whether payments are open and how many committed."},
		logged(s, "get_workshop", s.getWorkshop))
	mcp.AddTool(s.srv, &mcp.Tool{Name: "join_workshop", Annotations: &mcp.ToolAnnotations{},
		Description: "Join a workshop for a person, from its share link. Returns that person's private participant link."},
		logged(s, "join_workshop", s.joinWorkshop))
	mcp.AddTool(s.srv, &mcp.Tool{Name: "submit_availability", Annotations: &mcp.ToolAnnotations{},
		Description: "Send what the person says about when they can come, in their own words. Returns how CommitPool read it per candidate date; show that reading to the person and have them confirm it."},
		logged(s, "submit_availability", s.submitAvailability))
	mcp.AddTool(s.srv, &mcp.Tool{Name: "get_my_status", Annotations: readOnly,
		Description: "Read a participant's seat: its status, the payment terms once payments are open, and the consent text the person must read before committing."},
		logged(s, "get_my_status", s.getMyStatus))
	mcp.AddTool(s.srv, &mcp.Tool{Name: "get_checkout_link", Annotations: &mcp.ToolAnnotations{},
		Description: "After the person has read and accepted the consent text, get their PayPal approval link. Only the person's approval at PayPal places a hold; nothing is charged unless the group reaches its minimum."},
		logged(s, "get_checkout_link", s.getCheckoutLink))
	mcp.AddTool(s.srv, &mcp.Tool{Name: "get_status", Annotations: readOnly,
		Description: "Read a workshop from its owner link: the recommended date and why, who can come, who has committed or paid, what would happen if payments closed now, and who the organizer could nudge, each with a message Claude drafted for the organizer to send. Nothing is sent: show the messages to the organizer and let them decide."},
		logged(s, "get_status", s.getStatus))
	additive := false
	mcp.AddTool(s.srv, &mcp.Tool{Name: "preview_open_payments", Annotations: &mcp.ToolAnnotations{DestructiveHint: &additive, IdempotentHint: true},
		Description: "Prepare opening payments on a candidate date: returns the exact terms that will be locked (date, price, minimum, capacity, payment window) and an approve_url for the organizer. Nothing about the workshop changes. Show the terms to the organizer and ask them to open the approve_url and tap Approve on their own page; the approval lasts thirty minutes."},
		logged(s, "preview_open_payments", s.previewOpenPayments))
	mcp.AddTool(s.srv, &mcp.Tool{Name: "confirm_open_payments", Annotations: &mcp.ToolAnnotations{},
		Description: "Open payments with the approval_id from preview_open_payments. Works only after the organizer tapped Approve on the approve_url page; refused while they have not, after the approval expired or was used, or when any term changed since the preview."},
		logged(s, "confirm_open_payments", s.confirmOpenPayments))
}

type money struct {
	Minor    int64  `json:"minor"`
	Currency string `json:"currency"`
	Value    string `json:"value"`
}

func (m money) String() string { return m.Value + " " + m.Currency }

type workshopJSON struct {
	Title            string `json:"title"`
	Description      string `json:"description"`
	DurationMin      int    `json:"duration_min"`
	Timezone         string `json:"timezone"`
	Minimum          int    `json:"minimum"`
	Capacity         int    `json:"capacity"`
	Price            money  `json:"price"`
	PaymentWindowMin int    `json:"payment_window_min"`
	Status           string `json:"status"`
	Resolution       string `json:"resolution"`
	HostName         string `json:"host_name"`
	Location         string `json:"location"`
	VenueCost        *money `json:"venue_cost"`
	BreakEvenSeats   int    `json:"break_even_seats"`
}

type dateJSON struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type proposalJSON struct {
	ID           string    `json:"id"`
	DateID       string    `json:"date_id"`
	Price        money     `json:"price"`
	Minimum      int       `json:"minimum"`
	Capacity     int       `json:"capacity"`
	ClosesAt     time.Time `json:"closes_at"`
	PaymentsOpen bool      `json:"payments_open"`
}

type tallyJSON struct {
	Interested int `json:"interested"`
	Authorized int `json:"authorized"`
	Paid       int `json:"paid"`
	Minimum    int `json:"minimum"`
	Capacity   int `json:"capacity"`
}

type enrollmentJSON struct {
	DisplayName string `json:"display_name"`
	Status      string `json:"status"`
	StatusLabel string `json:"status_label"`
}

type availabilityJSON struct {
	DisplayName        string          `json:"display_name"`
	Interpretation     json.RawMessage `json:"interpretation"`
	Corrected          json.RawMessage `json:"corrected"`
	NeedsClarification bool            `json:"needs_clarification"`
}

type ownerView struct {
	ShareURL       string             `json:"share_url"`
	Workshop       workshopJSON       `json:"workshop"`
	Dates          []dateJSON         `json:"dates"`
	Proposal       *proposalJSON      `json:"proposal"`
	Tally          tallyJSON          `json:"tally"`
	Availabilities []availabilityJSON `json:"availabilities"`
	Enrollments    []enrollmentJSON   `json:"enrollments"`
	Recommendation struct {
		DateID       string `json:"date_id"`
		MeetsMinimum bool   `json:"meets_minimum"`
		PerDate      []struct {
			DateID     string            `json:"date_id"`
			Compatible []string          `json:"compatible"`
			Blockers   map[string]string `json:"blockers"`
		} `json:"per_date"`
		Explanation string `json:"explanation"`
	} `json:"recommendation"`
	Suggestions []Suggestion `json:"suggestions"`
}

type Suggestion struct {
	Kind    string `json:"kind"`
	Name    string `json:"name,omitempty"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
	Pending bool   `json:"pending,omitempty"`
}

type participantView struct {
	Participant struct {
		DisplayName string `json:"display_name"`
	} `json:"participant"`
	Workshop   workshopJSON    `json:"workshop"`
	Dates      []dateJSON      `json:"dates"`
	Proposal   *proposalJSON   `json:"proposal"`
	Enrollment *enrollmentJSON `json:"enrollment"`
	Consent    struct {
		Version string `json:"version"`
		Text    string `json:"text"`
	} `json:"consent"`
}

type interpretation struct {
	PerDate []struct {
		CandidateDateID string `json:"candidate_date_id"`
		Verdict         string `json:"verdict"`
		Condition       struct {
			Type string `json:"type"`
			Time string `json:"time"`
		} `json:"condition"`
		Evidence string `json:"evidence"`
	} `json:"per_date"`
	Ambiguities []struct {
		Question string `json:"question"`
	} `json:"ambiguities"`
	NeedsClarification bool `json:"needs_clarification"`
}

func labelOf(dates []dateJSON, id string) string {
	for _, d := range dates {
		if d.ID == id {
			return d.Label
		}
	}
	return id
}

type CreateWorkshopIn struct {
	Title            string   `json:"title" jsonschema:"workshop title, at most 120 characters"`
	Description      string   `json:"description,omitempty" jsonschema:"what participants will do and bring"`
	DurationMin      int      `json:"duration_min" jsonschema:"length of the workshop in minutes"`
	Timezone         string   `json:"timezone" jsonschema:"IANA time zone of the venue, such as America/New_York"`
	Minimum          int      `json:"minimum" jsonschema:"completed payments needed for the workshop to happen"`
	Capacity         int      `json:"capacity" jsonschema:"most seats"`
	Price            string   `json:"price" jsonschema:"price per seat with two decimals, such as 10.00"`
	Currency         string   `json:"currency" jsonschema:"currency code; only USD is supported"`
	PaymentWindowMin int      `json:"payment_window_min,omitempty" jsonschema:"minutes payments stay open once opened, at most 4320 (72 hours); default 1440"`
	DateStarts       []string `json:"date_starts" jsonschema:"start of each candidate date in RFC 3339 with an offset, such as 2026-11-14T10:00:00-05:00"`
	HostName         string   `json:"host_name,omitempty" jsonschema:"the organizer's name as participants will see it"`
	Location         string   `json:"location,omitempty" jsonschema:"where the workshop takes place, such as a studio and neighbourhood"`
	VenueCost        string   `json:"venue_cost,omitempty" jsonschema:"what the room or studio costs the organizer, in the price's currency, such as 120.00; private to the organizer"`
}

type CreateWorkshopOut struct {
	OwnerLink string     `json:"owner_link"`
	ShareLink string     `json:"share_link"`
	Dates     []dateJSON `json:"dates"`
	Next      string     `json:"next"`
}

func (s *Server) createWorkshop(ctx context.Context, _ *mcp.CallToolRequest, in CreateWorkshopIn) (*mcp.CallToolResult, CreateWorkshopOut, error) {
	type date struct {
		StartsAt time.Time `json:"starts_at"`
		EndsAt   time.Time `json:"ends_at"`
	}
	dates := make([]date, 0, len(in.DateStarts))
	for _, raw := range in.DateStarts {
		start, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
		if err != nil {
			return nil, CreateWorkshopOut{}, fmt.Errorf("date %q is not RFC 3339 with an offset, such as 2026-11-14T10:00:00-05:00", raw)
		}
		dates = append(dates, date{StartsAt: start, EndsAt: start.Add(time.Duration(in.DurationMin) * time.Minute)})
	}
	body := map[string]any{
		"title": in.Title, "description": in.Description, "duration_min": in.DurationMin, "timezone": in.Timezone,
		"minimum": in.Minimum, "capacity": in.Capacity, "price": in.Price, "currency": in.Currency, "dates": dates,
		"host_name": in.HostName, "location": in.Location, "venue_cost": in.VenueCost,
	}
	if in.PaymentWindowMin > 0 {
		body["payment_window_min"] = in.PaymentWindowMin
	}
	var created struct {
		OwnerURL string     `json:"owner_url"`
		ShareURL string     `json:"share_url"`
		Dates    []dateJSON `json:"dates"`
	}
	if err := s.call(ctx, "POST", "/api/v1/workshops", body, &created); err != nil {
		return nil, CreateWorkshopOut{}, err
	}
	return nil, CreateWorkshopOut{
		OwnerLink: created.OwnerURL, ShareLink: created.ShareURL, Dates: created.Dates,
		Next: "Give the owner link only to the organizer: it controls the workshop. Send the share link to participants; each joins and says when they can come.",
	}, nil
}

type OwnerIn struct {
	OwnerLink string `json:"owner_link" jsonschema:"the organizer's private owner link"`
}

type DateStatus struct {
	ID         string   `json:"id"`
	Label      string   `json:"label"`
	Compatible int      `json:"compatible"`
	Blockers   []string `json:"blockers,omitempty"`
}

type SeatStatus struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

type StatusOut struct {
	Title              string       `json:"title"`
	HostName           string       `json:"host_name,omitempty"`
	Location           string       `json:"location,omitempty"`
	VenueCost          string       `json:"venue_cost,omitempty"`
	BreakEvenSeats     int          `json:"break_even_seats,omitempty"`
	Status             string       `json:"status"`
	Resolution         string       `json:"resolution"`
	PaymentsOpen       bool         `json:"payments_open"`
	RecommendedDateID  string       `json:"recommended_date_id,omitempty"`
	RecommendedDate    string       `json:"recommended_date,omitempty"`
	MeetsMinimum       bool         `json:"meets_minimum"`
	Explanation        string       `json:"explanation,omitempty"`
	Dates              []DateStatus `json:"dates"`
	NeedsClarification []string     `json:"needs_clarification,omitempty"`
	Seats              []SeatStatus `json:"seats"`
	Interested         int          `json:"interested"`
	Authorized         int          `json:"authorized"`
	Paid               int          `json:"paid"`
	Minimum            int          `json:"minimum"`
	Capacity           int          `json:"capacity"`
	PaymentsCloseAt    *time.Time   `json:"payments_close_at,omitempty"`
	IfClosedNow        string       `json:"if_closed_now"`
	Suggestions        []Suggestion `json:"suggestions,omitempty"`
}

func (s *Server) getStatus(ctx context.Context, _ *mcp.CallToolRequest, in OwnerIn) (*mcp.CallToolResult, StatusOut, error) {
	tok, err := token(in.OwnerLink, "owner")
	if err != nil {
		return nil, StatusOut{}, err
	}
	v, err := s.owner(ctx, tok)
	if err != nil {
		return nil, StatusOut{}, err
	}
	return nil, statusOf(v), nil
}

func (s *Server) owner(ctx context.Context, tok string) (ownerView, error) {
	var v ownerView
	return v, s.call(ctx, "GET", "/api/v1/owner/"+tok, nil, &v)
}

func statusOf(v ownerView) StatusOut {
	out := StatusOut{
		Title: v.Workshop.Title, Status: v.Workshop.Status, Resolution: v.Workshop.Resolution,
		HostName: v.Workshop.HostName, Location: v.Workshop.Location, BreakEvenSeats: v.Workshop.BreakEvenSeats,
		RecommendedDateID: v.Recommendation.DateID, MeetsMinimum: v.Recommendation.MeetsMinimum,
		Explanation: v.Recommendation.Explanation,
		Interested:  v.Tally.Interested, Authorized: v.Tally.Authorized, Paid: v.Tally.Paid,
		Minimum: v.Workshop.Minimum, Capacity: v.Workshop.Capacity, Seats: []SeatStatus{},
		Suggestions: v.Suggestions,
	}
	if v.Workshop.VenueCost != nil {
		out.VenueCost = v.Workshop.VenueCost.String()
	}
	if v.Recommendation.DateID != "" {
		out.RecommendedDate = labelOf(v.Dates, v.Recommendation.DateID)
	}
	for _, d := range v.Dates {
		ds := DateStatus{ID: d.ID, Label: d.Label}
		for _, p := range v.Recommendation.PerDate {
			if p.DateID == d.ID {
				ds.Compatible = len(p.Compatible)
				for _, why := range p.Blockers {
					ds.Blockers = append(ds.Blockers, why)
				}
			}
		}
		out.Dates = append(out.Dates, ds)
	}
	for _, a := range v.Availabilities {
		if a.NeedsClarification {
			out.NeedsClarification = append(out.NeedsClarification, a.DisplayName)
		}
	}
	for _, e := range v.Enrollments {
		out.Seats = append(out.Seats, SeatStatus{Name: e.DisplayName, Status: e.StatusLabel})
	}
	if p := v.Proposal; p != nil {
		out.PaymentsOpen = p.PaymentsOpen
		closes := p.ClosesAt
		out.PaymentsCloseAt = &closes
		out.Minimum, out.Capacity = p.Minimum, p.Capacity
	}
	out.IfClosedNow = ifClosedNow(v)
	return out
}

func ifClosedNow(v ownerView) string {
	switch {
	case v.Proposal == nil:
		return "Reservations are not open yet: nothing is held or charged."
	case v.Workshop.Status == "CANCELLED":
		return cancelledOutcome(v.Workshop.Resolution)
	case v.Tally.Paid >= v.Proposal.Minimum:
		return fmt.Sprintf("The workshop is on: %d payments reach the minimum of %d. Holds not charged by then are released.", v.Tally.Paid, v.Proposal.Minimum)
	default:
		return fmt.Sprintf("The workshop would be cancelled: %d payments are short of the minimum of %d, so %d holds would be released and %d payments refunded.",
			v.Tally.Paid, v.Proposal.Minimum, v.Tally.Authorized, v.Tally.Paid)
	}
}

func cancelledOutcome(resolution string) string {
	switch resolution {
	case "SETTLED":
		return "The workshop is cancelled: every hold was released and every payment refunded."
	case "NEEDS_ATTENTION":
		return "The workshop is cancelled, and a release or a refund needs the organizer's check in the web app."
	}
	return "The workshop is cancelled: holds are being released and payments refunded."
}

type PreviewIn struct {
	OwnerLink string `json:"owner_link" jsonschema:"the organizer's private owner link"`
	DateID    string `json:"date_id" jsonschema:"id of the candidate date to open payments on (get_status lists them)"`
}

type PreviewOut struct {
	DateID           string    `json:"date_id"`
	Date             string    `json:"date"`
	Price            string    `json:"price"`
	Minimum          int       `json:"minimum"`
	Capacity         int       `json:"capacity"`
	PaymentWindowMin int       `json:"payment_window_min"`
	Compatible       int       `json:"compatible"`
	ApprovalID       string    `json:"approval_id"`
	ApproveURL       string    `json:"approve_url"`
	ExpiresAt        time.Time `json:"expires_at"`
	Next             string    `json:"next"`
}

type approvalJSON struct {
	Approval struct {
		ID               string    `json:"id"`
		State            string    `json:"state"`
		DateID           string    `json:"date_id"`
		DateLabel        string    `json:"date_label"`
		Price            money     `json:"price"`
		Minimum          int       `json:"minimum"`
		Capacity         int       `json:"capacity"`
		PaymentWindowMin int       `json:"payment_window_min"`
		ExpiresAt        time.Time `json:"expires_at"`
	} `json:"approval"`
	ApproveURL string `json:"approve_url"`
}

func (s *Server) previewOpenPayments(ctx context.Context, _ *mcp.CallToolRequest, in PreviewIn) (*mcp.CallToolResult, PreviewOut, error) {
	tok, err := token(in.OwnerLink, "owner")
	if err != nil {
		return nil, PreviewOut{}, err
	}
	v, err := s.owner(ctx, tok)
	if err != nil {
		return nil, PreviewOut{}, err
	}
	if v.Proposal != nil {
		return nil, PreviewOut{}, errors.New("payments are already open for this workshop; get_status shows how they are going")
	}
	known := false
	for _, d := range v.Dates {
		known = known || d.ID == in.DateID
	}
	if !known {
		return nil, PreviewOut{}, fmt.Errorf("date %q is not one of this workshop's candidate dates; get_status lists them", in.DateID)
	}
	var a approvalJSON
	if err := s.call(ctx, "POST", "/api/v1/owner/"+tok+"/approvals", map[string]string{"date_id": in.DateID}, &a); err != nil {
		return nil, PreviewOut{}, err
	}
	out := PreviewOut{
		DateID: a.Approval.DateID, Date: a.Approval.DateLabel, Price: a.Approval.Price.String(), Minimum: a.Approval.Minimum,
		Capacity: a.Approval.Capacity, PaymentWindowMin: a.Approval.PaymentWindowMin,
		ApprovalID: a.Approval.ID, ApproveURL: a.ApproveURL, ExpiresAt: a.Approval.ExpiresAt,
	}
	for _, p := range v.Recommendation.PerDate {
		if p.DateID == in.DateID {
			out.Compatible = len(p.Compatible)
		}
	}
	out.Next = fmt.Sprintf("Show these terms to the organizer and ask them to open this link and tap Approve; then call confirm_open_payments. "+
		"The link is %s and it works until %s. Once reservations open, the date, the price of %s, the minimum of %d and the capacity of %d are locked and every participant who reserves places a PayPal hold; nothing is charged until enough people reserve. "+
		"Never open the link or tap Approve yourself: the approval must be the organizer's.",
		out.ApproveURL, out.ExpiresAt.Format(time.RFC3339), out.Price, out.Minimum, out.Capacity)
	return nil, out, nil
}

type ConfirmIn struct {
	OwnerLink  string `json:"owner_link" jsonschema:"the organizer's private owner link"`
	ApprovalID string `json:"approval_id" jsonschema:"the approval_id preview_open_payments returned"`
}

var approvalIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

var confirmRefusals = map[string]string{
	"approval_pending": "The organizer has not approved yet — ask them to open the approve link from preview_open_payments and tap Approve, then call confirm_open_payments again. Do not approve it yourself.",
	"approval_expired": "This approval expired before reservations opened; call preview_open_payments again and send the organizer the new approve link.",
	"approval_used":    "This approval was already used to open reservations; get_status shows how they are going.",
	"terms_changed":    "The workshop's terms changed since the preview, so the organizer's approval no longer applies; call preview_open_payments again and send the organizer the new approve link.",
	"approval_closed":  "Reservations are already open or the workshop was cancelled; get_status shows where it stands.",
	"not_found":        "That approval_id does not belong to this workshop; call preview_open_payments and use the approval_id it returns.",
}

func (s *Server) confirmOpenPayments(ctx context.Context, _ *mcp.CallToolRequest, in ConfirmIn) (*mcp.CallToolResult, StatusOut, error) {
	tok, err := token(in.OwnerLink, "owner")
	if err != nil {
		return nil, StatusOut{}, err
	}
	id := strings.TrimSpace(in.ApprovalID)
	if !approvalIDPattern.MatchString(id) {
		return nil, StatusOut{}, errors.New(confirmRefusals["not_found"])
	}
	if err := s.call(ctx, "POST", "/api/v1/owner/"+tok+"/approvals/"+id+"/open", nil, nil); err != nil {
		var apiErr *inproc.Error
		if errors.As(err, &apiErr) && confirmRefusals[apiErr.Code] != "" {
			return nil, StatusOut{}, errors.New(confirmRefusals[apiErr.Code])
		}
		return nil, StatusOut{}, err
	}
	v, err := s.owner(ctx, tok)
	if err != nil {
		return nil, StatusOut{}, err
	}
	return nil, statusOf(v), nil
}

type ShareIn struct {
	ShareLink string `json:"share_link" jsonschema:"the workshop's share link"`
}

type WorkshopOut struct {
	Title        string     `json:"title"`
	Description  string     `json:"description"`
	HostName     string     `json:"host_name,omitempty"`
	Location     string     `json:"location,omitempty"`
	Price        string     `json:"price"`
	Minimum      int        `json:"minimum"`
	Capacity     int        `json:"capacity"`
	Dates        []dateJSON `json:"dates"`
	PaymentsOpen bool       `json:"payments_open"`
	ProposedDate string     `json:"proposed_date,omitempty"`
	Committed    int        `json:"committed"`
	Paid         int        `json:"paid"`
}

func (s *Server) getWorkshop(ctx context.Context, _ *mcp.CallToolRequest, in ShareIn) (*mcp.CallToolResult, WorkshopOut, error) {
	tok, err := token(in.ShareLink, "share")
	if err != nil {
		return nil, WorkshopOut{}, err
	}
	var v struct {
		Workshop workshopJSON  `json:"workshop"`
		Dates    []dateJSON    `json:"dates"`
		Proposal *proposalJSON `json:"proposal"`
		Tally    tallyJSON     `json:"tally"`
	}
	if err := s.call(ctx, "GET", "/api/v1/w/"+tok, nil, &v); err != nil {
		return nil, WorkshopOut{}, err
	}
	out := WorkshopOut{
		Title: v.Workshop.Title, Description: v.Workshop.Description, Price: v.Workshop.Price.String(),
		HostName: v.Workshop.HostName, Location: v.Workshop.Location,
		Minimum: v.Workshop.Minimum, Capacity: v.Workshop.Capacity, Dates: v.Dates,
		Committed: v.Tally.Authorized + v.Tally.Paid, Paid: v.Tally.Paid,
	}
	if v.Proposal != nil {
		out.PaymentsOpen, out.ProposedDate = v.Proposal.PaymentsOpen, labelOf(v.Dates, v.Proposal.DateID)
	}
	return nil, out, nil
}

type JoinIn struct {
	ShareLink   string `json:"share_link" jsonschema:"the workshop's share link"`
	DisplayName string `json:"display_name" jsonschema:"the name the person wants the organizer to see"`
	Email       string `json:"email,omitempty" jsonschema:"optional: an address for news about the seat; the welcome message carries the private link"`
}

type JoinOut struct {
	ParticipantLink string `json:"participant_link"`
	Next            string `json:"next"`
}

func (s *Server) joinWorkshop(ctx context.Context, _ *mcp.CallToolRequest, in JoinIn) (*mcp.CallToolResult, JoinOut, error) {
	tok, err := token(in.ShareLink, "share")
	if err != nil {
		return nil, JoinOut{}, err
	}
	var joined struct {
		ParticipantURL string `json:"participant_url"`
	}
	if err := s.call(ctx, "POST", "/api/v1/w/"+tok+"/participants", map[string]string{"display_name": in.DisplayName, "email": in.Email}, &joined); err != nil {
		return nil, JoinOut{}, err
	}
	return nil, JoinOut{
		ParticipantLink: joined.ParticipantURL,
		Next:            "This link is the person's private seat: give it to them and keep it out of shared chats. Next, ask them when they can come and call submit_availability with their words.",
	}, nil
}

type AvailabilityIn struct {
	ParticipantLink string `json:"participant_link" jsonschema:"the person's private participant link"`
	Message         string `json:"message" jsonschema:"what the person said about when they can come, in their own words"`
}

type DateReading struct {
	DateID    string `json:"date_id"`
	Date      string `json:"date"`
	Verdict   string `json:"verdict"`
	Condition string `json:"condition,omitempty"`
	Evidence  string `json:"evidence,omitempty"`
}

type ReadingOut struct {
	Reading            []DateReading `json:"reading"`
	NeedsClarification bool          `json:"needs_clarification"`
	Questions          []string      `json:"questions,omitempty"`
	Next               string        `json:"next"`
}

func (s *Server) submitAvailability(ctx context.Context, _ *mcp.CallToolRequest, in AvailabilityIn) (*mcp.CallToolResult, ReadingOut, error) {
	tok, err := token(in.ParticipantLink, "participant")
	if err != nil {
		return nil, ReadingOut{}, err
	}
	var saved struct {
		Availability availabilityJSON `json:"availability"`
	}
	if err := s.call(ctx, "POST", "/api/v1/p/"+tok+"/availability", map[string]string{"message": in.Message}, &saved); err != nil {
		return nil, ReadingOut{}, err
	}
	var v participantView
	if err := s.call(ctx, "GET", "/api/v1/p/"+tok, nil, &v); err != nil {
		return nil, ReadingOut{}, err
	}
	var read interpretation
	if err := json.Unmarshal(saved.Availability.Interpretation, &read); err != nil {
		return nil, ReadingOut{}, fmt.Errorf("read the interpretation: %w", err)
	}
	out := ReadingOut{NeedsClarification: saved.Availability.NeedsClarification, Reading: []DateReading{}}
	for _, d := range read.PerDate {
		r := DateReading{DateID: d.CandidateDateID, Date: labelOf(v.Dates, d.CandidateDateID), Verdict: d.Verdict, Evidence: d.Evidence}
		if d.Condition.Type != "" && d.Condition.Type != "none" {
			r.Condition = strings.TrimSpace(strings.ReplaceAll(d.Condition.Type, "_", " ") + " " + d.Condition.Time)
		}
		out.Reading = append(out.Reading, r)
	}
	for _, a := range read.Ambiguities {
		out.Questions = append(out.Questions, a.Question)
	}
	out.Next = "Show this reading to the person and ask them to confirm it. If it is wrong, ask them to say it differently and call submit_availability again."
	if out.NeedsClarification {
		out.Next = "CommitPool could not read this for certain and counts the person as not available until it can: ask them the questions and call submit_availability again with their answer, then have them confirm the reading."
	}
	return nil, out, nil
}

type ParticipantIn struct {
	ParticipantLink string `json:"participant_link" jsonschema:"the person's private participant link"`
}

type MyStatusOut struct {
	Name           string `json:"name"`
	Workshop       string `json:"workshop"`
	Seat           string `json:"seat"`
	PaymentsOpen   bool   `json:"payments_open"`
	Date           string `json:"date,omitempty"`
	Price          string `json:"price,omitempty"`
	ConsentVersion string `json:"consent_version"`
	ConsentText    string `json:"consent_text"`
	Next           string `json:"next"`
}

func (s *Server) getMyStatus(ctx context.Context, _ *mcp.CallToolRequest, in ParticipantIn) (*mcp.CallToolResult, MyStatusOut, error) {
	tok, err := token(in.ParticipantLink, "participant")
	if err != nil {
		return nil, MyStatusOut{}, err
	}
	var v participantView
	if err := s.call(ctx, "GET", "/api/v1/p/"+tok, nil, &v); err != nil {
		return nil, MyStatusOut{}, err
	}
	out := MyStatusOut{
		Name: v.Participant.DisplayName, Workshop: v.Workshop.Title, Seat: "Not committed",
		ConsentVersion: v.Consent.Version, ConsentText: v.Consent.Text,
	}
	if v.Enrollment != nil {
		out.Seat = v.Enrollment.StatusLabel
	}
	if p := v.Proposal; p != nil {
		out.PaymentsOpen, out.Date, out.Price = p.PaymentsOpen, labelOf(v.Dates, p.DateID), p.Price.String()
	}
	switch {
	case out.PaymentsOpen && v.Enrollment == nil:
		out.Next = "Reservations are open. Show the person the consent text; if they accept it, call get_checkout_link with consent_version and human_accepted_terms true."
	case out.PaymentsOpen:
		out.Next = "The person's seat status is above; nothing to do unless it says the payment failed and they want to try again."
	default:
		out.Next = "Reservations are not open yet. The organizer opens them once a date works for enough people."
	}
	return nil, out, nil
}

type CheckoutIn struct {
	ParticipantLink    string `json:"participant_link" jsonschema:"the person's private participant link"`
	ConsentVersion     string `json:"consent_version" jsonschema:"the consent version the person read (get_my_status gives it)"`
	HumanAcceptedTerms bool   `json:"human_accepted_terms" jsonschema:"true only when the person read the consent text and said yes"`
}

type CheckoutOut struct {
	ApproveURL string `json:"approve_url,omitempty"`
	Seat       string `json:"seat"`
	Next       string `json:"next"`
}

func (s *Server) getCheckoutLink(ctx context.Context, _ *mcp.CallToolRequest, in CheckoutIn) (*mcp.CallToolResult, CheckoutOut, error) {
	tok, err := token(in.ParticipantLink, "participant")
	if err != nil {
		return nil, CheckoutOut{}, err
	}
	if !in.HumanAcceptedTerms {
		return nil, CheckoutOut{}, errors.New("the person must read the consent text (get_my_status) and accept it before a checkout link is created; call again with human_accepted_terms true only after they say yes")
	}
	var v participantView
	if err := s.call(ctx, "GET", "/api/v1/p/"+tok, nil, &v); err != nil {
		return nil, CheckoutOut{}, err
	}
	if v.Proposal == nil || !v.Proposal.PaymentsOpen {
		return nil, CheckoutOut{}, errors.New("payments are not open for this workshop")
	}
	if in.ConsentVersion != v.Consent.Version {
		return nil, CheckoutOut{}, fmt.Errorf("the consent the person accepted is not the current one (%s); show them the current consent text from get_my_status and ask again", v.Consent.Version)
	}
	var enrolled struct {
		ApproveURL  string `json:"approve_url"`
		StatusLabel string `json:"status_label"`
	}
	body := map[string]string{"consent_version": in.ConsentVersion, "proposal_id": v.Proposal.ID}
	if err := s.call(ctx, "POST", "/api/v1/p/"+tok+"/enrollment", body, &enrolled); err != nil {
		return nil, CheckoutOut{}, err
	}
	out := CheckoutOut{ApproveURL: enrolled.ApproveURL, Seat: enrolled.StatusLabel}
	if out.ApproveURL == "" {
		out.Next = "There is no checkout to open: the seat status above says where the person stands."
		return nil, out, nil
	}
	out.Next = fmt.Sprintf("Give this PayPal link to the person. Only their approval at PayPal places a hold of %s; nothing is charged unless the group reaches its minimum of %d completed payments, and every hold is released otherwise.",
		v.Proposal.Price.String(), v.Proposal.Minimum)
	return nil, out, nil
}
