package demo_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

type fakeAPI struct {
	calls  []call
	fail   string
	busyOn map[string]int
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
	if m.HostName != "Maya Okafor" || m.Title != "Wheel-throwing for beginners" || m.Price != "45.00" || m.VenueCost != "120.00" || m.Minimum != 3 || m.Capacity != 6 ||
		m.StartHour != 10 || m.DurationMin != 120 {
		t.Errorf("Maya = %+v", m)
	}
	if len(m.People) != 6 || m.People[len(m.People)-1].Name != "Grace Liu" || m.People[len(m.People)-1].Expect.Question == "" {
		t.Errorf("people = %+v, want six with Grace's unclear answer last", m.People)
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

func mayaDates(t *testing.T) ([]domain.CandidateDate, []ai.DateOption, *time.Location) {
	t.Helper()
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	sat := demo.FirstSaturday(mayaOptions().Now, mayaOptions().WindowMin, demo.Maya.StartHour, ny)
	var dates []domain.CandidateDate
	var options []ai.DateOption
	for i, id := range []string{"sat", "sun"} {
		start := sat.AddDate(0, 0, i)
		end := start.Add(time.Duration(demo.Maya.DurationMin) * time.Minute)
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

func TestMaya_SaturdayIsOneShortUntilGraceAnswers(t *testing.T) {
	dates, options, ny := mayaDates(t)
	w := domain.Workshop{Minimum: demo.Maya.Minimum, Capacity: demo.Maya.Capacity}
	readings := map[string]domain.Interpretation{}
	for _, p := range demo.Maya.People {
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
	now := mayaOptions().Now
	proposal := domain.Proposal{Minimum: demo.Maya.Minimum, Capacity: demo.Maya.Capacity, OpensAt: now, ClosesAt: now.Add(24 * time.Hour), StartsAt: dates[0].StartsAt}
	if err := domain.ValidateProposal(collecting, proposal, dates[0], len(sat.Compatible)); !errors.Is(err, domain.ErrNotEnoughCompatible) {
		t.Errorf("opening Saturday with %d compatible = %v, want refused for lack of people", len(sat.Compatible), err)
	}

	for _, r := range demo.Maya.Replies {
		readings[r.Name] = r.Expect.Reading(options)
	}
	rec = domain.Recommend(w, dates, readings, ny)
	if got := count(rec); rec.DateID != "sat" || !rec.MeetsMinimum || len(got["sat"]) != 3 || len(got["sun"]) != 2 {
		t.Errorf("after Grace's answer: %s meets %v with %v, want Saturday with 3", rec.DateID, rec.MeetsMinimum, got)
	}
}

func TestScript_PlaysEveryAnswerAndTheReplyInContext(t *testing.T) {
	_, options, _ := mayaDates(t)
	fake := ai.Fake{Script: demo.Script(demo.Maya)}
	read := func(message string, earlier *ai.EarlierAnswer) domain.Interpretation {
		t.Helper()
		in, meta, err := fake.Interpret(context.Background(), ai.InterpretInput{Dates: options, Message: message, Earlier: earlier})
		if err != nil || meta.Provider != "fake" {
			t.Fatalf("Interpret(%q) = %+v, %v", message, meta, err)
		}
		return in
	}
	for _, p := range demo.Maya.People {
		if got := read(p.Message, nil); !reflect.DeepEqual(got, p.Expect.Reading(options)) {
			t.Errorf("%s read as %+v, want %+v", p.Name, got, p.Expect.Reading(options))
		}
	}
	grace := demo.Maya.People[len(demo.Maya.People)-1]
	if q := grace.Expect.Question; !strings.Contains(q, "10 AM–12 PM") || !strings.Contains(q, "Saturday") || !strings.Contains(q, "Sunday") {
		t.Errorf("Grace's question %q does not name the class hours and both days", q)
	}
	reply := demo.Maya.Replies[0]
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
	if err := demo.Reply(context.Background(), api, people, demo.Maya.Replies, nil); err != nil {
		t.Fatal(err)
	}
	if len(api.calls) != 1 || api.calls[0].Path != "/api/v1/p/P6/availability" || api.answers()[0] != demo.Maya.Replies[0].Message {
		t.Errorf("calls = %+v, want Grace's reply on her page", api.calls)
	}
	if err := demo.Reply(context.Background(), &fakeAPI{}, people[:1], demo.Maya.Replies, nil); err == nil || !strings.Contains(err.Error(), "Grace Liu") {
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
