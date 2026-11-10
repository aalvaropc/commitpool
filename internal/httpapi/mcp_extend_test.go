package httpapi_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

type mcpExtendPreview struct {
	Available  bool       `json:"available"`
	Deadline   time.Time  `json:"deadline"`
	ExtendTo   *time.Time `json:"extend_to"`
	Reason     string     `json:"reason"`
	Refusal    string     `json:"refusal"`
	ApproveURL string     `json:"approve_url"`
	Next       string     `json:"next"`
}

func TestMCP_PreviewExtendHandsTheOrganizerADeepLink(t *testing.T) {
	e := newEnv(t)
	h, p, _ := e.shortOfTheMinimum()
	cs := e.mcpSession()

	var out mcpExtendPreview
	if res := e.tool(cs, "preview_extend_reservations", map[string]any{"owner_link": h.created.OwnerURL}, &out); res.IsError {
		t.Fatalf("preview_extend_reservations: %s", resultText(res))
	}
	want := appBaseURL + "/owner/" + h.ownerToken + "?extend=" + strconv.FormatInt(extendTo.Unix(), 10)
	if !out.Available || out.ExtendTo == nil || !out.ExtendTo.Equal(extendTo) || !out.Deadline.Equal(p.ClosesAt) || out.ApproveURL != want {
		t.Errorf("preview = %+v, want %s on offer behind %s", out, extendTo, want)
	}
	if !strings.Contains(out.Next, "Never open the link or confirm it yourself") || !strings.Contains(out.Reason, "once") {
		t.Errorf("next = %q, reason = %q", out.Next, out.Reason)
	}
	if v := e.extendView(h); v.Proposal.ExtendedFrom != nil || !v.Proposal.ClosesAt.Equal(p.ClosesAt) {
		t.Errorf("a preview moved the deadline: %+v", v.Proposal)
	}

	e.extend(h, extendBody(p.ClosesAt, extendTo), http.StatusOK, nil)
	out = mcpExtendPreview{}
	e.tool(cs, "preview_extend_reservations", map[string]any{"owner_link": h.created.OwnerURL}, &out)
	if out.Available || out.Refusal != "already_extended" || out.ApproveURL != "" || !strings.Contains(out.Next, "cannot be extended") {
		t.Errorf("preview after extending = %+v", out)
	}
}

func TestMCP_PreviewExtendBeforeReservationsOpen(t *testing.T) {
	e := newEnv(t)
	h := e.createWorkshop()
	cs := e.mcpSession()
	e.wantToolError(e.tool(cs, "preview_extend_reservations", map[string]any{"owner_link": h.created.OwnerURL}, nil), "not open yet")
	e.wantToolError(e.tool(cs, "preview_extend_reservations", map[string]any{"owner_link": h.created.ShareURL}, nil), "not a CommitPool owner link")
}
