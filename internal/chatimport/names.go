package chatimport

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

const (
	maxNameRunes      = 40
	maxStoredNameRune = 60
	maxNameWords      = 6
	maxStackedMarks   = 2
)

var (
	clockInName   = regexp.MustCompile(`\d{1,2}:\d{2}`)
	phoneName     = regexp.MustCompile(`^\+?[\d\s().-]{7,}$`)
	domainToken   = regexp.MustCompile(`\p{L}\.\p{L}{2,}`)
	optionLabel   = regexp.MustCompile(`^(option|choice|opcion) \d+$`)
	fillerRunes   = map[rune]bool{0x115f: true, 0x1160: true, 0x3164: true, 0xffa0: true, 0x2800: true}
	botSenders    = map[string]bool{"meta ai": true}
	labelWords    = buildLabelWords()
	labelWordList = []string{
		"re", "fw", "fwd", "note", "nb", "ps", "p.s.", "p.s", "edit", "update", "todo", "to do", "fyi",
		"reminder", "q", "a", "answer", "question", "eta", "plan", "subject", "option", "options", "choice",
		"time", "date", "when", "where", "location", "address", "venue", "price", "cost", "total",
		"today", "tomorrow", "tonight", "morning", "afternoon", "evening", "weekend",
		"mon", "tue", "tues", "wed", "thu", "thur", "thurs", "fri", "sat", "sun",
		"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday",
		"lunes", "martes", "miercoles", "jueves", "viernes", "sabado",
		"segunda", "terca", "quarta", "quinta", "sexta",
		"lundi", "mardi", "mercredi", "jeudi", "vendredi", "samedi", "dimanche",
	}
)

func buildLabelWords() map[string]bool {
	m := map[string]bool{}
	for _, w := range labelWordList {
		m[w] = true
	}
	return m
}

func cleanName(s string) string {
	s = strings.ToValidUTF8(s, "")
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsControl(r):
			b.WriteRune(' ')
		case unicode.Is(unicode.Cf, r):
		default:
			b.WriteRune(r)
		}
	}
	name := norm.NFC.String(strings.Join(strings.Fields(b.String()), " "))
	if utf8.RuneCountInString(name) > maxStoredNameRune {
		name = strings.TrimSpace(string([]rune(name)[:maxStoredNameRune]))
	}
	return name
}

func validSender(name string) bool {
	n := utf8.RuneCountInString(name)
	if n < 1 || n > maxNameRunes {
		return false
	}
	lower := strings.ToLower(name)
	if strings.Contains(lower, "http") || strings.HasPrefix(name, "[") || strings.Contains(name, "]") {
		return false
	}
	if clockInName.MatchString(name) {
		return false
	}
	words := strings.Fields(name)
	if len(words) > maxNameWords {
		return false
	}
	for _, w := range words {
		lw := strings.ToLower(w)
		if strings.Contains(lw, "://") || strings.HasPrefix(lw, "www.") || strings.Contains(lw, "@") ||
			strings.Contains(lw, "/") || domainToken.MatchString(w) {
			return false
		}
	}
	return hasNameRune(name) && !stackedMarks(name) && fold(name) != ""
}

func hasNameRune(name string) bool {
	for _, r := range name {
		if fillerRunes[r] {
			continue
		}
		if unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.Is(unicode.So, r) {
			return true
		}
	}
	return false
}

func stackedMarks(name string) bool {
	run := 0
	for _, r := range name {
		if unicode.Is(unicode.Mn, r) {
			run++
			if run > maxStackedMarks {
				return true
			}
			continue
		}
		run = 0
	}
	return false
}

func plainSender(name string) bool {
	if phoneName.MatchString(name) {
		return true
	}
	hasLetter := false
	for _, r := range name {
		if unicode.IsLetter(r) && !fillerRunes[r] {
			hasLetter = true
			break
		}
	}
	if !hasLetter {
		return false
	}
	f := fold(name)
	return !labelWords[f] && !optionLabel.MatchString(f)
}

func isBot(name string) bool {
	return botSenders[fold(name)]
}

func fold(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	out, _, err := transform.String(t, strings.ToLower(strings.Join(strings.Fields(s), " ")))
	if err != nil {
		return strings.ToLower(s)
	}
	return out
}

func SameName(a, b string) bool {
	return fold(a) == fold(b)
}

func nameWords(s string) string {
	return strings.Join(strings.FieldsFunc(fold(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }), " ")
}

func StartsWithName(name, first string) bool {
	want, got := nameWords(first), nameWords(name)
	return want != "" && (got == want || strings.HasPrefix(got, want+" "))
}
