package mcpserver

import (
	"context"
	"errors"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) addExtendTool() {
	mcp.AddTool(s.srv, &mcp.Tool{Name: "preview_extend_reservations", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
		Description: "From the owner link, while a class is short of its minimum: whether CommitPool can extend the reservation deadline, once, and to when (at most three days after reservations opened, while PayPal honours the holds, and a day before the class), with an approve_url for the organizer. Nothing changes: only the organizer extends the deadline, on their own page."},
		logged(s, "preview_extend_reservations", s.previewExtendReservations))
}

type ExtendPreviewOut struct {
	Available  bool       `json:"available"`
	Deadline   time.Time  `json:"deadline"`
	ExtendTo   *time.Time `json:"extend_to,omitempty"`
	Reason     string     `json:"reason"`
	Refusal    string     `json:"refusal,omitempty"`
	ApproveURL string     `json:"approve_url,omitempty"`
	Next       string     `json:"next"`
}

func (s *Server) previewExtendReservations(ctx context.Context, _ *mcp.CallToolRequest, in OwnerIn) (*mcp.CallToolResult, ExtendPreviewOut, error) {
	tok, err := token(in.OwnerLink, "owner")
	if err != nil {
		return nil, ExtendPreviewOut{}, err
	}
	var v struct {
		Extension *struct {
			Available  bool       `json:"available"`
			From       time.Time  `json:"from"`
			To         *time.Time `json:"to"`
			Refusal    string     `json:"refusal"`
			Reason     string     `json:"reason"`
			ApproveURL string     `json:"approve_url"`
		} `json:"extension"`
	}
	if err := s.call(ctx, "GET", "/api/v1/owner/"+tok, nil, &v); err != nil {
		return nil, ExtendPreviewOut{}, err
	}
	x := v.Extension
	if x == nil {
		return nil, ExtendPreviewOut{}, errors.New("reservations are not open yet, so there is no deadline to extend")
	}
	out := ExtendPreviewOut{Available: x.Available, Deadline: x.From, ExtendTo: x.To, Reason: x.Reason, Refusal: x.Refusal, ApproveURL: x.ApproveURL}
	if !x.Available {
		out.Next = "The deadline cannot be extended; tell the organizer why. Nothing was changed."
		return nil, out, nil
	}
	out.Next = "Show the organizer the new deadline and why it helps, and ask them to open this approve_url on their own device and confirm there. " +
		"Never open the link or confirm it yourself: only the organizer extends the deadline. Nothing has changed yet. " +
		"Once they confirm, call get_status: everyone with an email is told, and Claude's drafted message for the group carries the new deadline."
	return nil, out, nil
}
