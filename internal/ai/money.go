package ai

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const number = `(\d[\d,]*(?:\.\d+)?)`

var (
	amountPatterns = []*regexp.Regexp{
		regexp.MustCompile(`\p{Sc}\s?` + number),
		regexp.MustCompile(number + `\s?\p{Sc}`),
		regexp.MustCompile(`(?i)\b` + number + `[\s-]?(?:dollars?|euros?|pounds?|bucks|cents?|pesos?|rupees?|yen)\b`),
	}
	codeAmount    = regexp.MustCompile(`\b([A-Z]{3})\s?` + number + `|` + number + `\s?([A-Z]{3})\b`)
	notCurrency   = []string{"UTC", "GMT", "EST", "EDT", "CST", "CDT", "MST", "MDT", "PST", "PDT", "CET", "BST"}
	moneyTalk     = regexp.MustCompile(`(?i)\b(?:dollars?|euros?|bucks|cents?|pesos?|rupees?|for free|free of charge|refund(?:s|ed|ing)?)\b|\bfree (?:now|class|session|workshop|lesson|entry|admission)\b|\b(?:class|session|workshop|lesson) is (?:now )?free\b`)
	paymentTalk   = regexp.MustCompile(`(?i)\b(?:pay|pays|paid|paying|payments?|charges?|charged|charging|prices?|costs?)\b`)
	withoutCommas = strings.NewReplacer(",", "")
)

func normalAmount(raw string) string {
	digits := withoutCommas.Replace(raw)
	if f, err := strconv.ParseFloat(digits, 64); err == nil {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	return digits
}

func amounts(s string) map[string]bool {
	out := map[string]bool{}
	for _, re := range amountPatterns {
		for _, m := range re.FindAllStringSubmatch(s, -1) {
			out[normalAmount(m[1])] = true
		}
	}
	for _, m := range codeAmount.FindAllStringSubmatch(s, -1) {
		code, raw := m[1], m[2]
		if code == "" {
			code, raw = m[4], m[3]
		}
		if !slices.Contains(notCurrency, code) {
			out[normalAmount(raw)] = true
		}
	}
	return out
}

func phrases(re *regexp.Regexp, s string) map[string]bool {
	out := map[string]bool{}
	for _, m := range re.FindAllString(s, -1) {
		out[strings.ToLower(m)] = true
	}
	return out
}

func newTo(found, given map[string]bool) bool {
	for k := range found {
		if !given[k] {
			return true
		}
	}
	return false
}

func MentionsMoney(s string) bool {
	return len(amounts(s)) > 0 || moneyTalk.MatchString(s) || paymentTalk.MatchString(s)
}

func moneyNotIn(text string, in SuggestInput) bool {
	title := in.WorkshopTitle
	if newTo(amounts(text), amounts(title)) || newTo(phrases(moneyTalk, text), phrases(moneyTalk, title)) {
		return true
	}
	return in.Kind == groupReplyKind && newTo(phrases(paymentTalk, text), phrases(paymentTalk, title))
}
