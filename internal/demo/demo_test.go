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

type busy struct{}

func (busy) Error() string   { return "every reader is busy" }
func (busy) Temporary() bool { return true }

type throttled struct{}

func (throttled) Error() string   { return "Claude is reading a lot of messages right now" }
func (throttled) Temporary() bool { return true }
func (throttled) Throttled() bool { return true }

type fakeAPI struct {
	calls       []call
	fail        string
	busyOn      map[string]int
	throttledOn map[string]int
}

func (f *fakeAPI) Call(_ context.Context, method, path string, body, out any) error {
	raw, _ := json.Marshal(body)
	var b map[string]any
	_ = json.Unmarshal(raw, &b)
	f.calls = append(f.calls, call{method, path, b})
	if f.fail != "" && strings.Contains(path, f.fail) {
		return errors.New("refused")
	}
	for key, left := range f.busyOn {
		if strings.Contains(path, key) && left > 0 {
			f.busyOn[key] = left - 1
			return busy{}
		}
	}
	for key, left := range f.throttledOn {
		if strings.Contains(path, key) && left > 0 {
			f.throttledOn[key] = left - 1
			return throttled{}
		}
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

func (f *fakeAPI) answers() []string {
	var out []string
	for _, c := range f.calls {
		if strings.HasSuffix(c.Path, "/availability") {
			out = append(out, c.Body["message"].(string))
		}
	}
	return out
}

func mayaOptions() demo.Options {
	ny, _ := time.LoadLocation("America/New_York")
	return demo.Options{Now: time.Date(2026, 11, 6, 9, 0, 0, 0, ny), Timezone: "America/New_York", WindowMin: 1440, Demo: true}
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

func TestCreate_SendsMayasWorkshopAndThePriceItUsed(t *testing.T) {
	api := &fakeAPI{}
	res, err := demo.Create(context.Background(), api, demo.Maya, mayaOptions())
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
	if res.OwnerURL != "https://cp.test/owner/OWNER" || res.ShareToken != "SHARE" || res.Price != "45.00" {
		t.Errorf("result = %+v", res)
	}
	o := mayaOptions()
	o.Price = "10.00"
	if res, _ := demo.Create(context.Background(), &fakeAPI{}, demo.Maya, o); res.Price != "10.00" {
		t.Errorf("price = %q, want the override", res.Price)
	}
}

func TestJoinAndAnswer_InTheScenarioOrder(t *testing.T) {
	api := &fakeAPI{}
	people, err := demo.Join(context.Background(), api, demo.Result{ShareToken: "SHARE"}, demo.Maya.People)
	if err != nil {
		t.Fatal(err)
	}
	if len(people) != 6 || people[0].Name != "Priya Raman" || people[0].Token == "" {
		t.Fatalf("people = %+v", people)
	}
	var paced int
	if err := demo.Answer(context.Background(), api, people, func(context.Context) error { paced++; return nil }); err != nil {
		t.Fatal(err)
	}
	if got := api.answers(); len(got) != 6 || got[0] != demo.Maya.People[0].Message || paced != 6 {
		t.Errorf("answers = %v, paced %d", got, paced)
	}
}

func TestJoin_NamesWhoCouldNotJoin(t *testing.T) {
	_, err := demo.Join(context.Background(), &fakeAPI{fail: "/participants"}, demo.Result{ShareToken: "SHARE"}, demo.Maya.People)
	if err == nil || !strings.Contains(err.Error(), "Priya Raman") {
		t.Fatalf("err = %v, want the first person named", err)
	}
}

func TestAnswer_WaitsOutABusyReaderAndMovesOnPastARefusal(t *testing.T) {
	people := []demo.Participant{{Name: "Priya", Message: "a", Token: "P1"}, {Name: "Ben", Message: "b", Token: "P2"}, {Name: "Lucía", Message: "c", Token: "P3"}}
	api := &fakeAPI{busyOn: map[string]int{"/p/P1/": 2}, fail: "/p/P2/"}
	var paced int
	err := demo.Answer(context.Background(), api, people, func(context.Context) error { paced++; return nil })
	if err == nil || !strings.Contains(err.Error(), "Ben") || strings.Contains(err.Error(), "Priya") {
		t.Fatalf("err = %v, want only Ben's refusal", err)
	}
	if got := api.answers(); len(got) != 5 || got[4] != "c" {
		t.Errorf("answers = %v, want Priya three times, Ben once, then Lucía", got)
	}
	if paced != 3+1+2 {
		t.Errorf("paced %d times, want one step per person plus 1 and 2 steps of backoff", paced)
	}
}

func TestAnswer_GivesUpOnAReaderThatStaysBusy(t *testing.T) {
	people := []demo.Participant{{Name: "Priya", Message: "a", Token: "P1"}}
	api := &fakeAPI{busyOn: map[string]int{"/p/P1/": 10}}
	if err := demo.Answer(context.Background(), api, people, nil); err == nil || len(api.answers()) != 4 {
		t.Errorf("err %v after %d tries, want an error after 4", err, len(api.answers()))
	}
}

func TestAnswer_StopsWhenTheTimeIsUp(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	api := &fakeAPI{}
	err := demo.Answer(ctx, api, []demo.Participant{{Name: "Priya", Token: "P1"}}, func(ctx context.Context) error { return ctx.Err() })
	if !errors.Is(err, context.Canceled) || len(api.calls) != 0 {
		t.Errorf("err %v with %d calls, want cancelled before any call", err, len(api.calls))
	}
}

func TestAnswer_KeepsWaitingWhileClaudeIsThrottled(t *testing.T) {
	people := []demo.Participant{{Name: "Priya", Message: "a", Token: "P1"}, {Name: "Ben", Message: "b", Token: "P2"}}
	api := &fakeAPI{throttledOn: map[string]int{"/p/P1/": 9}}
	var paced int
	if err := demo.Answer(context.Background(), api, people, func(context.Context) error { paced++; return nil }); err != nil {
		t.Fatalf("err = %v, want every answer through once Claude has room", err)
	}
	if got := api.answers(); len(got) != 11 || got[9] != "a" || got[10] != "b" {
		t.Errorf("answers = %v, want Priya ten times, then Ben", got)
	}
	if want := 2 + 9*10/2; paced != want {
		t.Errorf("paced %d times, want %d: one step per person and a longer wait after each refusal", paced, want)
	}
}

func TestAnswer_StopsWaitingForClaudeWhenTheTimeIsUp(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	api := &fakeAPI{throttledOn: map[string]int{"/p/P1/": 1000}}
	calls := 0
	err := demo.Answer(ctx, api, []demo.Participant{{Name: "Priya", Message: "a", Token: "P1"}}, func(ctx context.Context) error {
		if calls++; calls > 20 {
			cancel()
		}
		return ctx.Err()
	})
	if !errors.Is(err, context.Canceled) || len(api.answers()) > 20 {
		t.Errorf("err %v after %d tries, want the deadline to end the wait", err, len(api.answers()))
	}
}

func TestAnswer_GivesUpOnAThrottledReaderAfterManyTries(t *testing.T) {
	api := &fakeAPI{throttledOn: map[string]int{"/p/P1/": 1000}}
	err := demo.Answer(context.Background(), api, []demo.Participant{{Name: "Priya", Message: "a", Token: "P1"}}, nil)
	if err == nil || len(api.answers()) != 30 {
		t.Errorf("err %v after %d tries, want an error after 30", err, len(api.answers()))
	}
}
