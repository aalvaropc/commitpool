package ai

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var (
	draftHost      = regexp.MustCompile(`\b(?:I['’]m|I am|[Mm]y name is)\s+(\p{Lu}[\p{L}'-]+(?:\s+\p{Lu}[\p{L}'-]+)?)`)
	draftTitle     = regexp.MustCompile(`(?i)\b(?:teach|teaching|run|running|host|hosting)\s+(?:an?\s+)?(.+?)(?:\s+at\s|\s+in\s|[.,;]|$)`)
	draftLeadTitle = regexp.MustCompile(`^([^.,;$]+?)\s+with\s+\p{Lu}`)
	draftPlace     = regexp.MustCompile(`\bat\s+(\D[^.;]*)`)
	draftPrice     = regexp.MustCompile(`(?i)\$\s?(\d+(?:\.\d{1,2})?)\s*(?:a|per|each|/)\s*(?:person|head|seat|spot|participant|student)`)
	draftVenue     = regexp.MustCompile(`(?i)\b(?:studio|room|venue|space|rent|hall)\b[^.$]*\$\s?(\d+(?:\.\d{1,2})?)`)
	draftCapacity  = regexp.MustCompile(`(?i)\b(?:(\d+)\s+(?:wheels|spots|seats|places|mats|stations|easels)|up to\s+(\d+)|max(?:imum)?(?: of)?\s+(\d+))\b`)
	draftMinimum   = regexp.MustCompile(`(?i)\b(?:need|needs)\s+(?:at least\s+)?(\d+)|\bat least\s+(\d+)\s+(?:people|students|participants)`)
	draftHours     = regexp.MustCompile(`(?i)\b(\d+|one|two|three|four|five|six)\s+hours?\b`)
	draftMinutes   = regexp.MustCompile(`(?i)\b(\d+)\s*(?:min|mins|minutes)\b`)
	draftRange     = regexp.MustCompile(`(?i)\b(\d{1,2})(?::(\d{2}))?\s*(am|pm)?\s*(?:to|until|-|–)\s*(\d{1,2})(?::(\d{2}))?\s*(am|pm)?\b`)
	draftAt        = regexp.MustCompile(`(?i)\bat\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm)\b`)
	draftDay       = regexp.MustCompile(`(?i)\b(?:(jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)[a-z]*\.?\s+(\d{1,2})(?:st|nd|rd|th)?|(sunday|monday|tuesday|wednesday|thursday|friday|saturday))\b`)
	draftWordCount = map[string]int{"one": 1, "two": 2, "three": 3, "four": 4, "five": 5, "six": 6}
	draftZones     = []struct{ place, zone string }{
		{"brooklyn", "America/New_York"}, {"manhattan", "America/New_York"}, {"queens", "America/New_York"}, {"new york", "America/New_York"},
		{"chicago", "America/Chicago"}, {"los angeles", "America/Los_Angeles"}, {"san francisco", "America/Los_Angeles"},
		{"toronto", "America/Toronto"}, {"london", "Europe/London"}, {"paris", "Europe/Paris"}, {"berlin", "Europe/Berlin"},
	}
)

func (Fake) Draft(_ context.Context, in DraftInput) (Draft, Meta, error) {
	text := in.Text
	d := Draft{
		HostName:  firstGroup(draftHost, text),
		Location:  place(text),
		Price:     firstGroup(draftPrice, text),
		VenueCost: firstGroup(draftVenue, text),
		Capacity:  firstNumber(draftCapacity, text),
		Minimum:   firstNumber(draftMinimum, text),
	}
	d.Timezone = zoneOf(d.Location)
	d.Title = capitalize(firstGroup(draftTitle, text))
	if d.Title == "" {
		d.Title = capitalize(firstGroup(draftLeadTitle, text))
	}
	d.DurationMin = duration(text)
	d.Dates = sessions(text, in.Today)
	if d.DurationMin == 0 {
		d.DurationMin = rangeMinutes(text)
	}
	return d, Meta{Provider: fakeProvider, Model: fakeModel}, nil
}

func zoneOf(place string) string {
	lower := strings.ToLower(place)
	for _, z := range draftZones {
		if strings.Contains(lower, z.place) {
			return z.zone
		}
	}
	return ""
}

func firstGroup(re *regexp.Regexp, text string) string {
	m := re.FindStringSubmatch(text)
	if m == nil {
		return ""
	}
	for _, g := range m[1:] {
		if g != "" {
			return strings.TrimSpace(g)
		}
	}
	return ""
}

func firstNumber(re *regexp.Regexp, text string) int {
	n, _ := strconv.Atoi(firstGroup(re, text))
	return n
}

func place(text string) string {
	for _, m := range draftPlace.FindAllStringSubmatch(text, -1) {
		if !strings.ContainsAny(m[1][:1], "0123456789") {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

func capitalize(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if size == 0 {
		return ""
	}
	return string(unicode.ToUpper(r)) + s[size:]
}

func duration(text string) int {
	if m := draftHours.FindStringSubmatch(text); m != nil {
		if n, ok := draftWordCount[strings.ToLower(m[1])]; ok {
			return n * 60
		}
		n, _ := strconv.Atoi(m[1])
		return n * 60
	}
	if m := draftMinutes.FindStringSubmatch(text); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}

func hour24(h int, meridiem string) int {
	switch strings.ToLower(meridiem) {
	case "pm":
		if h < 12 {
			return h + 12
		}
	case "am":
		if h == 12 {
			return 0
		}
	default:
		if h >= 1 && h <= 7 {
			return h + 12
		}
	}
	return h
}

func clockOf(h, m string, meridiem string) (int, int) {
	hh, _ := strconv.Atoi(h)
	mm, _ := strconv.Atoi(m)
	return hour24(hh, meridiem), mm
}

func rangeMinutes(text string) int {
	m := draftRange.FindStringSubmatch(text)
	if m == nil {
		return 0
	}
	sh, sm := clockOf(m[1], m[2], m[3])
	eh, em := clockOf(m[4], m[5], m[6])
	if eh*60+em <= sh*60+sm {
		eh += 12
	}
	return eh*60 + em - sh*60 - sm
}

func startClock(text string) (string, bool) {
	if m := draftRange.FindStringSubmatch(text); m != nil {
		h, mm := clockOf(m[1], m[2], m[3])
		return fmt.Sprintf("%02d:%02d", h, mm), true
	}
	return "", false
}

func sessions(text string, today time.Time) []DraftDate {
	shared, hasShared := startClock(text)
	var out []DraftDate
	for _, loc := range draftDay.FindAllStringSubmatchIndex(text, -1) {
		day, ok := dayOf(text, loc, today)
		if !ok {
			continue
		}
		start := shared
		if at := draftAt.FindStringSubmatch(text[loc[1]:min(len(text), loc[1]+16)]); at != nil {
			h, m := clockOf(at[1], at[2], at[3])
			start = fmt.Sprintf("%02d:%02d", h, m)
		} else if !hasShared {
			continue
		}
		d := DraftDate{Date: day.Format(time.DateOnly), Start: start}
		if !slices.Contains(out, d) {
			out = append(out, d)
		}
	}
	return out
}

func dayOf(text string, loc []int, today time.Time) (time.Time, bool) {
	base := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	if loc[6] >= 0 {
		name := strings.ToLower(text[loc[6]:loc[7]])
		want := time.Weekday(slices.Index(weekdays, name))
		ahead := (int(want) - int(base.Weekday()) + 7) % 7
		return base.AddDate(0, 0, ahead+7), true
	}
	month := time.Month(slices.Index([]string{"jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec"}, strings.ToLower(text[loc[2]:loc[3]])) + 1)
	n, err := strconv.Atoi(text[loc[4]:loc[5]])
	if err != nil || n < 1 || n > 31 {
		return time.Time{}, false
	}
	d := time.Date(base.Year(), month, n, 0, 0, 0, 0, time.UTC)
	if !d.After(base) {
		d = d.AddDate(1, 0, 0)
	}
	return d, true
}
