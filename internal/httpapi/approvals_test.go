package httpapi_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/inproc"
)

type approvalViewJSON struct {
	Approval struct {
		ID               string     `json:"id"`
		State            string     `json:"state"`
		DateID           string     `json:"date_id"`
		DateLabel        string     `json:"date_label"`
		StartsAt         time.Time  `json:"starts_at"`
		Price            moneyJSON  `json:"price"`
		Minimum          int        `json:"minimum"`
		Capacity         int        `json:"capacity"`
		PaymentWindowMin int        `json:"payment_window_min"`
		ExpiresAt        time.Time  `json:"expires_at"`
		ApprovedAt       *time.Time `json:"approved_at"`
		UsedAt           *time.Time `json:"used_at"`
	} `json:"approval"`
	Workshop   workshopJSON `json:"workshop"`
	ApproveURL string       `json:"approve_url"`
}

type approvalPage struct {
	path   string
	cookie *http.Cookie
	view   approvalViewJSON
}

func (e *env) openableWorkshop() workshopHandle {
	e.t.Helper()
	h := e.createWorkshop()
	e.joinAndSay(h, "Ana", "Saturday works if we finish before six")
	e.joinAndSay(h, "Luis", "Any day works for me")
	return h
}

func (e *env) requestApproval(h workshopHandle, dateID string, want int) approvalViewJSON {
	e.t.Helper()
	var v approvalViewJSON
	e.call(http.MethodPost, h.ownerPath+"/approvals", map[string]string{"date_id": dateID}, want, &v)
	return v
}

func approvalAPIPath(approveURL string) string {
	rest := strings.TrimPrefix(approveURL, appBaseURL+"/owner/")
	token, id, _ := strings.Cut(rest, "/approve/")
	return "/api/v1/owner/" + token + "/approvals/" + id
}

func (e *env) openApprovalPage(approveURL string) approvalPage {
	e.t.Helper()
	page := approvalPage{path: approvalAPIPath(approveURL)}
	rec := e.request(http.MethodGet, page.path, nil)
	if rec.Code != http.StatusOK {
		e.t.Fatalf("GET %s = %d %s", page.path, rec.Code, rec.Body.String())
	}
	decodeInto(e.t, rec, &page.view)
	for _, c := range rec.Result().Cookies() {
		if c.Name == "commitpool_approval" {
			page.cookie = c
		}
	}
	return page
}

func decodeInto(t *testing.T, rec *httptest.ResponseRecorder, out any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
}

func sameOriginPage() map[string]string {
	return map[string]string{"Origin": appBaseURL, "Sec-Fetch-Site": "same-origin"}
}

func (e *env) tapApprove(page approvalPage, cookie *http.Cookie, headers map[string]string) *httptest.ResponseRecorder {
	e.t.Helper()
	req := httptest.NewRequestWithContext(e.ctx, http.MethodPost, page.path+"/approve", nil)
	req.Host = strings.TrimPrefix(appBaseURL, "https://")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if cookie != nil {
		req.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value})
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

func (e *env) approveInBrowser(approveURL string) approvalViewJSON {
	e.t.Helper()
	page := e.openApprovalPage(approveURL)
	if page.cookie == nil {
		e.t.Fatalf("the approval page set no cookie: %+v", page.view)
	}
	rec := e.tapApprove(page, page.cookie, sameOriginPage())
	if rec.Code != http.StatusOK {
		e.t.Fatalf("POST approve = %d %s, want 200", rec.Code, rec.Body.String())
	}
	var v approvalViewJSON
	decodeInto(e.t, rec, &v)
	return v
}

const needsPageMessage = "Only the organizer can approve, on the approve page in their own browser."

func wantRefusal(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	var got errorJSON
	if rec.Code != status || json.Unmarshal(rec.Body.Bytes(), &got) != nil || got.Error != code || got.Message == "" {
		t.Fatalf("response = %d %s, want %d %s", rec.Code, rec.Body.String(), status, code)
	}
}

func TestApproval_RequestCarriesTheExactTermsAndAnApproveLink(t *testing.T) {
	e := newEnv(t)
	h := e.openableWorkshop()

	v := e.requestApproval(h, h.satID, http.StatusCreated)
	a := v.Approval
	if v.ApproveURL != appBaseURL+"/owner/"+h.ownerToken+"/approve/"+a.ID {
		t.Errorf("approve_url = %q, want the organizer's approve page", v.ApproveURL)
	}
	if a.State != "pending" || a.DateID != h.satID || !a.StartsAt.Equal(satStart) || a.DateLabel == "" || a.Price.Value != "10.00" ||
		a.Price.Currency != "USD" || a.Minimum != 2 || a.Capacity != 3 || a.PaymentWindowMin != 1440 ||
		!a.ExpiresAt.Equal(t0.Add(30*time.Minute)) || a.ApprovedAt != nil || a.UsedAt != nil {
		t.Errorf("approval = %+v, want Saturday's exact terms, pending for 30 minutes", a)
	}
	if v.Workshop.Title != "Go for operators" || v.Workshop.Timezone != "America/Lima" {
		t.Errorf("workshop = %+v", v.Workshop)
	}
	e.clock.Set(t0.Add(20 * time.Minute))
	if again := e.requestApproval(h, h.satID, http.StatusOK); again.Approval.ID != a.ID {
		t.Errorf("a preview with 10 minutes left made approval %s, want the open one %s", again.Approval.ID, a.ID)
	}
	e.clock.Set(t0.Add(21 * time.Minute))
	fresh := e.requestApproval(h, h.satID, http.StatusCreated)
	if fresh.Approval.ID == a.ID || !fresh.Approval.ExpiresAt.Equal(t0.Add(51*time.Minute)) {
		t.Errorf("a preview with 9 minutes left = %+v, want a new approval for 30 minutes", fresh.Approval)
	}
	e.clock.Set(t0)
	if view := e.owner(h); view.Proposal != nil {
		t.Fatalf("requesting an approval opened reservations: %+v", view.Proposal)
	}

	page := e.openApprovalPage(v.ApproveURL)
	c := page.cookie
	if page.view.Approval.State != "pending" || c == nil {
		t.Fatalf("approval page = %+v, cookie %v; want pending with a cookie", page.view.Approval, c)
	}
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.Path != page.path || c.MaxAge != 1800 || c.Value == "" {
		t.Errorf("cookie = %+v, want HttpOnly, Secure, SameSite=Strict, scoped to %s, for 30 minutes", c, page.path)
	}
}

func TestApproval_RequestRefusedForADateThatCannotOpen(t *testing.T) {
	e := newEnv(t)
	h := e.createWorkshop()
	got := e.wantError(http.MethodPost, h.ownerPath+"/approvals", map[string]string{"date_id": h.satID}, http.StatusConflict, "proposal_invalid")
	if got.Reason != "not_enough_compatible" {
		t.Errorf("reason = %q, want not_enough_compatible", got.Reason)
	}
	e.wantError(http.MethodPost, h.ownerPath+"/approvals", map[string]string{"date_id": "4b0f3f0e-1b7e-4e9a-9f43-0d0c3c7b1a11"}, http.StatusBadRequest, "invalid_date")
}

func TestApproval_ApproveOnlyFromTheOrganizersPage(t *testing.T) {
	e := newEnv(t)
	h := e.openableWorkshop()
	v := e.requestApproval(h, h.satID, http.StatusCreated)
	page := e.openApprovalPage(v.ApproveURL)

	other := e.openableWorkshop()
	otherPage := e.openApprovalPage(e.requestApproval(other, other.satID, http.StatusCreated).ApproveURL)

	refused := map[string]struct {
		cookie  *http.Cookie
		headers map[string]string
	}{
		"no cookie":                          {nil, sameOriginPage()},
		"no browser headers":                 {page.cookie, nil},
		"a cross-site request":               {page.cookie, map[string]string{"Origin": appBaseURL, "Sec-Fetch-Site": "cross-site"}},
		"a same-site request":                {page.cookie, map[string]string{"Sec-Fetch-Site": "same-site"}},
		"another origin from an old browser": {page.cookie, map[string]string{"Origin": "https://elsewhere.example"}},
		"another approval's cookie":          {otherPage.cookie, sameOriginPage()},
		"a forged cookie":                    {&http.Cookie{Name: "commitpool_approval", Value: "forged"}, sameOriginPage()},
	}
	for name, r := range refused {
		t.Run(name, func(t *testing.T) {
			rec := e.tapApprove(page, r.cookie, r.headers)
			wantRefusal(t, rec, http.StatusForbidden, "approval_needs_page")
			var got errorJSON
			_ = json.Unmarshal(rec.Body.Bytes(), &got)
			if got.Message != needsPageMessage {
				t.Errorf("message = %q, want %q", got.Message, needsPageMessage)
			}
		})
	}

	err := inproc.Client{Handler: e.h}.Call(e.ctx, http.MethodPost, page.path+"/approve", nil, nil)
	var apiErr *inproc.Error
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusForbidden || apiErr.Code != "approval_needs_page" {
		t.Fatalf("in-process approve = %v, want 403 approval_needs_page", err)
	}
	if again := e.openApprovalPage(v.ApproveURL); again.view.Approval.State != "pending" {
		t.Fatalf("after refused approvals the state is %q, want pending", again.view.Approval.State)
	}

	e.clock.Set(t0.Add(25 * time.Minute))
	approved := e.approveInBrowser(v.ApproveURL)
	if approved.Approval.State != "approved" || approved.Approval.ApprovedAt == nil || !approved.Approval.ApprovedAt.Equal(t0.Add(25*time.Minute)) ||
		!approved.Approval.ExpiresAt.Equal(t0.Add(55*time.Minute)) {
		t.Fatalf("approve = %+v, want approved now with 30 fresh minutes for the assistant", approved.Approval)
	}
	shown := e.openApprovalPage(v.ApproveURL)
	if shown.view.Approval.State != "approved" || shown.cookie != nil {
		t.Errorf("approval page after approving = %q, cookie %v; want approved and no new cookie", shown.view.Approval.State, shown.cookie)
	}
	if rec := e.tapApprove(page, page.cookie, sameOriginPage()); rec.Code != http.StatusOK {
		t.Errorf("approving twice = %d %s, want 200", rec.Code, rec.Body.String())
	}
	if view := e.owner(h); view.Proposal != nil {
		t.Fatalf("approving opened reservations by itself: %+v", view.Proposal)
	}
}

func TestApproval_EveryBrowserHeaderSetApproves(t *testing.T) {
	e := newEnv(t)
	sets := map[string]map[string]string{
		"fetch metadata and origin": sameOriginPage(),
		"origin only":               {"Origin": appBaseURL},
		"fetch metadata only":       {"Sec-Fetch-Site": "same-origin"},
	}
	for name, headers := range sets {
		t.Run(name, func(t *testing.T) {
			h := e.openableWorkshop()
			page := e.openApprovalPage(e.requestApproval(h, h.satID, http.StatusCreated).ApproveURL)
			rec := e.tapApprove(page, page.cookie, headers)
			var v approvalViewJSON
			if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &v) != nil || v.Approval.State != "approved" {
				t.Fatalf("approve with %v = %d %s, want approved", headers, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestApproval_ApproveRefusedWhenExpiredOrTermsChanged(t *testing.T) {
	e := newEnv(t)
	h := e.openableWorkshop()
	page := e.openApprovalPage(e.requestApproval(h, h.satID, http.StatusCreated).ApproveURL)
	e.clock.Set(t0.Add(30 * time.Minute))
	wantRefusal(t, e.tapApprove(page, page.cookie, sameOriginPage()), http.StatusConflict, "approval_expired")
	if expired := e.openApprovalPage(page.view.ApproveURL); expired.view.Approval.State != "expired" || expired.cookie != nil {
		t.Errorf("expired approval page = %q, cookie %v; want expired and no cookie", expired.view.Approval.State, expired.cookie)
	}

	changed := e.openableWorkshop()
	page = e.openApprovalPage(e.requestApproval(changed, changed.satID, http.StatusCreated).ApproveURL)
	if _, err := e.st.Pool.Exec(e.ctx, `update workshops set price_minor = 1200 where id = $1`, changed.created.Workshop.ID); err != nil {
		t.Fatal(err)
	}
	if shown := e.openApprovalPage(page.view.ApproveURL); shown.view.Approval.State != "changed" {
		t.Errorf("approval page after a price change = %q, want changed", shown.view.Approval.State)
	}
	wantRefusal(t, e.tapApprove(page, page.cookie, sameOriginPage()), http.StatusConflict, "terms_changed")
}

func TestApproval_WrongWorkshopFindsNothing(t *testing.T) {
	e := newEnv(t)
	h := e.openableWorkshop()
	v := e.requestApproval(h, h.satID, http.StatusCreated)
	other := e.createWorkshop()

	foreign := other.ownerPath + "/approvals/" + v.Approval.ID
	e.wantError(http.MethodGet, foreign, nil, http.StatusNotFound, "not_found")
	e.wantError(http.MethodPost, foreign+"/open", nil, http.StatusNotFound, "not_found")
	page := e.openApprovalPage(v.ApproveURL)
	wantRefusal(t, e.tapApprove(approvalPage{path: foreign}, page.cookie, sameOriginPage()), http.StatusNotFound, "not_found")
	e.wantError(http.MethodGet, h.ownerPath+"/approvals/not-an-id", nil, http.StatusNotFound, "not_found")
}

func TestApproval_OpensReservationsOnlyAfterApprovalAndOnce(t *testing.T) {
	e := newEnv(t)
	h := e.openableWorkshop()
	v := e.requestApproval(h, h.satID, http.StatusCreated)
	open := approvalAPIPath(v.ApproveURL) + "/open"

	e.wantError(http.MethodPost, open, nil, http.StatusConflict, "approval_pending")
	if view := e.owner(h); view.Proposal != nil {
		t.Fatalf("a refused open left reservations open: %+v", view.Proposal)
	}

	e.clock.Set(t0.Add(25 * time.Minute))
	e.approveInBrowser(v.ApproveURL)
	e.clock.Set(t0.Add(40 * time.Minute))
	var opened struct {
		Proposal proposalJSON `json:"proposal"`
	}
	e.call(http.MethodPost, open, nil, http.StatusCreated, &opened)
	if opened.Proposal.DateID != h.satID || opened.Proposal.Price.Value != "10.00" || opened.Proposal.Minimum != 2 || !opened.Proposal.PaymentsOpen {
		t.Fatalf("opened = %+v, want Saturday's approved terms", opened.Proposal)
	}
	e.wantError(http.MethodPost, open, nil, http.StatusConflict, "approval_used")
	if used := e.openApprovalPage(v.ApproveURL); used.view.Approval.State != "used" || used.view.Approval.UsedAt == nil {
		t.Errorf("approval page after opening = %+v, want used", used.view.Approval)
	}
	e.wantError(http.MethodPost, h.ownerPath+"/approvals", map[string]string{"date_id": h.satID}, http.StatusConflict, "proposal_already_open")
}

func TestApproval_OpenRefusedAfterExpiryOrATermChange(t *testing.T) {
	e := newEnv(t)
	h := e.openableWorkshop()
	v := e.requestApproval(h, h.satID, http.StatusCreated)
	e.approveInBrowser(v.ApproveURL)
	e.clock.Set(t0.Add(30 * time.Minute))
	e.wantError(http.MethodPost, approvalAPIPath(v.ApproveURL)+"/open", nil, http.StatusConflict, "approval_expired")

	changed := e.openableWorkshop()
	v = e.requestApproval(changed, changed.satID, http.StatusCreated)
	e.approveInBrowser(v.ApproveURL)
	if _, err := e.st.Pool.Exec(e.ctx, `update workshops set minimum = 3 where id = $1`, changed.created.Workshop.ID); err != nil {
		t.Fatal(err)
	}
	e.wantError(http.MethodPost, approvalAPIPath(v.ApproveURL)+"/open", nil, http.StatusConflict, "terms_changed")
	if view := e.owner(changed); view.Proposal != nil {
		t.Fatalf("a refused open left reservations open: %+v", view.Proposal)
	}
}

func TestApproval_InProcessCallersCannotOpenWithoutApproval(t *testing.T) {
	e := newEnv(t)
	h := e.openableWorkshop()
	err := inproc.Client{Handler: e.h}.Call(e.ctx, http.MethodPost, h.ownerPath+"/proposal", map[string]string{"date_id": h.satID}, nil)
	var apiErr *inproc.Error
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusForbidden || apiErr.Code != "approval_required" {
		t.Fatalf("in-process POST /proposal = %v, want 403 approval_required", err)
	}
	if view := e.owner(h); view.Proposal != nil {
		t.Fatalf("an in-process call opened reservations: %+v", view.Proposal)
	}
	e.openProposal(h, h.satID)
}
