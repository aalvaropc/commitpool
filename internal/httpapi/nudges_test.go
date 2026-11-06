package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/aalvaropc/commitpool/internal/ai"
)

func TestSuggestions_AskTheVagueOneWhileFindingTheDate(t *testing.T) {
	e := newEnv(t)
	h := e.createWorkshop()
	e.joinAndSay(h, "Ana", "Saturday works for me")
	e.joinAndSay(h, "Grace Liu", "I can make it either day, ideally in the afternoon.")
	v := e.settledOwner(h)
	if len(v.Suggestions) != 1 {
		t.Fatalf("suggestions = %+v, want one for Grace", v.Suggestions)
	}
	sg := v.Suggestions[0]
	if sg.Kind != "clarify" || sg.Name != "Grace Liu" || sg.Reason != "Grace Liu’s message needs clarifying." || sg.Pending {
		t.Errorf("suggestion = %+v", sg)
	}
	if !strings.HasPrefix(sg.Message, "Hi Grace!") || !strings.Contains(sg.Message, "?") || strings.Contains(sg.Message, "http") {
		t.Errorf("message = %q", sg.Message)
	}
}

func TestSuggestions_NudgeWhoCanComeButHasNotReserved(t *testing.T) {
	e := newEnv(t)
	h, p, ps := e.fundingWorkshop()
	e.enroll(ps[0], p.ID, http.StatusCreated)
	v := e.settledOwner(h)
	var reserve []suggestionJSON
	for _, sg := range v.Suggestions {
		if sg.Kind == "reserve" {
			reserve = append(reserve, sg)
		}
	}
	if len(reserve) == 0 {
		t.Fatalf("suggestions = %+v, want a nudge to reserve", v.Suggestions)
	}
	for _, sg := range reserve {
		if sg.ParticipantID == "" || strings.Contains(sg.Message, "http") || !strings.Contains(sg.Reason, "hasn’t reserved yet") {
			t.Errorf("suggestion = %+v", sg)
		}
	}
	for _, sg := range v.Suggestions {
		if sg.Name == "Ana" && sg.Kind == "reserve" {
			t.Errorf("Ana reserved but is still nudged: %+v", sg)
		}
	}
}

type failingNudgeAI struct{ ai.Fake }

func (failingNudgeAI) Suggest(context.Context, ai.SuggestInput) (string, ai.Meta, error) {
	return "", ai.Meta{}, context.DeadlineExceeded
}

func TestSuggestions_FallBackToThePlainMessage(t *testing.T) {
	e := newEnv(t, withAI(failingNudgeAI{}))
	h := e.createWorkshop()
	e.joinAndSay(h, "Grace Liu", "I can make it either day, ideally in the afternoon.")
	v := e.settledOwner(h)
	if len(v.Suggestions) != 1 || !strings.HasPrefix(v.Suggestions[0].Message, "Hi Grace!") {
		t.Errorf("suggestions = %+v, want the plain message", v.Suggestions)
	}
}

func TestSuggestions_ReachTheAssistantThroughGetStatus(t *testing.T) {
	e := newEnv(t)
	cs := e.mcpSession()
	created, _ := e.mcpWorkshop(cs)
	var grace struct {
		ParticipantLink string `json:"participant_link"`
	}
	e.tool(cs, "join_workshop", map[string]any{"share_link": created.ShareLink, "display_name": "Grace Liu"}, &grace)
	e.tool(cs, "submit_availability", map[string]any{"participant_link": grace.ParticipantLink, "message": "I can make it either day, ideally in the afternoon."}, nil)
	e.settledOwner(workshopHandle{ownerPath: "/api/v1/owner/" + strings.TrimPrefix(created.OwnerLink, appBaseURL+"/owner/")})

	var status struct {
		Suggestions []suggestionJSON `json:"suggestions"`
	}
	e.tool(cs, "get_status", map[string]any{"owner_link": created.OwnerLink}, &status)
	if len(status.Suggestions) != 1 || status.Suggestions[0].Kind != "clarify" || !strings.HasPrefix(status.Suggestions[0].Message, "Hi Grace!") {
		t.Errorf("get_status suggestions = %+v", status.Suggestions)
	}
}
