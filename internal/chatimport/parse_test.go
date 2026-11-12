package chatimport

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func names(c Chat) []string {
	out := make([]string, 0, len(c.People))
	for _, p := range c.People {
		out = append(out, p.Name)
	}
	return out
}

func mustParse(t *testing.T, text string) Chat {
	t.Helper()
	c, err := Parse(text)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return c
}

func TestParse_IOSExportWithDirectionMarks(t *testing.T) {
	raw := fixture(t, "ios_export.txt")
	if !strings.Contains(raw, "\u200e") || !strings.Contains(raw, "\u202f") {
		t.Fatal("fixture lost its direction marks")
	}
	c := mustParse(t, raw)
	want := []string{"Iris Calder", "Priya Raman", "Ben Whitaker"}
	if got := names(c); !reflect.DeepEqual(got, want) {
		t.Fatalf("names = %v, want %v", got, want)
	}
	iris := c.People[0].Messages
	wantIris := []string{"I can do Saturday morning, Sunday is tight", "Saturday it is then"}
	if !reflect.DeepEqual(iris, wantIris) {
		t.Fatalf("iris = %q, want %q", iris, wantIris)
	}
	if got := c.People[2].Messages; !reflect.DeepEqual(got, []string{"Sunday afternoon is best"}) {
		t.Fatalf("ben = %q", got)
	}
}

func TestParse_AndroidExport24h(t *testing.T) {
	c := mustParse(t, fixture(t, "android_24h.txt"))
	want := []string{"Lucía Ortega", "Tomás Rivera", "Sam Okafor"}
	if got := names(c); !reflect.DeepEqual(got, want) {
		t.Fatalf("names = %v, want %v", got, want)
	}
	if got := c.People[0].Messages; !reflect.DeepEqual(got, []string{"Saturday morning works", "or Sunday after lunch"}) {
		t.Fatalf("lucia = %q", got)
	}
	if got := c.People[1].Messages; !reflect.DeepEqual(got, []string{"I can only do Sunday"}) {
		t.Fatalf("tomas = %q", got)
	}
}

func TestParse_AndroidExport12h(t *testing.T) {
	c := mustParse(t, fixture(t, "android_12h.txt"))
	want := []string{"Dana Whit", "Kofi Mensah"}
	if got := names(c); !reflect.DeepEqual(got, want) {
		t.Fatalf("names = %v, want %v", got, want)
	}
	if got := c.People[0].Messages; !reflect.DeepEqual(got, []string{"Saturday works\nfor me, but not before nine"}) {
		t.Fatalf("dana = %q", got)
	}
	if got := c.People[1].Messages; !reflect.DeepEqual(got, []string{"Sunday only", "I could do Saturday too"}) {
		t.Fatalf("kofi = %q", got)
	}
}

func TestParse_PlainLines(t *testing.T) {
	c := mustParse(t, "Priya: Saturday works\nBen: Sunday")
	if len(c.People) != 2 {
		t.Fatalf("people = %d, want 2", len(c.People))
	}
	c = mustParse(t, fixture(t, "plain.txt"))
	if got := names(c); !reflect.DeepEqual(got, []string{"Priya", "Ben", "Noor"}) {
		t.Fatalf("names = %v", got)
	}
	if got := c.People[0].Messages; !reflect.DeepEqual(got, []string{"Saturday works", "but only after 10"}) {
		t.Fatalf("priya = %q", got)
	}
}

func TestParse_PlainLinesWithTimes(t *testing.T) {
	c := mustParse(t, "[9:14 PM] Priya: Saturday works\n21:15 Ben: Sunday")
	if got := names(c); !reflect.DeepEqual(got, []string{"Priya", "Ben"}) {
		t.Fatalf("names = %v", got)
	}
	if c.People[1].Messages[0] != "Sunday" {
		t.Fatalf("ben = %q", c.People[1].Messages)
	}
}

func TestParse_MultilineMessage(t *testing.T) {
	text := "11/10/26, 21:14 - Priya: Saturday works\nafter ten though\nnot Sunday: school run\n11/10/26, 21:15 - Ben: Sunday"
	c := mustParse(t, text)
	if got := names(c); !reflect.DeepEqual(got, []string{"Priya", "Ben"}) {
		t.Fatalf("names = %v", got)
	}
	want := "Saturday works\nafter ten though\nnot Sunday: school run"
	if got := c.People[0].Messages; !reflect.DeepEqual(got, []string{want}) {
		t.Fatalf("priya = %q, want %q", got, want)
	}
}

func TestParse_SkipsSystemLines(t *testing.T) {
	lines := []string{
		"[11/10/26, 9:10:41 PM] Group: Messages and calls are end-to-end encrypted. No one outside of this chat can read them.",
		"[11/10/26, 9:11:00 PM] Iris: Saturday works",
		"11/10/26, 21:12 - Ben joined using this group's invite link",
		"11/10/26, 21:12 - Iris added Ben",
		"11/10/26, 21:12 - Ben left",
		"11/10/26, 21:12 - Iris changed the subject to \"Pottery\"",
		"11/10/26, 21:12 - Iris created group \"Pottery\"",
		"11/10/26, 21:13 - Ben: <Media omitted>",
		"11/10/26, 21:13 - Ben: image omitted",
		"11/10/26, 21:13 - Ben: This message was deleted",
		"11/10/26, 21:13 - Ben:",
		"11/10/26, 21:14 - Ben: Sunday works",
	}
	c := mustParse(t, strings.Join(lines, "\n"))
	if got := names(c); !reflect.DeepEqual(got, []string{"Iris", "Ben"}) {
		t.Fatalf("names = %v", got)
	}
	if got := c.People[1].Messages; !reflect.DeepEqual(got, []string{"Sunday works"}) {
		t.Fatalf("ben = %q", got)
	}
	if c.Skipped != len(lines)-2 {
		t.Fatalf("skipped = %d, want %d", c.Skipped, len(lines)-2)
	}
}

func TestParse_KeepsRealMessagesThatMentionSystemWords(t *testing.T) {
	c := mustParse(t, "11/10/26, 21:14 - Priya: I changed the subject of my week, Saturday works\n11/10/26, 21:15 - Ben: I left early last time, Sunday please")
	if len(c.People) != 2 || c.Skipped != 0 {
		t.Fatalf("people = %d skipped = %d", len(c.People), c.Skipped)
	}
}

func TestParse_MergesSameSender(t *testing.T) {
	c := mustParse(t, "lucía: Saturday\nLucía: or Sunday\nLUCÍA: not Friday")
	if len(c.People) != 1 {
		t.Fatalf("people = %v", names(c))
	}
	if c.People[0].Name != "lucía" || len(c.People[0].Messages) != 3 {
		t.Fatalf("person = %+v", c.People[0])
	}
}

func TestParse_KeepsEmojiAndAccentedNames(t *testing.T) {
	c := mustParse(t, "[11/10/26, 9:14 PM] Zoë \U0001F3FA: Saturday\n[11/10/26, 9:15 PM] José María: Sunday")
	if got := names(c); !reflect.DeepEqual(got, []string{"Zoë \U0001F3FA", "José María"}) {
		t.Fatalf("names = %v", got)
	}
}

func TestParse_SanitizesNames(t *testing.T) {
	text := "11/10/26, 21:14 - \u202eAna\u200b\x07\u2066 Ruiz\u202c: Saturday works"
	c := mustParse(t, text)
	if got := names(c); !reflect.DeepEqual(got, []string{"Ana Ruiz"}) {
		t.Fatalf("names = %q", got)
	}

	if got := cleanName("  \t" + strings.Repeat("é", 80) + "  "); utf8.RuneCountInString(got) != 60 {
		t.Fatalf("cleanName length = %d, want 60", utf8.RuneCountInString(got))
	}
	if got := cleanName("a\u200fb\u0000c\u2067d"); got != "ab cd" {
		t.Fatalf("cleanName = %q", got)
	}

	c = mustParse(t, "Priya: Saturday\nsee http://evil.example/x: click\nBen: Sunday")
	if got := names(c); !reflect.DeepEqual(got, []string{"Priya", "Ben"}) {
		t.Fatalf("names = %v", got)
	}
	if got := c.People[0].Messages; !reflect.DeepEqual(got, []string{"Saturday\nsee http://evil.example/x: click"}) {
		t.Fatalf("priya = %q", got)
	}

	c = mustParse(t, "Priya: Saturday\nhttp://evil.example/x: click\nBen: Sunday")
	if len(c.People) != 2 {
		t.Fatalf("a URL name became a sender: %v", names(c))
	}

	c = mustParse(t, "[11/10/26, 9:14 PM] Priya: Saturday\n[11/10/26, 9:15 PM] http://evil.example: hi")
	if len(c.People) != 1 || c.Skipped != 1 {
		t.Fatalf("people = %v skipped = %d", names(c), c.Skipped)
	}

	long := strings.Repeat("x", 41)
	c = mustParse(t, "Priya: Saturday\n"+long+": hello\nBen: Sunday")
	if len(c.People) != 2 {
		t.Fatalf("a 41 rune name became a sender: %v", names(c))
	}

	c = mustParse(t, "Priya: Saturday\none two three four five six seven: hello\nBen: Sunday")
	if len(c.People) != 2 {
		t.Fatalf("a 7 word name became a sender: %v", names(c))
	}
}

func TestParse_Limits(t *testing.T) {
	if _, err := Parse("Priya: " + strings.Repeat("a", MaxChars-7)); err != nil {
		t.Fatalf("exactly %d characters: %v", MaxChars, err)
	}
	if _, err := Parse("Priya: " + strings.Repeat("a", MaxChars-6)); err != ErrTooLong {
		t.Fatalf("20001 characters: err = %v, want ErrTooLong", err)
	}
	if _, err := Parse(strings.Repeat("é", MaxChars+1)); err != ErrTooLong {
		t.Fatalf("20001 runes: err = %v, want ErrTooLong", err)
	}

	var thirty, thirtyOne strings.Builder
	for i := 0; i < 31; i++ {
		line := "Person" + string(rune('A'+i%26)) + string(rune('a'+i/26)) + ": free Saturday\n"
		if i < 30 {
			thirty.WriteString(line)
		}
		thirtyOne.WriteString(line)
	}
	c, err := Parse(thirty.String())
	if err != nil || len(c.People) != 30 {
		t.Fatalf("30 senders: %d people, err = %v", len(c.People), err)
	}
	if _, err := Parse(thirtyOne.String()); err != ErrTooManyPeople {
		t.Fatalf("31 senders: err = %v, want ErrTooManyPeople", err)
	}

	for _, empty := range []string{"", " \n\t \n", "\u200e\u202f \n"} {
		if _, err := Parse(empty); err != ErrEmpty {
			t.Fatalf("%q: err = %v, want ErrEmpty", empty, err)
		}
	}

	prose := "Hello everyone, we should pick a day for the pottery class\nI think the weekend would be best for most people"
	if _, err := Parse(prose); err != ErrNoLines {
		t.Fatalf("prose: err = %v, want ErrNoLines", err)
	}
	if _, err := Parse("11/10/26, 21:12 - Ben left\n11/10/26, 21:13 - Ben: <Media omitted>"); err != ErrNoLines {
		t.Fatalf("only system lines: err = %v, want ErrNoLines", err)
	}
}

func TestPerson_TextKeepsNewest(t *testing.T) {
	var msgs []string
	for i := 0; i < 50; i++ {
		msgs = append(msgs, strings.Repeat(string(rune('a'+i%26)), 99)+string(rune('0'+i%10)))
	}
	p := Person{Name: "Priya", Messages: msgs}
	got := p.Text(MaxTextRunes)
	if n := utf8.RuneCountInString(got); n > MaxTextRunes {
		t.Fatalf("runes = %d, want <= %d", n, MaxTextRunes)
	}
	if !strings.HasSuffix(got, msgs[len(msgs)-1]) {
		t.Fatal("the newest message was dropped")
	}
	if strings.Contains(got, msgs[0]) {
		t.Fatal("the oldest message was kept")
	}
	if want := strings.Join(msgs[len(msgs)-19:], "\n"); got != want {
		t.Fatalf("text is not a clean tail of whole messages: %d runes", utf8.RuneCountInString(got))
	}

	short := Person{Messages: []string{"one", "two"}}
	if got := short.Text(MaxTextRunes); got != "one\ntwo" {
		t.Fatalf("short = %q", got)
	}
	if got := short.Text(7); got != "one\ntwo" {
		t.Fatalf("exact fit = %q", got)
	}
	if got := short.Text(6); got != "two" {
		t.Fatalf("one fewer = %q", got)
	}
	huge := Person{Messages: []string{"old", strings.Repeat("é", 30) + "tail"}}
	if got, want := huge.Text(10), strings.Repeat("é", 6)+"tail"; got != want {
		t.Fatalf("oversized newest = %q, want %q", got, want)
	}
	if got := (Person{}).Text(10); got != "" {
		t.Fatalf("empty = %q", got)
	}
	if got := short.Text(0); got != "" {
		t.Fatalf("zero limit = %q", got)
	}
}

func TestSameName(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"Lucia", "Lucía", true},
		{"lucía", "LUCÍA", true},
		{"José  María", "jose maria", true},
		{"Ana", "Anna", false},
		{"Ana", "Ana Ruiz", false},
		{"", "", true},
	}
	for _, c := range cases {
		if got := SameName(c.a, c.b); got != c.want {
			t.Errorf("SameName(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestStartsWithName(t *testing.T) {
	cases := []struct {
		name, first string
		want        bool
	}{
		{"Iris", "Iris", true},
		{"Iris 🎨", "Iris", true},
		{"Iris C.", "Iris", true},
		{"iris calder", "Iris", true},
		{"Íris ✨ Calder", "Iris", true},
		{"🎨 Iris", "Iris", true},
		{"Irisa", "Iris", false},
		{"Iri", "Iris", false},
		{"Ben", "Iris", false},
		{"Mary-Jane 🌸", "Mary-Jane", true},
		{"Mary", "Mary-Jane", false},
		{"🎨", "🎨", false},
		{"Iris", "", false},
	}
	for _, c := range cases {
		if got := StartsWithName(c.name, c.first); got != c.want {
			t.Errorf("StartsWithName(%q, %q) = %v, want %v", c.name, c.first, got, c.want)
		}
	}
}
