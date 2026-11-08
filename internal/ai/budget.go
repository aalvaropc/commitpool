package ai

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/aalvaropc/commitpool/internal/clock"
	"github.com/aalvaropc/commitpool/internal/domain"
)

var ErrBusy = errors.New("ai: too many model calls")

type BusyError struct {
	Wait time.Duration
}

func (e *BusyError) Error() string {
	return fmt.Sprintf("%s; try again in %s", ErrBusy, e.Wait.Round(time.Second))
}

func (e *BusyError) Is(target error) bool { return target == ErrBusy }

func BusyWait(err error) (time.Duration, bool) {
	var busy *BusyError
	if errors.As(err, &busy) {
		return busy.Wait, true
	}
	return 0, false
}

type BudgetConfig struct {
	Hourly int
	Daily  int
	Clock  clock.Clock
}

type Budget struct {
	next   Interpreter
	clock  clock.Clock
	mu     sync.Mutex
	hourly window
	daily  window
}

var _ Interpreter = (*Budget)(nil)

type window struct {
	limit int
	span  time.Duration
	calls []time.Time
}

func NewBudget(next Interpreter, cfg BudgetConfig) *Budget {
	clk := cfg.Clock
	if clk == nil {
		clk = clock.Real{}
	}
	return &Budget{
		next:   next,
		clock:  clk,
		hourly: window{limit: cfg.Hourly, span: time.Hour},
		daily:  window{limit: cfg.Daily, span: 24 * time.Hour},
	}
}

func (w *window) wait(now time.Time) time.Duration {
	expired := 0
	for expired < len(w.calls) && !now.Before(w.calls[expired].Add(w.span)) {
		expired++
	}
	w.calls = slices.Delete(w.calls, 0, expired)
	if w.limit <= 0 || len(w.calls) < w.limit {
		return 0
	}
	return w.calls[len(w.calls)-w.limit].Add(w.span).Sub(now)
}

func (b *Budget) take() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.clock.Now()
	if wait := max(b.hourly.wait(now), b.daily.wait(now)); wait > 0 {
		return &BusyError{Wait: wait}
	}
	b.hourly.calls = append(b.hourly.calls, now)
	b.daily.calls = append(b.daily.calls, now)
	return nil
}

func (b *Budget) Interpret(ctx context.Context, in InterpretInput) (domain.Interpretation, Meta, error) {
	if err := b.take(); err != nil {
		return domain.Interpretation{}, Meta{}, err
	}
	return b.next.Interpret(ctx, in)
}

func (b *Budget) Explain(ctx context.Context, in ExplainInput) (string, Meta, error) {
	if err := b.take(); err != nil {
		return "", Meta{}, err
	}
	return b.next.Explain(ctx, in)
}

func (b *Budget) Draft(ctx context.Context, in DraftInput) (Draft, Meta, error) {
	if err := b.take(); err != nil {
		return Draft{}, Meta{}, err
	}
	return b.next.Draft(ctx, in)
}

func (b *Budget) Suggest(ctx context.Context, in SuggestInput) (string, Meta, error) {
	if err := b.take(); err != nil {
		return "", Meta{}, err
	}
	return b.next.Suggest(ctx, in)
}
