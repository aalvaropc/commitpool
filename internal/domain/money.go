package domain

import (
	"fmt"
	"strconv"
	"strings"
)

type Money struct {
	Minor    int64
	Currency string
}

func ParseMoney(value, currency string) (Money, error) {
	cur, err := parseCurrency(currency)
	if err != nil {
		return Money{}, err
	}
	minor, err := parseMinor(value)
	if err != nil {
		return Money{}, err
	}
	return Money{Minor: minor, Currency: cur}, nil
}

func (m Money) String() string {
	return fmt.Sprintf("%d.%02d", m.Minor/100, m.Minor%100)
}

func parseCurrency(currency string) (string, error) {
	if len(currency) != 3 {
		return "", fmt.Errorf("invalid currency %q: want three ASCII letters", currency)
	}
	for i := 0; i < len(currency); i++ {
		c := currency[i]
		if (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') {
			return "", fmt.Errorf("invalid currency %q: want three ASCII letters", currency)
		}
	}
	return strings.ToUpper(currency), nil
}

func parseMinor(value string) (int64, error) {
	whole, frac, hasFraction := strings.Cut(value, ".")
	if !isDigits(whole) || (hasFraction && (len(frac) == 0 || len(frac) > 2 || !isDigits(frac))) {
		return 0, fmt.Errorf("invalid amount %q: want a non-negative number with at most two decimals", value)
	}
	minor, err := strconv.ParseInt(whole+(frac + "00")[:2], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid amount %q: out of range", value)
	}
	return minor, nil
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
