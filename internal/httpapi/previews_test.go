package httpapi_test

import (
	"bytes"
	"image/png"
	"net/http"
	"strings"
	"testing"
)

func classWorkshop(body map[string]any) {
	body["title"] = `Life drawing <for> "beginners"`
	body["host_name"] = "Iris Calder"
	body["location"] = "The Long Room · Red Hook, Brooklyn"
	body["timezone"] = "America/New_York"
	body["price"] = "45.00"
	body["minimum"] = 3
	body["capacity"] = 6
}

func TestInvite_LinkPreviewNamesTheClassInTheHead(t *testing.T) {
	e := newEnv(t)
	h := e.createWorkshop(classWorkshop)
	rec := e.request(http.MethodGet, "/w/"+h.shareToken, nil)
	page := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.HasPrefix(page, "<!doctype html>") {
		t.Fatalf("GET /w/ = %d %q", rec.Code, page)
	}
	for _, want := range []string{
		`<title>Life drawing &lt;for&gt; &#34;beginners&#34; – CommitPool</title>`,
		`<meta property="og:title" content="Life drawing &lt;for&gt; &#34;beginners&#34;" />`,
		`<meta property="og:image" content="` + appBaseURL + `/api/v1/w/` + h.shareToken + `/og.png?v=`,
		`<meta property="og:url" content="` + appBaseURL + `/w/` + h.shareToken + `" />`,
		`<meta property="og:description" content="With Iris Calder · The Long Room · Red Hook, Brooklyn. $45 a spot; it happens if 3 reserve, and nobody pays otherwise. Times in New York time." />`,
		`<meta name="twitter:card" content="summary_large_image" />`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %s\n%s", want, page)
		}
	}
	if strings.Contains(page, h.ownerToken) || strings.Contains(page, "<for>") {
		t.Errorf("page leaks the organizer token or raw markup:\n%s", page)
	}
	if strings.Count(page, `name="description"`) != 1 || strings.Index(page, `property="og:title"`) > strings.Index(page, "</head>") {
		t.Errorf("want one description and the Open Graph tags inside the head:\n%s", page)
	}
	if rec.Header().Get("X-Robots-Tag") != "noindex, nofollow" {
		t.Error("the invite can be indexed by search engines")
	}
}

func TestLanding_LinkPreviewUsesTheDefaultCard(t *testing.T) {
	e := newEnv(t)
	rec := e.request(http.MethodGet, "/", nil)
	page := rec.Body.String()
	for _, want := range []string{
		`<title>Find the date that fills the class – CommitPool</title>`,
		`<meta property="og:title" content="Find the date that fills the class" />`,
		`<meta property="og:description" content="Paste your group chat. Claude reads everyone’s maybes and asks when it matters. CommitPool picks the date that reaches your minimum, and PayPal charges only once enough people reserve." />`,
		`<meta property="og:image" content="` + appBaseURL + `/og-default.png" />`,
		`<meta property="og:url" content="` + appBaseURL + `/" />`,
		`<meta name="twitter:card" content="summary_large_image" />`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("landing page lacks %s\n%s", want, page)
		}
	}
	if strings.Count(page, `name="description"`) != 1 {
		t.Errorf("want exactly one description:\n%s", page)
	}
	if got := rec.Header().Get("X-Robots-Tag"); got != "" {
		t.Errorf("X-Robots-Tag = %q; the landing page stays indexable", got)
	}
	if rec := e.request(http.MethodGet, "/new", nil); rec.Body.String() != indexHTML {
		t.Errorf("GET /new = %q, want the plain index", rec.Body.String())
	}
}

func TestInvite_UnknownTokensGetThePlainPage(t *testing.T) {
	e := newEnv(t)
	token := strings.Repeat("a", 43)
	if rec := e.request(http.MethodGet, "/w/"+token, nil); rec.Body.String() != indexHTML {
		t.Errorf("unknown invite = %q, want the plain index", rec.Body.String())
	}
}

func TestInvite_PrivatePagesAreNotIndexed(t *testing.T) {
	e := newEnv(t)
	for _, path := range []string{"/p/xyz", "/owner/abc", "/w/abc"} {
		rec := e.request(http.MethodGet, path, nil)
		if rec.Header().Get("X-Robots-Tag") != "noindex, nofollow" || rec.Body.String() != indexHTML {
			t.Errorf("GET %s: X-Robots-Tag %q, body %q", path, rec.Header().Get("X-Robots-Tag"), rec.Body.String())
		}
	}
}

func TestInvite_ImageIsALinkPreviewPNG(t *testing.T) {
	e := newEnv(t)
	h := e.createWorkshop(classWorkshop)
	rec := e.request(http.MethodGet, h.sharePath+"/og.png", nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" || rec.Header().Get("Cache-Control") != "public, max-age=300" {
		t.Fatalf("og.png = %d %q %q", rec.Code, rec.Header().Get("Content-Type"), rec.Header().Get("Cache-Control"))
	}
	page := e.request(http.MethodGet, "/w/"+h.shareToken, nil).Body.String()
	start := strings.Index(page, "/og.png?v=")
	version := page[start+len("/og.png?v=") : start+len("/og.png?v=")+12]
	if got := e.request(http.MethodGet, h.sharePath+"/og.png?v="+version, nil).Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("versioned og.png Cache-Control = %q", got)
	}
	img, err := png.Decode(bytes.NewReader(rec.Body.Bytes()))
	if err != nil || img.Bounds().Dx() != 1200 || img.Bounds().Dy() != 630 {
		t.Fatalf("og.png is not a 1200×630 PNG: %v", err)
	}
	if r, g, b, _ := img.At(4, 4).RGBA(); r>>8 != 0x14 || g>>8 != 0x10 || b>>8 != 0x0d {
		t.Errorf("og.png canvas = %v, want the warm charcoal #14100d", img.At(4, 4))
	}
	again := e.request(http.MethodGet, h.sharePath+"/og.png", nil)
	if !bytes.Equal(again.Body.Bytes(), rec.Body.Bytes()) {
		t.Error("the same invite rendered two different images")
	}
	e.wantError(http.MethodGet, "/api/v1/w/"+strings.Repeat("a", 43)+"/og.png", nil, http.StatusNotFound, "not_found")
}

func TestInvite_CalendarOnceTheDateIsPicked(t *testing.T) {
	e := newEnv(t)
	h := e.createWorkshop(classWorkshop)
	e.wantError(http.MethodGet, h.sharePath+"/calendar.ics", nil, http.StatusNotFound, "no_date_yet")

	var priya participantHandle
	for i, name := range []string{"Priya", "Ben", "Lucía"} {
		p := e.joinAndSay(h, name, "Both days work for me.")
		if i == 0 {
			priya = p
		}
	}
	if p := e.openProposal(h, h.satID); p.CalendarPath != "/api/v1/w/"+h.shareToken+"/calendar.ics" {
		t.Errorf("proposal calendar_path = %q", p.CalendarPath)
	}
	rec := e.request(http.MethodGet, h.sharePath+"/calendar.ics", nil)
	body := strings.ReplaceAll(rec.Body.String(), "\r\n ", "")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/calendar; charset=utf-8" {
		t.Fatalf("calendar = %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if cd := rec.Header().Get("Content-Disposition"); cd != `attachment; filename="life-drawing-for-beginners.ics"` {
		t.Errorf("Content-Disposition = %q", cd)
	}
	for _, want := range []string{"DTSTART:20261114T200000Z", "DTEND:20261114T230000Z", "STATUS:TENTATIVE", "SEQUENCE:10",
		"LOCATION:The Long Room · Red Hook\\, Brooklyn", "URL:" + appBaseURL + "/w/" + h.shareToken} {
		if !strings.Contains(body, want+"\r\n") {
			t.Errorf("calendar lacks %s:\n%s", want, body)
		}
	}
	if strings.Contains(body, h.ownerToken) || strings.Contains(body, "/p/") {
		t.Errorf("calendar carries a private link:\n%s", body)
	}

	own := e.request(http.MethodGet, "/api/v1/p/"+priya.token+"/calendar.ics", nil)
	ownBody := strings.ReplaceAll(own.Body.String(), "\r\n ", "")
	if own.Code != http.StatusOK || !strings.Contains(ownBody, "DTSTART:20261114T200000Z\r\n") {
		t.Fatalf("participant calendar = %d:\n%s", own.Code, ownBody)
	}
	for _, secret := range []string{priya.token, h.shareToken, h.ownerToken, "URL:"} {
		if strings.Contains(ownBody, secret) {
			t.Errorf("participant calendar carries %q:\n%s", secret, ownBody)
		}
	}

	e.call(http.MethodPost, h.ownerPath+"/cancel", map[string]string{"reason": "Room flooded"}, http.StatusOK, nil)
	body = e.request(http.MethodGet, h.sharePath+"/calendar.ics", nil).Body.String()
	if !strings.Contains(body, "STATUS:CANCELLED\r\n") || !strings.Contains(body, "SEQUENCE:12\r\n") {
		t.Errorf("cancelled calendar:\n%s", body)
	}
}

func TestInvite_ImageIsLimitedPerAddress(t *testing.T) {
	e := newEnv(t, withRateLimit(4))
	h := e.createWorkshop(classWorkshop)
	if rec := e.request(http.MethodGet, h.sharePath+"/og.png", nil); rec.Code != http.StatusOK {
		t.Fatalf("first og.png = %d", rec.Code)
	}
	e.wantError(http.MethodGet, h.sharePath+"/og.png", nil, http.StatusTooManyRequests, "rate_limited")
}
