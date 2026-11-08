package ai_test

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/ai"
	"github.com/aalvaropc/commitpool/internal/config"
	"github.com/aalvaropc/commitpool/internal/demo"
	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/present"
)

func liveBedrock(t *testing.T) (*ai.Bedrock, context.Context) {
	t.Helper()
	if os.Getenv("COMMITPOOL_LIVE_AI") != "1" {
		t.Skip("set COMMITPOOL_LIVE_AI=1, AWS_REGION and AWS credentials to call Bedrock for real")
	}
	region := os.Getenv("AWS_REGION")
	if region == "" {
		t.Fatal("COMMITPOOL_LIVE_AI=1 needs AWS_REGION (and AWS credentials) to reach Bedrock")
	}
	model := envOr("COMMITPOOL_BEDROCK_MODEL", config.DefaultBedrockModel)
	fallback := envOr("COMMITPOOL_BEDROCK_FALLBACK_MODEL", config.DefaultBedrockFallbackModel)
	effort := envOr("COMMITPOOL_BEDROCK_EFFORT", "none")
	if effort == "none" {
		effort = ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)

	b, err := ai.NewBedrock(ctx, ai.BedrockConfig{
		Region: region, Model: model, FallbackModel: fallback, Effort: effort,
		Log: slog.New(slog.NewTextHandler(os.Stderr, nil)),
	})
	if err != nil {
		t.Fatalf("NewBedrock: %v", err)
	}
	return b, ctx
}

func TestBedrock_Live(t *testing.T) {
	b, ctx := liveBedrock(t)

	in, meta, err := b.Interpret(ctx, ai.InterpretInput{
		WorkshopTitle: "Go for operators",
		Timezone:      "America/Lima",
		Dates:         dates,
		Message:       "Saturday works if we finish before six. Sunday mornings are impossible for me.",
	})
	if err != nil {
		t.Fatalf("Interpret: %v", err)
	}
	t.Logf("interpret meta: %+v", meta)
	if len(in.PerDate) != len(dates) {
		t.Fatalf("per_date has %d verdicts, want one per candidate date (%d): %+v", len(in.PerDate), len(dates), in.PerDate)
	}
	seen := map[string]bool{}
	for _, v := range in.PerDate {
		seen[v.CandidateDateID] = true
	}
	for _, d := range dates {
		if !seen[d.ID] {
			t.Errorf("no verdict for date %s: %+v", d.ID, in.PerDate)
		}
	}
	t.Logf("reading: %+v", in)

	text, meta, err := b.Explain(ctx, explainInput())
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	t.Logf("explain meta: %+v", meta)
	if text == "" || len(strings.Fields(text)) > 60 {
		t.Errorf("explanation %q, want 1 to 60 words", text)
	}
	if strings.Contains(text, "Nov 15") || strings.Contains(text, "Sun") {
		t.Errorf("explanation %q names a date the server did not recommend", text)
	}
	t.Logf("explanation: %s", text)

	loc, _ := time.LoadLocation("America/New_York")
	friday := time.Date(2026, 11, 6, 9, 0, 0, 0, loc)
	draft, meta, err := b.Draft(ctx, ai.DraftInput{Text: mayaDescription, Timezone: "America/New_York", Today: friday})
	if err != nil {
		t.Fatalf("Draft: %v", err)
	}
	t.Logf("draft meta: %+v", meta)
	t.Logf("draft: %+v", draft)
	if draft.Price != "45" && draft.Price != "45.00" || draft.Capacity != 6 || draft.DurationMin != 120 || draft.Minimum != 0 {
		t.Errorf("draft price %q capacity %d duration %d minimum %d, want 45, 6, 120 and no invented minimum", draft.Price, draft.Capacity, draft.DurationMin, draft.Minimum)
	}
	if len(draft.Dates) != 2 || draft.Dates[0].Start != "10:00" {
		t.Errorf("draft dates = %+v, want the two weekend mornings", draft.Dates)
	}
}

func envOr(name, def string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return def
}

func mayaDates(t *testing.T) []ai.DateOption {
	t.Helper()
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	sat := time.Date(2026, 11, 21, demo.Maya.StartHour, 0, 0, 0, ny)
	var out []ai.DateOption
	for i, id := range []string{"sat", "sun"} {
		start := sat.AddDate(0, 0, i)
		end := start.Add(time.Duration(demo.Maya.DurationMin) * time.Minute)
		out = append(out, ai.DateOption{ID: id, Label: present.DateLabel(start, end, ny), StartsAt: start})
	}
	return out
}

func checkLiveReading(t *testing.T, who string, want ai.Scripted, got domain.Interpretation) {
	t.Helper()
	if want.Question != "" {
		if !got.NeedsClarification || len(got.Ambiguities) == 0 {
			t.Errorf("%s: reading %+v, want a question before counting them", who, got)
			return
		}
		q := got.Ambiguities[0].Question
		if !strings.Contains(q, "10") || !strings.Contains(q, "12") {
			t.Errorf("%s: question %q does not name the class hours 10 AM–12 PM", who, q)
		}
		return
	}
	if got.NeedsClarification {
		t.Errorf("%s: reading needs clarification (%+v), want a clear one", who, got.Ambiguities)
	}
	for i, w := range want.Verdicts {
		if i >= len(got.PerDate) {
			t.Errorf("%s: no verdict for date %d in %+v", who, i, got.PerDate)
			continue
		}
		v := got.PerDate[i]
		cond := w.Condition
		if cond.Type == "" {
			cond.Type = domain.ConditionNone
		}
		if v.Verdict != w.Verdict || v.Condition != cond {
			t.Errorf("%s: date %d = %s %+v, want %s %+v", who, i, v.Verdict, v.Condition, w.Verdict, cond)
		}
	}
}

func TestBedrock_LiveMaya(t *testing.T) {
	b, ctx := liveBedrock(t)
	dates := mayaDates(t)
	read := func(message string, earlier *ai.EarlierAnswer) domain.Interpretation {
		t.Helper()
		in, meta, err := b.Interpret(ctx, ai.InterpretInput{WorkshopTitle: demo.Maya.Title, Timezone: "America/New_York", Dates: dates, Message: message, Earlier: earlier})
		if err != nil {
			t.Fatalf("Interpret(%q): %v", message, err)
		}
		t.Logf("%q -> %+v (%s)", message, in, meta.Model)
		return in
	}
	questions := map[string][]string{}
	for _, p := range demo.Maya.People {
		got := read(p.Message, nil)
		checkLiveReading(t, p.Name, p.Expect, got)
		for _, a := range got.Ambiguities {
			questions[p.Name] = append(questions[p.Name], a.Question)
		}
	}
	for _, r := range demo.Maya.Replies {
		var first string
		for _, p := range demo.Maya.People {
			if p.Name == r.Name {
				first = p.Message
			}
		}
		got := read(r.Message, &ai.EarlierAnswer{Message: first, Questions: questions[r.Name]})
		checkLiveReading(t, r.Name+"'s reply", r.Expect, got)
	}
}

func TestBedrock_LiveAsksAboutTheClassHoursOfAnyClass(t *testing.T) {
	b, ctx := liveBedrock(t)
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	tue := time.Date(2026, 11, 17, 18, 0, 0, 0, ny)
	var dates []ai.DateOption
	for i, id := range []string{"tue", "thu"} {
		start := tue.AddDate(0, 0, 2*i)
		dates = append(dates, ai.DateOption{ID: id, Label: present.DateLabel(start, start.Add(2*time.Hour), ny), StartsAt: start})
	}
	in, meta, err := b.Interpret(ctx, ai.InterpretInput{
		WorkshopTitle: "Evening sketching", Timezone: "America/New_York", Dates: dates,
		Message: "Count me in! Evenings are tricky with the kids, but I might get a sitter one night. Not sure which yet.",
	})
	if err != nil {
		t.Fatalf("Interpret: %v", err)
	}
	t.Logf("reading %+v (%s)", in, meta.Model)
	if !in.NeedsClarification || len(in.Ambiguities) == 0 {
		t.Fatalf("reading = %+v, want a question", in)
	}
	q := in.Ambiguities[0].Question
	if !strings.Contains(q, "6") || !strings.Contains(q, "8") || strings.Contains(q, "10") || strings.Contains(q, "12") {
		t.Errorf("question %q does not name this class's hours, 6–8 PM", q)
	}
}
