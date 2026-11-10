package demo_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/ai"
	"github.com/aalvaropc/commitpool/internal/demo"
	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/present"
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

type capped struct{ wait time.Duration }

func (capped) Error() string               { return "Claude has read a lot of messages this hour" }
func (capped) Temporary() bool             { return true }
func (capped) Throttled() bool             { return true }
func (c capped) RetryAfter() time.Duration { return c.wait }

type fakeAPI struct {
	calls       []call
	fail        string
	busyOn      map[string]int
	throttledOn map[string]int
	cappedOn    map[string]int
	cappedFor   time.Duration
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
	for key, left := range f.cappedOn {
		if strings.Contains(path, key) && left > 0 {
			f.cappedOn[key] = left - 1
			return capped{wait: f.cappedFor}
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

func classOptions() demo.Options {
	ny, _ := time.LoadLocation("America/New_York")
	return demo.Options{Now: time.Date(2026, 11, 6, 9, 0, 0, 0, ny), Timezone: "America/New_York", WindowMin: 1440, Demo: true}
}

func TestLifeDrawing_IsTheDemoPersona(t *testing.T) {
	m := demo.LifeDrawing
	if m.HostName != "Iris Calder" || m.Title != "Life drawing for beginners" || m.Price != "45.00" || m.VenueCost != "120.00" || m.Minimum != 3 || m.Capacity != 6 ||
		m.StartHour != 10 || m.DurationMin != 120 {
		t.Errorf("Iris = %+v", m)
	}
	if len(m.People) != 6 || m.People[len(m.People)-1].Name != "Grace Liu" || m.People[len(m.People)-1].Expect.Question == "" {
		t.Errorf("people = %+v, want six with Grace's unclear answer last", m.People)
	}
	if grace := m.People[len(m.People)-1].Message; strings.Contains(grace, "afternoon") || !strings.Contains(grace, "might be able to swap one") || !strings.Contains(grace, "Not sure which day") {
		t.Errorf("Grace's message %q should leave open which morning she can make, without a firm limit", grace)
	}
	if len(m.Replies) != 1 || m.Replies[0].Name != "Grace Liu" || m.Replies[0].Expect.Follows != m.People[len(m.People)-1].Message {
		t.Errorf("replies = %+v, want Grace's answer to Claude's question", m.Replies)
	}
	for _, p := range append(slices.Clone(m.People), m.Replies...) {
		if len(p.Expect.Verdicts) != 2 {
			t.Errorf("%s expects %d verdicts, want one for Saturday and one for Sunday", p.Name, len(p.Expect.Verdicts))
		}
	}
}

func classDates(t *testing.T) ([]domain.CandidateDate, []ai.DateOption, *time.Location) {
	t.Helper()
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	sat := demo.FirstSaturday(classOptions().Now, classOptions().WindowMin, demo.LifeDrawing.StartHour, ny)
	var dates []domain.CandidateDate
	var options []ai.DateOption
	for i, id := range []string{"sat", "sun"} {
		start := sat.AddDate(0, 0, i)
		end := start.Add(time.Duration(demo.LifeDrawing.DurationMin) * time.Minute)
		dates = append(dates, domain.CandidateDate{ID: id, StartsAt: start, EndsAt: end})
		options = append(options, ai.DateOption{ID: id, Label: present.DateLabel(start, end, ny), StartsAt: start})
	}
	return dates, options, ny
}

func count(rec domain.Recommendation) map[string][]string {
	out := map[string][]string{}
	for _, d := range rec.PerDate {
		out[d.DateID] = d.Compatible
	}
	return out
}

func TestLifeDrawing_SaturdayIsOneShortUntilGraceAnswers(t *testing.T) {
	dates, options, ny := classDates(t)
	w := domain.Workshop{Minimum: demo.LifeDrawing.Minimum, Capacity: demo.LifeDrawing.Capacity}
	readings := map[string]domain.Interpretation{}
	for _, p := range demo.LifeDrawing.People {
		readings[p.Name] = p.Expect.Reading(options)
	}
	rec := domain.Recommend(w, dates, readings, ny)
	got := count(rec)
	if !slices.Equal(got["sat"], []string{"Ben Whitaker", "Priya Raman"}) || !slices.Equal(got["sun"], []string{"Lucía Torres", "Tomás Silva"}) {
		t.Fatalf("compatible = %v, want Ben and Priya on Saturday, Lucía and Tomás on Sunday", got)
	}
	if rec.DateID != "sat" || rec.MeetsMinimum {
		t.Errorf("recommendation = %s meeting the minimum %v, want Saturday, the earlier of a tie, one short", rec.DateID, rec.MeetsMinimum)
	}
	sat := rec.PerDate[0]
	if sat.Blockers["Hannah Kim"] != "ends after 11:30" || sat.Blockers["Grace Liu"] != "needs clarification" {
		t.Errorf("Saturday blockers = %v, want Hannah's 11:30 limit and Grace's open question", sat.Blockers)
	}
	collecting := w
	collecting.Status, collecting.PaymentWindow = domain.WorkshopCollecting, 24*time.Hour
	now := classOptions().Now
	proposal := domain.Proposal{Minimum: demo.LifeDrawing.Minimum, Capacity: demo.LifeDrawing.Capacity, OpensAt: now, ClosesAt: now.Add(24 * time.Hour), StartsAt: dates[0].StartsAt}
	if err := domain.ValidateProposal(collecting, proposal, dates[0], len(sat.Compatible)); !errors.Is(err, domain.ErrNotEnoughCompatible) {
		t.Errorf("opening Saturday with %d compatible = %v, want refused for lack of people", len(sat.Compatible), err)
	}

	for _, r := range demo.LifeDrawing.Replies {
		readings[r.Name] = r.Expect.Reading(options)
	}
	rec = domain.Recommend(w, dates, readings, ny)
	if got := count(rec); rec.DateID != "sat" || !rec.MeetsMinimum || len(got["sat"]) != 3 || len(got["sun"]) != 2 {
		t.Errorf("after Grace's answer: %s meets %v with %v, want Saturday with 3", rec.DateID, rec.MeetsMinimum, got)
	}
}

func TestScript_PlaysEveryAnswerAndTheReplyInContext(t *testing.T) {
	_, options, _ := classDates(t)
	fake := ai.Fake{Script: demo.Script(demo.LifeDrawing)}
	read := func(message string, earlier *ai.EarlierAnswer) domain.Interpretation {
		t.Helper()
		in, meta, err := fake.Interpret(context.Background(), ai.InterpretInput{Dates: options, Message: message, Earlier: earlier})
		if err != nil || meta.Provider != "fake" {
			t.Fatalf("Interpret(%q) = %+v, %v", message, meta, err)
		}
		return in
	}
	for _, p := range demo.LifeDrawing.People {
		if got := read(p.Message, nil); !reflect.DeepEqual(got, p.Expect.Reading(options)) {
			t.Errorf("%s read as %+v, want %+v", p.Name, got, p.Expect.Reading(options))
		}
	}
	grace := demo.LifeDrawing.People[len(demo.LifeDrawing.People)-1]
	if q := grace.Expect.Question; !strings.Contains(q, "10 AM–12 PM") || !strings.Contains(q, "Saturday") || !strings.Contains(q, "Sunday") {
		t.Errorf("Grace's question %q does not name the class hours and both days", q)
	}
	reply := demo.LifeDrawing.Replies[0]
	earlier := &ai.EarlierAnswer{Message: grace.Message, Questions: []string{grace.Expect.Question}}
	if got := read(reply.Message, earlier); !reflect.DeepEqual(got, reply.Expect.Reading(options)) || got.NeedsClarification {
		t.Errorf("Grace's reply read as %+v, want %+v", got, reply.Expect.Reading(options))
	}
	if got := read(reply.Message, nil); reflect.DeepEqual(got, reply.Expect.Reading(options)) {
		t.Errorf("Grace's reply without her earlier message was read as if in context: %+v", got)
	}
}

func TestReply_SendsEachReplyAsItsPerson(t *testing.T) {
	api := &fakeAPI{}
	people := []demo.Participant{{Name: "Priya Raman", Token: "P1"}, {Name: "Grace Liu", Token: "P6"}}
	if err := demo.Reply(context.Background(), api, people, demo.LifeDrawing.Replies, nil); err != nil {
		t.Fatal(err)
	}
	if len(api.calls) != 1 || api.calls[0].Path != "/api/v1/p/P6/availability" || api.answers()[0] != demo.LifeDrawing.Replies[0].Message {
		t.Errorf("calls = %+v, want Grace's reply on her page", api.calls)
	}
	if err := demo.Reply(context.Background(), &fakeAPI{}, people[:1], demo.LifeDrawing.Replies, nil); err == nil || !strings.Contains(err.Error(), "Grace Liu") {
		t.Errorf("err = %v, want Grace named as missing", err)
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

func TestCreate_SendsTheLifeDrawingWorkshopAndThePriceItUsed(t *testing.T) {
	api := &fakeAPI{}
	res, err := demo.Create(context.Background(), api, demo.LifeDrawing, classOptions())
	if err != nil {
		t.Fatal(err)
	}
	create := api.calls[0]
	if create.Path != "/api/v1/workshops" || create.Body["host_name"] != "Iris Calder" || create.Body["venue_cost"] != "120.00" || create.Body["demo"] != true {
		t.Errorf("create = %+v", create)
	}
	if dates := create.Body["dates"].([]any); len(dates) != 2 {
		t.Errorf("dates = %v, want Saturday and Sunday", dates)
	}
	if res.OwnerURL != "https://cp.test/owner/OWNER" || res.ShareToken != "SHARE" || res.Price != "45.00" {
		t.Errorf("result = %+v", res)
	}
	o := classOptions()
	o.Price = "10.00"
	if res, _ := demo.Create(context.Background(), &fakeAPI{}, demo.LifeDrawing, o); res.Price != "10.00" {
		t.Errorf("price = %q, want the override", res.Price)
	}
}

func TestJoinAndAnswer_InTheScenarioOrder(t *testing.T) {
	api := &fakeAPI{}
	people, err := demo.Join(context.Background(), api, demo.Result{ShareToken: "SHARE"}, demo.LifeDrawing.People)
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
	if got := api.answers(); len(got) != 6 || got[0] != demo.LifeDrawing.People[0].Message || paced != 6 {
		t.Errorf("answers = %v, paced %d", got, paced)
	}
}

func TestJoin_NamesWhoCouldNotJoin(t *testing.T) {
	_, err := demo.Join(context.Background(), &fakeAPI{fail: "/participants"}, demo.Result{ShareToken: "SHARE"}, demo.LifeDrawing.People)
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

func TestLifeDrawing_TheWebGuideQuotesGracesReply(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "lib", "demo.ts"))
	if err != nil {
		t.Fatal(err)
	}
	reply := demo.LifeDrawing.Replies[0]
	if !strings.Contains(string(raw), `"`+reply.Message+`"`) || !strings.Contains(string(raw), `'`+reply.Name+`'`) {
		t.Errorf("web/src/lib/demo.ts does not quote %s's reply %q; the guide's example must be the scenario's reply", reply.Name, reply.Message)
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

func TestAnswer_WaitsAsLongAsTheAPIAsks(t *testing.T) {
	var slept []time.Duration
	demo.SetSleep(t, func(_ context.Context, d time.Duration) error {
		slept = append(slept, d)
		return nil
	})
	people := []demo.Participant{{Name: "Priya", Message: "a", Token: "P1"}, {Name: "Ben", Message: "b", Token: "P2"}}
	api := &fakeAPI{cappedOn: map[string]int{"/p/P1/": 3}, cappedFor: 90 * time.Second}
	var paced int
	if err := demo.Answer(context.Background(), api, people, func(context.Context) error { paced++; return nil }); err != nil {
		t.Fatalf("err = %v, want every answer through", err)
	}
	if got := api.answers(); len(got) != 5 {
		t.Errorf("answers = %v, want Priya four times, then Ben", got)
	}
	if want := []time.Duration{90 * time.Second, 90 * time.Second, 90 * time.Second}; !reflect.DeepEqual(slept, want) {
		t.Errorf("slept %v, want the 90 seconds the API asked for after each refusal", slept)
	}
	if paced != 2 {
		t.Errorf("paced %d times, want one step per person and no backoff on top of the asked wait", paced)
	}
}

func TestAnswer_DoesNotWaitPastItsTime(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	api := &fakeAPI{cappedOn: map[string]int{"/p/P1/": 1}, cappedFor: time.Hour}
	start := time.Now()
	err := demo.Answer(ctx, api, []demo.Participant{{Name: "Priya", Message: "a", Token: "P1"}}, nil)
	if !errors.Is(err, context.DeadlineExceeded) || len(api.answers()) != 1 {
		t.Errorf("err %v after %d tries, want to give up at once when the wait outlasts the time left", err, len(api.answers()))
	}
	if waited := time.Since(start); waited > 5*time.Second {
		t.Errorf("waited %v for an answer that could not arrive in time", waited)
	}
}

func TestFakeScript_ReadsTheLandingQuickRepliesAsClaudeDoes(t *testing.T) {
	dates, options, ny := classDates(t)
	fake := ai.Fake{Script: demo.FakeScript()}
	want := map[string][2]bool{
		"No problem — Saturday works!":       {true, false},
		"El domingo sí puedo":                {false, true},
		"The first one, if we’re done by 11": {false, false},
	}
	for message, counted := range want {
		in, meta, err := fake.Interpret(context.Background(), ai.InterpretInput{Dates: options, Message: message})
		if err != nil || meta.Model != "script-v1" {
			t.Fatalf("Interpret(%q) = %+v, %v; want the scripted reading", message, meta, err)
		}
		for i, d := range dates {
			if ok, _ := domain.Compatible(in, d, ny); ok != counted[i] {
				t.Errorf("%q counted on %s = %v, want %v (%+v)", message, d.ID, ok, counted[i], in.PerDate)
			}
		}
	}
	if len(demo.FakeScript()) != len(demo.Script(demo.LifeDrawing))+len(demo.Landing) {
		t.Errorf("FakeScript has %d answers, want Iris's and the landing's", len(demo.FakeScript()))
	}
}
