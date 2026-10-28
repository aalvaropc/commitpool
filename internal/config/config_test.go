package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/config"
)

func baseEnv() map[string]string {
	return map[string]string{
		"COMMITPOOL_BASE_URL":  "http://localhost:8080",
		"DATABASE_URL":         "postgres://x",
		"PAYPAL_CLIENT_ID":     "a",
		"PAYPAL_CLIENT_SECRET": "b",
		"AWS_REGION":           "us-east-1",
	}
}

func load(env map[string]string) (config.Config, error) {
	return config.Load(func(k string) string { return env[k] })
}

func TestLoad_DefaultsAndRequired(t *testing.T) {
	env := baseEnv()
	cfg, err := load(env)
	if err != nil {
		t.Fatalf("Load with the required variables: %v", err)
	}
	if cfg.PayPalBaseURL != "https://api-m.sandbox.paypal.com" {
		t.Errorf("PayPalBaseURL = %q", cfg.PayPalBaseURL)
	}
	if cfg.BedrockModel != "anthropic.claude-opus-5-5" {
		t.Errorf("BedrockModel = %q", cfg.BedrockModel)
	}
	if cfg.BedrockFallbackModel != "anthropic.claude-opus-4-8" {
		t.Errorf("BedrockFallbackModel = %q", cfg.BedrockFallbackModel)
	}
	if cfg.SeatTTL != 30*time.Minute {
		t.Errorf("SeatTTL = %v", cfg.SeatTTL)
	}
	if cfg.Port != "8080" {
		t.Errorf("Port = %q", cfg.Port)
	}
	if cfg.AIProvider != "bedrock" {
		t.Errorf("AIProvider = %q", cfg.AIProvider)
	}
	if cfg.DemoMode || cfg.SandboxMocks || cfg.FakePayPal {
		t.Errorf("flags must default to false: demo=%v mocks=%v fake=%v", cfg.DemoMode, cfg.SandboxMocks, cfg.FakePayPal)
	}

	delete(env, "DATABASE_URL")
	if _, err := load(env); err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("missing DATABASE_URL: err = %v, want an error naming DATABASE_URL", err)
	}

	env["DATABASE_URL"] = "postgres://x"
	env["PAYPAL_BASE_URL"] = "https://api-m.paypal.com"
	env["COMMITPOOL_SANDBOX_MOCKS"] = "1"
	if _, err := load(env); err == nil || !strings.Contains(err.Error(), "COMMITPOOL_SANDBOX_MOCKS") {
		t.Fatalf("mocks outside sandbox: err = %v, want an error naming COMMITPOOL_SANDBOX_MOCKS", err)
	}

	env["PAYPAL_BASE_URL"] = "https://api-m.sandbox.paypal.com"
	cfg, err = load(env)
	if err != nil {
		t.Fatalf("mocks against the sandbox: %v", err)
	}
	if !cfg.SandboxMocks {
		t.Error("SandboxMocks = false, want true")
	}
}

func TestLoad_RequiredVariables(t *testing.T) {
	for _, name := range []string{"COMMITPOOL_BASE_URL", "DATABASE_URL", "PAYPAL_CLIENT_ID", "PAYPAL_CLIENT_SECRET", "AWS_REGION"} {
		env := baseEnv()
		delete(env, name)
		if _, err := load(env); err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("missing %s: err = %v, want an error naming it", name, err)
		}
	}
}

func TestLoad_ConditionalRequirements(t *testing.T) {
	env := baseEnv()
	delete(env, "PAYPAL_CLIENT_ID")
	delete(env, "PAYPAL_CLIENT_SECRET")
	env["E2E_FAKE_PAYPAL"] = "1"
	cfg, err := load(env)
	if err != nil {
		t.Fatalf("E2E_FAKE_PAYPAL=1 must not require PayPal credentials: %v", err)
	}
	if !cfg.FakePayPal {
		t.Error("FakePayPal = false, want true")
	}

	env = baseEnv()
	delete(env, "AWS_REGION")
	env["COMMITPOOL_AI_PROVIDER"] = "fake"
	cfg, err = load(env)
	if err != nil {
		t.Fatalf("COMMITPOOL_AI_PROVIDER=fake must not require AWS_REGION: %v", err)
	}
	if cfg.AIProvider != "fake" {
		t.Errorf("AIProvider = %q, want fake", cfg.AIProvider)
	}

	env = baseEnv()
	env["COMMITPOOL_AI_PROVIDER"] = "openai"
	if _, err := load(env); err == nil || !strings.Contains(err.Error(), "COMMITPOOL_AI_PROVIDER") {
		t.Errorf("unknown provider: err = %v, want an error naming COMMITPOOL_AI_PROVIDER", err)
	}
}

func TestLoad_BooleansAndOverrides(t *testing.T) {
	for value, want := range map[string]bool{"1": true, "true": true, "0": false, "false": false, "": false} {
		env := baseEnv()
		env["COMMITPOOL_DEMO_MODE"] = value
		cfg, err := load(env)
		if err != nil {
			t.Fatalf("COMMITPOOL_DEMO_MODE=%q: %v", value, err)
		}
		if cfg.DemoMode != want {
			t.Errorf("COMMITPOOL_DEMO_MODE=%q: DemoMode = %v, want %v", value, cfg.DemoMode, want)
		}
	}

	env := baseEnv()
	env["COMMITPOOL_DEMO_MODE"] = "maybe"
	if _, err := load(env); err == nil || !strings.Contains(err.Error(), "COMMITPOOL_DEMO_MODE") {
		t.Errorf("invalid boolean: err = %v, want an error naming COMMITPOOL_DEMO_MODE", err)
	}

	env = baseEnv()
	env["PORT"] = "9090"
	env["COMMITPOOL_BASE_URL"] = "https://example.test/"
	env["COMMITPOOL_SEAT_TTL_MIN"] = "5"
	cfg, err := load(env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "9090" || cfg.BaseURL != "https://example.test" || cfg.SeatTTL != 5*time.Minute {
		t.Errorf("overrides not applied: port=%q base=%q ttl=%v", cfg.Port, cfg.BaseURL, cfg.SeatTTL)
	}

	env = baseEnv()
	env["COMMITPOOL_SEAT_TTL_MIN"] = "0"
	if _, err := load(env); err == nil || !strings.Contains(err.Error(), "COMMITPOOL_SEAT_TTL_MIN") {
		t.Errorf("non-positive TTL: err = %v, want an error naming COMMITPOOL_SEAT_TTL_MIN", err)
	}
}

func TestLoad_BedrockModelOverrides(t *testing.T) {
	env := baseEnv()
	env["COMMITPOOL_BEDROCK_MODEL"] = "anthropic.claude-sonnet-5-5"
	env["COMMITPOOL_BEDROCK_FALLBACK_MODEL"] = "anthropic.claude-opus-5"
	cfg, err := load(env)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.BedrockModel != "anthropic.claude-sonnet-5-5" || cfg.BedrockFallbackModel != "anthropic.claude-opus-5" {
		t.Errorf("models = %q, %q; want the overrides", cfg.BedrockModel, cfg.BedrockFallbackModel)
	}

	env["COMMITPOOL_BEDROCK_FALLBACK_MODEL"] = "none"
	if cfg, err = load(env); err != nil || cfg.BedrockFallbackModel != "" {
		t.Errorf("fallback model none = %q, %v; want no fallback", cfg.BedrockFallbackModel, err)
	}
}
