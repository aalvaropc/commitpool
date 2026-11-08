package ai_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/ai"
	"github.com/aalvaropc/commitpool/internal/clock"
	"github.com/aalvaropc/commitpool/internal/domain"
)

type countingModel struct {
	ai.Fake
	calls atomic.Int32
	fail  error
}

func (c *countingModel) Interpret(ctx context.Context, in ai.InterpretInput) (domain.Interpretation, ai.Meta, error) {
	c.calls.Add(1)
	if c.fail != nil {
		return domain.Interpretation{}, ai.Meta{}, c.fail
	}
	return c.Fake.Interpret(ctx, in)
}

func (c *countingModel) Explain(ctx context.Context, in ai.ExplainInput) (string, ai.Meta, error) {
	c.calls.Add(1)
	return c.Fake.Explain(ctx, in)
}

func (c *countingModel) Draft(ctx context.Context, in ai.DraftInput) (ai.Draft, ai.Meta, error) {
	c.calls.Add(1)
	return c.Fake.Draft(ctx, in)
}

func (c *countingModel) Suggest(ctx context.Context, in ai.SuggestInput) (string, ai.Meta, error) {
	c.calls.Add(1)
	return c.Fake.Suggest(ctx, in)
}

var budgetStart = time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC)

func budgetInput() ai.InterpretInput {
	return ai.InterpretInput{
		WorkshopTitle: "Wheel-throwing for beginners",
		Timezone:      "America/New_York",
		Dates:         []ai.DateOption{{ID: "sat", Label: "Sat, Nov 14, 10 AM", StartsAt: budgetStart.Add(5 * 24 * time.Hour)}},
		Message:       "Saturday works for me.",
	}
}

func wantBusy(t *testing.T, err error, wait time.Duration) {
	t.Helper()
	if !errors.Is(err, ai.ErrBusy) {
		t.Fatalf("err = %v, want ai.ErrBusy", err)
	}
	got, ok := ai.BusyWait(err)
	if !ok || got != wait {
		t.Errorf("suggested wait = %v, %v; want %v", got, ok, wait)
	}
}

func TestBudget_RefusesPastTheHourlyCapUntilTheOldestCallIsAnHourOld(t *testing.T) {
	model := &countingModel{}
	clk := &clock.Fake{T: budgetStart}
	b := ai.NewBudget(model, ai.BudgetConfig{Hourly: 3, Daily: 100, Clock: clk})
	ctx := context.Background()
	for i := range 3 {
		clk.Set(budgetStart.Add(time.Duration(i) * 10 * time.Minute))
		if _, _, err := b.Interpret(ctx, budgetInput()); err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
	}
	clk.Set(budgetStart.Add(40 * time.Minute))
	_, _, err := b.Interpret(ctx, budgetInput())
	wantBusy(t, err, 20*time.Minute)
	if n := model.calls.Load(); n != 3 {
		t.Errorf("model calls = %d, want 3: a refused call never reaches the model", n)
	}

	clk.Set(budgetStart.Add(time.Hour))
	if _, _, err := b.Interpret(ctx, budgetInput()); err != nil {
		t.Fatalf("an hour after the first call: %v", err)
	}
	_, _, err = b.Interpret(ctx, budgetInput())
	wantBusy(t, err, 10*time.Minute)
}

func TestBudget_RefusesPastTheDailyCapOverASlidingDay(t *testing.T) {
	model := &countingModel{}
	clk := &clock.Fake{T: budgetStart}
	b := ai.NewBudget(model, ai.BudgetConfig{Hourly: 4, Daily: 6, Clock: clk})
	ctx := context.Background()
	for i := range 6 {
		clk.Set(budgetStart.Add(time.Duration(i/3) * 2 * time.Hour))
		if _, _, err := b.Interpret(ctx, budgetInput()); err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
	}
	clk.Set(budgetStart.Add(10 * time.Hour))
	_, _, err := b.Interpret(ctx, budgetInput())
	wantBusy(t, err, 14*time.Hour)

	clk.Set(budgetStart.Add(24 * time.Hour))
	for i := range 3 {
		if _, _, err := b.Interpret(ctx, budgetInput()); err != nil {
			t.Fatalf("a day later, call %d: %v", i+1, err)
		}
	}
	_, _, err = b.Interpret(ctx, budgetInput())
	wantBusy(t, err, 2*time.Hour)
}

func TestBudget_CountsEveryKindOfCall(t *testing.T) {
	model := &countingModel{}
	clk := &clock.Fake{T: budgetStart}
	b := ai.NewBudget(model, ai.BudgetConfig{Hourly: 4, Daily: 10, Clock: clk})
	ctx := context.Background()
	if _, _, err := b.Interpret(ctx, budgetInput()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.Explain(ctx, ai.ExplainInput{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.Draft(ctx, ai.DraftInput{Text: "pottery", Timezone: "UTC", Today: budgetStart}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.Suggest(ctx, ai.SuggestInput{Kind: "share"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.Explain(ctx, ai.ExplainInput{}); !errors.Is(err, ai.ErrBusy) {
		t.Errorf("explain past the cap: %v, want ai.ErrBusy", err)
	}
	if _, _, err := b.Draft(ctx, ai.DraftInput{Text: "pottery", Timezone: "UTC", Today: budgetStart}); !errors.Is(err, ai.ErrBusy) {
		t.Errorf("draft past the cap: %v, want ai.ErrBusy", err)
	}
	if _, _, err := b.Suggest(ctx, ai.SuggestInput{Kind: "share"}); !errors.Is(err, ai.ErrBusy) {
		t.Errorf("suggest past the cap: %v, want ai.ErrBusy", err)
	}
	if n := model.calls.Load(); n != 4 {
		t.Errorf("model calls = %d, want 4", n)
	}
}

func TestBudget_AFailedCallStillCounts(t *testing.T) {
	model := &countingModel{fail: errors.New("bedrock answered 500")}
	b := ai.NewBudget(model, ai.BudgetConfig{Hourly: 1, Daily: 10, Clock: &clock.Fake{T: budgetStart}})
	if _, _, err := b.Interpret(context.Background(), budgetInput()); err == nil || errors.Is(err, ai.ErrBusy) {
		t.Fatalf("first call = %v, want the model's failure", err)
	}
	if _, _, err := b.Interpret(context.Background(), budgetInput()); !errors.Is(err, ai.ErrBusy) {
		t.Errorf("second call = %v, want ai.ErrBusy", err)
	}
}

func TestBudget_HoldsUnderConcurrentCalls(t *testing.T) {
	model := &countingModel{}
	b := ai.NewBudget(model, ai.BudgetConfig{Hourly: 20, Daily: 100, Clock: &clock.Fake{T: budgetStart}})
	var wg sync.WaitGroup
	var busy atomic.Int32
	for range 50 {
		wg.Go(func() {
			if _, _, err := b.Interpret(context.Background(), budgetInput()); errors.Is(err, ai.ErrBusy) {
				busy.Add(1)
			}
		})
	}
	wg.Wait()
	if n, refused := model.calls.Load(), busy.Load(); n != 20 || refused != 30 {
		t.Errorf("model calls = %d, refused = %d; want 20 and 30", n, refused)
	}
}

func TestBusyWait_IgnoresOtherErrors(t *testing.T) {
	if _, ok := ai.BusyWait(errors.New("bedrock answered 500")); ok {
		t.Error("BusyWait matched an unrelated error")
	}
}
