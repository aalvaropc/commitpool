package demo

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"time"
)

type Caller interface {
	Call(ctx context.Context, method, path string, body, out any) error
}

type Person struct {
	Name, Message string
}

type Scenario struct {
	Title, Description, HostName, Location string
	DurationMin, Minimum, Capacity         int
	StartHour                              int
	Price, VenueCost, Currency             string
	People                                 []Person
}

var Maya = Scenario{
	Title:       "Wheel-throwing for beginners",
	Description: "Your first bowl on the wheel. Two hours with all the clay, tools and firing included, in a small group. No experience needed; wear clothes that can get muddy.",
	HostName:    "Maya Okafor",
	Location:    "Maya's studio · Greenpoint, Brooklyn",
	DurationMin: 120,
	Minimum:     3,
	Capacity:    6,
	StartHour:   10,
	Price:       "45.00",
	VenueCost:   "120.00",
	Currency:    "USD",
	People: []Person{
		{"Priya Raman", "Saturday works for me, Sunday I can't."},
		{"Ben Whitaker", "Either day is fine, but on Saturday only if we finish before six."},
		{"Lucía Torres", "I can't do Saturday; Sunday morning is perfect."},
		{"Hannah Kim", "Count me in on Saturday. Sunday is no good for me."},
		{"Tomás Silva", "Both days work for me."},
		{"Grace Liu", "I can make it either day, ideally in the afternoon."},
	},
}

type Options struct {
	Now       time.Time
	Timezone  string
	WindowMin int
	Price     string
	Demo      bool
}

type Participant struct {
	Name, Message, URL, Token string
}

type Result struct {
	OwnerURL, ShareURL     string
	OwnerToken, ShareToken string
	Participants           []Participant
}

type Pace func(context.Context) error

func FirstSaturday(now time.Time, windowMin, startHour int, loc *time.Location) time.Time {
	earliest := now.Add(time.Duration(windowMin)*time.Minute + 3*24*time.Hour).In(loc)
	day := time.Date(earliest.Year(), earliest.Month(), earliest.Day(), startHour, 0, 0, 0, loc)
	if day.Before(earliest) {
		day = day.AddDate(0, 0, 1)
	}
	for day.Weekday() != time.Saturday {
		day = day.AddDate(0, 0, 1)
	}
	return day
}

func Create(ctx context.Context, api Caller, sc Scenario, o Options) (Result, error) {
	loc, err := time.LoadLocation(o.Timezone)
	if err != nil {
		return Result{}, fmt.Errorf("time zone %q: %w", o.Timezone, err)
	}
	price := sc.Price
	if o.Price != "" {
		price = o.Price
	}
	saturday := FirstSaturday(o.Now, o.WindowMin, sc.StartHour, loc)
	type date struct {
		StartsAt time.Time `json:"starts_at"`
		EndsAt   time.Time `json:"ends_at"`
	}
	dates := []date{}
	for _, start := range []time.Time{saturday, saturday.AddDate(0, 0, 1)} {
		dates = append(dates, date{StartsAt: start, EndsAt: start.Add(time.Duration(sc.DurationMin) * time.Minute)})
	}
	var created struct {
		OwnerURL string `json:"owner_url"`
		ShareURL string `json:"share_url"`
	}
	err = api.Call(ctx, http.MethodPost, "/api/v1/workshops", map[string]any{
		"title":              sc.Title,
		"description":        sc.Description,
		"host_name":          sc.HostName,
		"location":           sc.Location,
		"duration_min":       sc.DurationMin,
		"timezone":           o.Timezone,
		"minimum":            sc.Minimum,
		"capacity":           sc.Capacity,
		"price":              price,
		"venue_cost":         sc.VenueCost,
		"currency":           sc.Currency,
		"payment_window_min": o.WindowMin,
		"demo":               o.Demo,
		"dates":              dates,
	}, &created)
	if err != nil {
		return Result{}, fmt.Errorf("create the workshop: %w", err)
	}
	return Result{
		OwnerURL: created.OwnerURL, ShareURL: created.ShareURL,
		OwnerToken: path.Base(created.OwnerURL), ShareToken: path.Base(created.ShareURL),
	}, nil
}

func Join(ctx context.Context, api Caller, res Result, people []Person) ([]Participant, error) {
	out := make([]Participant, 0, len(people))
	for _, p := range people {
		var joined struct {
			ParticipantURL string `json:"participant_url"`
		}
		if err := api.Call(ctx, http.MethodPost, "/api/v1/w/"+res.ShareToken+"/participants", map[string]any{"display_name": p.Name}, &joined); err != nil {
			return out, fmt.Errorf("join %s: %w", p.Name, err)
		}
		out = append(out, Participant{Name: p.Name, Message: p.Message, URL: joined.ParticipantURL, Token: path.Base(joined.ParticipantURL)})
	}
	return out, nil
}

func Answer(ctx context.Context, api Caller, people []Participant, pace Pace) error {
	for _, p := range people {
		if pace != nil {
			if err := pace(ctx); err != nil {
				return err
			}
		}
		if err := api.Call(ctx, http.MethodPost, "/api/v1/p/"+p.Token+"/availability", map[string]any{"message": p.Message}, nil); err != nil {
			return fmt.Errorf("availability of %s: %w", p.Name, err)
		}
	}
	return nil
}

func Run(ctx context.Context, api Caller, sc Scenario, o Options, pace Pace) (Result, error) {
	res, err := Create(ctx, api, sc, o)
	if err != nil {
		return res, err
	}
	if res.Participants, err = Join(ctx, api, res, sc.People); err != nil {
		return res, err
	}
	return res, Answer(ctx, api, res.Participants, pace)
}
