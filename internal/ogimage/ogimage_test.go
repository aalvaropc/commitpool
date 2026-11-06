package ogimage_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"

	"github.com/aalvaropc/commitpool/internal/ogimage"
)

func maya() ogimage.Card {
	return ogimage.Card{
		Status: "Finding the date", Title: "Wheel-throwing for beginners", Host: "Maya Okafor",
		Place: "Maya's studio · Greenpoint, Brooklyn", When: "Sat, Nov 14 or Sun, Nov 15 · 10 AM–12 PM",
		Price: "$45", Needed: 3, Capacity: 6,
	}
}

func decode(t *testing.T, raw []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("not a PNG: %v", err)
	}
	return img
}

func near(a, b color.Color) bool {
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	d := func(x, y uint32) uint32 {
		if x > y {
			return x - y
		}
		return y - x
	}
	return d(ar, br) < 0x0800 && d(ag, bg) < 0x0800 && d(ab, bb) < 0x0800
}

func TestRender_IsALinkPreviewInTheBrand(t *testing.T) {
	raw, err := ogimage.Render(maya())
	if err != nil {
		t.Fatal(err)
	}
	if path := os.Getenv("OGIMAGE_OUT"); path != "" {
		_ = os.WriteFile(path, raw, 0o600)
	}
	img := decode(t, raw)
	if b := img.Bounds(); b.Dx() != ogimage.Width || b.Dy() != ogimage.Height {
		t.Fatalf("size = %v, want 1200×630", b)
	}
	if !near(img.At(10, 10), color.RGBA{0xf3, 0xf2, 0xf7, 0xff}) {
		t.Errorf("canvas at the corner = %v", img.At(10, 10))
	}
	if !near(img.At(100, 300), color.RGBA{0x5b, 0x21, 0xb6, 0xff}) {
		t.Errorf("title tile = %v, want the brand violet", img.At(100, 300))
	}
	var light int
	for x := 88; x < 650; x++ {
		for y := 380; y < 480; y++ {
			if r, g, b, _ := img.At(x, y).RGBA(); r > 0xe000 && g > 0xe000 && b > 0xe000 {
				light++
			}
		}
	}
	if light < 2000 {
		t.Errorf("only %d light pixels where the title goes", light)
	}
}

func TestRender_IsDeterministic(t *testing.T) {
	a, _ := ogimage.Render(maya())
	b, _ := ogimage.Render(maya())
	if !bytes.Equal(a, b) {
		t.Error("the same card renders differently")
	}
}

func TestRender_SurvivesLongAndMissingText(t *testing.T) {
	c := maya()
	c.Title = strings.Repeat("Sourdough, pizza and focaccia for absolute beginners ", 4)
	c.When = strings.Repeat("Sat, Nov 14 · ", 10)
	c.Place, c.Host, c.Status = "", "", ""
	c.Paid, c.Reserved = 2, 9
	if _, err := ogimage.Render(c); err != nil {
		t.Fatal(err)
	}
	c.Capacity, c.Needed = 0, 0
	if _, err := ogimage.Render(c); err != nil {
		t.Fatal(err)
	}
}
