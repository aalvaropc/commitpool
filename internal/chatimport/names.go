package chatimport

import (
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
)

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
	name := strings.Join(strings.Fields(b.String()), " ")
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
	if strings.Contains(strings.ToLower(name), "http") {
		return false
	}
	return len(strings.Fields(name)) <= maxNameWords
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
