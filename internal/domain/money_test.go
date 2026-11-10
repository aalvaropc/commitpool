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
		{name: "studio covered by three seats", cost: usd(12000), price: usd(4500), want: 3},
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

func TestCaptureFees(t *testing.T) {
	usd := func(minor int64) *domain.Money { return &domain.Money{Minor: minor, Currency: "USD"} }
	eur := func(minor int64) *domain.Money { return &domain.Money{Minor: minor, Currency: "EUR"} }
	price := domain.Money{Minor: 4500, Currency: "USD"}
	tests := []struct {
		name                        string
		gross, fee, net, receivable *domain.Money
		wantFee, wantNet            int64
		wantOK                      bool
	}{
		{name: "fee and net add up to the price", gross: usd(4500), fee: usd(206), net: usd(4294), wantFee: 206, wantNet: 4294, wantOK: true},
		{name: "no fee and the whole price as net", gross: usd(4500), net: usd(4500), wantFee: 0, wantNet: 4500, wantOK: true},
		{name: "a zero fee", gross: usd(4500), fee: usd(0), net: usd(4500), wantFee: 0, wantNet: 4500, wantOK: true},
		{name: "a fee as large as the price", gross: usd(4500), fee: usd(4500), net: usd(0), wantFee: 4500, wantNet: 0, wantOK: true},
		{name: "no fee but a smaller net", gross: usd(4500), net: usd(4294)},
		{name: "net that does not add up", gross: usd(4500), fee: usd(206), net: usd(4295)},
		{name: "gross other than the price", gross: usd(4400), fee: usd(203), net: usd(4197)},
		{name: "gross in another currency", gross: eur(4500), fee: usd(206), net: usd(4294)},
		{name: "fee in another currency", gross: usd(4500), fee: eur(206), net: usd(4294)},
		{name: "net in another currency", gross: usd(4500), fee: usd(206), net: eur(4294)},
		{name: "a receivable amount after a conversion", gross: usd(4500), fee: usd(206), net: usd(4294), receivable: eur(3950)},
		{name: "a receivable amount in the same currency", gross: usd(4500), fee: usd(206), net: usd(4294), receivable: usd(4294)},
		{name: "no gross", fee: usd(206), net: usd(4294)},
		{name: "no net", gross: usd(4500), fee: usd(206)},
		{name: "a fee above the price", gross: usd(4500), fee: usd(4600), net: &domain.Money{Minor: -100, Currency: "USD"}},
		{name: "a negative fee", gross: usd(4500), fee: &domain.Money{Minor: -10, Currency: "USD"}, net: usd(4510)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fee, net, ok := domain.CaptureFees(price, tt.gross, tt.fee, tt.net, tt.receivable)
			if ok != tt.wantOK || fee != tt.wantFee || net != tt.wantNet {
				t.Fatalf("CaptureFees = %d, %d, %v; want %d, %d, %v", fee, net, ok, tt.wantFee, tt.wantNet, tt.wantOK)
			}
		})
	}
}
