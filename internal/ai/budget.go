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

type Capacity interface {
	Room(calls int) time.Duration
	RoomLeaving(calls int, reserve func(limit int) int) time.Duration
}

type BudgetConfig struct {
	Hourly  int
	Daily   int
	Monthly int
	Clock   clock.Clock
}

const monthlySpan = 30 * 24 * time.Hour

type Budget struct {
	next    Interpreter
	clock   clock.Clock
	mu      sync.Mutex
	hourly  window
	daily   window
	monthly window
}

var (
	_ Interpreter = (*Budget)(nil)
	_ Capacity    = (*Budget)(nil)
)

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
		next:    next,
		clock:   clk,
		hourly:  window{limit: cfg.Hourly, span: time.Hour},
		daily:   window{limit: cfg.Daily, span: 24 * time.Hour},
		monthly: window{limit: cfg.Monthly, span: monthlySpan},
	}
}

func readingShare(limit int) int {
	return limit
}

func textShare(limit int) int {
	return limit - limit/4
}

func (w *window) wait(now time.Time, calls int, share func(int) int) time.Duration {
	expired := 0
	for expired < len(w.calls) && !now.Before(w.calls[expired].Add(w.span)) {
		expired++
	}
	w.calls = slices.Delete(w.calls, 0, expired)
	if w.limit <= 0 {
		return 0
	}
	room := share(w.limit)
	if calls > room && calls <= w.limit {
		if len(w.calls) == 0 {
			return 0
		}
		return w.calls[len(w.calls)-1].Add(w.span).Sub(now)
	}
	over := len(w.calls) + calls - room
	switch {
	case over <= 0:
		return 0
	case over > len(w.calls):
		return w.span
	}
	return w.calls[over-1].Add(w.span).Sub(now)
}

func (w *window) record(now time.Time) {
	if w.limit > 0 {
		w.calls = append(w.calls, now)
	}
}

func (b *Budget) waitFor(now time.Time, calls int, share func(int) int) time.Duration {
	return max(b.hourly.wait(now, calls, share), b.daily.wait(now, calls, share), b.monthly.wait(now, calls, share))
}

func (b *Budget) take(share func(int) int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.clock.Now()
	if wait := b.waitFor(now, 1, share); wait > 0 {
		return &BusyError{Wait: wait}
	}
	b.hourly.record(now)
	b.daily.record(now)
	b.monthly.record(now)
	return nil
}

func (b *Budget) Room(calls int) time.Duration {
	return b.RoomLeaving(calls, func(int) int { return 0 })
}

func (b *Budget) RoomLeaving(calls int, reserve func(limit int) int) time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.waitFor(b.clock.Now(), calls, func(limit int) int { return limit - reserve(limit) })
}

func (b *Budget) Interpret(ctx context.Context, in InterpretInput) (domain.Interpretation, Meta, error) {
	if err := b.take(readingShare); err != nil {
		return domain.Interpretation{}, Meta{}, err
	}
	return b.next.Interpret(ctx, in)
}

func (b *Budget) Explain(ctx context.Context, in ExplainInput) (string, Meta, error) {
	if err := b.take(textShare); err != nil {
		return "", Meta{}, err
	}
	return b.next.Explain(ctx, in)
}

func (b *Budget) Draft(ctx context.Context, in DraftInput) (Draft, Meta, error) {
	if err := b.take(textShare); err != nil {
		return Draft{}, Meta{}, err
	}
	return b.next.Draft(ctx, in)
}

func (b *Budget) Suggest(ctx context.Context, in SuggestInput) (string, Meta, error) {
	if err := b.take(textShare); err != nil {
		return "", Meta{}, err
	}
	return b.next.Suggest(ctx, in)
}
