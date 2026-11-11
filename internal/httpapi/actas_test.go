package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/aalvaropc/commitpool/internal/demo"
)

type actAsJSON struct {
	Name           string `json:"name"`
	ParticipantURL string `json:"participant_url"`
}

func demoHandle(d demoJSON) workshopHandle {
	return workshopHandle{
		ownerPath: "/api/v1/owner/" + strings.TrimPrefix(d.OwnerURL, appBaseURL+"/owner/"),
		sharePath: "/api/v1/w/" + strings.TrimPrefix(d.ShareURL, appBaseURL+"/w/"),
	}
}

func (e *env) importDemoChat(h workshopHandle) chatImportJSON {
	e.t.Helper()
	chat := e.owner(h).DemoChat
	if chat == nil {
		e.t.Fatal("the demo owner view has no demo_chat")
	}
	return e.importChat(h, *chat, castNames()...)
}

func (e *env) startDemo() (demoJSON, string) {
	e.t.Helper()
	var d demoJSON
	e.call(http.MethodPost, "/api/v1/demo/workshops", nil, http.StatusCreated, &d)
	h := demoHandle(d)
	d.Participants = e.importDemoChat(h).Imported
	e.background.Wait()
	return d, h.ownerPath
}

func participantToken(url string) string {
	return strings.TrimPrefix(url, appBaseURL+"/p/")
}

func TestDemo_OwnerViewCarriesTheSixWorkingLinks(t *testing.T) {
	e := newEnv(t, withDemoMode())
	d, ownerPath := e.startDemo()
	var v struct {
		ActAs []actAsJSON `json:"act_as"`
	}
	e.call(http.MethodGet, ownerPath, nil, http.StatusOK, &v)
	if len(v.ActAs) != len(demo.LifeDrawing.People) {
		t.Fatalf("act_as = %+v, want the %d people of the demo", v.ActAs, len(demo.LifeDrawing.People))
	}
	for i, p := range v.ActAs {
		if p.Name != demo.LifeDrawing.People[i].Name || p.ParticipantURL != d.Participants[i].ParticipantURL {
			t.Errorf("act_as[%d] = %+v, want %s at %s", i, p, demo.LifeDrawing.People[i].Name, d.Participants[i].ParticipantURL)
		}
		var pv participantViewJSON
		e.call(http.MethodGet, "/api/v1/p/"+participantToken(p.ParticipantURL), nil, http.StatusOK, &pv)
		if pv.Participant.DisplayName != p.Name {
			t.Errorf("the link for %s opens %s's page", p.Name, pv.Participant.DisplayName)
		}
	}
}

func TestOwner_ARegularWorkshopHasNoActAs(t *testing.T) {
	e := newEnv(t, withDemoMode())
	h := e.createWorkshop()
	e.joinAndSay(h, "Ana", "Any day works for me")
	rec := e.ownerRequest(h)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode owner view %s: %v", rec.Body, err)
	}
	if got, ok := raw["act_as"]; ok {
		t.Errorf("a regular owner view carries act_as = %s", got)
	}
}

func TestDemo_SomeoneWhoJoinsThroughTheInviteIsNotOffered(t *testing.T) {
	e := newEnv(t, withDemoMode())
	d, ownerPath := e.startDemo()
	visitor := e.join(workshopHandle{sharePath: "/api/v1/w/" + strings.TrimPrefix(d.ShareURL, appBaseURL+"/w/")}, "A judge")
	rec := e.request(http.MethodGet, ownerPath, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("owner view = %d %s", rec.Code, rec.Body)
	}
	if body := rec.Body.String(); strings.Contains(body, visitor.token) {
		t.Errorf("the owner view offers the link of someone who joined through the invite:\n%s", body)
	}
	var v struct {
		ActAs []actAsJSON `json:"act_as"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil || len(v.ActAs) != len(demo.LifeDrawing.People) {
		t.Errorf("act_as = %+v, %v; want only the demo's people", v.ActAs, err)
	}
}

func TestDemo_ParticipantLinksStayWithTheOwner(t *testing.T) {
	e := newEnv(t, withDemoMode())
	d, _ := e.startDemo()
	tokens := make([]string, len(d.Participants))
	for i, p := range d.Participants {
		tokens[i] = participantToken(p.ParticipantURL)
	}
	leaks := func(where, body string, except int) {
		t.Helper()
		for i, tok := range tokens {
			if i != except && strings.Contains(body, tok) {
				t.Errorf("%s carries %s's link", where, d.Participants[i].Name)
			}
		}
	}

	public := e.request(http.MethodGet, "/api/v1/w/"+strings.TrimPrefix(d.ShareURL, appBaseURL+"/w/"), nil)
	if public.Code != http.StatusOK {
		t.Fatalf("public view = %d %s", public.Code, public.Body)
	}
	leaks("the public view", public.Body.String(), -1)

	for i, tok := range tokens {
		rec := e.request(http.MethodGet, "/api/v1/p/"+tok, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("participant view = %d %s", rec.Code, rec.Body)
		}
		leaks(d.Participants[i].Name+"'s view", rec.Body.String(), i)
	}

	cs := e.mcpSession()
	res := e.tool(cs, "get_status", map[string]any{"owner_link": d.OwnerURL}, nil)
	if res.IsError {
		t.Fatalf("get_status: %s", resultText(res))
	}
	structured, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(structured), "Life drawing for beginners") {
		t.Fatalf("get_status did not describe the demo; the check would be vacuous: %s", structured)
	}
	leaks("get_status", resultText(res)+string(structured), -1)
}
