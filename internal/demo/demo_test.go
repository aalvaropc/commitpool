package demo_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/demo"
)

type call struct {
	Method, Path string
	Body         map[string]any
}

type fakeAPI struct {
	calls []call
	fail  string
}

func (f *fakeAPI) Call(_ context.Context, method, path string, body, out any) error {
	raw, _ := json.Marshal(body)
	var b map[string]any
	_ = json.Unmarshal(raw, &b)
	f.calls = append(f.calls, call{method, path, b})
	if f.fail != "" && strings.Contains(path, f.fail) {
		return errors.New("refused")
	}
	var answer any
	switch {
	case path == "/api/v1/workshops":
		answer = map[string]string{"owner_url": "https://cp.test/owner/OWNER", "share_url": "https://cp.test/w/SHARE"}
	case strings.HasSuffix(path, "/participants"):
		answer = map[string]string{"participant_url": fmt.Sprintf("https://cp.test/p/P%d", len(f.calls))}
	default:
		return nil
	}
	raw, _ = json.Marshal(answer)
	return json.Unmarshal(raw, out)
}

func TestMaya_IsTheDemoPersona(t *testing.T) {
	m := demo.Maya
	if m.HostName != "Maya Okafor" || m.Title != "Wheel-throwing for beginners" || m.Price != "45.00" || m.VenueCost != "120.00" || m.Minimum != 3 || m.Capacity != 6 {
		t.Errorf("Maya = %+v", m)
	}
	if len(m.People) != 6 || !strings.Contains(m.People[len(m.People)-1].Message, "afternoon") {
		t.Errorf("people = %+v, want six with the vague answer last", m.People)
	}
}

func TestFirstSaturday_LeavesTheWindowAndLeadTime(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	now := time.Date(2026, 11, 6, 9, 0, 0, 0, ny)
	got := demo.FirstSaturday(now, 1440, 10, ny)
	if got.Weekday() != time.Saturday || !got.After(now.Add(24*time.Hour+3*24*time.Hour)) || got.Hour() != 10 {
		t.Errorf("first Saturday = %s", got)
	}
}

func TestRun_CreatesJoinsAndAnswersInOrder(t *testing.T) {
	api := &fakeAPI{}
	ny, _ := time.LoadLocation("America/New_York")
	var paced int
	res, err := demo.Run(context.Background(), api, demo.Maya, demo.Options{
		Now: time.Date(2026, 11, 6, 9, 0, 0, 0, ny), Timezone: "America/New_York", WindowMin: 1440, Demo: true,
	}, func(context.Context) error { paced++; return nil })
	if err != nil {
		t.Fatal(err)
	}
	create := api.calls[0]
	if create.Path != "/api/v1/workshops" || create.Body["host_name"] != "Maya Okafor" || create.Body["venue_cost"] != "120.00" || create.Body["demo"] != true {
		t.Errorf("create = %+v", create)
	}
	if dates := create.Body["dates"].([]any); len(dates) != 2 {
		t.Errorf("dates = %v, want Saturday and Sunday", dates)
	}
	if res.OwnerURL != "https://cp.test/owner/OWNER" || len(res.Participants) != 6 || paced != 6 {
		t.Errorf("result = %+v, paced %d", res, paced)
	}
	if res.Participants[0].Name != "Priya Raman" || res.Participants[0].Token == "" {
		t.Errorf("first participant = %+v", res.Participants[0])
	}
	var answers []string
	for _, c := range api.calls {
		if strings.HasSuffix(c.Path, "/availability") {
			answers = append(answers, c.Body["message"].(string))
		}
	}
	if len(answers) != 6 || answers[0] != demo.Maya.People[0].Message {
		t.Errorf("answers = %v", answers)
	}
}

func TestRun_StopsAtTheFirstRefusal(t *testing.T) {
	api := &fakeAPI{fail: "/participants"}
	_, err := demo.Run(context.Background(), api, demo.Maya, demo.Options{Now: time.Now(), Timezone: "UTC", WindowMin: 60}, nil)
	if err == nil || !strings.Contains(err.Error(), "Priya Raman") {
		t.Fatalf("err = %v, want the join of the first person named", err)
	}
}
