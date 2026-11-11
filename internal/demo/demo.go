package demo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/aalvaropc/commitpool/internal/ai"
	"github.com/aalvaropc/commitpool/internal/domain"
)

type Caller interface {
	Call(ctx context.Context, method, path string, body, out any) error
}

type Person struct {
	Name, Message string
	Expect        ai.Scripted
}

type Scenario struct {
	Title, Description, HostName, Location string
	DurationMin, Minimum, Capacity         int
	StartHour                              int
	Price, VenueCost, Currency             string
	Opener, Closing                        string
	People                                 []Person
	Replies                                []Person
}

const (
	chatStartsAtHour   = 19
	chatStartsAtMinute = 5
	chatStartsAtSecond = 14
	repliesAtHour      = 19
	repliesAtMinute    = 52
	repliesAtSecond    = 7
)

var chatGaps = []time.Duration{
	3*time.Minute + 12*time.Second,
	4*time.Minute + 40*time.Second,
	2*time.Minute + 5*time.Second,
	6*time.Minute + 31*time.Second,
	time.Minute + 48*time.Second,
	5*time.Minute + 3*time.Second,
	11*time.Minute + 41*time.Second,
}

type chatLine struct {
	name, text string
	at         time.Time
}

func (sc Scenario) chatLines(now time.Time, loc *time.Location) ([]chatLine, time.Time) {
	lines := []chatLine{{name: sc.HostName, text: sc.Opener}}
	for _, p := range sc.People {
		lines = append(lines, chatLine{name: p.Name, text: p.Message})
	}
	lines = append(lines, chatLine{name: sc.HostName, text: sc.Closing})
	today := now.In(loc)
	evening := time.Date(today.Year(), today.Month(), today.Day()-1, chatStartsAtHour, chatStartsAtMinute, chatStartsAtSecond, 0, loc)
	at := evening
	for i := range lines {
		lines[i].at = at
		at = at.Add(chatGaps[i%len(chatGaps)])
	}
	return lines, evening
}

func writeChat(lines []chatLine) string {
	var b strings.Builder
	for i, l := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "[%s\u202f%s] %s: %s", l.at.Format("1/2/06, 3:04:05"), l.at.Format("PM"), l.name, l.text)
	}
	return b.String()
}

func (sc Scenario) Chat(now time.Time, loc *time.Location) string {
	lines, _ := sc.chatLines(now, loc)
	return writeChat(lines)
}

func (sc Scenario) ChatWithReplies(now time.Time, loc *time.Location) string {
	lines, evening := sc.chatLines(now, loc)
	at := time.Date(evening.Year(), evening.Month(), evening.Day(), repliesAtHour, repliesAtMinute, repliesAtSecond, 0, loc)
	for i, r := range sc.Replies {
		lines = append(lines, chatLine{name: r.Name, text: r.Message, at: at})
		at = at.Add(chatGaps[i%len(chatGaps)])
	}
	return writeChat(lines)
}

var noCondition = domain.Condition{Type: domain.ConditionNone}

func can(evidence string) domain.DateVerdict {
	return domain.DateVerdict{Verdict: domain.VerdictAvailable, Condition: noCondition, Evidence: evidence}
}

func cannot(evidence string) domain.DateVerdict {
	return domain.DateVerdict{Verdict: domain.VerdictUnavailable, Condition: noCondition, Evidence: evidence}
}

func endsBy(hhmm, evidence string) domain.DateVerdict {
	return domain.DateVerdict{Verdict: domain.VerdictConditional, Condition: domain.Condition{Type: domain.ConditionEndBefore, Time: hhmm}, Evidence: evidence}
}

func unsure(evidence string) domain.DateVerdict {
	return domain.DateVerdict{Verdict: domain.VerdictUnknown, Condition: noCondition, Evidence: evidence}
}

func verdicts(v ...domain.DateVerdict) []domain.DateVerdict { return v }

const graceMessage = "Count me in! Mornings are tricky — I'm usually at the market — but I might be able to swap one. Not sure which day yet."

var LifeDrawing = Scenario{
	Title:       "Life drawing for beginners",
	Description: "Two hours of short and long poses with a live model, paper and charcoal included. No experience needed.",
	HostName:    "Iris Calder",
	Location:    "The Long Room · Red Hook, Brooklyn",
	DurationMin: 120,
	Minimum:     3,
	Capacity:    6,
	StartHour:   10,
	Price:       "45.00",
	VenueCost:   "120.00",
	Currency:    "USD",
	Opener:      "Life drawing at The Long Room, Saturday or Sunday 10–12, $45. Who's in?",
	Closing:     "Thanks all! 🎨",
	People: []Person{
		{
			Name: "Priya Raman", Message: "Saturday works for me, Sunday I can't.",
			Expect: ai.Scripted{Verdicts: verdicts(can("Saturday works for me"), cannot("Sunday I can't"))},
		},
		{
			Name: "Ben Whitaker", Message: "The first date works if we're done before 6; the second one I'm away.",
			Expect: ai.Scripted{
				Verdicts:        verdicts(endsBy("18:00", "The first date works if we're done before 6"), cannot("the second one I'm away")),
				HardConstraints: []string{"done before 6 PM"},
			},
		},
		{
			Name: "Lucía Torres", Message: "El sábado no puedo, pero el domingo me viene perfecto.",
			Expect: ai.Scripted{Verdicts: verdicts(cannot("El sábado no puedo"), can("el domingo me viene perfecto"))},
		},
		{
			Name: "Hannah Kim", Message: "Saturday only if we're done by 11:30 — I have a lunch at noon. Sunday is out.",
			Expect: ai.Scripted{
				Verdicts:        verdicts(endsBy("11:30", "Saturday only if we're done by 11:30"), cannot("Sunday is out")),
				HardConstraints: []string{"done by 11:30 AM on Saturday"},
			},
		},
		{
			Name: "Tomás Silva", Message: "Sunday's good. Saturday I'm driving my daughter to a tournament all day.",
			Expect: ai.Scripted{Verdicts: verdicts(cannot("Saturday I'm driving my daughter to a tournament all day"), can("Sunday's good"))},
		},
		{
			Name: "Grace Liu", Message: graceMessage,
			Expect: ai.Scripted{
				Verdicts: verdicts(unsure("Mornings are tricky — I'm usually at the market"), unsure("Not sure which day yet")),
				Question: "The class runs 10 AM–12 PM on Saturday and Sunday — can you make either morning, and which?",
				Why:      "She might swap one market morning but has not said which, and the class only runs in the morning.",
			},
		},
	},
	Replies: []Person{
		{
			Name: "Grace Liu", Message: "No problem — I swapped Saturday. Sunday I'm still at the market.",
			Expect: ai.Scripted{
				Verdicts: verdicts(can("I swapped Saturday"), cannot("Sunday I'm still at the market")),
				Follows:  graceMessage,
			},
		},
	},
}

var Landing = []Person{
	{
		Name: "Landing visitor", Message: "No problem — Saturday works!",
		Expect: ai.Scripted{Verdicts: verdicts(can("Saturday works"), unsure(""))},
	},
	{
		Name: "Landing visitor", Message: "El domingo sí puedo",
		Expect: ai.Scripted{Verdicts: verdicts(unsure(""), can("El domingo sí puedo"))},
	},
	{
		Name: "Landing visitor", Message: "The first one, if we're done by 11",
		Expect: ai.Scripted{
			Verdicts:        verdicts(endsBy("11:00", "The first one, if we're done by 11"), unsure("")),
			HardConstraints: []string{"done by 11 AM"},
		},
	},
}

func Script(sc Scenario) map[string]ai.Scripted {
	out := make(map[string]ai.Scripted, len(sc.People)+len(sc.Replies))
	for _, p := range slices.Concat(sc.People, sc.Replies) {
		out[p.Message] = p.Expect
	}
	return out
}

func FakeScript() map[string]ai.Scripted {
	out := Script(LifeDrawing)
	for _, p := range Landing {
		out[p.Message] = p.Expect
	}
	return out
}

type Options struct {
	Now       time.Time
	Timezone  string
	WindowMin int
	Price     string
	Demo      bool
}

type Participant struct {
	Name, Message, URL, Token string
}

type Result struct {
	OwnerURL, ShareURL     string
	OwnerToken, ShareToken string
	Price                  string
}

const PreparedNote = "Prepared demo data: Iris, her class and the six participants were written for this demo."

const (
	answerAttempts    = 4
	throttledAttempts = 30
)

type temporary interface {
	Temporary() bool
}

type throttled interface {
	Throttled() bool
}

type retryAfter interface {
	RetryAfter() time.Duration
}

type Pace func(context.Context) error

func FirstSaturday(now time.Time, windowMin, startHour int, loc *time.Location) time.Time {
	earliest := now.Add(time.Duration(windowMin)*time.Minute + 3*24*time.Hour).In(loc)
	day := time.Date(earliest.Year(), earliest.Month(), earliest.Day(), startHour, 0, 0, 0, loc)
	if day.Before(earliest) {
		day = day.AddDate(0, 0, 1)
	}
	for day.Weekday() != time.Saturday {
		day = day.AddDate(0, 0, 1)
	}
	return day
}

func Create(ctx context.Context, api Caller, sc Scenario, o Options) (Result, error) {
	loc, err := time.LoadLocation(o.Timezone)
	if err != nil {
		return Result{}, fmt.Errorf("time zone %q: %w", o.Timezone, err)
	}
	price := sc.Price
	if o.Price != "" {
		price = o.Price
	}
	saturday := FirstSaturday(o.Now, o.WindowMin, sc.StartHour, loc)
	type date struct {
		StartsAt time.Time `json:"starts_at"`
		EndsAt   time.Time `json:"ends_at"`
	}
	dates := []date{}
	for _, start := range []time.Time{saturday, saturday.AddDate(0, 0, 1)} {
		dates = append(dates, date{StartsAt: start, EndsAt: start.Add(time.Duration(sc.DurationMin) * time.Minute)})
	}
	var created struct {
		OwnerURL string `json:"owner_url"`
		ShareURL string `json:"share_url"`
	}
	err = api.Call(ctx, http.MethodPost, "/api/v1/workshops", map[string]any{
		"title":              sc.Title,
		"description":        sc.Description,
		"host_name":          sc.HostName,
		"location":           sc.Location,
		"duration_min":       sc.DurationMin,
		"timezone":           o.Timezone,
		"minimum":            sc.Minimum,
		"capacity":           sc.Capacity,
		"price":              price,
		"venue_cost":         sc.VenueCost,
		"currency":           sc.Currency,
		"payment_window_min": o.WindowMin,
		"demo":               o.Demo,
		"dates":              dates,
	}, &created)
	if err != nil {
		return Result{}, fmt.Errorf("create the workshop: %w", err)
	}
	return Result{
		OwnerURL: created.OwnerURL, ShareURL: created.ShareURL,
		OwnerToken: path.Base(created.OwnerURL), ShareToken: path.Base(created.ShareURL),
		Price: price,
	}, nil
}

func Join(ctx context.Context, api Caller, res Result, people []Person) ([]Participant, error) {
	out := make([]Participant, 0, len(people))
	for _, p := range people {
		var joined struct {
			ParticipantURL string `json:"participant_url"`
		}
		if err := api.Call(ctx, http.MethodPost, "/api/v1/w/"+res.ShareToken+"/participants", map[string]any{"display_name": p.Name}, &joined); err != nil {
			return out, fmt.Errorf("join %s: %w", p.Name, err)
		}
		out = append(out, Participant{Name: p.Name, Message: p.Message, URL: joined.ParticipantURL, Token: path.Base(joined.ParticipantURL)})
	}
	return out, nil
}

func Answer(ctx context.Context, api Caller, people []Participant, pace Pace) error {
	var errs []error
	for _, p := range people {
		if err := wait(ctx, pace, 1); err != nil {
			return errors.Join(append(errs, err)...)
		}
		if err := AnswerOne(ctx, api, p, pace); err != nil {
			errs = append(errs, fmt.Errorf("availability of %s: %w", p.Name, err))
		}
	}
	return errors.Join(errs...)
}

func Reply(ctx context.Context, api Caller, people []Participant, replies []Person, pace Pace) error {
	var errs []error
	for _, r := range replies {
		i := slices.IndexFunc(people, func(p Participant) bool { return p.Name == r.Name })
		if i < 0 {
			errs = append(errs, fmt.Errorf("reply of %s: nobody by that name joined", r.Name))
			continue
		}
		p := people[i]
		p.Message = r.Message
		if err := AnswerOne(ctx, api, p, pace); err != nil {
			errs = append(errs, fmt.Errorf("reply of %s: %w", r.Name, err))
		}
	}
	return errors.Join(errs...)
}

func AnswerOne(ctx context.Context, api Caller, p Participant, pace Pace) error {
	for attempt := 1; ; attempt++ {
		err := api.Call(ctx, http.MethodPost, "/api/v1/p/"+p.Token+"/availability", map[string]any{"message": p.Message}, nil)
		if err == nil || attempt >= attempts(err) {
			return err
		}
		if werr := backoff(ctx, err, pace, attempt); werr != nil {
			return errors.Join(err, werr)
		}
	}
}

func backoff(ctx context.Context, err error, pace Pace, attempt int) error {
	var asked retryAfter
	if errors.As(err, &asked) && asked.RetryAfter() > 0 {
		return sleep(ctx, asked.RetryAfter())
	}
	return wait(ctx, pace, attempt)
}

var sleep = func(ctx context.Context, d time.Duration) error {
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < d {
		return fmt.Errorf("the %s the API asked to wait outlasts the time left: %w", d, context.DeadlineExceeded)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func attempts(err error) int {
	var th throttled
	if errors.As(err, &th) && th.Throttled() {
		return throttledAttempts
	}
	var t temporary
	if errors.As(err, &t) && t.Temporary() {
		return answerAttempts
	}
	return 1
}

func wait(ctx context.Context, pace Pace, steps int) error {
	if pace == nil {
		return ctx.Err()
	}
	for range steps {
		if err := pace(ctx); err != nil {
			return err
		}
	}
	return nil
}
