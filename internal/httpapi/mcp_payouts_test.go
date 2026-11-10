package httpapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type mcpPayout struct {
	State         string `json:"state"`
	Receiver      string `json:"receiver"`
	Amount        string `json:"amount"`
	Estimated     bool   `json:"estimated"`
	PayPalBatchID string `json:"paypal_batch_id"`
	Summary       string `json:"summary"`
}

func (e *env) mcpPayoutWorkshop(cs *mcp.ClientSession, payoutEmail string) (mcpCreated, workshopHandle) {
	e.t.Helper()
	body := workshopRequest()
	var starts []string
	for _, d := range body["dates"].([]map[string]string) {
		starts = append(starts, d["starts_at"])
	}
	var created mcpCreated
	if res := e.tool(cs, "create_workshop", map[string]any{
		"title": body["title"], "duration_min": body["duration_min"], "timezone": body["timezone"],
		"minimum": body["minimum"], "capacity": body["capacity"], "price": body["price"], "currency": body["currency"],
		"date_starts": starts, "payout_email": payoutEmail,
	}, &created); res.IsError {
		e.t.Fatalf("create_workshop = %q", resultText(res))
	}
	token := created.OwnerLink[strings.LastIndex(created.OwnerLink, "/")+1:]
	share := created.ShareLink[strings.LastIndex(created.ShareLink, "/")+1:]
	h := workshopHandle{ownerToken: token, ownerPath: "/api/v1/owner/" + token, sharePath: "/api/v1/w/" + share}
	for _, d := range created.Dates {
		if strings.HasPrefix(d.Label, "Sat") {
			h.satID = d.ID
		}
	}
	return created, h
}

func TestMCP_PayoutEmailIsSetAtCreationAndShownBeforeApproval(t *testing.T) {
	e := newEnv(t)
	cs := e.mcpSession()
	created, h := e.mcpPayoutWorkshop(cs, payoutEmail)
	if v := e.ownerPayout(h); v.Workshop.PayoutEmail != payoutEmail {
		t.Fatalf("owner view payout_email = %q, want %q", v.Workshop.PayoutEmail, payoutEmail)
	}
	e.joinAndSay(h, "Ana", "Saturday works if we finish before six")
	e.joinAndSay(h, "Luis", "Any day works for me")

	var preview struct {
		PayoutEmail string `json:"payout_email"`
		Next        string `json:"next"`
	}
	if res := e.tool(cs, "preview_open_payments", map[string]any{"owner_link": created.OwnerLink, "date_id": h.satID}, &preview); res.IsError {
		t.Fatalf("preview_open_payments = %q", resultText(res))
	}
	if preview.PayoutEmail != payoutEmail || !strings.Contains(preview.Next, "paid out to "+payoutEmail) {
		t.Errorf("preview = %+v, want the payout email named in the terms", preview)
	}

	e.wantToolError(e.tool(cs, "create_workshop", map[string]any{
		"title": "Bad", "duration_min": 60, "timezone": "UTC", "minimum": 1, "capacity": 2, "price": "10.00", "currency": "USD",
		"date_starts": []string{satStart.Format("2006-01-02T15:04:05Z07:00")}, "payout_email": "not an email",
	}, nil), "PayPal email")
}

func TestMCP_GetStatusSaysWhereThePayoutStands(t *testing.T) {
	e := newEnv(t, withPayouts(), withDemoMode())
	cs := e.mcpSession()
	h, _ := e.confirmedWorkshop(withPayoutEmail(payoutEmail))
	link := appBaseURL + "/owner/" + h.ownerToken
	status := func() mcpPayout {
		t.Helper()
		var out struct {
			Payout *mcpPayout `json:"payout"`
		}
		if res := e.tool(cs, "get_status", map[string]any{"owner_link": link}, &out); res.IsError || out.Payout == nil {
			t.Fatalf("get_status = %q, payout %+v", resultText(res), out.Payout)
		}
		return *out.Payout
	}

	got := status()
	if got.State != "scheduled" || got.Amount != "18.32 USD" || !got.Estimated || !strings.Contains(got.Summary, "When the class starts, CommitPool sends 18.32 USD") ||
		!strings.Contains(got.Summary, payoutEmail) {
		t.Fatalf("payout before the start = %+v", got)
	}

	e.call(http.MethodPost, h.ownerPath+"/demo/close-now", nil, http.StatusAccepted, nil)
	paid := e.startClass(h)
	got = status()
	if got.State != "paid" || got.Estimated || got.PayPalBatchID != paid.PayoutBatchID || !strings.HasPrefix(got.Summary, "Paid out 18.32 USD to "+payoutEmail) {
		t.Fatalf("payout after the start = %+v", got)
	}
}

func TestMCP_GetStatusLeavesOutWhatNoLongerApplies(t *testing.T) {
	e := newEnv(t, withPayouts(), withDemoMode())
	cs := e.mcpSession()
	status := func(h workshopHandle) map[string]any {
		t.Helper()
		var out map[string]any
		if res := e.tool(cs, "get_status", map[string]any{"owner_link": appBaseURL + "/owner/" + h.ownerToken}, &out); res.IsError {
			t.Fatalf("get_status = %q", resultText(res))
		}
		return out
	}

	h, _ := e.confirmedWorkshop(withPayoutEmail(payoutEmail))
	open := status(h)
	if open["status"] != "CONFIRMED" || open["payments_open"] != true {
		t.Fatalf("before the close = %v, want a confirmed class still taking reservations", open)
	}
	if got, _ := open["if_closed_now"].(string); !strings.HasPrefix(got, "The workshop is on: 2 payments reach the minimum of 2.") {
		t.Errorf("if_closed_now = %q, want what closing reservations now would do", got)
	}
	if got, ok := open["resolution"]; ok {
		t.Errorf("resolution = %v while no money is being resolved, want it left out", got)
	}

	e.call(http.MethodPost, h.ownerPath+"/demo/close-now", nil, http.StatusAccepted, nil)
	e.startClass(h)
	closed := status(h)
	if closed["status"] != "CONFIRMED" || closed["payments_open"] != false {
		t.Fatalf("after the class started = %v, want confirmed with reservations closed", closed)
	}
	for _, field := range []string{"resolution", "if_closed_now"} {
		if got, ok := closed[field]; ok {
			t.Errorf("%s = %v for a confirmed class that is closed and paid out, want it left out", field, got)
		}
	}

	cancelled, _, _ := e.fundingWorkshop()
	e.call(http.MethodPost, cancelled.ownerPath+"/cancel", map[string]string{}, http.StatusOK, nil)
	gone := status(cancelled)
	if got := gone["resolution"]; gone["status"] != "CANCELLED" || (got != "RESOLVING" && got != "SETTLED") {
		t.Errorf("cancelled = %v, want CANCELLED with the money resolving or settled", gone)
	}
	if got, ok := gone["if_closed_now"]; ok {
		t.Errorf("if_closed_now = %v for a cancelled class, want it left out", got)
	}
}
