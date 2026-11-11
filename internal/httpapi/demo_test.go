package httpapi_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/aalvaropc/commitpool/internal/demo"
)

type demoJSON struct {
	OwnerURL     string `json:"owner_url"`
	ShareURL     string `json:"share_url"`
	Participants []struct {
		Name           string `json:"name"`
		ParticipantURL string `json:"participant_url"`
	} `json:"participants"`
}

func TestDemo_OnlyOnADemoServer(t *testing.T) {
	e := newEnv(t)
	e.wantError(http.MethodPost, "/api/v1/demo/workshops", nil, http.StatusNotFound, "not_found")
	e.wantError(http.MethodGet, "/api/v1/demo", nil, http.StatusNotFound, "not_found")
}

func TestCreateDemo_StartsEmptyWithItsChat(t *testing.T) {
	e := newEnv(t, withDemoMode())
	var info struct {
		Workshops bool `json:"workshops"`
	}
	e.call(http.MethodGet, "/api/v1/demo", nil, http.StatusOK, &info)
	if !info.Workshops {
		t.Fatal("a demo server does not offer demo workshops")
	}

	var d demoJSON
	e.call(http.MethodPost, "/api/v1/demo/workshops", nil, http.StatusCreated, &d)
	if !strings.HasPrefix(d.OwnerURL, appBaseURL+"/owner/") || !strings.HasPrefix(d.ShareURL, appBaseURL+"/w/") {
		t.Fatalf("demo links = %+v", d)
	}
	if d.Participants == nil || len(d.Participants) != 0 {
		t.Errorf("participants = %+v, want an empty list: Iris's people come from her group chat", d.Participants)
	}

	e.background.Wait()
	v := e.owner(demoHandle(d))
	if v.Workshop.HostName != "Iris Calder" || v.Workshop.Title != "Life drawing for beginners" {
		t.Fatalf("workshop = %+v", v.Workshop)
	}
	if len(v.Participants) != 0 || len(v.Availabilities) != 0 || v.ChatImported {
		t.Errorf("a new demo has %d participants, %d answers and chat_imported %v; want none until the chat is read", len(v.Participants), len(v.Availabilities), v.ChatImported)
	}
	var public struct {
		Group struct {
			Answered int `json:"answered"`
		} `json:"group"`
	}
	e.call(http.MethodGet, "/api/v1/w/"+strings.TrimPrefix(d.ShareURL, appBaseURL+"/w/"), nil, http.StatusOK, &public)
	if public.Group.Answered != 0 {
		t.Errorf("answered = %d, want 0", public.Group.Answered)
	}
	if v.DemoChat == nil || !strings.Contains(*v.DemoChat, "] Iris Calder: Life drawing at The Long Room") {
		t.Fatalf("demo_chat = %v, want Iris's group chat", v.DemoChat)
	}
	for _, p := range demo.LifeDrawing.People {
		if !strings.Contains(*v.DemoChat, "] "+p.Name+": "+p.Message+"\n") {
			t.Errorf("demo_chat lacks %s's message %q", p.Name, p.Message)
		}
	}

	if regular := e.owner(e.createWorkshop()); regular.DemoChat != nil {
		t.Errorf("a regular workshop carries demo_chat = %q", *regular.DemoChat)
	}
}

func TestCreateDemo_ImportReadsTheCast(t *testing.T) {
	e := newEnv(t, withDemoMode())
	var d demoJSON
	e.call(http.MethodPost, "/api/v1/demo/workshops", nil, http.StatusCreated, &d)
	h := demoHandle(d)
	fresh := e.owner(h)
	if fresh.DemoChat == nil || len(fresh.Participants) != 0 {
		t.Fatalf("a new demo has demo_chat %v and %d participants, want the chat and nobody yet", fresh.DemoChat, len(fresh.Participants))
	}
	chat := *fresh.DemoChat

	preview := e.previewChat(h, chat)
	if len(preview.People) != 7 || preview.People[0].Name != "Iris Calder" || !preview.People[0].Host {
		t.Fatalf("preview = %+v, want Iris first, as the host, then her six people", preview.People)
	}
	var names []string
	for _, p := range preview.People[1:] {
		if p.Host || p.Joined {
			t.Errorf("%s is marked host %v, joined %v; want neither", p.Name, p.Host, p.Joined)
		}
		names = append(names, p.Name)
	}
	if got := e.importChat(h, chat, names...); len(got.Imported) != 6 {
		t.Fatalf("import = %+v, want the six people", got)
	}

	e.background.Wait()
	v := e.settledOwner(h)
	if len(v.Availabilities) != 6 || v.Reading.Pending != 0 || v.Reading.Total != 6 || !v.ChatImported {
		t.Fatalf("%d of 6 answered, reading %+v, chat_imported %v", len(v.Availabilities), v.Reading, v.ChatImported)
	}
	grace := participantNamed(t, v, "Grace Liu")
	for _, a := range v.Availabilities {
		if a.ParticipantID != grace.ID {
			continue
		}
		asked := a.Interpretation.Ambiguities
		if !a.NeedsClarification || len(asked) == 0 || !strings.Contains(asked[0].Question, "10 AM–12 PM") {
			t.Errorf("Grace's reading = %+v, want unclear with her question about the class hours", a.Interpretation)
		}
	}
	sat := v.Dates[0].ID
	for _, day := range v.Recommendation.PerDate {
		if day.DateID == sat && len(day.Compatible) != 2 {
			t.Errorf("Saturday has %d who can come, want 2", len(day.Compatible))
		}
	}
	if v.Recommendation.DateID != sat || v.Recommendation.MeetsMinimum {
		t.Errorf("recommendation = %+v, want Saturday, one short", v.Recommendation)
	}
}

func TestDemo_AVisitorStartsFiveThenWaits(t *testing.T) {
	e := newEnv(t, withDemoMode(), withRateLimit(120))
	for range 5 {
		e.call(http.MethodPost, "/api/v1/demo/workshops", nil, http.StatusCreated, nil)
	}
	rec := e.request(http.MethodPost, "/api/v1/demo/workshops", nil)
	if rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), "demo_rate_limited") {
		t.Fatalf("sixth demo = %d %s, want 429 demo_rate_limited", rec.Code, rec.Body)
	}
	if wait, _ := strconv.Atoi(rec.Header().Get("Retry-After")); wait < 1100 || wait > 1200 {
		t.Errorf("Retry-After = %q, want about 20 minutes", rec.Header().Get("Retry-After"))
	}
	if !strings.Contains(rec.Body.String(), "20 minutes") {
		t.Errorf("message = %s, want the real wait", rec.Body)
	}
}

type demoInfoJSON struct {
	Workshops  bool    `json:"workshops"`
	VideoURL   *string `json:"video_url"`
	JudgeLogin *struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	} `json:"judge_login"`
}

func TestDemoInfo_OmitsTheJudgeKitUntilItIsConfigured(t *testing.T) {
	e := newEnv(t, withDemoMode())
	rec := e.request(http.MethodGet, "/api/v1/demo", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("demo info = %d %s", rec.Code, rec.Body)
	}
	if body := rec.Body.String(); strings.Contains(body, "video_url") || strings.Contains(body, "judge_login") {
		t.Errorf("demo info without a video or a judge login = %s, want neither key", body)
	}
}

func TestDemoInfo_CarriesTheVideoAndTheSandboxBuyerOnADemoServer(t *testing.T) {
	const video = "https://www.youtube-nocookie.com/embed/aB3_x-9Zk0Q"
	e := newEnv(t, withDemoMode(), withJudgeKit(video, "buyer@example.com", "sandbox-pass"))
	var info demoInfoJSON
	e.call(http.MethodGet, "/api/v1/demo", nil, http.StatusOK, &info)
	if !info.Workshops || info.VideoURL == nil || *info.VideoURL != video {
		t.Errorf("demo info = %+v, want workshops and the embed URL", info)
	}
	if info.JudgeLogin == nil || info.JudgeLogin.Email != "buyer@example.com" || info.JudgeLogin.Password != "sandbox-pass" {
		t.Errorf("judge login = %+v", info.JudgeLogin)
	}
}

func TestDemoInfo_AVideoWithoutALoginShowsTheVideoAlone(t *testing.T) {
	e := newEnv(t, withDemoMode(), withJudgeKit("https://www.youtube-nocookie.com/embed/aB3_x-9Zk0Q", "", ""))
	var info demoInfoJSON
	e.call(http.MethodGet, "/api/v1/demo", nil, http.StatusOK, &info)
	if info.VideoURL == nil || info.JudgeLogin != nil {
		t.Errorf("video without a login = %+v, want the video alone", info)
	}
}

func TestDemoInfo_NeverLeaksTheJudgeKitOutsideDemoMode(t *testing.T) {
	e := newEnv(t, withJudgeKit("https://www.youtube-nocookie.com/embed/aB3_x-9Zk0Q", "buyer@example.com", "sandbox-pass"))
	rec := e.request(http.MethodGet, "/api/v1/demo", nil)
	if rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), "sandbox-pass") || strings.Contains(rec.Body.String(), "youtube") {
		t.Errorf("demo info outside demo mode = %d %s, want a plain 404", rec.Code, rec.Body)
	}
}
