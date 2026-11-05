//go:build !e2e

package main

import (
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/aalvaropc/commitpool/internal/config"
)

func TestNewPayPal_FakeNeedsAnE2EBuild(t *testing.T) {
	cfg := config.Config{FakePayPal: true, PayPalBaseURL: "http://127.0.0.1:1/sandbox"}
	_, _, err := newPayPal(&cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil || !strings.Contains(err.Error(), "-tags e2e") {
		t.Fatalf("newPayPal with E2E_FAKE_PAYPAL in a default build = %v, want a refusal naming -tags e2e", err)
	}
}
