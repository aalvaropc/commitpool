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
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/aalvaropc/commitpool/internal/demo"
	"github.com/aalvaropc/commitpool/internal/present"
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
	flag.StringVar(&opts.Price, "price", "", "price per person in USD (default: the scenario's 45.00)")
	flag.StringVar(&opts.Timezone, "timezone", "America/New_York", "IANA time zone of the workshop")
	flag.IntVar(&opts.WindowMin, "window-min", 1440, "minutes reservations stay open, counted from when they open")
	flag.BoolVar(&opts.OpenPayments, "open-payments", false, "also open reservations on the recommended date")
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

func run(ctx context.Context, client *http.Client, opts options, out io.Writer) (result, error) {
	var res result
	loc, err := time.LoadLocation(opts.Timezone)
	if err != nil {
		return res, fmt.Errorf("time zone %q: %w", opts.Timezone, err)
	}
	api := apiClient{client: client, base: strings.TrimRight(opts.BaseURL, "/")}
	sc := demo.Maya
	price := opts.Price
	if price == "" {
		price = sc.Price
	}
	seeded, err := demo.Create(ctx, api, sc, demo.Options{Now: opts.Now, Timezone: opts.Timezone, WindowMin: opts.WindowMin, Price: price, Demo: true})
	if err != nil {
		return res, err
	}
	res.OwnerURL, res.ShareURL = seeded.OwnerURL, seeded.ShareURL
	ownerToken := seeded.OwnerToken
	fmt.Fprintf(out, "%s with %s: %d needed of %d spots, %s %s a person, studio %s %s, reservations stay open %s once opened\n",
		sc.Title, sc.HostName, sc.Minimum, sc.Capacity, sc.Currency, price, sc.Currency, sc.VenueCost, window(opts.WindowMin))
	fmt.Fprintf(out, "  organizer link (keep it private): %s\n", res.OwnerURL)
	fmt.Fprintf(out, "  invite link:                      %s\n\n", res.ShareURL)

	people, err := demo.Join(ctx, api, seeded, sc.People)
	if err != nil {
		return res, err
	}
	if err := demo.Answer(ctx, api, people, nil); err != nil {
		return res, err
	}
	fmt.Fprintln(out, "Participants, availability already sent:")
	for _, p := range people {
		res.Participants = append(res.Participants, participant{Name: p.Name, Message: p.Message, URL: p.URL})
		fmt.Fprintf(out, "  %-12s %q\n               %s\n", p.Name, p.Message, p.URL)
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
	if err := api.Call(ctx, http.MethodGet, "/api/v1/owner/"+ownerToken, nil, &view); err != nil {
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
		if err := api.Call(ctx, http.MethodPost, "/api/v1/owner/"+ownerToken+"/proposal", map[string]any{"date_id": rec.DateID}, &proposal); err != nil {
			return res, fmt.Errorf("open payments: %w", err)
		}
		res.PaymentsOpened = true
		closes := "until the window closes"
		if !proposal.Proposal.ClosesAt.IsZero() {
			closes = "until " + present.Moment(proposal.Proposal.ClosesAt, loc)
		}
		fmt.Fprintf(out, "Reservations are open on that date %s.\n", closes)
	}
	return res, nil
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

func (a apiClient) Call(ctx context.Context, method, route string, body any, out any) error {
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
