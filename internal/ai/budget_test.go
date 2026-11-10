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
		WorkshopTitle: "Life drawing for beginners",
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

func background(b *ai.Budget) map[string]func() error {
	ctx := context.Background()
	return map[string]func() error{
		"explain": func() error { _, _, err := b.Explain(ctx, ai.ExplainInput{}); return err },
		"draft": func() error {
			_, _, err := b.Draft(ctx, ai.DraftInput{Text: "life drawing", Timezone: "UTC", Today: budgetStart})
			return err
		},
		"suggest": func() error { _, _, err := b.Suggest(ctx, ai.SuggestInput{Kind: "share"}); return err },
	}
}

func TestBudget_TextsUseAtMostThreeQuartersOfTheHour(t *testing.T) {
	for name := range background(nil) {
		t.Run(name, func(t *testing.T) {
			model := &countingModel{}
			clk := &clock.Fake{T: budgetStart}
			b := ai.NewBudget(model, ai.BudgetConfig{Hourly: 8, Daily: 100, Clock: clk})
			call := background(b)[name]
			for i := range 6 {
				clk.Set(budgetStart.Add(time.Duration(i) * time.Minute))
				if err := call(); err != nil {
					t.Fatalf("%s %d of 6: %v", name, i+1, err)
				}
			}
			clk.Set(budgetStart.Add(10 * time.Minute))
			wantBusy(t, call(), 50*time.Minute)
			for i := range 2 {
				if _, _, err := b.Interpret(context.Background(), budgetInput()); err != nil {
					t.Fatalf("reading %d in the quarter kept for readings: %v", i+1, err)
				}
			}
			_, _, err := b.Interpret(context.Background(), budgetInput())
			wantBusy(t, err, 50*time.Minute)
			wantBusy(t, call(), 52*time.Minute)
			if n := model.calls.Load(); n != 8 {
				t.Errorf("model calls = %d, want 8", n)
			}
		})
	}
}

func TestBudget_TextsUseAtMostThreeQuartersOfTheDay(t *testing.T) {
	model := &countingModel{}
	clk := &clock.Fake{T: budgetStart}
	b := ai.NewBudget(model, ai.BudgetConfig{Hourly: 100, Daily: 4, Clock: clk})
	calls := background(b)
	for i, name := range []string{"explain", "draft", "suggest"} {
		clk.Set(budgetStart.Add(time.Duration(i) * time.Hour))
		if err := calls[name](); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	clk.Set(budgetStart.Add(5 * time.Hour))
	wantBusy(t, calls["explain"](), 19*time.Hour)
	if _, _, err := b.Interpret(context.Background(), budgetInput()); err != nil {
		t.Fatalf("a reading in the quarter of the day kept for readings: %v", err)
	}
	_, _, err := b.Interpret(context.Background(), budgetInput())
	wantBusy(t, err, 19*time.Hour)
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

func TestBudget_RoomSaysWhenThereIsSpaceForMoreCalls(t *testing.T) {
	model := &countingModel{}
	clk := &clock.Fake{T: budgetStart}
	b := ai.NewBudget(model, ai.BudgetConfig{Hourly: 12, Daily: 20, Clock: clk})
	var c ai.Capacity = b
	if wait := c.Room(10); wait != 0 {
		t.Fatalf("Room(10) on a fresh budget = %v, want 0", wait)
	}
	for i := range 3 {
		clk.Set(budgetStart.Add(time.Duration(i) * 5 * time.Minute))
		if _, _, err := b.Interpret(context.Background(), budgetInput()); err != nil {
			t.Fatal(err)
		}
	}
	clk.Set(budgetStart.Add(15 * time.Minute))
	if wait := c.Room(9); wait != 0 {
		t.Errorf("Room(9) with 9 calls left this hour = %v, want 0", wait)
	}
	if wait := c.Room(10); wait != 45*time.Minute {
		t.Errorf("Room(10) with 9 calls left this hour = %v, want 45m until the first call is an hour old", wait)
	}
	if wait := c.Room(11); wait != 50*time.Minute {
		t.Errorf("Room(11) = %v, want 50m until the second call is an hour old", wait)
	}
	if wait := c.Room(30); wait != 24*time.Hour {
		t.Errorf("Room(30) beyond the daily cap = %v, want a day", wait)
	}
	clk.Set(budgetStart.Add(2 * time.Hour))
	for range 8 {
		if _, _, err := b.Interpret(context.Background(), budgetInput()); err != nil {
			t.Fatal(err)
		}
	}
	if wait := c.Room(10); wait != 22*time.Hour {
		t.Errorf("Room(10) with 9 calls left today = %v, want 22h until the first call is a day old", wait)
	}
	if n := model.calls.Load(); n != 11 {
		t.Errorf("model calls = %d, want 11: Room never counts a call", n)
	}
}

func TestMemo_PassesTheBudgetsRoomThrough(t *testing.T) {
	clk := &clock.Fake{T: budgetStart}
	b := ai.NewBudget(&countingModel{}, ai.BudgetConfig{Hourly: 2, Daily: 10, Clock: clk})
	var m ai.Interpreter = ai.NewMemo(b, ai.MemoSize)
	c, ok := m.(ai.Capacity)
	if !ok {
		t.Fatal("the memo hides the budget's room")
	}
	if wait := c.Room(3); wait != time.Hour {
		t.Errorf("Room(3) through the memo = %v, want an hour", wait)
	}
	if wait := ai.NewMemo(ai.Fake{}, ai.MemoSize).Room(100); wait != 0 {
		t.Errorf("Room on a memo without a budget = %v, want 0", wait)
	}
}
