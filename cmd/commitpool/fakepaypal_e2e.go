//go:build e2e

package main

import "github.com/aalvaropc/commitpool/internal/paypal/paypaltest"

// startFakePayPal starts the in-process PayPal sandbox of an end-to-end build
// and returns its URL and the func that stops it.
func startFakePayPal() (string, func(), error) {
	f := paypaltest.NewServer()
	return f.URL, f.Close, nil
}
