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
	"unicode/utf8"

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
	Width    = 1200
	Height   = 630
	Revision = "kiln-1"
)

var (
	//go:embed fonts/BricolageGrotesque-ExtraBoldCondensed.ttf
	bricolageDisplay []byte
	//go:embed fonts/BricolageGrotesque-SemiBold.ttf
	bricolageSemiBold []byte
	//go:embed fonts/BricolageGrotesque-Medium.ttf
	bricolageMedium []byte
	//go:embed fonts/BricolageGrotesque-ExtraBold-Wordmark.ttf
	bricolageWordmark []byte
)

var (
	canvas       = color.RGBA{0x14, 0x10, 0x0d, 0xff}
	surfaceMuted = color.RGBA{0x2a, 0x22, 0x1c, 0xff}
	ink          = color.RGBA{0xf4, 0xec, 0xe0, 0xff}
	inkSoft      = color.RGBA{0xc2, 0xb5, 0xa5, 0xff}
	brand        = color.RGBA{0xff, 0x6a, 0x3d, 0xff}
	onBrand      = color.RGBA{0x1a, 0x0d, 0x07, 0xff}
	paid         = color.RGBA{0x7b, 0xe0, 0xae, 0xff}
	hold         = color.RGBA{0x8a, 0xa4, 0xff, 0xff}
	holdWash     = color.RGBA{0x18, 0x1f, 0x3d, 0xff}
	no           = color.RGBA{0xf0, 0x80, 0x96, 0xff}
	noWash       = color.RGBA{0x35, 0x1a, 0x21, 0xff}
)

type Tone int

const (
	ToneNeutral Tone = iota
	ToneHold
	ToneOn
	ToneCancelled
)

type Card struct {
	Status   string
	Tone     Tone
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
	display, semi, medium typeface
	wordmark              *opentype.Font
}

var loadFonts = sync.OnceValues(func() (fonts, error) {
	wordmark, err := opentype.Parse(bricolageWordmark)
	if err != nil {
		return fonts{}, err
	}
	out := fonts{wordmark: wordmark}
	for _, spec := range []struct {
		dst      *typeface
		raw, alt []byte
	}{
		{&out.display, bricolageDisplay, gobold.TTF}, {&out.semi, bricolageSemiBold, gomedium.TTF}, {&out.medium, bricolageMedium, goregular.TTF},
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
	tf    typeface
	size  float64
	track float64
}

type span struct {
	face  font.Face
	text  string
	track fixed.Int26_6
}

func (s span) with(text string) span {
	s.text = text
	return s
}

func (s span) width() int {
	var x fixed.Int26_6
	prev := rune(-1)
	for _, r := range s.text {
		if prev >= 0 {
			x += s.face.Kern(prev, r) + s.track
		}
		adv, ok := s.face.GlyphAdvance(r)
		if !ok {
			continue
		}
		x += adv
		prev = r
	}
	return x.Ceil()
}

func (s span) draw(img *image.RGBA, c color.Color, x, y int) int {
	src := image.NewUniform(c)
	dot := fixed.P(x, y)
	prev := rune(-1)
	for _, r := range s.text {
		if prev >= 0 {
			dot.X += s.face.Kern(prev, r) + s.track
		}
		dr, mask, mp, adv, ok := s.face.Glyph(dot, r)
		if !ok {
			continue
		}
		draw.DrawMask(img, dr, src, image.Point{}, mask, mp, draw.Over)
		dot.X += adv
		prev = r
	}
	return dot.X.Ceil()
}

func (s span) ellipsize(width int) span {
	if s.width() <= width {
		return s
	}
	runes := []rune(s.text)
	for len(runes) > 0 && s.with(string(runes)+"…").width() > width {
		runes = runes[:len(runes)-1]
	}
	return s.with(strings.TrimSpace(string(runes)) + "…")
}

func (s span) wrap(width, maxLines int) ([]span, bool) {
	maxLines = max(maxLines, 1)
	var lines []span
	current := ""
	words := breaks(s.text)
	for i, w := range words {
		next := current + w
		if current == "" || s.with(strings.TrimSpace(next)).width() <= width {
			current = next
			continue
		}
		if len(lines) == maxLines-1 {
			current += strings.Join(words[i:], "")
			break
		}
		lines = append(lines, s.with(strings.TrimSpace(current)))
		current = strings.TrimLeft(w, " ")
	}
	if current = strings.TrimSpace(current); current != "" {
		lines = append(lines, s.with(current))
	}
	fits := true
	for i, l := range lines {
		if l.width() > width {
			fits = false
			lines[i] = l.ellipsize(width)
		}
	}
	return lines, fits
}

func breaks(s string) []string {
	var out []string
	for i, word := range strings.Fields(s) {
		lead := " "
		if i == 0 {
			lead = ""
		}
		for {
			cut := strings.IndexAny(word, "-–—/")
			if cut <= 0 {
				break
			}
			_, size := utf8.DecodeRuneInString(word[cut:])
			if cut+size >= len(word) {
				break
			}
			out = append(out, lead+word[:cut+size])
			word, lead = word[cut+size:], ""
		}
		out = append(out, lead+word)
	}
	return out
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

func (c *canvasImage) keep(s string, fonts ...*opentype.Font) string {
	kept := strings.Map(func(r rune) rune {
		for _, f := range fonts {
			if c.covers(f, r) {
				return r
			}
		}
		return -1
	}, s)
	return strings.Join(strings.Fields(kept), " ")
}

func (c *canvasImage) choose(st style, s string) (font.Face, string) {
	drawable := c.keep(s, st.tf.font, st.tf.fallback)
	if c.keep(drawable, st.tf.font) == drawable {
		return c.face(st.tf.font, st.size), drawable
	}
	return c.face(st.tf.fallback, st.size), c.keep(drawable, st.tf.fallback)
}

func (c *canvasImage) span(st style, s string) span {
	face, text := c.choose(st, s)
	return span{face: face, text: text, track: tracking(st.size, st.track)}
}

func tracking(size, em float64) fixed.Int26_6 {
	return fixed.Int26_6(size * em * 64)
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

const (
	margin    = 64
	right     = Width - margin
	bandTop   = 438
	bandBot   = 574
	gap       = 16
	tilePad   = 30
	tileRound = 24
	whenMin   = 440
)

func (c *canvasImage) draw(f fonts, card Card) {
	draw.Draw(c.img, c.img.Bounds(), image.NewUniform(canvas), image.Point{}, draw.Src)
	logo(c.img, margin, 50, 52)
	span{face: c.face(f.wordmark, 36), text: "CommitPool", track: tracking(36, -0.02)}.draw(c.img, ink, margin+66, 89)
	c.status(f, card)

	byline := bandTop - 42
	if card.Host == "" && card.Place == "" {
		c.title(f, card.Title, bandTop-50)
	} else {
		c.title(f, card.Title, byline-64)
		c.byline(f, card, byline)
	}

	meterW := c.meterTile(f, card)
	priceW := c.priceTile(f, card, right-meterW-gap, right-meterW-gap-whenMin-gap-margin)
	c.whenTile(f, card.When, margin, right-meterW-gap-priceW-gap)
}

func (c *canvasImage) status(f fonts, card Card) {
	if card.Status == "" {
		return
	}
	fill, dot, fg := surfaceMuted, inkSoft, ink
	switch card.Tone {
	case ToneHold:
		fill, dot, fg = holdWash, hold, hold
	case ToneCancelled:
		fill, dot, fg = noWash, no, no
	case ToneOn:
		label := c.span(style{f.display, 34, -0.01}, strings.ToUpper(card.Status))
		w := label.width() + 56
		roundRect(c.img, image.Rect(right-w, 44, right, 106), 31, brand)
		label.draw(c.img, onBrand, right-w+28, 87)
		return
	}
	label := c.span(style{f.semi, 28, 0}, card.Status)
	w := label.width() + 50 + 28
	roundRect(c.img, image.Rect(right-w, 47, right, 103), 28, fill)
	disc(c.img, float32(right-w+32), 75, 7, dot)
	label.draw(c.img, fg, right-w+50, 85)
}

func (c *canvasImage) title(f fonts, title string, baseline int) {
	const top = 128
	var lines []span
	var lead int
	for _, size := range []float64{120, 108, 96, 86, 76, 68, 60} {
		sp := c.span(style{f.display, size, -0.02}, strings.ToUpper(title))
		lead = int(size * 0.92)
		maxLines := 1 + (baseline-top-sp.face.Metrics().CapHeight.Ceil())/lead
		var fits bool
		lines, fits = sp.wrap(right-margin, min(maxLines, 3))
		if fits {
			break
		}
	}
	y := baseline - lead*(len(lines)-1)
	for _, l := range lines {
		l.draw(c.img, ink, margin-4, y)
		y += lead
	}
}

func (c *canvasImage) byline(f fonts, card Card, y int) {
	x := margin
	if card.Host != "" {
		x = c.span(style{f.semi, 34, 0}, "with "+card.Host).ellipsize(right-x).draw(c.img, ink, x, y) + 12
	}
	if card.Place == "" || right-x < 280 {
		return
	}
	place := card.Place
	if card.Host != "" {
		place = "· " + place
	}
	c.span(style{f.medium, 34, 0}, place).ellipsize(right-x).draw(c.img, inkSoft, x, y)
}

func (c *canvasImage) whenTile(f fonts, when string, x0, x1 int) {
	roundRect(c.img, image.Rect(x0, bandTop, x1, bandBot), tileRound, surfaceMuted)
	inner := x1 - x0 - 2*tilePad
	x := x0 + tilePad
	if i := strings.LastIndex(when, " · "); i >= 0 {
		for _, size := range []float64{36, 32} {
			head := c.span(style{f.semi, size, 0}, when[:i])
			if head.width() <= inner {
				head.draw(c.img, ink, x, bandTop+60)
				c.span(style{f.medium, size - 4, 0}, when[i+len(" · "):]).ellipsize(inner).draw(c.img, inkSoft, x, bandTop+106)
				return
			}
		}
	}
	lines, _ := c.span(style{f.semi, 32, 0}, when).wrap(inner, 2)
	y := bandTop + 60
	if len(lines) == 1 {
		y = bandTop + 80
	}
	for _, l := range lines {
		l.draw(c.img, ink, x, y)
		y += 44
	}
}

func (c *canvasImage) priceTile(f fonts, card Card, x1, most int) int {
	var price span
	for _, size := range []float64{76, 64, 54, 46} {
		if price = c.span(style{f.display, size, -0.02}, card.Price); price.width() <= most-2*tilePad {
			break
		}
	}
	unit := c.span(style{f.medium, 28, 0}, "a spot")
	w := max(price.width(), unit.width()) + 2*tilePad
	x0 := x1 - w
	roundRect(c.img, image.Rect(x0, bandTop, x1, bandBot), tileRound, surfaceMuted)
	price.draw(c.img, brand, x0+tilePad, bandTop+76)
	unit.draw(c.img, inkSoft, x0+tilePad, bandTop+108)
	return w
}

func (c *canvasImage) meterTile(f fonts, card Card) int {
	lead := fmt.Sprintf("%d needed", card.Needed)
	rest := fmt.Sprintf("· %d spots", card.Capacity)
	if card.Paid >= card.Needed && card.Paid > 0 {
		lead = fmt.Sprintf("%d paid", card.Paid)
	} else if reserved := card.Paid + card.Reserved; reserved > 0 {
		lead = fmt.Sprintf("%d reserved", reserved)
		rest = fmt.Sprintf("· %d needed", card.Needed)
	}
	strong := c.span(style{f.semi, 28, 0}, lead)
	soft := c.span(style{f.medium, 28, 0}, rest)
	w := max(strong.width()+8+soft.width(), 260) + 2*tilePad
	x0 := right - w
	roundRect(c.img, image.Rect(x0, bandTop, right, bandBot), tileRound, surfaceMuted)
	meter(c.img, card, float32(x0+tilePad), float32(bandTop+40), float32(w-2*tilePad))
	x := strong.draw(c.img, ink, x0+tilePad, bandTop+106)
	soft.draw(c.img, inkSoft, x+8, bandTop+106)
	return w
}

func meter(img *image.RGBA, card Card, x, y, width float32) {
	const h = 18
	capsule(img, x, y, width, h, canvas)
	if card.Capacity <= 0 {
		return
	}
	unit := width / float32(card.Capacity)
	paidN := min(max(card.Paid, 0), card.Capacity)
	heldN := min(max(card.Reserved, 0), card.Capacity-paidN)
	if filled := float32(paidN+heldN) * unit; filled > 0 {
		bounds := image.Rect(int(x), int(y), int(x+width)+1, int(y+h)+1)
		mask := image.NewAlpha(bounds)
		z := vector.NewRasterizer(bounds.Dx(), bounds.Dy())
		pill(z, x-float32(bounds.Min.X), y-float32(bounds.Min.Y), filled, h)
		z.Draw(mask, bounds, image.Opaque, image.Point{})
		split := int(x + float32(paidN)*unit)
		draw.DrawMask(img, image.Rect(bounds.Min.X, bounds.Min.Y, split, bounds.Max.Y), image.NewUniform(paid), image.Point{}, mask, bounds.Min, draw.Over)
		draw.DrawMask(img, image.Rect(split, bounds.Min.Y, bounds.Max.X, bounds.Max.Y), image.NewUniform(hold), image.Point{}, mask, image.Pt(split, bounds.Min.Y), draw.Over)
	}
	if card.Needed > 0 && card.Needed <= card.Capacity {
		capsule(img, x+unit*float32(card.Needed)-2.5, y-8, 5, h+16, ink)
	}
}

func logo(img *image.RGBA, x, y, size int) {
	s := float32(size) / 32
	ox, oy := float32(x), float32(y)
	z := vector.NewRasterizer(size, size)
	r, cx, cy := 14*s, 16*s, 11*s
	z.MoveTo(cx-r, cy)
	z.LineTo(cx+r, cy)
	z.CubeTo(cx+r, cy+r*kappa, cx+r*kappa, cy+r, cx, cy+r)
	z.CubeTo(cx-r*kappa, cy+r, cx-r, cy+r*kappa, cx-r, cy)
	z.ClosePath()
	z.Draw(img, image.Rect(x, y, x+size, y+size), image.NewUniform(brand), image.Point{})
	capsule(img, ox+5*s, oy+10*s, 22*s, 2*s, canvas)
	capsule(img, ox+7.8*s, oy+6.3*s, 16.4*s, 2.4*s, ink)
}

const kappa = 0.5523

func pill(z *vector.Rasterizer, x, y, w, h float32) {
	roundedPath(z, x, y, w, h, min(w, h)/2)
}

func roundedPath(z *vector.Rasterizer, x, y, w, h, r float32) {
	z.MoveTo(x+r, y)
	z.LineTo(x+w-r, y)
	z.CubeTo(x+w-r+r*kappa, y, x+w, y+r-r*kappa, x+w, y+r)
	z.LineTo(x+w, y+h-r)
	z.CubeTo(x+w, y+h-r+r*kappa, x+w-r+r*kappa, y+h, x+w-r, y+h)
	z.LineTo(x+r, y+h)
	z.CubeTo(x+r-r*kappa, y+h, x, y+h-r+r*kappa, x, y+h-r)
	z.LineTo(x, y+r)
	z.CubeTo(x, y+r-r*kappa, x+r-r*kappa, y, x+r, y)
	z.ClosePath()
}

func capsule(img *image.RGBA, x, y, w, h float32, c color.Color) {
	if w <= 0 || h <= 0 {
		return
	}
	bounds := image.Rect(int(x), int(y), int(x+w)+2, int(y+h)+2)
	z := vector.NewRasterizer(bounds.Dx(), bounds.Dy())
	pill(z, x-float32(bounds.Min.X), y-float32(bounds.Min.Y), w, h)
	z.Draw(img, bounds, image.NewUniform(c), image.Point{})
}

func roundRect(img *image.RGBA, rect image.Rectangle, radius int, c color.Color) {
	if rect.Dx() <= 0 || rect.Dy() <= 0 {
		return
	}
	z := vector.NewRasterizer(rect.Dx(), rect.Dy())
	roundedPath(z, 0, 0, float32(rect.Dx()), float32(rect.Dy()), float32(min(radius, rect.Dx()/2, rect.Dy()/2)))
	z.Draw(img, rect, image.NewUniform(c), image.Point{})
}

func disc(img *image.RGBA, cx, cy, r float32, c color.Color) {
	capsule(img, cx-r, cy-r, 2*r, 2*r, c)
}
