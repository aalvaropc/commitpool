//go:build !e2e

package main

import "errors"

func startFakePayPal() (string, func(), error) {
	return "", nil, errors.New("E2E_FAKE_PAYPAL needs a binary built with -tags e2e")
}
