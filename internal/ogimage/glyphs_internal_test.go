package ogimage

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
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
		" $€£¥.,:;!?&@#%+*=()[]/'\"-–—·•…’‘“”×№≈≠≤≥" +
		"ÁÉÍÓÚÜÑáéíóúüñÀÈÌÒÙàèìòùÂÊÎÔÛâêîôûÄËÏÖäëïöÇçÃÕãõÅåÆæØøß" +
		"ĄĆĘŁŃŚŹŻąćęłńśźżČĎĚŇŘŠŤŮŽčďěňřšťůžŐŰőűĞİŞğışœŒȘșȚțĂăÂâÎî" +
		"ẠạẢảẤấẦầẨẩẪẫẬậẮắẰằẲẳẴẵẶặẸẹẺẻẼẽẾếỀềỂểỄễỆệỈỉỊịỌọỎỏỐốỒồỔổỖỗỘộỚớỜờỞởỠỡỢợỤụỦủỨứỪừỬửỮữỰựỲỳỴỵỶỷỸỹƠơƯưĐđ"
	for name, tf := range map[string]typeface{"display": f.display, "semi": f.semi, "medium": f.medium} {
		for _, r := range printed + strings.ToUpper(printed) {
			if !c.covers(tf.font, r) {
				t.Errorf("%s face lacks %q (%U)", name, r, r)
			}
		}
	}
}

func TestSpan_FallsBackOneCharacterAtATime(t *testing.T) {
	c, f := newCanvas(t)
	title := style{f.display, 96, -0.02}
	cases := []struct {
		in, want, fallback string
	}{
		{"Wheel-throwing for beginners", "Wheel-throwing for beginners", ""},
		{"Phở & bánh mì", "Phở & bánh mì", ""},
		{"Ștefan și Țara Românească", "Ștefan și Țara Românească", ""},
		{"éclair № 2 ≈ 3", "éclair № 2 ≈ 3", ""},
		{"Pottery ♥ night", "Pottery ♥ night", "♥"},
		{"Пекарня Ёлка", "Пекарня Ёлка", "ПекарняЁлка"},
		{"Sourdough面包night", "Sourdough night", ""},
		{"🍞 Sourdough 面包 night", "Sourdough night", ""},
		{"Sat, Nov 14", "Sat, Nov 14", ""},
		{"面包", "", ""},
	}
	for _, tc := range cases {
		sp := c.span(title, tc.in)
		if sp.text != tc.want {
			t.Errorf("span(%q) = %q, want %q", tc.in, sp.text, tc.want)
		}
		for _, r := range sp.text {
			face := sp.face(r)
			if face == nil {
				t.Errorf("span(%q) keeps %q, which no face draws", tc.in, r)
				continue
			}
			if fell := face == sp.faces[1]; fell != strings.ContainsRune(tc.fallback, r) && r != ' ' {
				t.Errorf("span(%q): %q fell back = %v", tc.in, r, fell)
			}
		}
	}
}

func TestWords_LeaveOutWhatCannotBeDrawn(t *testing.T) {
	c, f := newCanvas(t)
	cases := []struct {
		card               Card
		title, host, place string
	}{
		{Card{Title: "Phở & bánh mì", Host: "Lê Văn Tám", Place: "Quận 1"}, "PHỞ & BÁNH MÌ", "Lê Văn Tám", "Quận 1"},
		{Card{Title: "面包课", Host: "王老师", Place: "Maya's studio"}, "A CLASS ON COMMITPOOL", "", "Maya's studio"},
		{Card{Title: "Pottery ♥ night 陶艺", Host: "王小明", Place: "Studio 3 · 上海"}, "POTTERY ♥ NIGHT", "", "Studio 3"},
		{Card{Title: "🍞🥖", Host: "🙂", Place: "📍"}, "A CLASS ON COMMITPOOL", "", ""},
	}
	for _, tc := range cases {
		title, host, place := c.words(f, tc.card)
		if title != tc.title || host != tc.host || place != tc.place {
			t.Errorf("words(%+v) = %q, %q, %q; want %q, %q, %q", tc.card, title, host, place, tc.title, tc.host, tc.place)
		}
	}
}

func TestKeepTogether_JoinsDatesAndTimes(t *testing.T) {
	got := keepTogether("Sat, Nov 14 or Sun, Nov 15 · 10:30 AM–12 PM")
	want := "Sat, Nov 14 or Sun, Nov 15 · 10:30 AM–12 PM"
	if got != want {
		t.Errorf("keepTogether = %q, want %q", got, want)
	}
	if got := keepTogether("4 possible dates from Wed, Sep 30"); got != "4 possible dates from Wed, Sep 30" {
		t.Errorf("keepTogether = %q", got)
	}
}

func TestWrap_BreaksBetweenLabelsAndAfterHyphens(t *testing.T) {
	c, f := newCanvas(t)
	sp := c.span(style{f.display, 128, -0.02}, "WHEEL-THROWING FOR ABSOLUTE BEGINNERS")
	narrow := sp.with("WHEEL-THROWING").width() - 10
	lines, fits := sp.wrap(narrow, 6)
	if !fits || lines[0].text != "WHEEL-" || !strings.HasPrefix(lines[1].text, "THROWING") {
		t.Errorf("lines = %v (fits %v); want the break after the hyphen", lines, fits)
	}
	lines, fits = sp.wrap(narrow, 2)
	if fits || len(lines) != 2 || !strings.HasSuffix(lines[1].text, "…") {
		t.Errorf("two lines = %v (fits %v), want an ellipsis on the last", lines, fits)
	}

	when := c.span(style{f.semi, 32, 0}, keepTogether("Sat, Nov 14 or Sun, Nov 15 · 10 AM–12 PM"))
	width := when.with("Sat, Nov 14 or Sun, Nov").width() + 2
	lines, _ = when.wrap(width, 3)
	want := []string{"Sat, Nov 14 or", "Sun, Nov 15 ·", "10 AM–12 PM"}
	for i, w := range want {
		if i >= len(lines) || lines[i].text != w {
			t.Fatalf("when lines = %v, want %q", lines, want)
		}
	}
}

func tileRight(t *testing.T, card Card) int {
	t.Helper()
	raw, err := Render(card)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	y := bandTop + 14
	for x := margin + tileRound; x < Width; x++ {
		if img.At(x, y) == color.Color(color.RGBA{0x14, 0x10, 0x0d, 0xff}) || img.At(x, y) == color.Color(color.NRGBA{0x14, 0x10, 0x0d, 0xff}) {
			return x
		}
	}
	return Width
}

func TestBand_KeepsTheDatesWide(t *testing.T) {
	base := Card{
		Status: "Reservations open", Tone: ToneHold, Title: "Wheel-throwing for beginners", Host: "Maya Okafor",
		When: "Wed, Sep 30 or Wed, Dec 30 · 10:30 AM–12:45 PM", Price: "$45", Needed: 3, Capacity: 6,
	}
	huge := base
	huge.Price = "$92,233,720,368,547,758.07"
	crowded := base
	crowded.Price, crowded.Needed, crowded.Capacity, crowded.Reserved = "$1,234,567.89", 250, 500, 499
	for name, card := range map[string]Card{"base": base, "huge price": huge, "large counts": crowded} {
		if got := tileRight(t, card) - margin; got < whenMin-3 {
			t.Errorf("%s: the dates tile is %dpx wide, want at least %d", name, got, whenMin)
		}
	}
}

func TestBand_ShrinksThePriceBeforeCuttingIt(t *testing.T) {
	c, f := newCanvas(t)
	two := "Wed, Sep 30 or Wed, Dec 30 · 10:30 AM–12:45 PM"
	cases := []struct {
		card  Card
		price string
	}{
		{Card{When: two, Price: "$45", Needed: 3, Capacity: 6}, "$45"},
		{Card{When: two, Price: "$1,234,567.89", Needed: 250, Capacity: 500, Reserved: 499}, "$1,234,567.89"},
		{Card{When: two, Price: "$99,999.99", Needed: 500, Capacity: 500, Paid: 500}, "$99,999.99"},
		{Card{When: two, Price: "$92,233,720,368,547,758.07", Needed: 3, Capacity: 6, Reserved: 2}, ""},
	}
	for _, tc := range cases {
		b := c.layoutBand(f, tc.card)
		if b.when < whenMin || b.when+b.priceW+b.meterW != bandWidth {
			t.Errorf("%s: dates %dpx, price %dpx, meter %dpx; want the dates at least %d in %d", tc.card.Price, b.when, b.priceW, b.meterW, whenMin, bandWidth)
		}
		if tc.price != "" && b.price.text != tc.price {
			t.Errorf("price %q drawn as %q", tc.card.Price, b.price.text)
		}
		if tc.price == "" && !strings.HasSuffix(b.price.text, "…") {
			t.Errorf("price %q drawn as %q, want it cut with an ellipsis", tc.card.Price, b.price.text)
		}
		if w := max(b.price.width(), b.unit.width()) + 2*tilePad; w > b.priceW {
			t.Errorf("price %q needs %dpx in a %dpx tile", tc.card.Price, w, b.priceW)
		}
	}
}
