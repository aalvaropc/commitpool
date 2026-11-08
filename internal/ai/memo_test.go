package ai_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/ai"
	"github.com/aalvaropc/commitpool/internal/domain"
)

type pricedModel struct {
	countingModel
}

func spent(meta ai.Meta) ai.Meta {
	meta.Provider, meta.Model, meta.InputTokens, meta.OutputTokens = "bedrock", "claude", 900, 120
	return meta
}

func (p *pricedModel) Interpret(ctx context.Context, in ai.InterpretInput) (domain.Interpretation, ai.Meta, error) {
	out, meta, err := p.countingModel.Interpret(ctx, in)
	return out, spent(meta), err
}

func (p *pricedModel) Explain(ctx context.Context, in ai.ExplainInput) (string, ai.Meta, error) {
	out, meta, err := p.countingModel.Explain(ctx, in)
	return out, spent(meta), err
}

func (p *pricedModel) Draft(ctx context.Context, in ai.DraftInput) (ai.Draft, ai.Meta, error) {
	out, meta, err := p.countingModel.Draft(ctx, in)
	return out, spent(meta), err
}

func (p *pricedModel) Suggest(ctx context.Context, in ai.SuggestInput) (string, ai.Meta, error) {
	out, meta, err := p.countingModel.Suggest(ctx, in)
	return out, spent(meta), err
}

var memoSaturday = time.Date(2026, 11, 14, 15, 0, 0, 0, time.UTC)

func workshopDates(prefix string) []ai.DateOption {
	return []ai.DateOption{
		{ID: prefix + "-sat", Label: "Sat, Nov 14, 10 AM", StartsAt: memoSaturday},
		{ID: prefix + "-sun", Label: "Sun, Nov 15, 10 AM", StartsAt: memoSaturday.Add(24 * time.Hour)},
	}
}

func priya(prefix string) ai.InterpretInput {
	return ai.InterpretInput{
		WorkshopTitle: "Wheel-throwing for beginners",
		Timezone:      "America/New_York",
		Dates:         workshopDates(prefix),
		Message:       "Saturday works for me, Sunday I can't.",
	}
}

func verdicts(in domain.Interpretation) map[string]domain.Verdict {
	out := map[string]domain.Verdict{}
	for _, v := range in.PerDate {
		out[v.CandidateDateID] = v.Verdict
	}
	return out
}

func TestMemo_AnIdenticalRequestReusesTheAnswer(t *testing.T) {
	model := &pricedModel{}
	m := ai.NewMemo(model, ai.MemoSize)
	ctx := context.Background()
	first, firstMeta, err := m.Interpret(ctx, priya("a"))
	if err != nil {
		t.Fatal(err)
	}
	if firstMeta.Cached || firstMeta.InputTokens != 900 {
		t.Errorf("first meta = %+v, want a fresh call", firstMeta)
	}
	again, meta, err := m.Interpret(ctx, priya("a"))
	if err != nil {
		t.Fatal(err)
	}
	if n := model.calls.Load(); n != 1 {
		t.Errorf("model calls = %d, want 1", n)
	}
	if !reflect.DeepEqual(again, first) {
		t.Errorf("cached reading = %+v, want %+v", again, first)
	}
	want := ai.Meta{Provider: "bedrock", Model: "claude", Cached: true}
	if meta != want {
		t.Errorf("cached meta = %+v, want %+v", meta, want)
	}
}

func TestMemo_ANewWorkshopWithTheSameDatesHitsAndGetsItsOwnIDs(t *testing.T) {
	model := &pricedModel{}
	m := ai.NewMemo(model, ai.MemoSize)
	ctx := context.Background()
	if _, _, err := m.Interpret(ctx, priya("a")); err != nil {
		t.Fatal(err)
	}
	got, meta, err := m.Interpret(ctx, priya("b"))
	if err != nil {
		t.Fatal(err)
	}
	if n := model.calls.Load(); n != 1 || !meta.Cached {
		t.Fatalf("model calls = %d, cached %v; want the earlier answer", n, meta.Cached)
	}
	want := map[string]domain.Verdict{"b-sat": domain.VerdictAvailable, "b-sun": domain.VerdictUnavailable}
	if v := verdicts(got); !reflect.DeepEqual(v, want) {
		t.Errorf("verdicts = %v, want %v", v, want)
	}
}

func TestMemo_EveryFieldOfTheRequestIsPartOfTheKey(t *testing.T) {
	edits := map[string]func(*ai.InterpretInput){
		"message":  func(in *ai.InterpretInput) { in.Message = "Sunday works for me, Saturday I can't." },
		"title":    func(in *ai.InterpretInput) { in.WorkshopTitle = "Glazing" },
		"timezone": func(in *ai.InterpretInput) { in.Timezone = "Europe/Madrid" },
		"label":    func(in *ai.InterpretInput) { in.Dates[0].Label = "Sat, Nov 14, 11 AM" },
		"start":    func(in *ai.InterpretInput) { in.Dates[0].StartsAt = in.Dates[0].StartsAt.Add(time.Hour) },
		"order":    func(in *ai.InterpretInput) { in.Dates[0], in.Dates[1] = in.Dates[1], in.Dates[0] },
		"count":    func(in *ai.InterpretInput) { in.Dates = in.Dates[:1] },
	}
	for name, edit := range edits {
		t.Run(name, func(t *testing.T) {
			model := &pricedModel{}
			m := ai.NewMemo(model, ai.MemoSize)
			if _, _, err := m.Interpret(context.Background(), priya("a")); err != nil {
				t.Fatal(err)
			}
			in := priya("a")
			edit(&in)
			if _, meta, err := m.Interpret(context.Background(), in); err != nil || meta.Cached {
				t.Errorf("edited request: cached %v, %v; want a new call", meta.Cached, err)
			}
			if n := model.calls.Load(); n != 2 {
				t.Errorf("model calls = %d, want 2", n)
			}
		})
	}
}

func TestMemo_OnlyCachesSuccess(t *testing.T) {
	model := &pricedModel{}
	model.fail = errors.New("bedrock answered 500")
	m := ai.NewMemo(model, ai.MemoSize)
	if _, _, err := m.Interpret(context.Background(), priya("a")); err == nil {
		t.Fatal("want the model's failure")
	}
	model.fail = nil
	if _, meta, err := m.Interpret(context.Background(), priya("a")); err != nil || meta.Cached {
		t.Errorf("after a failure: cached %v, %v; want a new call", meta.Cached, err)
	}
	if n := model.calls.Load(); n != 2 {
		t.Errorf("model calls = %d, want 2", n)
	}
}

func TestMemo_DoesNotCacheABusyAnswer(t *testing.T) {
	model := &pricedModel{}
	budget := ai.NewBudget(model, ai.BudgetConfig{Hourly: 1, Daily: 10})
	m := ai.NewMemo(budget, ai.MemoSize)
	ctx := context.Background()
	if _, _, err := m.Interpret(ctx, priya("a")); err != nil {
		t.Fatal(err)
	}
	if _, meta, err := m.Interpret(ctx, priya("b")); err != nil || !meta.Cached {
		t.Fatalf("a cached answer past the cap = %+v, %v; want it served without the budget", meta, err)
	}
	other := priya("a")
	other.Message = "Both days work for me."
	if _, _, err := m.Interpret(ctx, other); !errors.Is(err, ai.ErrBusy) {
		t.Errorf("a new message past the cap = %v, want ai.ErrBusy", err)
	}
}

func TestMemo_EvictsTheLeastRecentlyUsed(t *testing.T) {
	model := &pricedModel{}
	m := ai.NewMemo(model, 2)
	ctx := context.Background()
	ask := func(message string) bool {
		in := priya("a")
		in.Message = message
		_, meta, err := m.Interpret(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		return meta.Cached
	}
	ask("one")
	ask("two")
	if !ask("one") {
		t.Fatal("one was not cached")
	}
	ask("three")
	if ask("two") {
		t.Error("two survived although it was the least recently used")
	}
	if !ask("three") {
		t.Error("three was evicted")
	}
}

func TestMemo_ACallerCannotChangeTheCachedAnswer(t *testing.T) {
	m := ai.NewMemo(&pricedModel{}, ai.MemoSize)
	ctx := context.Background()
	first, _, _ := m.Interpret(ctx, priya("a"))
	first.PerDate[0].Verdict = domain.VerdictUnknown
	first.HardConstraints = append(first.HardConstraints[:0], "changed")
	again, _, _ := m.Interpret(ctx, priya("a"))
	if again.PerDate[0].Verdict != domain.VerdictAvailable || len(again.HardConstraints) != 1 || again.HardConstraints[0] == "changed" {
		t.Errorf("cached answer = %+v, want the model's", again)
	}
}

func explainIn(prefix string) ai.ExplainInput {
	dates := workshopDates(prefix)
	priya, ben := prefix+"-priya", prefix+"-ben"
	return ai.ExplainInput{
		WorkshopTitle: "Wheel-throwing for beginners",
		Dates:         dates,
		Minimum:       3,
		Names:         map[string]string{priya: "Priya Raman", ben: "Ben Whitaker"},
		Recommendation: domain.Recommendation{
			DateID: dates[0].ID,
			PerDate: []domain.DateSummary{
				{DateID: dates[0].ID, Compatible: []string{priya, ben}},
				{DateID: dates[1].ID, Compatible: []string{ben}, Blockers: map[string]string{priya: "unavailable"}},
			},
		},
	}
}

func TestMemo_ExplainHitsInANewWorkshopWithTheSamePeople(t *testing.T) {
	model := &pricedModel{}
	m := ai.NewMemo(model, ai.MemoSize)
	ctx := context.Background()
	first, _, err := m.Explain(ctx, explainIn("a"))
	if err != nil {
		t.Fatal(err)
	}
	again, meta, err := m.Explain(ctx, explainIn("b"))
	if err != nil || !meta.Cached || again != first {
		t.Fatalf("explain in a new workshop = %q, %+v, %v; want %q from the cache", again, meta, err, first)
	}
	renamed := explainIn("b")
	renamed.Names["b-ben"] = "Ben Ortiz"
	if _, meta, _ := m.Explain(ctx, renamed); meta.Cached {
		t.Error("another name hit the cache")
	}
	moved := explainIn("b")
	moved.Recommendation.PerDate[1].Blockers = map[string]string{"b-ben": "unavailable"}
	if _, meta, _ := m.Explain(ctx, moved); meta.Cached {
		t.Error("another blocker hit the cache")
	}
	if n := model.calls.Load(); n != 3 {
		t.Errorf("model calls = %d, want 3", n)
	}
}

func TestMemo_DraftHitsTheSameDayOnly(t *testing.T) {
	model := &pricedModel{}
	m := ai.NewMemo(model, ai.MemoSize)
	ctx := context.Background()
	ny, _ := time.LoadLocation("America/New_York")
	morning := time.Date(2026, 11, 9, 8, 0, 0, 0, ny)
	in := ai.DraftInput{Text: "Pottery for beginners, $45, Saturday 10 to 12.", Timezone: "America/New_York", Today: morning}
	first, _, err := m.Draft(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	in.Today = morning.Add(9 * time.Hour)
	again, meta, err := m.Draft(ctx, in)
	if err != nil || !meta.Cached || !reflect.DeepEqual(again, first) {
		t.Errorf("draft later the same day = %+v, %+v, %v; want the cached draft", again, meta, err)
	}
	in.Today = morning.Add(24 * time.Hour)
	if _, meta, _ := m.Draft(ctx, in); meta.Cached {
		t.Error("a draft on the next day hit the cache")
	}
}

func TestMemo_SuggestHitsAnIdenticalRequest(t *testing.T) {
	model := &pricedModel{}
	m := ai.NewMemo(model, ai.MemoSize)
	ctx := context.Background()
	in := ai.SuggestInput{WorkshopTitle: "Wheel-throwing for beginners", Kind: "clarify", Name: "Grace Liu", Date: "Sat, Nov 14", Question: "Which hours?"}
	first, _, _ := m.Suggest(ctx, in)
	again, meta, err := m.Suggest(ctx, in)
	if err != nil || !meta.Cached || again != first {
		t.Errorf("suggest again = %q, %+v, %v", again, meta, err)
	}
	in.Name = "Ben Whitaker"
	if _, meta, _ := m.Suggest(ctx, in); meta.Cached {
		t.Error("a nudge for another person hit the cache")
	}
}

func TestMeta_CachedIsOmittedWhenFalse(t *testing.T) {
	raw, err := json.Marshal(ai.Meta{Provider: "bedrock"})
	if err != nil || strings.Contains(string(raw), "cached") {
		t.Errorf("meta = %s, %v; want no cached key on a fresh answer", raw, err)
	}
	raw, _ = json.Marshal(ai.Meta{Provider: "bedrock", Cached: true})
	if !strings.Contains(string(raw), `"cached":true`) {
		t.Errorf("meta = %s, want cached true", raw)
	}
}
