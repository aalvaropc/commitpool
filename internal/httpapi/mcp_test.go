package httpapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpSession connects an in-memory MCP client to the env's /mcp server.
func (e *env) mcpSession() *mcp.ClientSession {
	e.t.Helper()
	serverSide, clientSide := mcp.NewInMemoryTransports()
	if _, err := e.mcp.Connect(e.ctx, serverSide, nil); err != nil {
		e.t.Fatalf("server Connect: %v", err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test-assistant", Version: "v0"}, nil).Connect(e.ctx, clientSide, nil)
	if err != nil {
		e.t.Fatalf("client Connect: %v", err)
	}
	e.t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// tool calls the tool and decodes its structured result into out (when not
// nil). It returns the result so a test can check IsError and the text.
func (e *env) tool(cs *mcp.ClientSession, name string, args map[string]any, out any) *mcp.CallToolResult {
	e.t.Helper()
	res, err := cs.CallTool(e.ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		e.t.Fatalf("CallTool(%s): %v", name, err)
	}
	if out != nil && !res.IsError {
		raw, err := json.Marshal(res.StructuredContent)
		if err != nil {
			e.t.Fatal(err)
		}
		if err := json.Unmarshal(raw, out); err != nil {
			e.t.Fatalf("%s: decode %s: %v", name, raw, err)
		}
	}
	return res
}

func resultText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			b.WriteString(t.Text)
		}
	}
	return b.String()
}

func (e *env) wantToolError(res *mcp.CallToolResult, contains string) {
	e.t.Helper()
	if !res.IsError || !strings.Contains(resultText(res), contains) {
		e.t.Fatalf("tool result = error %v %q, want an error containing %q", res.IsError, resultText(res), contains)
	}
}

type mcpCreated struct {
	OwnerLink string `json:"owner_link"`
	ShareLink string `json:"share_link"`
	Dates     []struct {
		ID    string `json:"id"`
		Label string `json:"label"`
	} `json:"dates"`
}

type mcpJoined struct {
	ParticipantLink string `json:"participant_link"`
}

type mcpReading struct {
	Reading []struct {
		Date    string `json:"date"`
		Verdict string `json:"verdict"`
	} `json:"reading"`
	NeedsClarification bool   `json:"needs_clarification"`
	Next               string `json:"next"`
}

type mcpPreview struct {
	ConfirmationToken string `json:"confirmation_token"`
	Date              string `json:"date"`
	Price             string `json:"price"`
	Minimum           int    `json:"minimum"`
	Capacity          int    `json:"capacity"`
	PaymentWindowMin  int    `json:"payment_window_min"`
	Compatible        int    `json:"compatible"`
}

type mcpStatus struct {
	Status            string `json:"status"`
	PaymentsOpen      bool   `json:"payments_open"`
	RecommendedDateID string `json:"recommended_date_id"`
	Paid              int    `json:"paid"`
	Minimum           int    `json:"minimum"`
	IfClosedNow       string `json:"if_closed_now"`
}

type mcpCheckout struct {
	ApproveURL string `json:"approve_url"`
	Next       string `json:"next"`
}

// mcpWorkshop has the assistant create the default workshop and two
// participants answer, so Saturday reaches the minimum of two.
func (e *env) mcpWorkshop(cs *mcp.ClientSession) (mcpCreated, []mcpJoined) {
	e.t.Helper()
	var created mcpCreated
	body := workshopRequest()
	var starts []string
	for _, d := range body["dates"].([]map[string]string) {
		starts = append(starts, d["starts_at"])
	}
	e.tool(cs, "create_workshop", map[string]any{
		"title": body["title"], "description": body["description"], "duration_min": body["duration_min"],
		"timezone": body["timezone"], "minimum": body["minimum"], "capacity": body["capacity"],
		"price": body["price"], "currency": body["currency"], "date_starts": starts,
	}, &created)
	var joined []mcpJoined
	for name, message := range map[string]string{"Ana": "Saturday works if we finish before six", "Luis": "Any day works for me"} {
		var j mcpJoined
		e.tool(cs, "join_workshop", map[string]any{"share_link": created.ShareLink, "display_name": name}, &j)
		e.tool(cs, "submit_availability", map[string]any{"participant_link": j.ParticipantLink, "message": message}, nil)
		joined = append(joined, j)
	}
	return created, joined
}

func TestMCP_NoMoneyMovingTools(t *testing.T) {
	e := newEnv(t)
	cs := e.mcpSession()

	list, err := cs.ListTools(e.ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	var names []string
	readOnly := map[string]bool{}
	for _, tl := range list.Tools {
		names = append(names, tl.Name)
		readOnly[tl.Name] = tl.Annotations != nil && tl.Annotations.ReadOnlyHint
		for _, word := range []string{"capture", "void", "refund", "cancel", "withdraw", "charge"} {
			if strings.Contains(tl.Name, word) {
				t.Errorf("tool %q can move money; an assistant must never", tl.Name)
			}
		}
	}
	slices.Sort(names)
	want := []string{"confirm_open_payments", "create_workshop", "get_checkout_link", "get_my_status", "get_status",
		"get_workshop", "join_workshop", "preview_open_payments", "submit_availability"}
	if !slices.Equal(names, want) {
		t.Errorf("tools = %v, want %v", names, want)
	}
	for _, name := range []string{"get_workshop", "get_my_status", "get_status", "preview_open_payments"} {
		if !readOnly[name] {
			t.Errorf("tool %q is not annotated read-only", name)
		}
	}
}

func TestMCP_SubmitAvailabilityReturnsReadingForConfirmation(t *testing.T) {
	e := newEnv(t)
	cs := e.mcpSession()
	created, _ := e.mcpWorkshop(cs)
	var j mcpJoined
	e.tool(cs, "join_workshop", map[string]any{"share_link": created.ShareLink, "display_name": "Marta"}, &j)

	var reading mcpReading
	e.tool(cs, "submit_availability", map[string]any{"participant_link": j.ParticipantLink, "message": "I can't do Saturday; Sunday works"}, &reading)

	if len(reading.Reading) != 2 {
		t.Fatalf("reading = %+v, want one verdict per candidate date", reading.Reading)
	}
	verdicts := map[string]string{}
	for _, r := range reading.Reading {
		verdicts[r.Date] = r.Verdict
	}
	if verdicts[created.Dates[0].Label] != "unavailable" || verdicts[created.Dates[1].Label] != "available" {
		t.Errorf("reading = %v, want Saturday unavailable and Sunday available", verdicts)
	}
	if !strings.Contains(reading.Next, "confirm") {
		t.Errorf("next = %q, want the assistant told to confirm the reading with the person", reading.Next)
	}
}

func TestMCP_ConfirmOpenPaymentsRequiresTheBoundToken(t *testing.T) {
	e := newEnv(t)
	cs := e.mcpSession()
	created, _ := e.mcpWorkshop(cs)
	var status mcpStatus
	e.tool(cs, "get_status", map[string]any{"owner_link": created.OwnerLink}, &status)
	if status.RecommendedDateID == "" {
		t.Fatalf("status = %+v, want a recommended date", status)
	}

	var preview mcpPreview
	e.tool(cs, "preview_open_payments", map[string]any{"owner_link": created.OwnerLink, "date_id": status.RecommendedDateID}, &preview)
	if preview.ConfirmationToken == "" || preview.Price != "10.00 USD" || preview.Minimum != 2 || preview.Capacity != 3 || preview.Compatible != 2 {
		t.Fatalf("preview = %+v, want the exact terms and a confirmation token", preview)
	}
	if v := e.ownerByLink(created.OwnerLink); v.Proposal != nil {
		t.Fatalf("a preview opened payments: %+v", v.Proposal)
	}

	tampered := preview.ConfirmationToken[:len(preview.ConfirmationToken)-2] + "xx"
	e.wantToolError(e.tool(cs, "confirm_open_payments", map[string]any{"owner_link": created.OwnerLink, "confirmation_token": tampered}, nil), "confirmation")

	other, _ := e.mcpWorkshop(cs)
	e.wantToolError(e.tool(cs, "confirm_open_payments", map[string]any{"owner_link": other.OwnerLink, "confirmation_token": preview.ConfirmationToken}, nil), "confirmation")

	e.clock.Set(e.clock.Now().Add(11 * time.Minute))
	e.wantToolError(e.tool(cs, "confirm_open_payments", map[string]any{"owner_link": created.OwnerLink, "confirmation_token": preview.ConfirmationToken}, nil), "expired")
	if v := e.ownerByLink(created.OwnerLink); v.Proposal != nil {
		t.Fatalf("a refused confirmation opened payments: %+v", v.Proposal)
	}

	e.tool(cs, "preview_open_payments", map[string]any{"owner_link": created.OwnerLink, "date_id": status.RecommendedDateID}, &preview)
	res := e.tool(cs, "confirm_open_payments", map[string]any{"owner_link": created.OwnerLink, "confirmation_token": preview.ConfirmationToken}, &status)
	if res.IsError || !status.PaymentsOpen {
		t.Fatalf("confirm = %q, status %+v; want payments open", resultText(res), status)
	}
}

func TestMCP_CheckoutLinkRequiresConsent(t *testing.T) {
	e := newEnv(t)
	cs := e.mcpSession()
	created, joined := e.mcpWorkshop(cs)
	var status mcpStatus
	e.tool(cs, "get_status", map[string]any{"owner_link": created.OwnerLink}, &status)
	var preview mcpPreview
	e.tool(cs, "preview_open_payments", map[string]any{"owner_link": created.OwnerLink, "date_id": status.RecommendedDateID}, &preview)
	e.tool(cs, "confirm_open_payments", map[string]any{"owner_link": created.OwnerLink, "confirmation_token": preview.ConfirmationToken}, nil)
	link := joined[0].ParticipantLink

	var mine struct {
		ConsentVersion string `json:"consent_version"`
		ConsentText    string `json:"consent_text"`
	}
	e.tool(cs, "get_my_status", map[string]any{"participant_link": link}, &mine)
	if mine.ConsentVersion == "" || mine.ConsentText == "" {
		t.Fatalf("get_my_status = %+v, want the consent the person must read", mine)
	}

	e.wantToolError(e.tool(cs, "get_checkout_link", map[string]any{"participant_link": link, "consent_version": mine.ConsentVersion, "human_accepted_terms": false}, nil), "accept")
	e.wantToolError(e.tool(cs, "get_checkout_link", map[string]any{"participant_link": link, "consent_version": "consent-v0", "human_accepted_terms": true}, nil), "consent")

	var checkout mcpCheckout
	res := e.tool(cs, "get_checkout_link", map[string]any{"participant_link": link, "consent_version": mine.ConsentVersion, "human_accepted_terms": true}, &checkout)
	if res.IsError || !strings.HasPrefix(checkout.ApproveURL, "http") || !strings.Contains(checkout.Next, "PayPal") {
		t.Fatalf("checkout = %q %+v; want a PayPal approval link the person opens", resultText(res), checkout)
	}
}

func TestMCP_TokensNeverLogged(t *testing.T) {
	e := newEnv(t)
	cs := e.mcpSession()
	created, joined := e.mcpWorkshop(cs)
	e.tool(cs, "get_status", map[string]any{"owner_link": created.OwnerLink}, nil)

	logs := e.logs.String()
	for _, link := range []string{created.OwnerLink, created.ShareLink, joined[0].ParticipantLink, joined[1].ParticipantLink} {
		token := link[strings.LastIndex(link, "/")+1:]
		if strings.Contains(logs, token) {
			t.Errorf("log contains a capability token from %s", link)
		}
	}
	if !strings.Contains(logs, "tool=get_status") {
		t.Errorf("log does not name the tool calls:\n%s", logs)
	}
}

func TestMCP_StreamableHTTPMount(t *testing.T) {
	e := newEnv(t)
	srv := httptest.NewServer(e.h)
	t.Cleanup(srv.Close)

	cs, err := mcp.NewClient(&mcp.Implementation{Name: "http-assistant", Version: "v0"}, nil).
		Connect(e.ctx, &mcp.StreamableClientTransport{Endpoint: srv.URL + "/mcp"}, nil)
	if err != nil {
		t.Fatalf("connect over HTTP: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	list, err := cs.ListTools(e.ctx, nil)
	if err != nil || len(list.Tools) == 0 {
		t.Fatalf("ListTools over HTTP = %v, %v; want the tools", list, err)
	}
}

// ownerByLink reads the owner view for an owner link.
func (e *env) ownerByLink(link string) ownerViewJSON {
	e.t.Helper()
	var v ownerViewJSON
	e.call("GET", "/api/v1/owner/"+link[strings.LastIndex(link, "/")+1:], nil, 200, &v)
	return v
}

func TestMCP_BatchRequestsRefused(t *testing.T) {
	e := newEnv(t, withRateLimit(2))
	var batch []map[string]any
	for i := range 20 {
		batch = append(batch, map[string]any{
			"jsonrpc": "2.0", "id": i + 1, "method": "tools/call",
			"params": map[string]any{"name": "create_workshop", "arguments": map[string]any{
				"title": "Batch", "duration_min": 60, "timezone": "UTC", "minimum": 1, "capacity": 2,
				"price": "10.00", "currency": "USD",
				"date_starts": []string{satStart.Format(time.RFC3339), sunStart.Format(time.RFC3339)},
			}},
		})
	}
	raw, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequestWithContext(e.ctx, http.MethodPost, "/mcp", bytes.NewReader(append([]byte(" \n"), raw...)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.RemoteAddr = "203.0.113.7:4000"
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("batch POST /mcp = %d %s, want 400", rec.Code, rec.Body.String())
	}
	var n int
	if err := e.st.Pool.QueryRow(e.ctx, `select count(*) from workshops`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("workshops created by one rate-limited batch = %d, want 0", n)
	}
}
