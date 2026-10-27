package clock_test

import (
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/clock"
)

func TestFake_SetMovesTime(t *testing.T) {
	start := time.Date(2026, 11, 14, 15, 0, 0, 0, time.UTC)
	var c clock.Clock = &clock.Fake{T: start}
	if got := c.Now(); !got.Equal(start) {
		t.Fatalf("Now() = %v, want %v", got, start)
	}
	later := start.Add(24 * time.Hour)
	c.(*clock.Fake).Set(later)
	if got := c.Now(); !got.Equal(later) {
		t.Fatalf("Now() after Set = %v, want %v", got, later)
	}
}

func TestReal_NowIsUTCAndCurrent(t *testing.T) {
	before := time.Now()
	got := clock.Real{}.Now()
	if got.Location() != time.UTC {
		t.Errorf("Location = %v, want UTC", got.Location())
	}
	if got.Before(before) || got.After(time.Now()) {
		t.Errorf("Now() = %v is outside the call window", got)
	}
}
