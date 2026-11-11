package chatimport

import (
	"os"
	"path/filepath"
	"testing"
	"unicode"
	"unicode/utf8"
)

func FuzzParse(f *testing.F) {
	seeds, _ := filepath.Glob(filepath.Join("testdata", "*.txt"))
	for _, s := range seeds {
		if b, err := os.ReadFile(s); err == nil {
			f.Add(string(b))
		}
	}
	f.Add("")
	f.Add("a: b")
	f.Add("[1/1/26, 1:00 AM] \u202e: x")
	f.Add("\x00\xff: \xfe")
	f.Fuzz(func(t *testing.T, text string) {
		c, err := Parse(text)
		if err != nil {
			return
		}
		if len(c.People) == 0 || len(c.People) > MaxPeople {
			t.Fatalf("people = %d", len(c.People))
		}
		for _, p := range c.People {
			if n := utf8.RuneCountInString(p.Name); n == 0 || n > 60 {
				t.Fatalf("name length %d: %q", n, p.Name)
			}
			for _, r := range p.Name {
				if unicode.IsControl(r) {
					t.Fatalf("control character in name %q", p.Name)
				}
			}
			if len(p.Messages) == 0 {
				t.Fatalf("person %q has no messages", p.Name)
			}
			if got := p.Text(MaxTextRunes); utf8.RuneCountInString(got) > MaxTextRunes {
				t.Fatalf("text too long for %q", p.Name)
			}
		}
	})
}
