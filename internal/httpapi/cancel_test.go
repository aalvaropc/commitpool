package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestOwnerCancel_CancelsAndRefusesASecondTime(t *testing.T) {
	e := newEnv(t)
	h := e.createWorkshop()
	e.joinAndSay(h, "Ana", "Saturday works if we finish before six")
	e.joinAndSay(h, "Luis", "Any day works for me")
	e.openProposal(h, h.satID)

	var out struct {
		Workshop workshopJSON `json:"workshop"`
	}
	e.call(http.MethodPost, h.ownerPath+"/cancel", map[string]string{"reason": "Venue flooded"}, http.StatusOK, &out)

	if out.Workshop.Status != "CANCELLED" {
		t.Fatalf("workshop after cancel = %+v, want CANCELLED", out.Workshop)
	}
	if v := e.owner(h); v.Workshop.Status != "CANCELLED" {
		t.Errorf("owner view after cancel = %s, want CANCELLED", v.Workshop.Status)
	}
	e.wantError(http.MethodPost, h.ownerPath+"/cancel", map[string]string{}, http.StatusConflict, "already_cancelled")
}

func TestParticipantWithdraw_ReleasesTheSeatOnce(t *testing.T) {
	e := newEnv(t)
	_, p, ps := e.fundingWorkshop()
	e.checkout(ps[0], p.ID)

	var out struct {
		Status      string `json:"status"`
		StatusLabel string `json:"status_label"`
	}
	e.call(http.MethodPost, ps[0].path+"/withdraw", map[string]string{}, http.StatusOK, &out)

	if out.Status != "RELEASED" || out.StatusLabel != "Gave up the spot" {
		t.Fatalf("withdraw = %+v, want RELEASED labelled Withdrawn, not an expired seat", out)
	}
	e.wantError(http.MethodPost, ps[0].path+"/withdraw", map[string]string{}, http.StatusConflict, "nothing_to_withdraw")
}

func TestParticipantWithdraw_AuthorizedHoldReadsWithdrawn(t *testing.T) {
	e := newEnv(t)
	h, p, ps := e.fundingWorkshop()
	en, _ := e.checkout(ps[0], p.ID)
	e.authorize(h.created.Workshop.ID, en.EnrollmentID)

	var out struct {
		Status      string `json:"status"`
		StatusLabel string `json:"status_label"`
	}
	e.call(http.MethodPost, ps[0].path+"/withdraw", map[string]string{}, http.StatusOK, &out)

	want := map[string]string{"VOIDING": "Gave up the spot — releasing the hold", "VOIDED": "Gave up the spot — hold released"}
	if label, ok := want[out.Status]; !ok || out.StatusLabel != label {
		t.Fatalf("withdraw of an authorized hold = %+v, want VOIDING or VOIDED with the label for that status", out)
	}
}

func TestParticipantWithdraw_HoldNotYetReleasedSaysSo(t *testing.T) {
	e := newEnv(t)
	h, p, ps := e.fundingWorkshop()
	en, _ := e.checkout(ps[0], p.ID)
	e.authorize(h.created.Workshop.ID, en.EnrollmentID)
	e.fake.FailNext("void", 503, "")

	var out struct {
		Status      string `json:"status"`
		StatusLabel string `json:"status_label"`
	}
	e.call(http.MethodPost, ps[0].path+"/withdraw", map[string]string{}, http.StatusOK, &out)

	if out.Status != "VOIDING" || out.StatusLabel != "Gave up the spot — releasing the hold" {
		t.Fatalf("withdraw while PayPal does not answer the void = %+v, want VOIDING, \"Gave up the spot — releasing the hold\"", out)
	}
}

func TestCheckout_RefusesADateTheReadingRulesOut(t *testing.T) {
	e := newEnv(t)
	h := e.createWorkshop()
	e.joinAndSay(h, "Ana", "Saturday works if we finish before six")
	e.joinAndSay(h, "Luis", "Any day works for me")
	marta := e.joinAndSay(h, "Marta", "I can't do Saturday; Sunday works")
	p := e.openProposal(h, h.satID)

	got := e.wantError(http.MethodPost, marta.path+"/enrollment", enrollBody(p.ID), http.StatusConflict, "date_incompatible")

	if got.Message == "" {
		t.Errorf("date_incompatible without a message")
	}
}

func TestOperatorRoutes_ActOnlyOnTheOwnersEnrollments(t *testing.T) {
	e := newEnv(t)
	_, p, ps := e.fundingWorkshop()
	en, _ := e.checkout(ps[0], p.ID)
	other := e.createWorkshop()

	e.wantError(http.MethodPost, other.ownerPath+"/operator/reconcile", map[string]string{"enrollment_id": en.EnrollmentID}, http.StatusNotFound, "not_found")
	e.wantError(http.MethodPost, other.ownerPath+"/operator/retry-refund", map[string]string{"enrollment_id": en.EnrollmentID}, http.StatusNotFound, "not_found")

	var out struct {
		Workshop workshopJSON `json:"workshop"`
	}
	e.call(http.MethodPost, other.ownerPath+"/operator/clear-flag", nil, http.StatusOK, &out)
	if out.Workshop.Resolution != "OPEN" {
		t.Errorf("clear-flag on a workshop with nothing flagged = %+v, want it left OPEN", out.Workshop)
	}
}

func TestJoin_StoresEmailAndSendsThePrivateLinkOnce(t *testing.T) {
	e := newEnv(t)
	h := e.createWorkshop()
	var out struct {
		ParticipantURL string `json:"participant_url"`
	}
	e.call(http.MethodPost, h.sharePath+"/participants", map[string]string{"display_name": "Ana", "email": "ana@example.com"}, http.StatusCreated, &out)
	e.runner.RunPending(e.ctx)

	sent := e.mailer.Sent()
	if len(sent) != 1 || sent[0].To != "ana@example.com" || !strings.Contains(sent[0].Text, out.ParticipantURL) {
		t.Fatalf("sent = %+v, want one message to Ana with her private link", sent)
	}
	if !strings.Contains(sent[0].HTML, out.ParticipantURL) {
		t.Errorf("html part lacks the private link")
	}
	var body, html string
	if err := e.st.Pool.QueryRow(e.ctx, `select body, html from notifications where to_email = 'ana@example.com'`).Scan(&body, &html); err != nil || body != "" || html != "" {
		t.Errorf("stored body %q html %q after sending, %v; want both cleared: they carried the private link", body, html, err)
	}
	e.wantError(http.MethodPost, h.sharePath+"/participants", map[string]string{"display_name": "Luis", "email": "not an address"}, http.StatusBadRequest, "invalid_email")
}

func TestCreate_EmailsTheOrganizerTheirLinkOnce(t *testing.T) {
	e := newEnv(t)
	h := e.createWorkshop(func(b map[string]any) {
		b["owner_email"] = "maya@example.com"
		b["host_name"] = "Maya Okafor"
	})
	e.runner.RunPending(e.ctx)
	sent := e.mailer.Sent()
	if len(sent) != 1 || sent[0].To != "maya@example.com" || !strings.Contains(sent[0].Text, h.created.OwnerURL) || !strings.Contains(sent[0].HTML, h.created.OwnerURL) {
		t.Fatalf("sent = %+v, want one message to Maya with her organizer link", sent)
	}
	if !strings.Contains(sent[0].Text, "Hi Maya,") || !strings.Contains(sent[0].Text, h.created.ShareURL) {
		t.Errorf("text = %s", sent[0].Text)
	}
	var body, html string
	if err := e.st.Pool.QueryRow(e.ctx, `select body, html from notifications where to_email = 'maya@example.com'`).Scan(&body, &html); err != nil || body != "" || html != "" {
		t.Errorf("stored body %q html %q after sending, %v; want both cleared: they carried the organizer link", body, html, err)
	}
	for _, path := range []string{h.sharePath, h.ownerPath} {
		if raw := e.request(http.MethodGet, path, nil).Body.String(); strings.Contains(raw, "maya@example.com") {
			t.Errorf("GET %s shows the organizer's email", path)
		}
	}
	e.wantError(http.MethodPost, "/api/v1/workshops", func() map[string]any {
		b := workshopRequest()
		b["owner_email"] = "Maya <maya@example.com>"
		return b
	}(), http.StatusBadRequest, "invalid_owner_email")
}

func TestCreate_WithoutAnEmailSendsNothing(t *testing.T) {
	e := newEnv(t)
	e.createWorkshop()
	e.runner.RunPending(e.ctx)
	if sent := e.mailer.Sent(); len(sent) != 0 {
		t.Errorf("sent = %+v, want nothing", sent)
	}
}
