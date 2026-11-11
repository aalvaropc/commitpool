package chatimport

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func messagesOf(c Chat, name string) []string {
	for _, p := range c.People {
		if p.Name == name {
			return p.Messages
		}
	}
	return nil
}

func TestParse_IOSGroupEventsAreNotPeople(t *testing.T) {
	lines := []string{
		"[11/10/26, 9:10:41\u202fPM] Pottery class: \u200eMessages and calls are end-to-end encrypted. No one outside of this chat can read them.",
		"\u200e[11/10/26, 9:10:41\u202fPM] Pottery class: \u200eIris Calder created group “Pottery class”",
		"\u200e[11/10/26, 9:11:00\u202fPM] Pottery class: \u200eIris Calder added Priya Raman",
		"\u200e[11/10/26, 9:11:00\u202fPM] Pottery class: \u200eBen Whitaker joined using this group's invite link",
		"\u200e[11/10/26, 9:12:00\u202fPM] Pottery class: \u200eIris Calder changed the group description",
		"[11/10/26, 9:14:00\u202fPM] Priya Raman: Saturday works",
		"\u200e[11/10/26, 9:15:00\u202fPM] Pottery class: \u200eBen Whitaker left",
	}
	c := mustParse(t, strings.Join(lines, "\n"))
	if got := names(c); !reflect.DeepEqual(got, []string{"Priya Raman"}) {
		t.Fatalf("names = %v", got)
	}
	if c.Skipped != 6 {
		t.Fatalf("skipped = %d, want 6", c.Skipped)
	}
}

func TestParse_GroupNameFromTheEncryptionNoticeNeverBecomesAPerson(t *testing.T) {
	text := strings.Join([]string{
		"[11/10/26, 9:10:41 PM] Pottery class: \u200eMessages and calls are end-to-end encrypted. No one outside of this chat can read them.",
		"[11/10/26, 9:11:00 PM] Pottery class: Iris Calder added Priya Raman",
		"[11/10/26, 9:14:00 PM] Priya Raman: Saturday works",
	}, "\n")
	c := mustParse(t, text)
	if got := names(c); !reflect.DeepEqual(got, []string{"Priya Raman"}) {
		t.Fatalf("names = %v", got)
	}
}

func TestParse_OnlyEventsIsNoLines(t *testing.T) {
	text := "\u200e[11/10/26, 9:11:00 PM] Pottery class: \u200eIris added Priya\n\u200e[11/10/26, 9:12:00 PM] Pottery class: \u200eBen left"
	if _, err := Parse(text); err != ErrNoLines {
		t.Fatalf("err = %v, want ErrNoLines", err)
	}
}

func TestParse_KeepsRealMessagesThatStartWithEventWords(t *testing.T) {
	rows := []struct {
		name string
		text string
		want []string
	}{
		{"added with continuation", "Added Ben to the carpool\nI can only do Saturday morning", []string{"Added Ben to the carpool\nI can only do Saturday morning"}},
		{"you added me", "You added me to the wrong group lol, Saturday works", []string{"You added me to the wrong group lol, Saturday works"}},
		{"removed", "Removed my Sunday conflict, both days work now", []string{"Removed my Sunday conflict, both days work now"}},
		{"you changed the group", "you changed the group time? fine, Sunday", []string{"you changed the group time? fine, Sunday"}},
		{"added bonus", "added bonus: I can do Sunday too", []string{"added bonus: I can do Sunday too"}},
		{"left", "Left the office at 5, Saturday works", []string{"Left the office at 5, Saturday works"}},
		{"joined", "Joined late, Sunday then", []string{"Joined late, Sunday then"}},
		{"created group", "Created group chat for the carpool, Saturday works", []string{"Created group chat for the carpool, Saturday works"}},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			c := mustParse(t, "11/10/26, 21:14 - Iris: "+strings.ReplaceAll(r.text, "\n", "\n"))
			if got := messagesOf(c, "Iris"); !reflect.DeepEqual(got, r.want) {
				t.Fatalf("iris = %q, want %q (skipped %d)", got, r.want, c.Skipped)
			}
			c = mustParse(t, "Iris: "+r.text)
			if got := messagesOf(c, "Iris"); !reflect.DeepEqual(got, r.want) {
				t.Fatalf("plain iris = %q, want %q", got, r.want)
			}
		})
	}
}

func TestParse_EncryptedWordsInARealMessage(t *testing.T) {
	c := mustParse(t, "11/10/26, 21:14 - Priya Raman: is this end-to-end encrypted? anyway Saturday works")
	if got := messagesOf(c, "Priya Raman"); len(got) != 1 {
		t.Fatalf("dated: %q", got)
	}
	c = mustParse(t, "Ben: Sunday\nPriya Raman: is this end-to-end encrypted? anyway Saturday works")
	if got := names(c); !reflect.DeepEqual(got, []string{"Ben", "Priya Raman"}) {
		t.Fatalf("plain names = %v", got)
	}
	if got := messagesOf(c, "Ben"); !reflect.DeepEqual(got, []string{"Sunday"}) {
		t.Fatalf("ben = %q", got)
	}
}

func TestParse_Locales(t *testing.T) {
	rows := []struct {
		name  string
		text  string
		names []string
		msgs  int
	}{
		{"spanish android", "10/11/26, 9:14 p. m. - Lucía: el sábado\n10/11/26, 9:15 a. m. - Lucía: o domingo", []string{"Lucía"}, 2},
		{"spanish ios", "[10/11/26, 9:14:03 p. m.] Lucía: el sábado", []string{"Lucía"}, 1},
		{"whatsapp web 24h", "[21:14, 10/11/2026] Iris Calder: Saturday works\n[21:15, 10/11/2026] Iris Calder: or Sunday", []string{"Iris Calder"}, 2},
		{"whatsapp web 12h", "[9:14 PM, 11/10/2026] Ben: Sunday", []string{"Ben"}, 1},
		{"iso", "2026-11-10 21:14 - Iris: Saturday", []string{"Iris"}, 1},
		{"dutch dashes", "10-11-2026 21:14 - Iris: Saturday", []string{"Iris"}, 1},
		{"french", "10/11/2026 à 21:14 - Iris: Saturday", []string{"Iris"}, 1},
		{"german dots", "10.11.26, 21:14 - Iris: Samstag", []string{"Iris"}, 1},
		{"german dots with seconds in brackets", "[10.11.26, 21:14:03] Iris: Samstag", []string{"Iris"}, 1},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			c := mustParse(t, r.text)
			if got := names(c); !reflect.DeepEqual(got, r.names) {
				t.Fatalf("names = %q, want %q", got, r.names)
			}
			if got := len(c.People[0].Messages); got != r.msgs {
				t.Fatalf("messages = %d, want %d", got, r.msgs)
			}
		})
	}
}

func TestParse_UnknownHeaderNeverPutsATimestampInAName(t *testing.T) {
	for _, text := range []string{
		"[21:14 10 Nov 2026] Iris: Saturday",
		"21:14 on 10 Nov - Iris: Saturday",
		"[x] 21:14, 10/11/2026] Iris: Saturday",
	} {
		c, err := Parse(text)
		if err != nil {
			continue
		}
		for _, p := range c.People {
			if strings.ContainsAny(p.Name, "[]") || clockInName.MatchString(p.Name) {
				t.Fatalf("%q gave name %q", text, p.Name)
			}
		}
	}
}

func TestParse_PlainLabelsAreNotPeople(t *testing.T) {
	rows := []struct {
		name  string
		text  string
		names []string
		first []string
	}{
		{"weekday abbreviations", "Priya: my options\nSat: yes\nSun: no\nBen: Sunday", []string{"Priya", "Ben"}, []string{"my options\nSat: yes\nSun: no"}},
		{"weekday names", "Iris: options\nSaturday: yes\nSunday: no", []string{"Iris"}, []string{"options\nSaturday: yes\nSunday: no"}},
		{"numbered options", "Priya: options\nOption 1: Saturday\nOption 2: Sunday", []string{"Priya"}, []string{"options\nOption 1: Saturday\nOption 2: Sunday"}},
		{"note", "Iris: Saturday\nNote: bring charcoal", []string{"Iris"}, []string{"Saturday\nNote: bring charcoal"}},
		{"re", "Iris: Saturday\nRe: the venue", []string{"Iris"}, []string{"Saturday\nRe: the venue"}},
		{"ps", "Iris: Saturday\nP.S.: bring charcoal", []string{"Iris"}, []string{"Saturday\nP.S.: bring charcoal"}},
		{"time label", "Iris: options\n10: 30 works", []string{"Iris"}, []string{"options\n10: 30 works"}},
		{"spanish weekday", "Iris: opciones\nSábado: sí", []string{"Iris"}, []string{"opciones\nSábado: sí"}},
		{"www", "Iris: x\nwww.evil.com: hi", []string{"Iris"}, []string{"x\nwww.evil.com: hi"}},
		{"email", "Iris: x\nme@evil.com: hi", []string{"Iris"}, []string{"x\nme@evil.com: hi"}},
		{"domain path", "Iris: x\nevil.com/x: hi", []string{"Iris"}, []string{"x\nevil.com/x: hi"}},
		{"phone sender", "+51 912 345 678: Saturday\nBen: Sunday", []string{"+51 912 345 678", "Ben"}, []string{"Saturday"}},
		{"lowercase name", "lucía: Saturday\nben: Sunday", []string{"lucía", "ben"}, []string{"Saturday"}},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			c := mustParse(t, r.text)
			if got := names(c); !reflect.DeepEqual(got, r.names) {
				t.Fatalf("names = %q, want %q", got, r.names)
			}
			if got := c.People[0].Messages; !reflect.DeepEqual(got, r.first) {
				t.Fatalf("first = %q, want %q", got, r.first)
			}
		})
	}
}

func TestParse_DateLinesInsideMessagesDoNotBreakModes(t *testing.T) {
	c := mustParse(t, "Priya: Options are\n12/11/26, 10:00 or\n13/11/26, 10:00\nBen: first one")
	if got := names(c); !reflect.DeepEqual(got, []string{"Priya", "Ben"}) {
		t.Fatalf("names = %v", got)
	}
	if got := c.People[0].Messages; !reflect.DeepEqual(got, []string{"Options are\n12/11/26, 10:00 or\n13/11/26, 10:00"}) {
		t.Fatalf("priya = %q", got)
	}

	c = mustParse(t, "11/10/26, 21:14 - Priya: I can do\n12/11/26, 10:00 or\n13/11/26, 14:00\nbut not later\n11/10/26, 21:15 - Ben: Sunday")
	if got := c.People[0].Messages; !reflect.DeepEqual(got, []string{"I can do\n12/11/26, 10:00 or\n13/11/26, 14:00\nbut not later"}) {
		t.Fatalf("priya = %q", got)
	}
	if c.Skipped != 0 {
		t.Fatalf("skipped = %d", c.Skipped)
	}
}

func TestParse_PlainSenderLineInADatedPaste(t *testing.T) {
	c := mustParse(t, "11/10/26, 21:14 - Iris: Saturday\nBen: Sunday")
	if got := messagesOf(c, "Iris"); !reflect.DeepEqual(got, []string{"Saturday\nBen: Sunday"}) {
		t.Fatalf("iris = %q", got)
	}
	c = mustParse(t, "11/10/26, 21:14 - Iris: Saturday\n11/10/26, 21:15 - Ben: Sunday\nBen: or Saturday after 3")
	if got := messagesOf(c, "Ben"); !reflect.DeepEqual(got, []string{"Sunday", "or Saturday after 3"}) {
		t.Fatalf("ben = %q", got)
	}
}

func TestParse_NamesAreNFC(t *testing.T) {
	c := mustParse(t, "11/10/26, 21:14 - Lucía: Saturday\n11/10/26, 21:15 - Lucía: Sunday")
	if len(c.People) != 1 {
		t.Fatalf("names = %q", names(c))
	}
	if c.People[0].Name != "Lucía" || utf8.RuneCountInString(c.People[0].Name) != 5 {
		t.Fatalf("name = %q", c.People[0].Name)
	}
}

func TestParse_RejectsInvisibleAndGarbageNames(t *testing.T) {
	rows := map[string]string{
		"hangul filler":   "ㅤ",
		"braille blank":   "⠀",
		"accents only":    "́́́",
		"zalgo":           "B" + strings.Repeat("́", 36) + "en",
		"hangul filler 2": "ᅟᅠ",
	}
	for name, n := range rows {
		t.Run(name, func(t *testing.T) {
			c := mustParse(t, "11/10/26, 21:14 - Iris: Saturday\n11/10/26, 21:15 - "+n+": Sunday")
			if got := names(c); !reflect.DeepEqual(got, []string{"Iris"}) {
				t.Fatalf("names = %q", got)
			}
		})
	}
}

func TestParse_SkipsMediaAndCallPlaceholders(t *testing.T) {
	rows := []string{
		"\u200eLocation: https://maps.google.com/?q=1,2",
		"\u200eMissed voice call",
		"\u200eMissed video call, Tap to call back",
		"\u200e<attached: 00000012-PHOTO-2026-11-10-21-14-00.jpg>",
		"schedule.pdf • 2 pages \u200edocument omitted",
		"view once voice message omitted",
		"This message was deleted by admin Ben.",
		"null",
		"IMG-20261110-WA0001.jpg (file attached)",
		"Waiting for this message. This may take a while.",
		"location: https://maps.google.com/?q=1,2",
	}
	for _, row := range rows {
		t.Run(row, func(t *testing.T) {
			text := "[11/10/26, 9:14 PM] Iris: Saturday\n[11/10/26, 9:15 PM] Ben: " + row
			c := mustParse(t, text)
			if got := names(c); !reflect.DeepEqual(got, []string{"Iris"}) {
				t.Fatalf("names = %q", got)
			}
			if c.Skipped != 1 {
				t.Fatalf("skipped = %d", c.Skipped)
			}
		})
	}
}

func TestParse_SkipsMetaAI(t *testing.T) {
	c := mustParse(t, "Iris: When can you come?\nMeta AI: I can help with that\nBen: Sunday")
	if got := names(c); !reflect.DeepEqual(got, []string{"Iris", "Ben"}) {
		t.Fatalf("names = %v", got)
	}
	if c.Skipped != 1 {
		t.Fatalf("skipped = %d", c.Skipped)
	}
}

func TestParse_LengthIsCheckedBeforeNormalizing(t *testing.T) {
	if _, err := Parse(strings.Repeat("\u200e", 3_000_000)); err != ErrTooLong {
		t.Fatalf("err = %v, want ErrTooLong", err)
	}
}

func TestParse_KeepsJoinersInMessages(t *testing.T) {
	family := "\U0001F469\u200d\U0001F469\u200d\U0001F467"
	c := mustParse(t, "Iris: Saturday works "+family+"\nPriya: می\u200cخواهم Sunday")
	if got := messagesOf(c, "Iris"); !reflect.DeepEqual(got, []string{"Saturday works " + family}) {
		t.Fatalf("iris = %q", got)
	}
	if got := messagesOf(c, "Priya"); !strings.Contains(got[0], "\u200c") {
		t.Fatalf("priya = %q", got)
	}
}

func TestParse_NoSpaceAfterColon(t *testing.T) {
	c := mustParse(t, "Priya: Saturday\nBen:Sunday only")
	if got := names(c); !reflect.DeepEqual(got, []string{"Priya", "Ben"}) {
		t.Fatalf("names = %v", got)
	}
	if got := messagesOf(c, "Ben"); !reflect.DeepEqual(got, []string{"Sunday only"}) {
		t.Fatalf("ben = %q", got)
	}
	c = mustParse(t, "Priya: Saturday\nMeet at 5:30 works\nhttp://x.example/y")
	if len(c.People) != 1 {
		t.Fatalf("names = %v", names(c))
	}
}

func TestParse_KeepsRealMessagesThatLookLikeNotices(t *testing.T) {
	rows := []struct{ name, text string }{
		{"missed voice call sentence", "Missed voice call yesterday sorry, Sunday works"},
		{"missed video call sentence", "Missed video call earlier. Saturday works"},
		{"encryption sentence", "Messages and calls are end-to-end encrypted, ok Saturday"},
		{"encryption sentence with period", "Messages and calls are end-to-end encrypted. Fine, Saturday"},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			c := mustParse(t, "11/10/26, 21:14 - Ana: "+r.text)
			if got := messagesOf(c, "Ana"); !reflect.DeepEqual(got, []string{r.text}) {
				t.Fatalf("ana = %q (skipped %d)", got, c.Skipped)
			}
		})
	}
}

func TestParse_SkipsExactCallPlaceholders(t *testing.T) {
	for _, row := range []string{"Missed voice call", "Missed video call", "Missed voice call, Tap to call back", "Missed group video call"} {
		c := mustParse(t, "11/10/26, 21:14 - Iris: Saturday\n11/10/26, 21:15 - Ben: "+row)
		if got := names(c); !reflect.DeepEqual(got, []string{"Iris"}) || c.Skipped != 1 {
			t.Fatalf("%q: names = %v skipped = %d", row, got, c.Skipped)
		}
	}
}

func TestParse_MonthAndDomingoNamesAreSenders(t *testing.T) {
	c := mustParse(t, "Iris: Saturday works\nApril: Sunday works\nDomingo: Saturday")
	if got := names(c); !reflect.DeepEqual(got, []string{"Iris", "April", "Domingo"}) {
		t.Fatalf("names = %v", got)
	}
}
