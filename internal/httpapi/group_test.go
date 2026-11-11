package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

type groupJSON struct {
	Group struct {
		Answered int            `json:"answered"`
		CanCome  map[string]int `json:"can_come"`
		Tied     bool           `json:"tied"`
	} `json:"group"`
}

func TestGroup_InviteAndParticipantViewsCountAnswersWithoutNames(t *testing.T) {
	e := newEnv(t)
	h := e.createWorkshop()
	ana := e.joinAndSay(h, "Ana", "Saturday works for me, Sunday I can't")
	e.joinAndSay(h, "Luis", "Any day works for me")
	e.join(h, "Marta")

	for name, path := range map[string]string{"invite": h.sharePath, "participant": ana.path} {
		var v groupJSON
		e.call(http.MethodGet, path, nil, http.StatusOK, &v)
		if v.Group.Answered != 2 {
			t.Errorf("%s answered = %d, want 2 (Marta joined without answering)", name, v.Group.Answered)
		}
		if v.Group.CanCome[h.satID] != 2 || v.Group.CanCome[h.sunID] != 1 {
			t.Errorf("%s can_come = %v, want Saturday 2 and Sunday 1", name, v.Group.CanCome)
		}
		rec := e.request(http.MethodGet, path, nil)
		for _, other := range []string{"Luis", "Marta", "Any day works for me"} {
			if strings.Contains(rec.Body.String(), other) {
				t.Errorf("%s view names %q", name, other)
			}
		}
	}
}

func TestGroup_EmptyBeforeAnyAnswer(t *testing.T) {
	e := newEnv(t)
	h := e.createWorkshop()
	var v groupJSON
	e.call(http.MethodGet, h.sharePath, nil, http.StatusOK, &v)
	if v.Group.Answered != 0 || v.Group.CanCome[h.satID] != 0 || v.Group.CanCome == nil {
		t.Errorf("group before any answer = %+v, want zero counts for every date", v.Group)
	}
}

func TestGroup_TieIsExposedToOwnerAndParticipants(t *testing.T) {
	e := newEnv(t)
	h := e.createWorkshop()
	ana := e.joinAndSay(h, "Ana", "Saturday works for me, Sunday I can't")
	e.joinAndSay(h, "Luis", "Sunday works for me, Saturday I can't")

	for name, path := range map[string]string{"invite": h.sharePath, "participant": ana.path} {
		var v groupJSON
		e.call(http.MethodGet, path, nil, http.StatusOK, &v)
		if !v.Group.Tied || v.Group.CanCome[h.satID] != 1 || v.Group.CanCome[h.sunID] != 1 {
			t.Errorf("%s group = %+v, want Saturday and Sunday tied at 1", name, v.Group)
		}
	}
	if rec := e.owner(h).Recommendation; !rec.Tied || rec.DateID != h.satID {
		t.Errorf("owner recommendation = %+v, want a tie won by Saturday, the earlier date", rec)
	}

	e.joinAndSay(h, "Marta", "Saturday works for me, Sunday I can't")
	var v groupJSON
	e.call(http.MethodGet, h.sharePath, nil, http.StatusOK, &v)
	if v.Group.Tied || e.owner(h).Recommendation.Tied {
		t.Errorf("Saturday leads 2 to 1 but is still reported as tied")
	}
}
