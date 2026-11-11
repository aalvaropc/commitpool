package main

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/aalvaropc/commitpool/internal/ai"
	"github.com/aalvaropc/commitpool/internal/config"
)

func TestNewInterpreter_TheFakeStaysUnlimited(t *testing.T) {
	got, err := newInterpreter(context.Background(), config.Config{AIProvider: config.AIProviderFake, AIHourlyCap: 1, AIDailyCap: 1}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.(ai.Fake); !ok {
		t.Errorf("interpreter = %T, want ai.Fake without a cap or a memo", got)
	}
}

func TestCheckConfig_RefusesWhatTheServerWouldRefuse(t *testing.T) {
	env := map[string]string{
		"COMMITPOOL_BASE_URL":    "https://commitpool.example",
		"DATABASE_URL":           "postgres://x",
		"PAYPAL_CLIENT_ID":       "a",
		"PAYPAL_CLIENT_SECRET":   "b",
		"COMMITPOOL_AI_PROVIDER": config.AIProviderFake,
		"COMMITPOOL_VIDEO_URL":   "https://youtu.be/aB3_x-9Zk0Q",
	}
	getenv := func(k string) string { return env[k] }
	var out strings.Builder
	if err := checkConfig(getenv, &out); err != nil || out.String() != "config: ok\n" {
		t.Fatalf("a good config: err = %v, out = %q", err, out.String())
	}
	env["COMMITPOOL_VIDEO_URL"] = "https://vimeo.com/1"
	out.Reset()
	if err := checkConfig(getenv, &out); err == nil || !strings.Contains(err.Error(), "COMMITPOOL_VIDEO_URL") || out.Len() != 0 {
		t.Errorf("a bad video link: err = %v, out = %q; want a refusal naming it and nothing printed", err, out.String())
	}
	env["COMMITPOOL_VIDEO_URL"] = "https://youtu.be/aB3_x-9Zk0Q"
	env["COMMITPOOL_DEVPOST_URL"] = "http://devpost.com/software/commitpool"
	out.Reset()
	if err := checkConfig(getenv, &out); err == nil || !strings.Contains(err.Error(), "COMMITPOOL_DEVPOST_URL") || out.Len() != 0 {
		t.Errorf("a bad Devpost link: err = %v, out = %q; want a refusal naming it and nothing printed", err, out.String())
	}
}
