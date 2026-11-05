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
