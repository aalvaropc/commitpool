package chatimport

import (
	"os"
	"path/filepath"
	"reflect"
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
		again, err2 := Parse(text)
		if err != err2 || !reflect.DeepEqual(c, again) {
			t.Fatal("Parse is not deterministic")
		}
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
				if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
					t.Fatalf("control character in name %q", p.Name)
				}
			}
			if len(p.Messages) == 0 {
				t.Fatalf("person %q has no messages", p.Name)
			}
			for _, n := range []int{1, 2, 7, MaxTextRunes} {
				got := p.Text(n)
				if !utf8.ValidString(got) || utf8.RuneCountInString(got) > n {
					t.Fatalf("Text(%d) is invalid or too long for %q", n, p.Name)
				}
			}
		}
	})
}
