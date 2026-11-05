package httpapi_test

import (
	"net/http"
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

	if out.Status != "RELEASED" {
		t.Fatalf("withdraw = %+v, want RELEASED", out)
	}
	e.wantError(http.MethodPost, ps[0].path+"/withdraw", map[string]string{}, http.StatusConflict, "nothing_to_withdraw")
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
