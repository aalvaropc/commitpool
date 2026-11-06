package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/ics"
	"github.com/aalvaropc/commitpool/internal/ogimage"
	"github.com/aalvaropc/commitpool/internal/present"
	"github.com/aalvaropc/commitpool/internal/store"
)

const (
	previewImageCacheSize = 256
	previewCacheSize      = 512
	previewTTL            = 15 * time.Second
	immutableImage        = "public, max-age=31536000, immutable"
)

var errNoDateYet = newError(http.StatusNotFound, "no_date_yet", "The organizer has not picked the date yet; the calendar invite comes once they do.")

type invitePreview struct {
	WorkshopID  string
	Title       string
	Description string
	URL         string
	Image       string
	ImageAlt    string
	Card        ogimage.Card
}

func cardVersion(c ogimage.Card) string {
	sum := sha256.Sum256(fmt.Appendf(nil, "%#v", c))
	return hex.EncodeToString(sum[:6])
}

func previewOf(st workshopState, baseURL string, now time.Time) invitePreview {
	w := st.w
	price := domain.Money{Minor: w.PriceMinor, Currency: w.Currency}
	if st.proposal != nil {
		price = domain.Money{Minor: st.proposal.PriceMinor, Currency: st.proposal.Currency}
	}
	t := st.tally()
	c := st.counts()
	card := ogimage.Card{
		Status:   inviteStatus(st, now),
		Title:    w.Title,
		Host:     w.HostName,
		Place:    w.Location,
		When:     whenText(st),
		Price:    present.Price(price),
		Needed:   t.Minimum,
		Capacity: t.Capacity,
		Paid:     c.Paid,
		Reserved: c.Authorized + c.Capturing + c.Pending,
	}
	var lead []string
	if w.HostName != "" {
		lead = append(lead, "With "+w.HostName)
	}
	if w.Location != "" {
		lead = append(lead, w.Location)
	}
	terms := fmt.Sprintf("%s a spot; it happens if %d reserve, and nobody pays otherwise.", card.Price, t.Minimum)
	switch w.Status {
	case domain.WorkshopConfirmed:
		terms = fmt.Sprintf("It’s on: %d paid %s a spot.", c.Paid, card.Price)
	case domain.WorkshopCancelled:
		terms = "This class is cancelled; nobody pays for it."
	}
	terms += " Times in " + present.ZoneName(st.loc) + "."
	description := terms
	if len(lead) > 0 {
		description = strings.Join(lead, " · ") + ". " + terms
	}
	share := baseURL + "/w/" + w.ShareToken
	return invitePreview{
		WorkshopID:  w.ID,
		Title:       w.Title,
		Description: description,
		URL:         share,
		Image:       baseURL + "/api/v1/w/" + w.ShareToken + "/og.png?v=" + cardVersion(card),
		ImageAlt:    fmt.Sprintf("%s, %s. %s", w.Title, card.When, terms),
		Card:        card,
	}
}

func inviteStatus(st workshopState, now time.Time) string {
	switch st.w.Status {
	case domain.WorkshopCollecting:
		return "Finding the date"
	case domain.WorkshopFunding:
		if paymentsOpen(st.w, st.proposal, now) {
			return "Reservations open"
		}
		return "Reservations closed"
	case domain.WorkshopConfirmed:
		return "It’s on"
	case domain.WorkshopCancelled:
		return "Cancelled"
	}
	return ""
}

func whenText(st workshopState) string {
	if st.proposal != nil {
		for _, d := range st.dates {
			if d.ID == st.proposal.ChosenDateID {
				return sessionText(d.StartsAt, d.EndsAt, st)
			}
		}
	}
	switch len(st.dates) {
	case 0:
		return "Dates to be announced"
	case 1:
		return sessionText(st.dates[0].StartsAt, st.dates[0].EndsAt, st)
	case 2:
		a, b := st.dates[0], st.dates[1]
		if present.Clock(a.StartsAt.In(st.loc)) == present.Clock(b.StartsAt.In(st.loc)) && present.Clock(a.EndsAt.In(st.loc)) == present.Clock(b.EndsAt.In(st.loc)) {
			return fmt.Sprintf("%s or %s · %s–%s", present.Day(a.StartsAt, st.loc), present.Day(b.StartsAt, st.loc), present.Clock(a.StartsAt.In(st.loc)), present.Clock(a.EndsAt.In(st.loc)))
		}
	}
	return fmt.Sprintf("%d possible dates from %s", len(st.dates), present.Day(st.dates[0].StartsAt, st.loc))
}

func sessionText(start, end time.Time, st workshopState) string {
	return fmt.Sprintf("%s · %s–%s", present.Day(start, st.loc), present.Clock(start.In(st.loc)), present.Clock(end.In(st.loc)))
}

type cachedPreview struct {
	p  invitePreview
	at time.Time
}

type previewCache struct {
	mu    sync.Mutex
	items map[string]cachedPreview
}

func (s *server) lookupPreview(ctx context.Context, shareToken string) (invitePreview, error) {
	now := s.Clock.Now()
	s.previews.mu.Lock()
	if c, ok := s.previews.items[shareToken]; ok && now.Sub(c.at) < previewTTL {
		s.previews.mu.Unlock()
		return c.p, nil
	}
	s.previews.mu.Unlock()
	ws, err := s.Store.WorkshopByShareToken(ctx, shareToken)
	if err != nil {
		return invitePreview{}, err
	}
	st, err := s.loadState(ctx, ws)
	if err != nil {
		return invitePreview{}, err
	}
	p := previewOf(st, s.Cfg.BaseURL, now)
	s.previews.mu.Lock()
	defer s.previews.mu.Unlock()
	if s.previews.items == nil {
		s.previews.items = map[string]cachedPreview{}
	}
	if len(s.previews.items) >= previewCacheSize {
		for k := range s.previews.items {
			delete(s.previews.items, k)
			break
		}
	}
	s.previews.items[shareToken] = cachedPreview{p: p, at: now}
	return p, nil
}

func (s *server) pagePreview(ctx context.Context, shareToken string) (invitePreview, bool) {
	p, err := s.lookupPreview(ctx, shareToken)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			s.Log.Warn("invite preview: lookup failed; serving the plain page", "error", err)
		}
		return invitePreview{}, false
	}
	return p, true
}

type cachedImage struct {
	card ogimage.Card
	png  []byte
}

type imageCache struct {
	mu     sync.Mutex
	render sync.Mutex
	items  map[string]cachedImage
}

func (c *imageCache) cached(workshopID string, card ogimage.Card) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if it, ok := c.items[workshopID]; ok && it.card == card {
		return it.png, true
	}
	return nil, false
}

func (c *imageCache) get(workshopID string, card ogimage.Card) ([]byte, error) {
	if raw, ok := c.cached(workshopID, card); ok {
		return raw, nil
	}
	c.render.Lock()
	defer c.render.Unlock()
	if raw, ok := c.cached(workshopID, card); ok {
		return raw, nil
	}
	raw, err := ogimage.Render(card)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.items == nil {
		c.items = map[string]cachedImage{}
	}
	if _, ok := c.items[workshopID]; !ok && len(c.items) >= previewImageCacheSize {
		for k := range c.items {
			delete(c.items, k)
			break
		}
	}
	c.items[workshopID] = cachedImage{card: card, png: raw}
	return raw, nil
}

func (s *server) inviteImage(w http.ResponseWriter, r *http.Request) error {
	token, err := pathToken(r)
	if err != nil {
		return err
	}
	p, err := s.lookupPreview(r.Context(), token)
	if err != nil {
		return notFoundIfMissing(err)
	}
	raw, err := s.images.get(p.WorkshopID, p.Card)
	if err != nil {
		return err
	}
	cache := "public, max-age=300"
	if r.URL.Query().Get("v") == cardVersion(p.Card) {
		cache = immutableImage
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", cache)
	_, _ = w.Write(raw)
	return nil
}

func (s *server) inviteCalendar(w http.ResponseWriter, r *http.Request) error {
	ws, err := s.sharedWorkshop(r)
	if err != nil {
		return err
	}
	return s.writeCalendar(w, r, ws, true)
}

func (s *server) participantCalendar(w http.ResponseWriter, r *http.Request) error {
	_, ws, err := s.participant(r)
	if err != nil {
		return err
	}
	return s.writeCalendar(w, r, ws, false)
}

func (s *server) writeCalendar(w http.ResponseWriter, r *http.Request, ws store.WorkshopRow, withInvite bool) error {
	st, err := s.loadState(r.Context(), ws)
	if err != nil {
		return err
	}
	now := s.Clock.Now()
	event, ok := calendarEvent(st, previewOf(st, s.Cfg.BaseURL, now), now, withInvite)
	if !ok {
		return errNoDateYet
	}
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+fileSlug(ws.Title)+`.ics"`)
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(ics.Calendar(event))
	return nil
}

func calendarEvent(st workshopState, p invitePreview, now time.Time, withInvite bool) (ics.Event, bool) {
	if st.proposal == nil {
		return ics.Event{}, false
	}
	var start, end time.Time
	for _, d := range st.dates {
		if d.ID == st.proposal.ChosenDateID {
			start, end = d.StartsAt, d.EndsAt
		}
	}
	if start.IsZero() {
		return ics.Event{}, false
	}
	status, step := ics.Tentative, 0
	switch st.w.Status {
	case domain.WorkshopConfirmed:
		status, step = ics.Confirmed, 1
	case domain.WorkshopCancelled:
		status, step = ics.Cancelled, 2
	}
	event := ics.Event{
		UID:         "workshop-" + st.w.ID + "@commitpool",
		Sequence:    st.proposal.Version*10 + step,
		Stamp:       now,
		Start:       start,
		End:         end,
		Summary:     st.w.Title,
		Description: p.Description,
		Location:    st.w.Location,
		Status:      status,
	}
	if withInvite {
		event.Description += "\nInvite: " + p.URL
		event.URL = p.URL
	}
	return event, true
}

var slugBreak = regexp.MustCompile(`[^a-z0-9]+`)

func fileSlug(title string) string {
	slug := strings.Trim(slugBreak.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if len(slug) > 60 {
		slug = strings.TrimRight(slug[:60], "-")
	}
	if slug == "" {
		return "workshop"
	}
	return slug
}

var (
	titleTag       = regexp.MustCompile(`<title>[^<]*</title>`)
	descriptionTag = regexp.MustCompile(`<meta\s+name="description"\s+content="[^"]*"\s*/?>`)
)

func injectPreview(index []byte, p invitePreview) []byte {
	attr := html.EscapeString
	page := []byte(titleTag.ReplaceAllLiteralString(string(index), "<title>"+attr(p.Title)+" – CommitPool</title>"))
	page = []byte(descriptionTag.ReplaceAllLiteralString(string(page), `<meta name="description" content="`+attr(p.Description)+`" />`))
	var tags strings.Builder
	for _, m := range [][2]string{
		{"og:type", "website"}, {"og:site_name", "CommitPool"}, {"og:title", p.Title}, {"og:description", p.Description},
		{"og:url", p.URL}, {"og:image", p.Image}, {"og:image:width", fmt.Sprint(ogimage.Width)}, {"og:image:height", fmt.Sprint(ogimage.Height)},
		{"og:image:alt", p.ImageAlt},
	} {
		fmt.Fprintf(&tags, `<meta property="%s" content="%s" />`, m[0], attr(m[1]))
	}
	tags.WriteString(`<meta name="twitter:card" content="summary_large_image" />`)
	if i := bytes.Index(page, []byte("</head>")); i >= 0 {
		return append(page[:i:i], append([]byte(tags.String()), page[i:]...)...)
	}
	if loc := titleTag.FindIndex(page); loc != nil {
		return append(page[:loc[1]:loc[1]], append([]byte(tags.String()), page[loc[1]:]...)...)
	}
	return page
}
