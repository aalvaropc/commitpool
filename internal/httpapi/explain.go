package httpapi

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

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

// afterExplanation, when set, runs each time a background explanation has
// been stored; tests wait on it.
var afterExplanation atomic.Pointer[func()]

// explanation puts the recommendation into words for the owner view and
// reports whether a better text is still being written. The recommendation
// itself, and so its date, always comes from domain.Recommend; the
// interpreter only phrases it. The view never waits for the interpreter: on a
// cache miss it answers with no text and pending true, and the explanation is
// written once in the background (explainInBackground), however many views
// ask meanwhile. An entry past its retry time is served while it is written
// again, also as pending. Without any availability there is nothing to
// explain.
func (s *server) explanation(ctx context.Context, st workshopState, rec domain.Recommendation, names map[string]string, availabilities int) (text string, pending bool) {
	if availabilities == 0 || rec.DateID == "" {
		return "", false
	}
	in := ai.ExplainInput{
		WorkshopTitle:  st.w.Title,
		Dates:          st.dateOptions(),
		Recommendation: rec,
		Names:          names,
		Minimum:        st.w.Minimum,
	}
	key := explanationKey(st.w.ID, in)
	text, fresh, start := s.explanations.claim(key, s.Clock.Now())
	if fresh {
		return text, false
	}
	if start {
		s.explainInBackground(ctx, key, st, in)
	}
	return text, true
}

// explainInBackground asks the interpreter for one explanation on its own
// goroutine, bounded by explainTimeout but not by the request that started
// it, and stores the result for later views. A failed call, a panic or a
// text that names a date the server did not recommend leaves
// ai.PlainExplanation instead; after a failure it is served for
// explanationRetryAfter before the interpreter is asked again.
func (s *server) explainInBackground(ctx context.Context, key [sha256.Size]byte, st workshopState, in ai.ExplainInput) {
	bctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), explainTimeout)
	go func() {
		defer cancel()
		text, retryAt := s.writeExplanation(bctx, st, in)
		s.explanations.finish(key, text, retryAt)
		if f := afterExplanation.Load(); f != nil {
			(*f)()
		}
	}()
}

func (s *server) writeExplanation(ctx context.Context, st workshopState, in ai.ExplainInput) (text string, retryAt time.Time) {
	defer func() {
		if p := recover(); p != nil {
			s.Log.Error("explain recommendation panicked", "workshop_id", st.w.ID, "panic", fmt.Sprint(p))
			text, retryAt = ai.PlainExplanation(in), s.Clock.Now().Add(explanationRetryAfter)
		}
	}()
	text, meta, err := s.AI.Explain(ctx, in)
	switch {
	case err != nil:
		s.Log.Error("explain recommendation", "workshop_id", st.w.ID, "error", err)
		return ai.PlainExplanation(in), s.Clock.Now().Add(explanationRetryAfter)
	case namesAnotherDate(text, in.Recommendation.DateID, st.dates, st.loc):
		s.Log.Warn("explanation named a date the server did not recommend; using the plain explanation",
			"workshop_id", st.w.ID, "model", meta.Model)
		return ai.PlainExplanation(in), time.Time{}
	}
	return text, time.Time{}
}

// explanationKey is a SHA-256 of the workshop and of exactly what the model
// is told, ai.ExplainFacts, so a change the model would not see (another
// date's blockers, the name of someone who only matters elsewhere) does not
// ask for a new explanation.
func explanationKey(workshopID string, in ai.ExplainInput) [sha256.Size]byte {
	return sha256.Sum256([]byte(workshopID + "\n" + ai.ExplainFacts(in)))
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
	lower := strings.ToLower(text)
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
			if containsWord(lower, strings.ToLower(f)) {
				return true
			}
		}
	}
	return false
}

// containsWord reports whether phrase occurs in text as whole words, with no
// letter or digit right before or after it. Both are already lower case.
func containsWord(text, phrase string) bool {
	if phrase == "" {
		return false
	}
	for from := 0; from <= len(text); {
		i := strings.Index(text[from:], phrase)
		if i < 0 {
			return false
		}
		start, end := from+i, from+i+len(phrase)
		before, _ := utf8.DecodeLastRuneInString(text[:start])
		after, _ := utf8.DecodeRuneInString(text[end:])
		if !isWordRune(before) && !isWordRune(after) {
			return true
		}
		_, size := utf8.DecodeRuneInString(text[start:])
		from = start + size
	}
	return false
}

// isWordRune is true for a letter or a digit; utf8.RuneError, which marks
// the start or end of the text, is neither.
func isWordRune(r rune) bool {
	return r != utf8.RuneError && (unicode.IsLetter(r) || unicode.IsDigit(r))
}

// explanationCache keeps up to explanationCacheSize explanations and evicts
// the oldest first. An entry with a retry time is fresh until then. inFlight
// marks the keys being written, so each is written once at a time.
type explanationCache struct {
	mu       sync.Mutex
	entries  map[[sha256.Size]byte]cachedExplanation
	order    [][sha256.Size]byte
	inFlight map[[sha256.Size]byte]bool
}

type cachedExplanation struct {
	text    string
	retryAt time.Time // zero: kept until evicted
}

func newExplanationCache() *explanationCache {
	return &explanationCache{
		entries:  make(map[[sha256.Size]byte]cachedExplanation),
		inFlight: make(map[[sha256.Size]byte]bool),
	}
}

// claim returns the entry for key. A fresh one is the answer. Otherwise text
// is whatever stale text there is, and start is true for the one caller that
// must write the explanation: the key is marked in flight until finish.
func (c *explanationCache) claim(key [sha256.Size]byte, now time.Time) (text string, fresh, start bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if ok && (e.retryAt.IsZero() || now.Before(e.retryAt)) {
		return e.text, true, false
	}
	if c.inFlight[key] {
		return e.text, false, false
	}
	c.inFlight[key] = true
	return e.text, false, true
}

// finish stores the written explanation and clears the key's in-flight mark.
func (c *explanationCache) finish(key [sha256.Size]byte, text string, retryAt time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.inFlight, key)
	if _, ok := c.entries[key]; !ok {
		if len(c.order) >= explanationCacheSize {
			delete(c.entries, c.order[0])
			c.order = c.order[1:]
		}
		c.order = append(c.order, key)
	}
	c.entries[key] = cachedExplanation{text: text, retryAt: retryAt}
}
