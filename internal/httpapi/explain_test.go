package httpapi

import (
	"crypto/sha256"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/store"
)

func TestNamesAnotherDate(t *testing.T) {
	lima, err := time.LoadLocation("America/Lima")
	if err != nil {
		t.Fatal(err)
	}
	dates := []store.CandidateDateRow{
		{ID: "sat", StartsAt: time.Date(2026, 11, 14, 20, 0, 0, 0, time.UTC)},
		{ID: "sun", StartsAt: time.Date(2026, 11, 15, 15, 0, 0, 0, time.UTC)},
		{ID: "sat-late", StartsAt: time.Date(2026, 11, 15, 2, 0, 0, 0, time.UTC)}, // Sat 14 Nov 21:00 in Lima
		{ID: "next-sat", StartsAt: time.Date(2026, 11, 21, 20, 0, 0, 0, time.UTC)},
	}
	cases := []struct {
		text string
		want bool
	}{
		{"Sat 14 Nov 2026, 15:00–18:00 America/Lima works for Ana and Luis.", false},
		{"Saturday 14 November suits everyone who replied.", false},
		{"Sun 15 Nov 2026 would be better.", true},
		{"Everyone can make it except on Sunday.", true},
		{"Nov 15 is another option.", true},
		{"Saturday 21 November could also work.", true},
		{"Nov 14 works; 115 Nov is not a date.", false},
	}
	for _, c := range cases {
		if got := namesAnotherDate(c.text, "sat", dates, lima); got != c.want {
			t.Errorf("namesAnotherDate(%q) = %v, want %v", c.text, got, c.want)
		}
	}
	if namesAnotherDate("Sunday", "missing", dates, lima) {
		t.Error("an unknown recommended date flagged the text")
	}
}

func TestExplanationCache_EvictsTheOldestAndHonoursRetry(t *testing.T) {
	c := newExplanationCache()
	now := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	key := func(i int) [sha256.Size]byte { return sha256.Sum256([]byte{byte(i), byte(i >> 8)}) }
	for i := 0; i <= explanationCacheSize; i++ {
		c.put(key(i), "text", time.Time{})
	}
	if len(c.entries) != explanationCacheSize || len(c.order) != explanationCacheSize {
		t.Fatalf("cache holds %d entries (%d in order), want %d", len(c.entries), len(c.order), explanationCacheSize)
	}
	if _, ok := c.get(key(0), now); ok {
		t.Error("the oldest entry survived eviction")
	}
	if _, ok := c.get(key(explanationCacheSize), now); !ok {
		t.Error("the newest entry is missing")
	}

	c.put(key(1), "plain", now.Add(time.Minute))
	if text, ok := c.get(key(1), now); !ok || text != "plain" {
		t.Errorf("before the retry time: %q, %v", text, ok)
	}
	if _, ok := c.get(key(1), now.Add(time.Minute)); ok {
		t.Error("an entry past its retry time is still served")
	}
	if len(c.order) != explanationCacheSize {
		t.Errorf("replacing an entry grew the order to %d", len(c.order))
	}
}
