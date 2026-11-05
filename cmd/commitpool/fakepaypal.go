//go:build !e2e

package main

import "errors"

// startFakePayPal refuses: the in-process PayPal sandbox is test code and
// ships only in a binary built with -tags e2e.
func startFakePayPal() (string, func(), error) {
	return "", nil, errors.New("E2E_FAKE_PAYPAL needs a binary built with -tags e2e")
}
