package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"testing"
	"testing/fstest"
	"time"
	_ "time/tzdata"

	"github.com/aalvaropc/commitpool/internal/ai"
	"github.com/aalvaropc/commitpool/internal/clock"
	"github.com/aalvaropc/commitpool/internal/config"
	"github.com/aalvaropc/commitpool/internal/demo"
	"github.com/aalvaropc/commitpool/internal/httpapi"
	"github.com/aalvaropc/commitpool/internal/paypal"
	"github.com/aalvaropc/commitpool/internal/paypal/paypaltest"
	"github.com/aalvaropc/commitpool/internal/store/storetest"
	"github.com/aalvaropc/commitpool/internal/worker"
)

var t0 = time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)

type ownerView struct {
	Workshop struct {
		Status   string `json:"status"`
		Minimum  int    `json:"minimum"`
		Capacity int    `json:"capacity"`
		Price    struct {
			Value string `json:"value"`
		} `json:"price"`
		PaymentWindowMin int `json:"payment_window_min"`
	} `json:"workshop"`
	Dates []struct {
		ID    string `json:"id"`
		Label string `json:"label"`
	} `json:"dates"`
	Proposal *struct {
		DateID string `json:"date_id"`
	} `json:"proposal"`
	Participants   []json.RawMessage `json:"participants"`
	Availabilities []struct {
		Message            string `json:"message"`
		NeedsClarification bool   `json:"needs_clarification"`
	} `json:"availabilities"`
	Recommendation struct {
		DateID       string `json:"date_id"`
		MeetsMinimum bool   `json:"meets_minimum"`
		PerDate      []struct {
			DateID     string   `json:"date_id"`
			Compatible []string `json:"compatible"`
		} `json:"per_date"`
	} `json:"recommendation"`
}

func newApp(t *testing.T) *httptest.Server {
	t.Helper()
	st := storetest.New(t)
	fake := paypaltest.NewServer()
	t.Cleanup(fake.Close)
	srv := httptest.NewUnstartedServer(nil)
	t.Cleanup(srv.Close)
	cfg := config.Config{
		BaseURL:            "http://" + srv.Listener.Addr().String(),
		PayPalBaseURL:      fake.URL,
		PayPalClientID:     "fake-client",
		PayPalClientSecret: "fake-secret",
		AIProvider:         config.AIProviderFake,
		SeatTTL:            30 * time.Minute,
		FakePayPal:         true,
	}
	pp, err := paypal.New(paypal.Config{BaseURL: fake.URL, ClientID: cfg.PayPalClientID, ClientSecret: cfg.PayPalClientSecret})
	if err != nil {
		t.Fatalf("paypal.New: %v", err)
	}
	clk := &clock.Fake{}
	clk.Set(t0)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	runner := worker.New(worker.Deps{Store: st, PayPal: pp, Clock: clk, Cfg: cfg, Log: log})
	srv.Config.Handler = httpapi.New(httpapi.Deps{
		Store: st, Worker: runner, PayPal: pp, AI: ai.Fake{}, Clock: clk, Cfg: cfg, Log: log,
		Dist: fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>CommitPool</title>")}},
	})
	srv.Start()
	return srv
}

func seed(t *testing.T, srv *httptest.Server, open bool) (result, string) {
	t.Helper()
	var out bytes.Buffer
	res, err := run(context.Background(), srv.Client(), options{
		BaseURL:      srv.URL,
		Timezone:     "America/New_York",
		WindowMin:    1440,
		OpenPayments: open,
		Now:          t0,
	}, &out)
	if err != nil {
		t.Fatalf("run: %v\noutput:\n%s", err, out.String())
	}
	return res, out.String()
}

func ownerViewOf(t *testing.T, srv *httptest.Server, res result) ownerView {
	t.Helper()
	resp, err := srv.Client().Get(srv.URL + "/api/v1/owner/" + path.Base(res.OwnerURL))
	if err != nil {
		t.Fatalf("GET owner view: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET owner view: HTTP %d", resp.StatusCode)
	}
	var view ownerView
	if err := json.NewDecoder(resp.Body).Decode(&view); err != nil {
		t.Fatalf("decode owner view: %v", err)
	}
	return view
}

func TestRun_SeedsTheDemoWorkshopWithItsCast(t *testing.T) {
	srv := newApp(t)
	res, out := seed(t, srv, false)
	view := ownerViewOf(t, srv, res)

	if got := view.Workshop; got.Status != "COLLECTING" || got.Minimum != 3 || got.Capacity != 6 || got.Price.Value != "45.00" || got.PaymentWindowMin != 1440 {
		t.Fatalf("workshop = %+v, want COLLECTING, 3 needed of 6 at 45.00 with a 1440 min window", got)
	}
	if view.Proposal != nil {
		t.Fatalf("payments were opened without -open-payments")
	}
	wantLabels := []string{"Sat, Nov 7, 10 AM–12 PM (New York time)", "Sun, Nov 8, 10 AM–12 PM (New York time)"}
	if len(view.Dates) != 2 || view.Dates[0].Label != wantLabels[0] || view.Dates[1].Label != wantLabels[1] {
		t.Fatalf("dates = %+v, want %q", view.Dates, wantLabels)
	}

	if len(res.Participants) != len(demo.Maya.People) || len(view.Participants) != len(demo.Maya.People) || len(view.Availabilities) != len(demo.Maya.People) {
		t.Fatalf("seeded %d participants, %d joined, %d availabilities; want %d of each", len(res.Participants), len(view.Participants), len(view.Availabilities), len(demo.Maya.People))
	}
	for i, p := range res.Participants {
		if p.Name != demo.Maya.People[i].Name || p.Message != demo.Maya.People[i].Message || !strings.HasPrefix(p.URL, srv.URL+"/p/") {
			t.Errorf("participant %d = %+v, want %s with %q and a participant link", i, p, demo.Maya.People[i].Name, demo.Maya.People[i].Message)
		}
	}
	unclear := 0
	for _, a := range view.Availabilities {
		if a.NeedsClarification {
			unclear++
			if a.Message != demo.Maya.People[len(demo.Maya.People)-1].Message {
				t.Errorf("needs clarification: %q, want only the last cast message", a.Message)
			}
		}
	}
	if unclear != 1 {
		t.Errorf("%d availabilities need clarification, want exactly 1", unclear)
	}

	rec := view.Recommendation
	if rec.DateID != view.Dates[0].ID || !rec.MeetsMinimum {
		t.Fatalf("recommendation = %+v, want the Saturday meeting the minimum", rec)
	}
	compatible := map[string]int{}
	for _, d := range rec.PerDate {
		compatible[d.DateID] = len(d.Compatible)
	}
	if compatible[view.Dates[0].ID] != 4 || compatible[view.Dates[1].ID] != 3 {
		t.Errorf("compatible = %v, want 4 on Saturday and 3 on Sunday", compatible)
	}
	if res.Recommended != wantLabels[0] || res.CanCome != 4 {
		t.Errorf("result recommended %q for %d, want %q for 4", res.Recommended, res.CanCome, wantLabels[0])
	}

	for _, want := range []string{res.OwnerURL, res.ShareURL, wantLabels[0], "4 can come"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	for _, p := range res.Participants {
		if !strings.Contains(out, p.Name) || !strings.Contains(out, p.Message) || !strings.Contains(out, p.URL) {
			t.Errorf("output lacks %s, their message or their link:\n%s", p.Name, out)
		}
	}
}

func TestRun_OpenPaymentsFixesTheRecommendedDate(t *testing.T) {
	srv := newApp(t)
	res, out := seed(t, srv, true)
	view := ownerViewOf(t, srv, res)

	if view.Workshop.Status != "FUNDING" || view.Proposal == nil || view.Proposal.DateID != view.Recommendation.DateID {
		t.Fatalf("status %s, proposal %+v; want FUNDING on the recommended date %s", view.Workshop.Status, view.Proposal, view.Recommendation.DateID)
	}
	if !res.PaymentsOpened || !strings.Contains(out, "Reservations are open") {
		t.Errorf("result/output do not say payments opened: %+v\n%s", res, out)
	}
}
