package ogimage

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gomedium"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

const (
	Width  = 1200
	Height = 630
)

var (
	//go:embed fonts/Outfit-Bold.ttf
	outfitBold []byte
	//go:embed fonts/Outfit-SemiBold.ttf
	outfitSemiBold []byte
	//go:embed fonts/WorkSans-Medium.ttf
	workSansMedium []byte
)

var (
	canvas      = color.RGBA{0xf3, 0xf2, 0xf7, 0xff}
	surface     = color.RGBA{0xff, 0xff, 0xff, 0xff}
	ink         = color.RGBA{0x18, 0x12, 0x2b, 0xff}
	inkSoft     = color.RGBA{0x5b, 0x54, 0x70, 0xff}
	brand       = color.RGBA{0x5b, 0x21, 0xb6, 0xff}
	brandWash   = color.RGBA{0xed, 0xe7, 0xfb, 0xff}
	brandRing   = color.RGBA{0x7c, 0x4d, 0xc9, 0xff}
	brandPill   = color.RGBA{0x6f, 0x3a, 0xc4, 0xff}
	onBrand     = color.RGBA{0xff, 0xff, 0xff, 0xff}
	onBrandSoft = color.RGBA{0xe4, 0xda, 0xfa, 0xff}
	paid        = color.RGBA{0x15, 0x80, 0x3d, 0xff}
)

var titleTile = image.Rect(48, 48, 716, 582)

type Card struct {
	Status   string
	Title    string
	Host     string
	Place    string
	When     string
	Price    string
	Needed   int
	Capacity int
	Paid     int
	Reserved int
}

type typeface struct {
	font, fallback *opentype.Font
}

type fonts struct {
	bold, semi, sans typeface
}

var loadFonts = sync.OnceValues(func() (fonts, error) {
	var out fonts
	for _, spec := range []struct {
		dst      *typeface
		raw, alt []byte
	}{
		{&out.bold, outfitBold, gobold.TTF}, {&out.semi, outfitSemiBold, gomedium.TTF}, {&out.sans, workSansMedium, goregular.TTF},
	} {
		f, err := opentype.Parse(spec.raw)
		if err != nil {
			return fonts{}, err
		}
		alt, err := opentype.Parse(spec.alt)
		if err != nil {
			return fonts{}, err
		}
		*spec.dst = typeface{font: f, fallback: alt}
	}
	return out, nil
})

type style struct {
	tf   typeface
	size float64
}

type canvasImage struct {
	img   *image.RGBA
	faces map[*opentype.Font]map[float64]font.Face
	buf   sfnt.Buffer
}

func (c *canvasImage) face(f *opentype.Font, size float64) font.Face {
	bySize := c.faces[f]
	if bySize == nil {
		bySize = map[float64]font.Face{}
		c.faces[f] = bySize
	}
	if face, ok := bySize[size]; ok {
		return face
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		panic(err)
	}
	bySize[size] = face
	return face
}

func (c *canvasImage) covers(f *opentype.Font, r rune) bool {
	if r == ' ' {
		return true
	}
	i, err := f.GlyphIndex(&c.buf, r)
	return err == nil && i != 0
}

func (c *canvasImage) choose(st style, s string) (font.Face, string) {
	if c.coversAll(st.tf.font, s) {
		return c.face(st.tf.font, st.size), s
	}
	kept := strings.Map(func(r rune) rune {
		if c.covers(st.tf.fallback, r) {
			return r
		}
		return -1
	}, s)
	return c.face(st.tf.fallback, st.size), strings.Join(strings.Fields(kept), " ")
}

func (c *canvasImage) coversAll(f *opentype.Font, s string) bool {
	for _, r := range s {
		if !c.covers(f, r) {
			return false
		}
	}
	return true
}

func Render(card Card) ([]byte, error) {
	f, err := loadFonts()
	if err != nil {
		return nil, fmt.Errorf("ogimage: fonts: %w", err)
	}
	c := &canvasImage{img: image.NewRGBA(image.Rect(0, 0, Width, Height)), faces: map[*opentype.Font]map[float64]font.Face{}}
	defer c.close()
	c.draw(f, card)
	var out bytes.Buffer
	if err := png.Encode(&out, c.img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func (c *canvasImage) close() {
	for _, bySize := range c.faces {
		for _, face := range bySize {
			_ = face.Close()
		}
	}
}

func (c *canvasImage) draw(f fonts, card Card) {
	draw.Draw(c.img, c.img.Bounds(), image.NewUniform(canvas), image.Point{}, draw.Src)
	roundRect(c.img, titleTile, 40, brand)
	c.ring(600, 30, 170, 22)

	label := style{f.semi, 22}
	if card.Status != "" {
		face, status := c.choose(label, card.Status)
		w := measure(face, status) + 44
		roundRect(c.img, image.Rect(88, 88, 88+w, 132), 22, brandPill)
		text(c.img, face, onBrand, 110, 118, status)
	}

	title := style{f.bold, 68}
	face, words := c.choose(title, card.Title)
	lines := wrap(face, words, 568, 3)
	if len(lines) > 2 {
		face, words = c.choose(style{f.bold, 54}, card.Title)
		lines = wrap(face, words, 568, 3)
	}
	lineHeight := face.Metrics().Height.Ceil()
	y := 462 - lineHeight*(len(lines)-1)
	if card.Place == "" {
		y += 40
	}
	if card.Host == "" {
		y += 40
	}
	for _, l := range lines {
		text(c.img, face, onBrand, 88, y, l)
		y += lineHeight
	}
	y -= lineHeight - 56
	body := style{f.sans, 30}
	if card.Host != "" {
		bf, host := c.choose(body, "with "+card.Host)
		text(c.img, bf, onBrandSoft, 88, y, ellipsize(bf, host, 568))
		y += 40
	}
	if card.Place != "" {
		bf, place := c.choose(body, card.Place)
		text(c.img, bf, onBrandSoft, 88, y, ellipsize(bf, place, 568))
	}

	roundRect(c.img, image.Rect(748, 48, 1152, 400), 36, surface)
	lf := c.face(f.semi.font, 22)
	text(c.img, lf, inkSoft, 784, 100, "WHEN")
	wf, when := c.choose(body, card.When)
	wy := 140
	for _, l := range wrap(wf, when, 336, 2) {
		text(c.img, wf, ink, 784, wy, l)
		wy += 38
	}
	text(c.img, lf, inkSoft, 784, 256, "PRICE")
	pf, price := c.choose(style{f.bold, 64}, card.Price)
	small := c.face(f.sans.font, 24)
	text(c.img, pf, ink, 784, 322, price)
	text(c.img, small, inkSoft, 784+measure(pf, price)+14, 320, "a spot")
	text(c.img, small, inkSoft, 784, 362, "Charged only if it happens")

	roundRect(c.img, image.Rect(748, 428, 1152, 582), 36, surface)
	meter(c.img, card, 784, 470, 336)
	needed := fmt.Sprintf("%d needed · %d spots", card.Needed, card.Capacity)
	if card.Paid+card.Reserved > 0 {
		needed = fmt.Sprintf("%d of %d needed have reserved", min(card.Paid+card.Reserved, card.Needed), card.Needed)
	}
	text(c.img, small, ink, 784, 530, ellipsize(small, needed, 336))
	logo(c.img, 784, 546)
	text(c.img, lf, brand, 818, 564, "CommitPool")
}

func logo(img *image.RGBA, x, y int) {
	roundRect(img, image.Rect(x, y, x+24, y+24), 7, brand)
	for i, h := range []int{6, 10, 14} {
		bx := x + 5 + i*5
		roundRect(img, image.Rect(bx, y+19-h, bx+3, y+19), 1, onBrand)
	}
}

func meter(img *image.RGBA, c Card, x, y, width int) {
	roundRect(img, image.Rect(x, y, x+width, y+20), 10, brandWash)
	if c.Capacity <= 0 {
		return
	}
	unit := float64(width) / float64(c.Capacity)
	if filled := min(c.Paid, c.Capacity); filled > 0 {
		roundRect(img, image.Rect(x, y, x+int(unit*float64(filled)), y+20), 10, paid)
	}
	if reserved := min(c.Paid+c.Reserved, c.Capacity); reserved > c.Paid {
		roundRect(img, image.Rect(x+int(unit*float64(c.Paid)), y, x+int(unit*float64(reserved)), y+20), 10, brand)
	}
	if c.Needed > 0 && c.Needed <= c.Capacity {
		mark := x + int(unit*float64(c.Needed))
		draw.Draw(img, image.Rect(mark-2, y-10, mark+2, y+30), image.NewUniform(ink), image.Point{}, draw.Src)
	}
}

const kappa = 0.5523

func roundedPath(z *vector.Rasterizer, w, h, r float32) {
	z.MoveTo(r, 0)
	z.LineTo(w-r, 0)
	z.CubeTo(w-r+r*kappa, 0, w, r-r*kappa, w, r)
	z.LineTo(w, h-r)
	z.CubeTo(w, h-r+r*kappa, w-r+r*kappa, h, w-r, h)
	z.LineTo(r, h)
	z.CubeTo(r-r*kappa, h, 0, h-r+r*kappa, 0, h-r)
	z.LineTo(0, r)
	z.CubeTo(0, r-r*kappa, r-r*kappa, 0, r, 0)
	z.ClosePath()
}

func roundRect(img draw.Image, rect image.Rectangle, radius int, c color.Color) {
	if rect.Dx() <= 0 || rect.Dy() <= 0 {
		return
	}
	radius = min(radius, rect.Dx()/2, rect.Dy()/2)
	z := vector.NewRasterizer(rect.Dx(), rect.Dy())
	roundedPath(z, float32(rect.Dx()), float32(rect.Dy()), float32(radius))
	z.Draw(img, rect, image.NewUniform(c), image.Point{})
}

func disc(img draw.Image, origin image.Point, cx, cy, r int, c color.Color) {
	bounds := img.Bounds()
	z := vector.NewRasterizer(bounds.Dx(), bounds.Dy())
	x, y, fr := float32(cx-origin.X), float32(cy-origin.Y), float32(r)
	z.MoveTo(x+fr, y)
	z.CubeTo(x+fr, y+fr*kappa, x+fr*kappa, y+fr, x, y+fr)
	z.CubeTo(x-fr*kappa, y+fr, x-fr, y+fr*kappa, x-fr, y)
	z.CubeTo(x-fr, y-fr*kappa, x-fr*kappa, y-fr, x, y-fr)
	z.CubeTo(x+fr*kappa, y-fr, x+fr, y-fr*kappa, x+fr, y)
	z.ClosePath()
	z.Draw(img, bounds, image.NewUniform(c), bounds.Min)
}

func (c *canvasImage) ring(cx, cy, r, width int) {
	region := image.Rect(cx-r, cy-r, cx+r, cy+r).Intersect(titleTile)
	if region.Empty() {
		return
	}
	layer := image.NewRGBA(region)
	disc(layer, region.Min, cx, cy, r, brandRing)
	disc(layer, region.Min, cx, cy, r-width, brand)
	mask := image.NewAlpha(region)
	z := vector.NewRasterizer(titleTile.Dx(), titleTile.Dy())
	roundedPath(z, float32(titleTile.Dx()), float32(titleTile.Dy()), 40)
	tile := image.NewAlpha(titleTile)
	z.Draw(tile, titleTile, image.Opaque, image.Point{})
	draw.Draw(mask, region, tile, region.Min, draw.Src)
	draw.DrawMask(c.img, region, layer, region.Min, mask, region.Min, draw.Over)
}

func text(img *image.RGBA, face font.Face, c color.Color, x, y int, s string) {
	d := font.Drawer{Dst: img, Src: image.NewUniform(c), Face: face, Dot: fixed.P(x, y)}
	d.DrawString(s)
}

func measure(face font.Face, s string) int {
	return font.MeasureString(face, s).Ceil()
}

func ellipsize(face font.Face, s string, width int) string {
	if measure(face, s) <= width {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 && measure(face, string(runes)+"…") > width {
		runes = runes[:len(runes)-1]
	}
	return strings.TrimSpace(string(runes)) + "…"
}

func wrap(face font.Face, s string, width, maxLines int) []string {
	words := strings.Fields(s)
	var lines []string
	current := ""
	for i, w := range words {
		next := strings.TrimSpace(current + " " + w)
		if current != "" && measure(face, next) > width {
			lines = append(lines, current)
			current = w
			if len(lines) == maxLines-1 {
				current = strings.Join(words[i:], " ")
				break
			}
			continue
		}
		current = next
	}
	if current != "" {
		lines = append(lines, current)
	}
	for i, l := range lines {
		lines[i] = ellipsize(face, l, width)
	}
	return lines
}
