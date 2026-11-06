//go:build e2e

package main

import "github.com/aalvaropc/commitpool/internal/paypal/paypaltest"

func startFakePayPal() (string, func(), error) {
	f := paypaltest.NewServer()
	return f.URL, f.Close, nil
}
