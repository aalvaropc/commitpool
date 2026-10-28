package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"regexp"
	"sync"
	"time"

	"github.com/aalvaropc/commitpool/internal/ai"
	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/store"
)

const (
	// explainTimeout bounds one call to the interpreter for an explanation.
	explainTimeout = 15 * time.Second
	// explanationCacheSize bounds the explanations kept in memory.
	explanationCacheSize = 256
	// explanationRetryAfter is how long a plain explanation stands in for a
	// failed call before a view asks the interpreter again.
	explanationRetryAfter = time.Minute
)

// explanation puts the recommendation into words for the owner view. The
// recommendation itself, and so its date, always comes from domain.Recommend;
// the interpreter only phrases it. Each distinct input is explained once and
// kept in memory, so views do not call the interpreter again. A failed call,
// or a text that names a date the server did not recommend, is replaced by
// ai.PlainExplanation. Without any availability there is nothing to explain.
func (s *server) explanation(ctx context.Context, st workshopState, rec domain.Recommendation, names map[string]string, availabilities int) string {
	if availabilities == 0 || rec.DateID == "" {
		return ""
	}
	in := ai.ExplainInput{
		WorkshopTitle:  st.w.Title,
		Dates:          st.dateOptions(),
		Recommendation: rec,
		Names:          names,
		Minimum:        st.w.Minimum,
	}
	key, err := explanationKey(st.w.ID, in)
	if err != nil {
		s.Log.Error("explain recommendation: cache key", "workshop_id", st.w.ID, "error", err)
		return ai.PlainExplanation(in)
	}
	now := s.Clock.Now()
	if text, ok := s.explanations.get(key, now); ok {
		return text
	}

	actx, cancel := context.WithTimeout(ctx, explainTimeout)
	defer cancel()
	text, meta, err := s.AI.Explain(actx, in)
	switch {
	case err != nil:
		s.Log.Error("explain recommendation", "workshop_id", st.w.ID, "error", err)
		text = ai.PlainExplanation(in)
		s.explanations.put(key, text, now.Add(explanationRetryAfter))
		return text
	case namesAnotherDate(text, rec.DateID, st.dates, st.loc):
		s.Log.Warn("explanation named a date the server did not recommend; using the plain explanation",
			"workshop_id", st.w.ID, "model", meta.Model)
		text = ai.PlainExplanation(in)
	}
	s.explanations.put(key, text, time.Time{})
	return text
}

// explanationKey is a SHA-256 of everything an explanation depends on: the
// workshop, the dates, the recommendation with its compatibility table, the
// names and the minimum. encoding/json sorts map keys, so equal inputs hash
// alike.
func explanationKey(workshopID string, in ai.ExplainInput) ([sha256.Size]byte, error) {
	raw, err := json.Marshal(struct {
		WorkshopID string
		Input      ai.ExplainInput
	}{workshopID, in})
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(raw), nil
}

// namesAnotherDate reports whether text names a candidate date other than
// the recommended one, by its day of the month ("15 Nov", "Nov 15") or, when
// it falls on another weekday, by its weekday ("Sunday").
func namesAnotherDate(text, recommendedID string, dates []store.CandidateDateRow, loc *time.Location) bool {
	var rec time.Time
	found := false
	for _, d := range dates {
		if d.ID == recommendedID {
			rec, found = d.StartsAt.In(loc), true
		}
	}
	if !found {
		return false
	}
	for _, d := range dates {
		if d.ID == recommendedID {
			continue
		}
		start := d.StartsAt.In(loc)
		var forms []string
		if y, m, day := start.Date(); y != rec.Year() || m != rec.Month() || day != rec.Day() {
			forms = append(forms, start.Format("2 Jan"), start.Format("Jan 2"), start.Format("2 January"), start.Format("January 2"))
		}
		if start.Weekday() != rec.Weekday() {
			forms = append(forms, start.Weekday().String())
		}
		for _, f := range forms {
			if regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(f) + `\b`).MatchString(text) {
				return true
			}
		}
	}
	return false
}

// explanationCache keeps up to explanationCacheSize explanations and evicts
// the oldest first. An entry with a retry time is served until then.
type explanationCache struct {
	mu      sync.Mutex
	entries map[[sha256.Size]byte]cachedExplanation
	order   [][sha256.Size]byte
}

type cachedExplanation struct {
	text    string
	retryAt time.Time // zero: kept until evicted
}

func newExplanationCache() *explanationCache {
	return &explanationCache{entries: make(map[[sha256.Size]byte]cachedExplanation)}
}

func (c *explanationCache) get(key [sha256.Size]byte, now time.Time) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || (!e.retryAt.IsZero() && !now.Before(e.retryAt)) {
		return "", false
	}
	return e.text, true
}

func (c *explanationCache) put(key [sha256.Size]byte, text string, retryAt time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.entries[key]; !ok {
		if len(c.order) >= explanationCacheSize {
			delete(c.entries, c.order[0])
			c.order = c.order[1:]
		}
		c.order = append(c.order, key)
	}
	c.entries[key] = cachedExplanation{text: text, retryAt: retryAt}
}
