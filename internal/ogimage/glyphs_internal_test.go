package ogimage

import (
	"image"
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

func TestChoose_DrawsEveryCharacterOrLeavesItOut(t *testing.T) {
	c, f := newCanvas(t)
	title := style{f.bold, 68}
	cases := []struct {
		in, want string
		fallback bool
	}{
		{"Wheel-throwing for beginners", "Wheel-throwing for beginners", false},
		{"Café crème · señor", "Café crème · señor", false},
		{"Dvořák & Łódź bread", "Dvořák & Łódź bread", true},
		{"🍞 Sourdough 面包 night", "Sourdough night", true},
		{"面包", "", true},
	}
	for _, tc := range cases {
		face, got := c.choose(title, tc.in)
		if got != tc.want {
			t.Errorf("choose(%q) = %q, want %q", tc.in, got, tc.want)
		}
		fallback := face != c.face(f.bold.font, 68)
		if fallback != tc.fallback {
			t.Errorf("choose(%q) fell back = %v, want %v", tc.in, fallback, tc.fallback)
		}
		used := f.bold.font
		if fallback {
			used = f.bold.fallback
		}
		for _, r := range got {
			if !c.covers(used, r) {
				t.Errorf("choose(%q) keeps %q, which the font cannot draw", tc.in, r)
			}
		}
	}
}
