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
		{ID: "sat-late", StartsAt: time.Date(2026, 11, 15, 2, 0, 0, 0, time.UTC)},
		{ID: "next-sat", StartsAt: time.Date(2026, 11, 21, 20, 0, 0, 0, time.UTC)},
	}
	cases := []struct {
		text string
		want bool
	}{
		{"Sat 14 Nov 2026, 15:00–18:00 America/Lima works for Ana and Luis.", false},
		{"Sat, Nov 14, 3 PM–6 PM (Lima time) works for Ana and Luis.", false},
		{"Sat, Nov 14 works for everyone who replied.", false},
		{"Sun, Nov 15 has fewer people.", true},
		{"Nov 15th has fewer people.", true},
		{"The 15th would be worse.", false},
		{"Not Sun because Luis is away.", true},
		{"A sunny Saturday morning for Ana and Luis.", false},
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

func TestContainsWord(t *testing.T) {
	cases := []struct {
		text, phrase string
		want         bool
	}{
		{"sunday works", "sunday", true},
		{"not on sunday.", "sunday", true},
		{"sundays work", "sunday", false},
		{"115 nov and 15 nov", "15 nov", true},
		{"115 nov only", "15 nov", false},
		{"nov 14", "nov 1", false},
		{"(nov 1)", "nov 1", true},
		{"", "nov 1", false},
		{"sábado 15 nov", "15 nov", true},
		{"día15 nov", "15 nov", false},
	}
	for _, c := range cases {
		if got := containsWord(c.text, c.phrase); got != c.want {
			t.Errorf("containsWord(%q, %q) = %v, want %v", c.text, c.phrase, got, c.want)
		}
	}
}

func TestExplanationCache_SingleFlightEvictionAndRetry(t *testing.T) {
	c := newExplanationCache()
	now := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	key := func(i int) [sha256.Size]byte { return sha256.Sum256([]byte{byte(i), byte(i >> 8)}) }

	if text, fresh, start := c.claim(key(0), now); fresh || !start || text != "" {
		t.Fatalf("first claim = %q, fresh %v, start %v; want a miss the caller must write", text, fresh, start)
	}
	if _, fresh, start := c.claim(key(0), now); fresh || start {
		t.Fatalf("claim while in flight: fresh %v, start %v; want pending without a second writer", fresh, start)
	}
	c.finish(key(0), "text 0", time.Time{})
	if text, fresh, _ := c.claim(key(0), now); !fresh || text != "text 0" {
		t.Fatalf("claim after finish = %q, fresh %v", text, fresh)
	}
	if len(c.inFlight) != 0 {
		t.Errorf("in-flight marks left after finish: %d", len(c.inFlight))
	}

	for i := 1; i <= explanationCacheSize; i++ {
		c.claim(key(i), now)
		c.finish(key(i), "text", time.Time{})
	}
	if len(c.entries) != explanationCacheSize || len(c.order) != explanationCacheSize {
		t.Fatalf("cache holds %d entries (%d in order), want %d", len(c.entries), len(c.order), explanationCacheSize)
	}
	if _, fresh, _ := c.claim(key(0), now); fresh {
		t.Error("the oldest entry survived eviction")
	}
	c.finish(key(0), "text 0", time.Time{})
	if _, fresh, _ := c.claim(key(explanationCacheSize), now); !fresh {
		t.Error("the newest entry is missing")
	}

	c.finish(key(5), "plain", now.Add(time.Minute))
	if text, fresh, _ := c.claim(key(5), now); !fresh || text != "plain" {
		t.Errorf("before the retry time: %q, fresh %v", text, fresh)
	}
	text, fresh, start := c.claim(key(5), now.Add(time.Minute))
	if fresh || !start || text != "plain" {
		t.Errorf("at the retry time: %q, fresh %v, start %v; want the stale text and one writer", text, fresh, start)
	}
	if len(c.order) != explanationCacheSize {
		t.Errorf("replacing an entry grew the order to %d", len(c.order))
	}
}
