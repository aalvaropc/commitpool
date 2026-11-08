package ogimage_test

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func samples() map[string]ogimage.Card {
	open := maya()
	open.Status, open.Tone, open.When, open.Reserved = "Reservations open", ogimage.ToneHold, "Sat, Nov 14 · 10 AM–12 PM", 2
	on := open
	on.Status, on.Tone, on.Paid, on.Reserved = "It’s on", ogimage.ToneOn, 4, 0
	long := maya()
	long.Title = "Sourdough, focaccia and pizza al taglio for absolute beginners who never baked bread"
	long.Host, long.Place = "Bartholomew Featherstonehaugh-Winterbottom", "The community kitchen on the second floor of the old library annex"
	long.When, long.Price, long.Paid, long.Reserved = "4 possible dates from Sat, Nov 14", "$1,250.50", 1, 1
	cancelled := maya()
	cancelled.Status, cancelled.Tone, cancelled.Title, cancelled.Host, cancelled.Place = "Cancelled", ogimage.ToneCancelled, "Café crème · Dvořák", "", ""
	overflow := long
	overflow.Title = strings.Repeat("Sourdough, pizza and focaccia for absolute beginners ", 4)
	overflow.When, overflow.Price, overflow.Status, overflow.Tone = "Sat, Nov 14 or Sun, Nov 15 · 10:30 AM–12:45 PM", "1,250.00 EUR", "Reservations closed", ogimage.ToneNeutral
	return map[string]ogimage.Card{"collecting": maya(), "open": open, "on": on, "long": long, "cancelled": cancelled, "overflow": overflow}
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

func count(img image.Image, area image.Rectangle, want color.Color) int {
	n := 0
	for x := area.Min.X; x < area.Max.X; x++ {
		for y := area.Min.Y; y < area.Max.Y; y++ {
			if near(img.At(x, y), want) {
				n++
			}
		}
	}
	return n
}

var (
	canvas = color.RGBA{0x14, 0x10, 0x0d, 0xff}
	bone   = color.RGBA{0xf4, 0xec, 0xe0, 0xff}
	ember  = color.RGBA{0xff, 0x6a, 0x3d, 0xff}
	white  = color.RGBA{0xff, 0xff, 0xff, 0xff}
)

func TestRender_IsALinkPreviewInTheKilnIdentity(t *testing.T) {
	dir := os.Getenv("OGIMAGE_OUT")
	for name, card := range samples() {
		raw, err := ogimage.Render(card)
		if err != nil {
			t.Fatal(err)
		}
		if dir != "" {
			_ = os.WriteFile(filepath.Join(dir, name+".png"), raw, 0o600)
		}
		img := decode(t, raw)
		if b := img.Bounds(); b.Dx() != ogimage.Width || b.Dy() != ogimage.Height {
			t.Fatalf("%s: size = %v, want 1200×630", name, b)
		}
		for _, p := range []image.Point{{4, 4}, {1195, 4}, {4, 625}, {1195, 625}} {
			if !near(img.At(p.X, p.Y), canvas) {
				t.Errorf("%s: canvas at %v = %v, want the warm charcoal", name, p, img.At(p.X, p.Y))
			}
		}
		if n := count(img, image.Rect(60, 130, 1140, 360), bone); n < 4000 {
			t.Errorf("%s: only %d bone pixels where the title goes", name, n)
		}
		if n := count(img, image.Rect(60, 40, 130, 110), ember); n < 400 {
			t.Errorf("%s: only %d ember pixels in the bowl mark", name, n)
		}
		if n := count(img, img.Bounds(), white); n > 0 {
			t.Errorf("%s: %d pure white pixels; the ink is bone", name, n)
		}
	}
}

func TestRender_ItsOnIsDarkTextOnEmber(t *testing.T) {
	raw, err := ogimage.Render(samples()["on"])
	if err != nil {
		t.Fatal(err)
	}
	img := decode(t, raw)
	pill := image.Rect(900, 44, 1136, 106)
	if n := count(img, pill, ember); n < 4000 {
		t.Errorf("only %d ember pixels in the status pill", n)
	}
	if n := count(img, pill, color.RGBA{0x1a, 0x0d, 0x07, 0xff}); n < 300 {
		t.Errorf("only %d dark pixels for the label on ember", n)
	}
}

func TestRender_IsDeterministic(t *testing.T) {
	a, _ := ogimage.Render(maya())
	b, _ := ogimage.Render(maya())
	if !bytes.Equal(a, b) {
		t.Error("the same card renders differently")
	}
	c := maya()
	c.Reserved = 1
	if d, _ := ogimage.Render(c); bytes.Equal(a, d) {
		t.Error("a reservation does not change the card")
	}
}

func TestRender_SurvivesLongAndMissingText(t *testing.T) {
	c := maya()
	c.Title = strings.Repeat("Sourdough, pizza and focaccia for absolute beginners ", 4)
	c.When = strings.Repeat("Sat, Nov 14 · ", 10)
	c.Price = "1,250,000.00 EUR"
	c.Place, c.Host, c.Status = "", "", ""
	c.Paid, c.Reserved = 2, 9
	if _, err := ogimage.Render(c); err != nil {
		t.Fatal(err)
	}
	c.Capacity, c.Needed, c.Title, c.When, c.Price = 0, 0, "", "", ""
	if _, err := ogimage.Render(c); err != nil {
		t.Fatal(err)
	}
	c.Title = strings.Repeat("W", 80)
	if _, err := ogimage.Render(c); err != nil {
		t.Fatal(err)
	}
}

func TestRender_ManyAtOnce(t *testing.T) {
	var wg sync.WaitGroup
	results := make([][]byte, 6)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := maya()
			c.Title = fmt.Sprintf("%s %d", c.Title, i%2)
			raw, err := ogimage.Render(c)
			if err != nil {
				t.Error(err)
			}
			results[i] = raw
		}()
	}
	wg.Wait()
	for i := 2; i < len(results); i++ {
		if !bytes.Equal(results[i], results[i%2]) {
			t.Errorf("render %d differs from the same card rendered alongside it", i)
		}
	}
}

func TestRender_DrawsTitlesOutsideTheBrandFont(t *testing.T) {
	c := maya()
	c.Title = "🍞 Пекарня & Łódź bread — 面包"
	if _, err := ogimage.Render(c); err != nil {
		t.Fatal(err)
	}
}
