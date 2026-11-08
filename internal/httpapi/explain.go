package httpapi

import (
	"context"
	"crypto/sha256"
	"fmt"
	"regexp"
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
	case namesAnotherDate(text, datesStarting(st.dates, func(d store.CandidateDateRow) bool { return d.ID == in.Recommendation.DateID }), st.loc):
		s.Log.Warn("explanation named a date the server did not recommend; using the plain explanation",
			"workshop_id", st.w.ID, "model", meta.Model)
		return ai.PlainExplanation(in), time.Time{}
	}
	return text, time.Time{}
}

func datesStarting(dates []store.CandidateDateRow, keep func(store.CandidateDateRow) bool) []time.Time {
	var out []time.Time
	for _, d := range dates {
		if keep(d) {
			out = append(out, d.StartsAt)
		}
	}
	return out
}

func explanationKey(workshopID string, in ai.ExplainInput) [sha256.Size]byte {
	return sha256.Sum256([]byte(workshopID + "\n" + ai.ExplainFacts(in)))
}

var (
	monthName      = `(jan(?:uary)?|feb(?:ruary)?|mar(?:ch)?|apr(?:il)?|may|june?|july?|aug(?:ust)?|sep(?:t(?:ember)?)?|oct(?:ober)?|nov(?:ember)?|dec(?:ember)?)`
	monthThenDay   = regexp.MustCompile(`\b` + monthName + `\.?\s+(\d{1,2})(?:st|nd|rd|th)?\b`)
	dayThenMonth   = regexp.MustCompile(`\b(\d{1,2})(?:st|nd|rd|th)?\s+(?:of\s+)?` + monthName + `\b`)
	weekdayAliases = map[time.Weekday][]string{time.Tuesday: {"tues"}, time.Wednesday: {"weds"}, time.Thursday: {"thur", "thurs"}}
)

func namesAnotherDate(text string, allowed []time.Time, loc *time.Location) bool {
	if len(allowed) == 0 {
		return false
	}
	weekdays := map[time.Weekday]bool{}
	days := map[[2]int]bool{}
	for _, t := range allowed {
		local := t.In(loc)
		weekdays[local.Weekday()] = true
		days[[2]int{int(local.Month()), local.Day()}] = true
	}
	lower := strings.ToLower(text)
	for wd := time.Sunday; wd <= time.Saturday; wd++ {
		if weekdays[wd] {
			continue
		}
		name := strings.ToLower(wd.String())
		for _, form := range append([]string{name, name[:3]}, weekdayAliases[wd]...) {
			if containsWord(lower, form) {
				return true
			}
		}
	}
	for _, m := range monthThenDay.FindAllStringSubmatch(lower, -1) {
		if !allowedDay(days, m[1], m[2]) {
			return true
		}
	}
	for _, m := range dayThenMonth.FindAllStringSubmatch(lower, -1) {
		if !allowedDay(days, m[2], m[1]) {
			return true
		}
	}
	return false
}

func allowedDay(days map[[2]int]bool, month, day string) bool {
	t, err := time.Parse("Jan", strings.ToUpper(month[:1])+month[1:3])
	if err != nil {
		return true
	}
	n, err := strconv.Atoi(day)
	if err != nil {
		return true
	}
	return days[[2]int{int(t.Month()), n}]
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
