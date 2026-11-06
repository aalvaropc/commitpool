package ics

import (
	"bytes"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

type Status string

const (
	Tentative Status = "TENTATIVE"
	Confirmed Status = "CONFIRMED"
	Cancelled Status = "CANCELLED"
)

const maxLineOctets = 75

type Event struct {
	UID         string
	Sequence    int
	Stamp       time.Time
	Start, End  time.Time
	Summary     string
	Description string
	Location    string
	URL         string
	Status      Status
}

var textEscaper = strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\r\n", `\n`, "\n", `\n`, "\r", `\n`)

func Calendar(events ...Event) []byte {
	var b bytes.Buffer
	line := func(name, value string) { writeFolded(&b, name+":"+value) }
	text := func(name, value string) {
		if value != "" {
			line(name, textEscaper.Replace(strings.Map(dropControls, value)))
		}
	}
	line("BEGIN", "VCALENDAR")
	line("VERSION", "2.0")
	line("PRODID", "-//CommitPool//Workshops//EN")
	line("CALSCALE", "GREGORIAN")
	line("METHOD", "PUBLISH")
	for _, e := range events {
		line("BEGIN", "VEVENT")
		line("UID", e.UID)
		line("SEQUENCE", fmt.Sprint(e.Sequence))
		line("DTSTAMP", utc(e.Stamp))
		line("DTSTART", utc(e.Start))
		line("DTEND", utc(e.End))
		text("SUMMARY", e.Summary)
		text("DESCRIPTION", e.Description)
		text("LOCATION", e.Location)
		if e.URL != "" {
			line("URL", e.URL)
		}
		line("STATUS", string(e.Status))
		line("END", "VEVENT")
	}
	line("END", "VCALENDAR")
	return b.Bytes()
}

func dropControls(r rune) rune {
	if r == '\n' || r == '\r' || r == '\t' {
		return r
	}
	if r < 0x20 || r == 0x7f {
		return -1
	}
	return r
}

func utc(t time.Time) string {
	return t.UTC().Format("20060102T150405Z")
}

func writeFolded(b *bytes.Buffer, s string) {
	limit := maxLineOctets
	for len(s) > limit {
		cut := limit
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		if cut == 0 {
			cut = limit
		}
		b.WriteString(s[:cut])
		b.WriteString("\r\n ")
		s = s[cut:]
		limit = maxLineOctets - 1
	}
	b.WriteString(s)
	b.WriteString("\r\n")
}
