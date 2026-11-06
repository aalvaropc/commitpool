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
	"golang.org/x/image/font/opentype"
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
	onBrand     = color.RGBA{0xff, 0xff, 0xff, 0xff}
	onBrandSoft = color.RGBA{0xe4, 0xda, 0xfa, 0xff}
	paid        = color.RGBA{0x15, 0x80, 0x3d, 0xff}
)

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

type faces struct {
	title, titleSmall, body, label, price, small font.Face
}

var loadFaces = sync.OnceValues(func() (faces, error) {
	parse := func(raw []byte) (*opentype.Font, error) { return opentype.Parse(raw) }
	bold, err := parse(outfitBold)
	if err != nil {
		return faces{}, err
	}
	semi, err := parse(outfitSemiBold)
	if err != nil {
		return faces{}, err
	}
	sans, err := parse(workSansMedium)
	if err != nil {
		return faces{}, err
	}
	face := func(f *opentype.Font, size float64) (font.Face, error) {
		return opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingNone})
	}
	var out faces
	for _, spec := range []struct {
		dst  *font.Face
		f    *opentype.Font
		size float64
	}{
		{&out.title, bold, 68}, {&out.titleSmall, bold, 54}, {&out.body, sans, 30}, {&out.label, semi, 22}, {&out.price, bold, 64}, {&out.small, sans, 24},
	} {
		if *spec.dst, err = face(spec.f, spec.size); err != nil {
			return faces{}, err
		}
	}
	return out, nil
})

func Render(c Card) ([]byte, error) {
	f, err := loadFaces()
	if err != nil {
		return nil, fmt.Errorf("ogimage: fonts: %w", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, Width, Height))
	draw.Draw(img, img.Bounds(), image.NewUniform(canvas), image.Point{}, draw.Src)

	roundRect(img, 48, 48, 716, 582, 40, brand)
	ring(img, 600, 30, 170, 22, color.RGBA{0x7c, 0x4d, 0xc9, 0xff})
	if c.Status != "" {
		w := measure(f.label, c.Status) + 44
		roundRect(img, 88, 88, 88+w, 132, 22, color.RGBA{0x6f, 0x3a, 0xc4, 0xff})
		text(img, f.label, onBrand, 110, 118, c.Status)
	}
	titleFace, lines := f.title, wrap(f.title, c.Title, 568, 3)
	if len(lines) > 2 {
		titleFace, lines = f.titleSmall, wrap(f.titleSmall, c.Title, 568, 3)
	}
	lineHeight := titleFace.Metrics().Height.Ceil()
	y := 462 - lineHeight*(len(lines)-1)
	if c.Place == "" {
		y += 40
	}
	if c.Host == "" {
		y += 40
	}
	for _, l := range lines {
		text(img, titleFace, onBrand, 88, y, l)
		y += lineHeight
	}
	y -= lineHeight - 56
	if c.Host != "" {
		text(img, f.body, onBrandSoft, 88, y, ellipsize(f.body, "with "+c.Host, 568))
		y += 40
	}
	if c.Place != "" {
		text(img, f.body, onBrandSoft, 88, y, ellipsize(f.body, c.Place, 568))
	}

	roundRect(img, 748, 48, 1152, 400, 36, surface)
	text(img, f.label, inkSoft, 784, 100, "WHEN")
	whenLines := wrap(f.body, c.When, 336, 2)
	wy := 140
	for _, l := range whenLines {
		text(img, f.body, ink, 784, wy, l)
		wy += 38
	}
	text(img, f.label, inkSoft, 784, 256, "PRICE")
	text(img, f.price, ink, 784, 322, c.Price)
	text(img, f.small, inkSoft, 784+measure(f.price, c.Price)+14, 320, "a spot")
	text(img, f.small, inkSoft, 784, 362, "Charged only if it happens")

	roundRect(img, 748, 428, 1152, 582, 36, surface)
	meter(img, c, 784, 470, 336)
	needed := fmt.Sprintf("%d needed · %d spots", c.Needed, c.Capacity)
	if c.Paid+c.Reserved > 0 {
		needed = fmt.Sprintf("%d of %d needed have reserved", min(c.Paid+c.Reserved, c.Needed), c.Needed)
	}
	text(img, f.small, ink, 784, 530, ellipsize(f.small, needed, 336))
	logo(img, 784, 546)
	text(img, f.label, brand, 818, 564, "CommitPool")

	var out bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func logo(img *image.RGBA, x, y int) {
	roundRect(img, x, y, x+24, y+24, 7, brand)
	for i, h := range []int{6, 10, 14} {
		bx := x + 5 + i*5
		roundRect(img, bx, y+19-h, bx+3, y+19, 1, onBrand)
	}
}

func meter(img *image.RGBA, c Card, x, y, width int) {
	roundRect(img, x, y, x+width, y+20, 10, brandWash)
	if c.Capacity <= 0 {
		return
	}
	unit := float64(width) / float64(c.Capacity)
	if filled := min(c.Paid, c.Capacity); filled > 0 {
		roundRect(img, x, y, x+int(unit*float64(filled)), y+20, 10, paid)
	}
	if reserved := min(c.Paid+c.Reserved, c.Capacity); reserved > c.Paid {
		roundRect(img, x+int(unit*float64(c.Paid)), y, x+int(unit*float64(reserved)), y+20, 10, brand)
	}
	if c.Needed > 0 && c.Needed <= c.Capacity {
		mark := x + int(unit*float64(c.Needed))
		draw.Draw(img, image.Rect(mark-2, y-10, mark+2, y+30), image.NewUniform(ink), image.Point{}, draw.Src)
	}
}

func roundRect(img *image.RGBA, x0, y0, x1, y1, r int, c color.Color) {
	if x1 <= x0 || y1 <= y0 {
		return
	}
	r = min(r, (x1-x0)/2, (y1-y0)/2)
	z := vector.NewRasterizer(Width, Height)
	fx0, fy0, fx1, fy1, fr := float32(x0), float32(y0), float32(x1), float32(y1), float32(r)
	const k = 0.5523
	z.MoveTo(fx0+fr, fy0)
	z.LineTo(fx1-fr, fy0)
	z.CubeTo(fx1-fr+fr*k, fy0, fx1, fy0+fr-fr*k, fx1, fy0+fr)
	z.LineTo(fx1, fy1-fr)
	z.CubeTo(fx1, fy1-fr+fr*k, fx1-fr+fr*k, fy1, fx1-fr, fy1)
	z.LineTo(fx0+fr, fy1)
	z.CubeTo(fx0+fr-fr*k, fy1, fx0, fy1-fr+fr*k, fx0, fy1-fr)
	z.LineTo(fx0, fy0+fr)
	z.CubeTo(fx0, fy0+fr-fr*k, fx0+fr-fr*k, fy0, fx0+fr, fy0)
	z.ClosePath()
	z.Draw(img, img.Bounds(), image.NewUniform(c), image.Point{})
}

func ring(img *image.RGBA, cx, cy, r, width int, c color.Color) {
	layer := image.NewRGBA(img.Bounds())
	disc(layer, cx, cy, r, c)
	disc(layer, cx, cy, r-width, brand)
	mask := image.NewAlpha(img.Bounds())
	roundMask(mask, 48, 48, 716, 582, 40)
	draw.DrawMask(img, img.Bounds(), layer, image.Point{}, mask, image.Point{}, draw.Over)
}

func disc(img *image.RGBA, cx, cy, r int, c color.Color) {
	z := vector.NewRasterizer(Width, Height)
	circle(z, float32(cx), float32(cy), float32(r))
	z.Draw(img, img.Bounds(), image.NewUniform(c), image.Point{})
}

func circle(z *vector.Rasterizer, cx, cy, r float32) {
	const k = 0.5523
	z.MoveTo(cx+r, cy)
	z.CubeTo(cx+r, cy+r*k, cx+r*k, cy+r, cx, cy+r)
	z.CubeTo(cx-r*k, cy+r, cx-r, cy+r*k, cx-r, cy)
	z.CubeTo(cx-r, cy-r*k, cx-r*k, cy-r, cx, cy-r)
	z.CubeTo(cx+r*k, cy-r, cx+r, cy-r*k, cx+r, cy)
	z.ClosePath()
}

func roundMask(mask *image.Alpha, x0, y0, x1, y1, r int) {
	tmp := image.NewRGBA(mask.Bounds())
	roundRect(tmp, x0, y0, x1, y1, r, color.White)
	draw.Draw(mask, mask.Bounds(), tmp, image.Point{}, draw.Src)
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
