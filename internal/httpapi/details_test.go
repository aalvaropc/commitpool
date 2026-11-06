package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

func withDetails(body map[string]any) {
	body["host_name"] = "  Maya Okafor "
	body["location"] = "Maya's studio · Greenpoint, Brooklyn"
	body["venue_cost"] = "120.00"
	body["price"] = "45.00"
}

func TestWorkshops_CreateKeepsHostPlaceAndStudioCost(t *testing.T) {
	e := newEnv(t)
	h := e.createWorkshop(withDetails)

	for name, w := range map[string]workshopJSON{"created": h.created.Workshop, "owner view": e.owner(h).Workshop} {
		if w.HostName != "Maya Okafor" || w.Location != "Maya's studio · Greenpoint, Brooklyn" {
			t.Errorf("%s host and place = %q, %q", name, w.HostName, w.Location)
		}
		if w.VenueCost == nil || *w.VenueCost != (moneyJSON{Minor: 12000, Currency: "USD", Value: "120.00"}) {
			t.Errorf("%s studio cost = %+v, want 120.00 USD", name, w.VenueCost)
		}
		if w.BreakEvenSeats != 3 {
			t.Errorf("%s break-even seats = %d, want 3", name, w.BreakEvenSeats)
		}
	}

	var public struct {
		Workshop workshopJSON `json:"workshop"`
	}
	e.call(http.MethodGet, h.sharePath, nil, http.StatusOK, &public)
	if public.Workshop.HostName != "Maya Okafor" || public.Workshop.Location != "Maya's studio · Greenpoint, Brooklyn" {
		t.Errorf("public host and place = %q, %q", public.Workshop.HostName, public.Workshop.Location)
	}
}

func TestWorkshops_StudioCostStaysWithTheOrganizer(t *testing.T) {
	e := newEnv(t)
	h := e.createWorkshop(withDetails)
	ana := e.join(h, "Ana")

	for name, path := range map[string]string{"public view": h.sharePath, "participant view": ana.path} {
		rec := e.request(http.MethodGet, path, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d %s", name, rec.Code, rec.Body.String())
		}
		for _, key := range []string{`"venue_cost"`, `"break_even_seats"`, "120.00"} {
			if strings.Contains(rec.Body.String(), key) {
				t.Errorf("%s contains %s:\n%s", name, key, rec.Body.String())
			}
		}
	}
}

func TestWorkshops_DetailsAreOptional(t *testing.T) {
	e := newEnv(t)
	h := e.createWorkshop()
	w := e.owner(h).Workshop
	if w.HostName != "" || w.Location != "" || w.VenueCost != nil || w.BreakEvenSeats != 0 {
		t.Errorf("workshop without details = host %q, place %q, cost %+v, break-even %d", w.HostName, w.Location, w.VenueCost, w.BreakEvenSeats)
	}
}

func TestMCP_WorkshopCarriesHostPlaceAndStudioCost(t *testing.T) {
	e := newEnv(t)
	cs := e.mcpSession()
	body := workshopRequest()
	var starts []string
	for _, d := range body["dates"].([]map[string]string) {
		starts = append(starts, d["starts_at"])
	}
	var created mcpCreated
	e.tool(cs, "create_workshop", map[string]any{
		"title": "Wheel-throwing for beginners", "duration_min": body["duration_min"], "timezone": body["timezone"],
		"minimum": 3, "capacity": 6, "price": "45.00", "currency": "USD", "date_starts": starts,
		"host_name": "Maya Okafor", "location": "Maya's studio · Greenpoint, Brooklyn", "venue_cost": "120.00",
	}, &created)

	var workshop struct {
		HostName string `json:"host_name"`
		Location string `json:"location"`
	}
	e.tool(cs, "get_workshop", map[string]any{"share_link": created.ShareLink}, &workshop)
	if workshop.HostName != "Maya Okafor" || workshop.Location != "Maya's studio · Greenpoint, Brooklyn" {
		t.Errorf("get_workshop host and place = %q, %q", workshop.HostName, workshop.Location)
	}

	var status struct {
		HostName       string `json:"host_name"`
		Location       string `json:"location"`
		VenueCost      string `json:"venue_cost"`
		BreakEvenSeats int    `json:"break_even_seats"`
	}
	e.tool(cs, "get_status", map[string]any{"owner_link": created.OwnerLink}, &status)
	if status.HostName != "Maya Okafor" || status.VenueCost != "120.00 USD" || status.BreakEvenSeats != 3 {
		t.Errorf("get_status = %+v, want Maya, 120.00 USD and 3 seats to break even", status)
	}
}

type demoLabelJSON struct {
	Workshop struct {
		Demo bool `json:"demo"`
	} `json:"workshop"`
}

func asDemo(body map[string]any) { body["demo"] = true }

func TestWorkshops_DemoServerLabelsPreparedWorkshops(t *testing.T) {
	e := newEnv(t, withDemoMode())
	h := e.createWorkshop(asDemo)
	var v demoLabelJSON
	e.call(http.MethodGet, h.sharePath, nil, http.StatusOK, &v)
	if !v.Workshop.Demo {
		t.Error("a demo server did not label the prepared workshop as a demo")
	}
}

func TestWorkshops_DemoLabelRefusedOutsideDemoMode(t *testing.T) {
	e := newEnv(t)
	h := e.createWorkshop(asDemo)
	var v demoLabelJSON
	e.call(http.MethodGet, h.sharePath, nil, http.StatusOK, &v)
	if v.Workshop.Demo {
		t.Error("a server outside demo mode accepted the demo label")
	}
}
