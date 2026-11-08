package ogimage

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"regexp"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gomedium"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
	"golang.org/x/text/unicode/norm"
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
	faces []font.Face
	text  string
	track fixed.Int26_6
}

func (s span) with(text string) span {
	s.text = text
	return s
}

func (s span) face(r rune) font.Face {
	for _, f := range s.faces {
		if _, ok := f.GlyphAdvance(r); ok {
			return f
		}
	}
	return nil
}

func (s span) each(at func(face font.Face, r rune, x fixed.Int26_6) fixed.Int26_6) fixed.Int26_6 {
	var x fixed.Int26_6
	var prevFace font.Face
	prev := rune(-1)
	for _, r := range s.text {
		face := s.face(r)
		if face == nil {
			continue
		}
		if prev >= 0 && !unicode.Is(unicode.Mn, r) {
			x += s.track
			if face == prevFace {
				x += face.Kern(prev, r)
			}
		}
		x += at(face, r, x)
		prev, prevFace = r, face
	}
	return x
}

func (s span) width() int {
	return s.each(func(face font.Face, r rune, _ fixed.Int26_6) fixed.Int26_6 {
		adv, _ := face.GlyphAdvance(r)
		return adv
	}).Ceil()
}

func (s span) draw(img *image.RGBA, c color.Color, x, y int) int {
	src := image.NewUniform(c)
	origin := fixed.P(x, y)
	end := s.each(func(face font.Face, r rune, at fixed.Int26_6) fixed.Int26_6 {
		dr, mask, mp, adv, ok := face.Glyph(fixed.Point26_6{X: origin.X + at, Y: origin.Y}, r)
		if !ok {
			return 0
		}
		draw.DrawMask(img, dr, src, image.Point{}, mask, mp, draw.Over)
		return adv
	})
	return (origin.X + end).Ceil()
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
	for i, word := range strings.FieldsFunc(s, func(r rune) bool { return r == ' ' }) {
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
			next, _ := utf8.DecodeRuneInString(word[cut+size:])
			if cut+size >= len(word) || unicode.IsDigit(next) {
				break
			}
			out = append(out, lead+word[:cut+size])
			word, lead = word[cut+size:], ""
		}
		out = append(out, lead+word)
	}
	return out
}

var (
	dayLabel   = regexp.MustCompile(`\b(Mon|Tue|Wed|Thu|Fri|Sat|Sun), (Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec) (\d{1,2})\b`)
	clockLabel = regexp.MustCompile(`\b(\d{1,2}(?::\d{2})?) (AM|PM)\b`)
)

func keepTogether(when string) string {
	when = dayLabel.ReplaceAllString(when, "$1, $2 $3")
	return clockLabel.ReplaceAllString(when, "$1 $2")
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
	i, err := f.GlyphIndex(&c.buf, r)
	return err == nil && i != 0
}

func (c *canvasImage) visible(tf typeface, s string) string {
	drawable := strings.Map(func(r rune) rune {
		if r == ' ' || !unicode.IsSpace(r) && (c.covers(tf.font, r) || c.covers(tf.fallback, r)) {
			return r
		}
		return ' '
	}, norm.NFC.String(s))
	return strings.Join(strings.FieldsFunc(drawable, func(r rune) bool { return r == ' ' }), " ")
}

func (c *canvasImage) span(st style, s string) span {
	faces := []font.Face{c.face(st.tf.font, st.size), c.face(st.tf.fallback, st.size)}
	return span{faces: faces, text: c.visible(st.tf, s), track: tracking(st.size, st.track)}
}

func tracking(size, em float64) fixed.Int26_6 {
	return fixed.Int26_6(size * em * 64)
}

const untitled = "A class on CommitPool"

const dangling = " ·•,;:|/-–—"

func (c *canvasImage) words(f fonts, card Card) (title, host, place string) {
	title = strings.Trim(c.visible(f.display, strings.ToUpper(card.Title)), dangling)
	if title == "" {
		title = strings.ToUpper(untitled)
	}
	return title, strings.Trim(c.visible(f.semi, card.Host), dangling), strings.Trim(c.visible(f.medium, card.Place), dangling)
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
	margin     = 64
	right      = Width - margin
	bandTop    = 438
	bandBot    = 574
	gap        = 16
	tilePad    = 30
	tileRound  = 24
	bandWidth  = right - margin - 2*gap
	whenMin    = 456
	priceFloor = 150
	meterFloor = 260
)

func (c *canvasImage) draw(f fonts, card Card) {
	draw.Draw(c.img, c.img.Bounds(), image.NewUniform(canvas), image.Point{}, draw.Src)
	logo(c.img, margin, 50, 52)
	span{faces: []font.Face{c.face(f.wordmark, 36)}, text: "CommitPool", track: tracking(36, -0.02)}.draw(c.img, ink, margin+66, 89)
	c.status(f, card)

	title, host, place := c.words(f, card)
	byline := bandTop - 42
	if host == "" && place == "" {
		c.title(f, title, bandTop-50)
	} else {
		c.title(f, title, byline-64)
		c.byline(f, host, place, byline)
	}

	c.band(f, card)
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
		sp := c.span(style{f.display, size, -0.02}, title)
		lead = int(size * 0.92)
		maxLines := 1 + (baseline-top-sp.faces[0].Metrics().CapHeight.Ceil())/lead
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

func (c *canvasImage) byline(f fonts, host, place string, y int) {
	x := margin
	if host != "" {
		x = c.span(style{f.semi, 34, 0}, "with "+host).ellipsize(right-x).draw(c.img, ink, x, y) + 12
	}
	if place == "" || right-x < 280 {
		return
	}
	if host != "" {
		place = "· " + place
	}
	c.span(style{f.medium, 34, 0}, place).ellipsize(right-x).draw(c.img, inkSoft, x, y)
}

func (c *canvasImage) whenTile(f fonts, when string, x0, x1 int) {
	roundRect(c.img, image.Rect(x0, bandTop, x1, bandBot), tileRound, surfaceMuted)
	inner := x1 - x0 - 2*tilePad
	x := x0 + tilePad
	when = keepTogether(when)
	if i := strings.LastIndex(when, " · "); i >= 0 {
		for _, size := range []float64{36, 32, 30} {
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

func counts(card Card) (lead, rest string) {
	switch reserved := card.Paid + card.Reserved; {
	case card.Paid >= card.Needed && card.Paid > 0:
		return fmt.Sprintf("%d paid", card.Paid), fmt.Sprintf("· %d spots", card.Capacity)
	case reserved > 0:
		return fmt.Sprintf("%d reserved", reserved), fmt.Sprintf("· %d needed", card.Needed)
	}
	return fmt.Sprintf("%d needed", card.Needed), fmt.Sprintf("· %d spots", card.Capacity)
}

type bandLayout struct {
	price, unit, strong, soft span
	when, priceW, meterW      int
}

func (c *canvasImage) layoutBand(f fonts, card Card) bandLayout {
	lead, rest := counts(card)
	b := bandLayout{unit: c.span(style{f.medium, 28, 0}, "a spot")}
	budget := bandWidth - whenMin
	fits := false
	for _, caption := range []float64{28, 24} {
		b.strong, b.soft = c.span(style{f.semi, caption, 0}, lead), c.span(style{f.medium, caption, 0}, rest)
		b.meterW = max(b.strong.width()+8+b.soft.width()+2*tilePad, meterFloor)
		for _, size := range []float64{76, 64, 54, 46, 40, 34} {
			b.price = c.span(style{f.display, size, -0.02}, card.Price)
			b.priceW = max(b.price.width(), b.unit.width()) + 2*tilePad
			if fits = b.priceW+b.meterW <= budget; fits {
				break
			}
		}
		if fits {
			break
		}
	}
	if !fits {
		b.meterW = min(b.meterW, budget-priceFloor)
		b.priceW = budget - b.meterW
		b.price = b.price.ellipsize(b.priceW - 2*tilePad)
	}
	b.when = bandWidth - b.priceW - b.meterW
	return b
}

func (c *canvasImage) band(f fonts, card Card) {
	b := c.layoutBand(f, card)
	c.meterTile(card, b.strong, b.soft, right-b.meterW)
	c.priceTile(b.price, b.unit, right-b.meterW-gap-b.priceW, right-b.meterW-gap)
	c.whenTile(f, card.When, margin, margin+b.when)
}

func (c *canvasImage) priceTile(price, unit span, x0, x1 int) {
	roundRect(c.img, image.Rect(x0, bandTop, x1, bandBot), tileRound, surfaceMuted)
	baseline := bandTop + 68 + (price.faces[0].Metrics().CapHeight.Ceil()-39)/2
	price.draw(c.img, brand, x0+tilePad, baseline)
	unit.draw(c.img, inkSoft, x0+tilePad, baseline+32)
}

func (c *canvasImage) meterTile(card Card, strong, soft span, x0 int) {
	inner := right - x0 - 2*tilePad
	strong = strong.ellipsize(inner)
	roundRect(c.img, image.Rect(x0, bandTop, right, bandBot), tileRound, surfaceMuted)
	meter(c.img, card, float32(x0+tilePad), float32(bandTop+40), float32(inner))
	x := strong.draw(c.img, ink, x0+tilePad, bandTop+106)
	if room := right - tilePad - x - 8; room >= soft.with("· …").width() {
		soft.ellipsize(room).draw(c.img, inkSoft, x+8, bandTop+106)
	}
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
