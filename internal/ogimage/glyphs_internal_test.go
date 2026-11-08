package ogimage

import (
	"image"
	"strings"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
)

func newCanvas(t *testing.T) (*canvasImage, fonts) {
	t.Helper()
	f, err := loadFonts()
	if err != nil {
		t.Fatal(err)
	}
	c := &canvasImage{img: image.NewRGBA(image.Rect(0, 0, 10, 10)), faces: map[*opentype.Font]map[float64]font.Face{}}
	t.Cleanup(c.close)
	return c, f
}

func TestBrandFaces_DrawEveryCharacterTheCardPrints(t *testing.T) {
	c, f := newCanvas(t)
	printed := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789" +
		" $€£¥.,:;!?&@#%+*=()[]/'\"-–—·•…’‘“”×" +
		"ÁÉÍÓÚÜÑáéíóúüñÀÈÌÒÙàèìòùÂÊÎÔÛâêîôûÄËÏÖäëïöÇçÃÕãõÅåÆæØøß" +
		"ĄĆĘŁŃŚŹŻąćęłńśźżČĎĚŇŘŠŤŮŽčďěňřšťůžŐŰőűĞİŞğışœŒ"
	for name, tf := range map[string]typeface{"display": f.display, "semi": f.semi, "medium": f.medium} {
		for _, r := range printed {
			if !c.covers(tf.font, r) {
				t.Errorf("%s face lacks %q (%U)", name, r, r)
			}
		}
		for _, r := range strings.ToUpper(printed) {
			if !c.covers(tf.font, r) {
				t.Errorf("%s face lacks the capital %q (%U)", name, r, r)
			}
		}
	}
}

func TestChoose_DrawsEveryCharacterOrLeavesItOut(t *testing.T) {
	c, f := newCanvas(t)
	title := style{f.display, 96, -0.02}
	cases := []struct {
		in, want string
		fallback bool
	}{
		{"Wheel-throwing for beginners", "Wheel-throwing for beginners", false},
		{"Café crème · señor – 10 AM—12 PM", "Café crème · señor – 10 AM—12 PM", false},
		{"Dvořák & Łódź bread", "Dvořák & Łódź bread", false},
		{"🍞 Sourdough 面包 night", "Sourdough night", false},
		{"Пекарня Ёлка", "Пекарня Ёлка", true},
		{"🍞 Ελληνική κουζίνα", "Ελληνική κουζίνα", true},
		{"面包", "", false},
	}
	for _, tc := range cases {
		face, got := c.choose(title, tc.in)
		if got != tc.want {
			t.Errorf("choose(%q) = %q, want %q", tc.in, got, tc.want)
		}
		fallback := face != c.face(f.display.font, 96)
		if fallback != tc.fallback {
			t.Errorf("choose(%q) fell back = %v, want %v", tc.in, fallback, tc.fallback)
		}
		used := f.display.font
		if fallback {
			used = f.display.fallback
		}
		for _, r := range got {
			if !c.covers(used, r) {
				t.Errorf("choose(%q) keeps %q, which the font cannot draw", tc.in, r)
			}
		}
	}
}

func TestWrap_BreaksAfterHyphensAndKeepsWords(t *testing.T) {
	c, f := newCanvas(t)
	sp := c.span(style{f.display, 128, -0.02}, "WHEEL-THROWING FOR ABSOLUTE BEGINNERS")
	narrow := sp.with("WHEEL-THROWING").width() - 10
	lines, fits := sp.wrap(narrow, 6)
	if !fits {
		t.Fatalf("wrap did not fit: %v", lines)
	}
	if lines[0].text != "WHEEL-" || !strings.HasPrefix(lines[1].text, "THROWING") {
		t.Errorf("first lines = %q, %q; want the break after the hyphen", lines[0].text, lines[1].text)
	}
	lines, fits = sp.wrap(narrow, 2)
	if fits || len(lines) != 2 || !strings.HasSuffix(lines[1].text, "…") {
		t.Errorf("two lines = %v (fits %v), want an ellipsis on the last", lines, fits)
	}
}
