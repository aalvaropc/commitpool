package httpapi

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/aalvaropc/commitpool/internal/ai"
	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/store"
)

const (
	explainTimeout        = 15 * time.Second
	explanationRetryAfter = time.Minute
)

var afterExplanation atomic.Pointer[func()]

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
	release, err := s.acquireAI(ctx)
	if err != nil {
		s.Log.Warn("explain recommendation: no interpreter slot; using the plain explanation for now", "workshop_id", st.w.ID)
		return ai.PlainExplanation(in), s.Clock.Now().Add(explanationRetryAfter)
	}
	defer release()
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

func explanationKey(workshopID string, in ai.ExplainInput) [sha256.Size]byte {
	return sha256.Sum256([]byte(workshopID + "\n" + ai.ExplainFacts(in)))
}

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
			forms = append(forms, dayOfMonthForms(start)...)
		}
		if start.Weekday() != rec.Weekday() {
			weekday := start.Weekday().String()
			forms = append(forms, weekday, weekday[:3])
		}
		for _, f := range forms {
			if containsWord(lower, strings.ToLower(f)) {
				return true
			}
		}
	}
	return false
}

func dayOfMonthForms(t time.Time) []string {
	nth := ordinal(t.Day())
	return []string{
		t.Format("2 Jan"), t.Format("Jan 2"), t.Format("2 January"), t.Format("January 2"),
		nth + " " + t.Format("Jan"), t.Format("Jan") + " " + nth,
		nth + " " + t.Format("January"), t.Format("January") + " " + nth, nth + " of " + t.Format("January"),
	}
}

func ordinal(day int) string {
	suffix := "th"
	if day%100 < 11 || day%100 > 13 {
		switch day % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return strconv.Itoa(day) + suffix
}

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

func isWordRune(r rune) bool {
	return r != utf8.RuneError && (unicode.IsLetter(r) || unicode.IsDigit(r))
}
