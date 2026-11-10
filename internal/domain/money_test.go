package domain_test

import (
	"testing"

	"github.com/aalvaropc/commitpool/internal/domain"
)

func TestMoney_ParseAndString(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		currency  string
		wantMinor int64
		wantCur   string
		wantStr   string
		wantErr   bool
	}{
		{name: "two decimals", value: "10.00", currency: "USD", wantMinor: 1000, wantCur: "USD", wantStr: "10.00"},
		{name: "one decimal", value: "10.5", currency: "USD", wantMinor: 1050, wantCur: "USD", wantStr: "10.50"},
		{name: "no decimals", value: "10", currency: "USD", wantMinor: 1000, wantCur: "USD", wantStr: "10.00"},
		{name: "cents only", value: "0.05", currency: "USD", wantMinor: 5, wantCur: "USD", wantStr: "0.05"},
		{name: "lower case currency is upper-cased", value: "10.00", currency: "usd", wantMinor: 1000, wantCur: "USD", wantStr: "10.00"},
		{name: "three decimals", value: "10.123", currency: "USD", wantErr: true},
		{name: "negative", value: "-1", currency: "USD", wantErr: true},
		{name: "explicit plus sign", value: "+1", currency: "USD", wantErr: true},
		{name: "not a number", value: "abc", currency: "USD", wantErr: true},
		{name: "empty", value: "", currency: "USD", wantErr: true},
		{name: "trailing dot", value: "10.", currency: "USD", wantErr: true},
		{name: "leading dot", value: ".5", currency: "USD", wantErr: true},
		{name: "thousands separator", value: "1,000", currency: "USD", wantErr: true},
		{name: "two dots", value: "1.2.3", currency: "USD", wantErr: true},
		{name: "overflows int64", value: "99999999999999999999", currency: "USD", wantErr: true},
		{name: "currency too short", value: "10.00", currency: "us", wantErr: true},
		{name: "currency too long", value: "10.00", currency: "USDX", wantErr: true},
		{name: "currency with digit", value: "10.00", currency: "U5D", wantErr: true},
		{name: "currency empty", value: "10.00", currency: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := domain.ParseMoney(tt.value, tt.currency)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseMoney(%q, %q) = %+v, want error", tt.value, tt.currency, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseMoney(%q, %q) error: %v", tt.value, tt.currency, err)
			}
			if got.Minor != tt.wantMinor || got.Currency != tt.wantCur {
				t.Fatalf("ParseMoney(%q, %q) = %+v, want {%d %s}", tt.value, tt.currency, got, tt.wantMinor, tt.wantCur)
			}
			if s := got.String(); s != tt.wantStr {
				t.Fatalf("String() = %q, want %q", s, tt.wantStr)
			}
		})
	}
}

func FuzzParseMoney(f *testing.F) {
	for _, seed := range []string{"10.00", "0.01", "1", "-1.00", "10.001", "1e3", " 5.00", "99999999999999999999.99", ""} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		m, err := domain.ParseMoney(value, "usd")
		if err != nil {
			return
		}
		if m.Minor < 0 || m.Currency != "USD" {
			t.Fatalf("ParseMoney(%q) = %+v, want a non-negative USD amount", value, m)
		}
		again, err := domain.ParseMoney(m.String(), "USD")
		if err != nil || again != m {
			t.Fatalf("ParseMoney(%q) = %+v, but %q reads back as %+v, %v", value, m, m.String(), again, err)
		}
	})
}

func TestBreakEvenSeats(t *testing.T) {
	usd := func(minor int64) domain.Money { return domain.Money{Minor: minor, Currency: "USD"} }
	tests := []struct {
		name  string
		cost  domain.Money
		price domain.Money
		want  int
	}{
		{name: "session costs covered by three seats", cost: usd(12000), price: usd(4500), want: 3},
		{name: "exact multiple", cost: usd(9000), price: usd(4500), want: 2},
		{name: "one cent over needs another seat", cost: usd(9001), price: usd(4500), want: 3},
		{name: "free venue", cost: usd(0), price: usd(4500), want: 0},
		{name: "venue cheaper than a seat", cost: usd(100), price: usd(4500), want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := domain.BreakEvenSeats(tt.cost, tt.price)
			if err != nil || got != tt.want {
				t.Fatalf("BreakEvenSeats(%v, %v) = %d, %v; want %d", tt.cost, tt.price, got, err, tt.want)
			}
		})
	}
}

func TestBreakEvenSeats_RefusesMismatchedOrFreePrices(t *testing.T) {
	if _, err := domain.BreakEvenSeats(domain.Money{Minor: 100, Currency: "EUR"}, domain.Money{Minor: 100, Currency: "USD"}); err == nil {
		t.Error("different currencies: want an error")
	}
	if _, err := domain.BreakEvenSeats(domain.Money{Minor: 100, Currency: "USD"}, domain.Money{Minor: 0, Currency: "USD"}); err == nil {
		t.Error("free seats: want an error")
	}
	if _, err := domain.BreakEvenSeats(domain.Money{Minor: -1, Currency: "USD"}, domain.Money{Minor: 100, Currency: "USD"}); err == nil {
		t.Error("negative cost: want an error")
	}
}
