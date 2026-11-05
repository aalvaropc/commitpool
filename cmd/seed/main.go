// Command seed creates the demo workshop through the HTTP API of a running
// CommitPool: a photography walk with two weekend dates, a minimum of three
// paid seats out of six at US$10, and six participants whose availability
// messages are already sent, one of them deliberately ambiguous. It prints
// the organizer link, the invite link and one link per participant.
//
// It needs no credentials and never touches the database directly, so it
// works the same against a local server and a deployment.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
	_ "time/tzdata"
)

// cast is the demo's participants with the message each one sends. With the
// candidate dates on a Saturday and a Sunday, four people can come on the
// Saturday and three on the Sunday; the last message names no time and asks
// for clarification.
var cast = []struct{ Name, Message string }{
	{"Ana", "Saturday works for me, Sunday I can't."},
	{"Ben", "Either day is fine, but on Saturday only if we finish before six."},
	{"Cleo", "I can't do Saturday; Sunday morning is perfect."},
	{"Dev", "Count me in on Saturday. Sunday is no good for me."},
	{"Eli", "Both days work for me."},
	{"Fay", "I can make it either day, ideally in the afternoon."},
}

const (
	workshopTitle       = "Street photography walk"
	workshopDescription = "A three-hour guided walk on composition and light, in a small group. Bring any camera.\n\nPrepared demo data: the participants and their answers were written for this demo."
	durationMin         = 180
	minimum             = 3
	capacity            = 6
	currency            = "USD"
	startHour           = 10
	// leadDays keeps the first candidate date at least this many days after
	// the payment window ends, so the organizer can open payments later and
	// the date still starts a day after the window closes.
	leadDays = 3
)

type options struct {
	BaseURL      string
	Price        string
	Timezone     string
	WindowMin    int
	OpenPayments bool
	Now          time.Time
}

type participant struct {
	Name, Message, URL string
}

type result struct {
	OwnerURL       string
	ShareURL       string
	Participants   []participant
	Recommended    string
	CanCome        int
	PaymentsOpened bool
}

func main() {
	var opts options
	flag.StringVar(&opts.BaseURL, "base", envOr("COMMITPOOL_BASE_URL", "http://localhost:8080"), "base URL of the running service")
	flag.StringVar(&opts.Price, "price", "10.00", "price per seat in USD")
	flag.StringVar(&opts.Timezone, "timezone", "America/New_York", "IANA time zone of the workshop")
	flag.IntVar(&opts.WindowMin, "window-min", 1440, "payment window in minutes, counted from when payments open")
	flag.BoolVar(&opts.OpenPayments, "open-payments", false, "also open payments on the recommended date")
	flag.Parse()
	opts.Now = time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	client := &http.Client{Timeout: 90 * time.Second}
	if _, err := run(ctx, client, opts, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
}

func envOr(name, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return fallback
}

// run seeds the workshop and writes the links to out.
func run(ctx context.Context, client *http.Client, opts options, out io.Writer) (result, error) {
	var res result
	loc, err := time.LoadLocation(opts.Timezone)
	if err != nil {
		return res, fmt.Errorf("time zone %q: %w", opts.Timezone, err)
	}
	base := strings.TrimRight(opts.BaseURL, "/")
	api := apiClient{client: client, base: base}

	saturday := firstSaturday(opts.Now, opts.WindowMin, loc)
	type date struct {
		StartsAt time.Time `json:"starts_at"`
		EndsAt   time.Time `json:"ends_at"`
	}
	dates := make([]date, 0, 2)
	for _, start := range []time.Time{saturday, saturday.AddDate(0, 0, 1)} {
		dates = append(dates, date{StartsAt: start, EndsAt: start.Add(durationMin * time.Minute)})
	}
	var created struct {
		OwnerURL string `json:"owner_url"`
		ShareURL string `json:"share_url"`
	}
	if err := api.call(ctx, http.MethodPost, "/api/v1/workshops", map[string]any{
		"title":              workshopTitle,
		"description":        workshopDescription,
		"duration_min":       durationMin,
		"timezone":           opts.Timezone,
		"minimum":            minimum,
		"capacity":           capacity,
		"price":              opts.Price,
		"currency":           currency,
		"payment_window_min": opts.WindowMin,
		"dates":              dates,
	}, &created); err != nil {
		return res, err
	}
	res.OwnerURL, res.ShareURL = created.OwnerURL, created.ShareURL
	ownerToken, shareToken := path.Base(created.OwnerURL), path.Base(created.ShareURL)
	fmt.Fprintf(out, "%s: minimum %d of %d seats, %s %s per seat, payments stay open %s once opened\n",
		workshopTitle, minimum, capacity, currency, opts.Price, window(opts.WindowMin))
	fmt.Fprintf(out, "  organizer link (keep it private): %s\n", res.OwnerURL)
	fmt.Fprintf(out, "  invite link:                      %s\n\n", res.ShareURL)

	fmt.Fprintln(out, "Participants, availability already sent:")
	for _, member := range cast {
		var joined struct {
			ParticipantURL string `json:"participant_url"`
		}
		if err := api.call(ctx, http.MethodPost, "/api/v1/w/"+shareToken+"/participants", map[string]any{"display_name": member.Name}, &joined); err != nil {
			return res, fmt.Errorf("join %s: %w", member.Name, err)
		}
		token := path.Base(joined.ParticipantURL)
		if err := api.call(ctx, http.MethodPost, "/api/v1/p/"+token+"/availability", map[string]any{"message": member.Message}, nil); err != nil {
			return res, fmt.Errorf("availability of %s: %w", member.Name, err)
		}
		res.Participants = append(res.Participants, participant{Name: member.Name, Message: member.Message, URL: joined.ParticipantURL})
		fmt.Fprintf(out, "  %-4s %q\n       %s\n", member.Name, member.Message, joined.ParticipantURL)
	}

	var view struct {
		Dates []struct {
			ID    string `json:"id"`
			Label string `json:"label"`
		} `json:"dates"`
		Recommendation struct {
			DateID       string `json:"date_id"`
			MeetsMinimum bool   `json:"meets_minimum"`
			PerDate      []struct {
				DateID     string   `json:"date_id"`
				Compatible []string `json:"compatible"`
			} `json:"per_date"`
		} `json:"recommendation"`
	}
	if err := api.call(ctx, http.MethodGet, "/api/v1/owner/"+ownerToken, nil, &view); err != nil {
		return res, err
	}
	rec := view.Recommendation
	for _, d := range view.Dates {
		if d.ID == rec.DateID {
			res.Recommended = d.Label
		}
	}
	for _, d := range rec.PerDate {
		if d.DateID == rec.DateID {
			res.CanCome = len(d.Compatible)
		}
	}
	if rec.DateID == "" {
		return res, errors.New("the owner view recommends no date")
	}
	verdict := "meets the minimum"
	if !rec.MeetsMinimum {
		verdict = "does not meet the minimum"
	}
	fmt.Fprintf(out, "\nRecommended date: %s, %d can come, %s\n", res.Recommended, res.CanCome, verdict)

	if opts.OpenPayments {
		var proposal struct {
			Proposal struct {
				ClosesAt time.Time `json:"closes_at"`
			} `json:"proposal"`
		}
		if err := api.call(ctx, http.MethodPost, "/api/v1/owner/"+ownerToken+"/proposal", map[string]any{"date_id": rec.DateID}, &proposal); err != nil {
			return res, fmt.Errorf("open payments: %w", err)
		}
		res.PaymentsOpened = true
		closes := "until the window closes"
		if !proposal.Proposal.ClosesAt.IsZero() {
			closes = "until " + proposal.Proposal.ClosesAt.In(loc).Format("Mon 2 Jan 2006, 15:04 MST")
		}
		fmt.Fprintf(out, "Payments are open on that date %s.\n", closes)
	}
	return res, nil
}

// firstSaturday is the first Saturday at startHour on the workshop's clock
// that starts at least leadDays after the payment window would end if it
// opened now.
func firstSaturday(now time.Time, windowMin int, loc *time.Location) time.Time {
	earliest := now.Add(time.Duration(windowMin)*time.Minute + leadDays*24*time.Hour).In(loc)
	day := time.Date(earliest.Year(), earliest.Month(), earliest.Day(), startHour, 0, 0, 0, loc)
	if day.Before(earliest) {
		day = day.AddDate(0, 0, 1)
	}
	for day.Weekday() != time.Saturday {
		day = day.AddDate(0, 0, 1)
	}
	return day
}

func window(minutes int) string {
	if minutes%60 == 0 {
		return fmt.Sprintf("%d h", minutes/60)
	}
	return fmt.Sprintf("%d min", minutes)
}

type apiClient struct {
	client *http.Client
	base   string
}

// call sends body as JSON and decodes a 2xx response into out when out is
// not nil; any other status is an error carrying the start of the body.
func (a apiClient) call(ctx context.Context, method, route string, body any, out any) error {
	var payload io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.base+route, payload)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, route, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, route, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("%s %s: HTTP %d: %s", method, route, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%s %s: decode: %w", method, route, err)
	}
	return nil
}
