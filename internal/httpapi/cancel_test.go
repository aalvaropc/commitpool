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

	if out.Status != "RELEASED" || out.StatusLabel != "Withdrawn" {
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

	if (out.Status != "VOIDED" && out.Status != "VOIDING") || out.StatusLabel != "Withdrawn — hold released" {
		t.Fatalf("withdraw of an authorized hold = %+v, want VOIDING or VOIDED labelled \"Withdrawn — hold released\"", out)
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
	var body string
	if err := e.st.Pool.QueryRow(e.ctx, `select body from notifications where to_email = 'ana@example.com'`).Scan(&body); err != nil || body != "" {
		t.Errorf("stored body after sending = %q, %v; want it cleared: it carried the private link", body, err)
	}
	e.wantError(http.MethodPost, h.sharePath+"/participants", map[string]string{"display_name": "Luis", "email": "not an address"}, http.StatusBadRequest, "invalid_email")
}
