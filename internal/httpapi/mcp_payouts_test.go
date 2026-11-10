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
