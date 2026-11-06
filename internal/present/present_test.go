package present_test

import (
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/domain"
	"github.com/aalvaropc/commitpool/internal/present"
)

func zone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestZoneName(t *testing.T) {
	cases := map[string]string{
		"America/New_York":               "New York time",
		"America/Argentina/Buenos_Aires": "Buenos Aires time",
		"Europe/London":                  "London time",
		"UTC":                            "UTC",
		"Etc/UTC":                        "UTC",
		"Etc/GMT+5":                      "UTC−5",
		"Etc/GMT-14":                     "UTC+14",
		"Etc/GMT":                        "UTC",
	}
	for name, want := range cases {
		if got := present.ZoneName(zone(t, name)); got != want {
			t.Errorf("ZoneName(%s) = %q, want %q", name, got, want)
		}
	}
}

func TestClock(t *testing.T) {
	loc := zone(t, "America/New_York")
	cases := []struct {
		hour, minute int
		want         string
	}{
		{10, 0, "10 AM"},
		{10, 30, "10:30 AM"},
		{12, 0, "12 PM"},
		{0, 0, "12 AM"},
		{18, 5, "6:05 PM"},
	}
	for _, c := range cases {
		at := time.Date(2026, 11, 14, c.hour, c.minute, 0, 0, loc)
		if got := present.Clock(at); got != c.want {
			t.Errorf("Clock(%02d:%02d) = %q, want %q", c.hour, c.minute, got, c.want)
		}
	}
}

func TestDateLabel(t *testing.T) {
	loc := zone(t, "America/New_York")
	start := time.Date(2026, 11, 14, 10, 0, 0, 0, loc)
	if got, want := present.DateLabel(start.UTC(), start.Add(2*time.Hour).UTC(), loc), "Sat, Nov 14, 10 AM–12 PM (New York time)"; got != want {
		t.Errorf("same day = %q, want %q", got, want)
	}
	late := time.Date(2026, 11, 14, 22, 0, 0, 0, loc)
	if got, want := present.DateLabel(late, late.Add(3*time.Hour), loc), "Sat, Nov 14, 10 PM – Sun, Nov 15, 1 AM (New York time)"; got != want {
		t.Errorf("overnight = %q, want %q", got, want)
	}
}

func TestDayAndMoment(t *testing.T) {
	loc := zone(t, "America/New_York")
	at := time.Date(2026, 11, 13, 18, 0, 0, 0, loc)
	if got := present.Day(at.UTC(), loc); got != "Fri, Nov 13" {
		t.Errorf("Day = %q", got)
	}
	if got := present.Moment(at.UTC(), loc); got != "Fri, Nov 13 at 6 PM (New York time)" {
		t.Errorf("Moment = %q", got)
	}
}

func TestPrice(t *testing.T) {
	cases := []struct {
		m    domain.Money
		want string
	}{
		{domain.Money{Minor: 4500, Currency: "USD"}, "$45"},
		{domain.Money{Minor: 4550, Currency: "USD"}, "$45.50"},
		{domain.Money{Minor: 1234567, Currency: "USD"}, "$12,345.67"},
		{domain.Money{Minor: 4500, Currency: "EUR"}, "45.00 EUR"},
	}
	for _, c := range cases {
		if got := present.Price(c.m); got != c.want {
			t.Errorf("Price(%+v) = %q, want %q", c.m, got, c.want)
		}
	}
}
