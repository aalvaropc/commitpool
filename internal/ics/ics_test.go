package ics_test

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/aalvaropc/commitpool/internal/ics"
)

func event() ics.Event {
	return ics.Event{
		UID:         "8f2c@commitpool",
		Sequence:    2,
		Stamp:       time.Date(2026, 11, 6, 14, 30, 0, 0, time.UTC),
		Start:       time.Date(2026, 11, 14, 10, 0, 0, 0, mustZone("America/New_York")),
		End:         time.Date(2026, 11, 14, 12, 0, 0, 0, mustZone("America/New_York")),
		Summary:     "Wheel-throwing for beginners",
		Description: "With Maya Okafor.\nInvite: https://example.test/w/abc",
		Location:    "Maya's studio · Greenpoint, Brooklyn",
		URL:         "https://example.test/w/abc",
		Status:      ics.Confirmed,
	}
}

func mustZone(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

func unfold(s string) string {
	return strings.ReplaceAll(s, "\r\n ", "")
}

func TestCalendar_IsAValidPublishedEvent(t *testing.T) {
	got := string(ics.Calendar(event()))
	if !strings.HasSuffix(got, "\r\n") || strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Fatalf("every line must end in CRLF:\n%q", got)
	}
	lines := strings.Split(strings.TrimSuffix(unfold(got), "\r\n"), "\r\n")
	want := []string{
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"PRODID:-//CommitPool//Workshops//EN",
		"CALSCALE:GREGORIAN",
		"METHOD:PUBLISH",
		"BEGIN:VEVENT",
		"UID:8f2c@commitpool",
		"SEQUENCE:2",
		"DTSTAMP:20261106T143000Z",
		"DTSTART:20261114T150000Z",
		"DTEND:20261114T170000Z",
		"SUMMARY:Wheel-throwing for beginners",
		`DESCRIPTION:With Maya Okafor.\nInvite: https://example.test/w/abc`,
		"LOCATION:Maya's studio · Greenpoint\\, Brooklyn",
		"URL:https://example.test/w/abc",
		"STATUS:CONFIRMED",
		"END:VEVENT",
		"END:VCALENDAR",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("calendar:\n%s\nwant:\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
}

func TestCalendar_EscapesText(t *testing.T) {
	e := event()
	e.Summary = `Clay; glaze, fire \ repeat`
	e.Description = "Line one\r\nLine two\nLine three"
	got := unfold(string(ics.Calendar(e)))
	if !strings.Contains(got, `SUMMARY:Clay\; glaze\, fire \\ repeat`+"\r\n") {
		t.Errorf("summary not escaped:\n%s", got)
	}
	if !strings.Contains(got, `DESCRIPTION:Line one\nLine two\nLine three`+"\r\n") {
		t.Errorf("description newlines not escaped:\n%s", got)
	}
}

func TestCalendar_FoldsLongLinesWithoutSplittingCharacters(t *testing.T) {
	e := event()
	e.Description = strings.Repeat("Cerámica en el torno — ", 12)
	raw := string(ics.Calendar(e))
	for _, line := range strings.Split(strings.TrimSuffix(raw, "\r\n"), "\r\n") {
		if len(line) > 75 {
			t.Errorf("line of %d octets: %q", len(line), line)
		}
		if !utf8.ValidString(line) {
			t.Errorf("line splits a character: %q", line)
		}
	}
	if !strings.Contains(unfold(raw), "DESCRIPTION:"+strings.Repeat("Cerámica en el torno — ", 12)+"\r\n") {
		t.Error("unfolding does not give the description back")
	}
}

func TestCalendar_LeavesOutEmptyFields(t *testing.T) {
	e := event()
	e.Location, e.URL, e.Description = "", "", ""
	got := string(ics.Calendar(e))
	for _, field := range []string{"LOCATION:", "URL:", "DESCRIPTION:"} {
		if strings.Contains(got, field) {
			t.Errorf("empty %s written", field)
		}
	}
}

func TestCalendar_StatusFollowsTheWorkshop(t *testing.T) {
	for _, s := range []ics.Status{ics.Tentative, ics.Confirmed, ics.Cancelled} {
		e := event()
		e.Status = s
		if got := string(ics.Calendar(e)); !strings.Contains(got, "STATUS:"+string(s)+"\r\n") {
			t.Errorf("status %s missing", s)
		}
	}
}

func TestCalendar_DropsControlCharacters(t *testing.T) {
	e := event()
	e.Summary = "Clay\x00\x07 night\tout"
	if got := unfold(string(ics.Calendar(e))); !strings.Contains(got, "SUMMARY:Clay night\tout\r\n") {
		t.Errorf("summary:\n%s", got)
	}
}

func TestCalendar_FoldsEvenBrokenText(t *testing.T) {
	e := event()
	e.Description = strings.Repeat("\x80", 300)
	raw := string(ics.Calendar(e))
	for _, line := range strings.Split(strings.TrimSuffix(raw, "\r\n"), "\r\n") {
		if len(line) > 75 {
			t.Fatalf("line of %d octets", len(line))
		}
	}
}
