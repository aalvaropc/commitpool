package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	defaultPayPalBaseURL = "https://api-m.sandbox.paypal.com"
	defaultSeatTTLMin    = 30

	// DefaultBedrockModel is the Bedrock model that reads availability and
	// explains recommendations; DefaultBedrockFallbackModel answers when it
	// declines a request.
	DefaultBedrockModel         = "anthropic.claude-opus-5-5"
	DefaultBedrockFallbackModel = "anthropic.claude-opus-4-8"
	// defaultBedrockEffort keeps a reading and an explanation quick.
	defaultBedrockEffort = "low"

	AIProviderBedrock = "bedrock"
	AIProviderFake    = "fake"
)

type Config struct {
	Port                 string
	BaseURL              string
	DatabaseURL          string
	PayPalBaseURL        string
	PayPalClientID       string
	PayPalClientSecret   string
	PayPalWebhookID      string
	AWSRegion            string
	AIProvider           string
	BedrockModel         string
	BedrockFallbackModel string
	// BedrockEffort is the output_config.effort sent to Bedrock; "" sends
	// none and leaves the model's default.
	BedrockEffort string
	DemoMode      bool
	SandboxMocks  bool
	SeatTTL       time.Duration
	FakePayPal    bool
}

func Load(getenv func(string) string) (Config, error) {
	var problems []string
	str := func(name, def string) string {
		if v := strings.TrimSpace(getenv(name)); v != "" {
			return v
		}
		return def
	}
	flag := func(name string) bool {
		switch strings.ToLower(strings.TrimSpace(getenv(name))) {
		case "", "0", "false":
			return false
		case "1", "true":
			return true
		}
		problems = append(problems, name+" must be 1, true, 0 or false")
		return false
	}

	cfg := Config{
		Port:                 str("PORT", "8080"),
		BaseURL:              strings.TrimRight(str("COMMITPOOL_BASE_URL", ""), "/"),
		DatabaseURL:          str("DATABASE_URL", ""),
		PayPalBaseURL:        strings.TrimRight(str("PAYPAL_BASE_URL", defaultPayPalBaseURL), "/"),
		PayPalClientID:       str("PAYPAL_CLIENT_ID", ""),
		PayPalClientSecret:   str("PAYPAL_CLIENT_SECRET", ""),
		PayPalWebhookID:      str("PAYPAL_WEBHOOK_ID", ""),
		AWSRegion:            str("AWS_REGION", ""),
		AIProvider:           str("COMMITPOOL_AI_PROVIDER", AIProviderBedrock),
		BedrockModel:         str("COMMITPOOL_BEDROCK_MODEL", DefaultBedrockModel),
		BedrockFallbackModel: str("COMMITPOOL_BEDROCK_FALLBACK_MODEL", DefaultBedrockFallbackModel),
		DemoMode:             flag("COMMITPOOL_DEMO_MODE"),
		SandboxMocks:         flag("COMMITPOOL_SANDBOX_MOCKS"),
		FakePayPal:           flag("E2E_FAKE_PAYPAL"),
	}

	ttl := defaultSeatTTLMin
	if raw := strings.TrimSpace(getenv("COMMITPOOL_SEAT_TTL_MIN")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			problems = append(problems, "COMMITPOOL_SEAT_TTL_MIN must be a positive number of minutes")
		} else {
			ttl = n
		}
	}
	cfg.SeatTTL = time.Duration(ttl) * time.Minute
	// "none" turns the refusal fallback off.
	if strings.EqualFold(cfg.BedrockFallbackModel, "none") {
		cfg.BedrockFallbackModel = ""
	}
	switch effort := strings.ToLower(str("COMMITPOOL_BEDROCK_EFFORT", defaultBedrockEffort)); effort {
	case "none":
	case "low", "medium", "high", "xhigh", "max":
		cfg.BedrockEffort = effort
	default:
		problems = append(problems, "COMMITPOOL_BEDROCK_EFFORT must be low, medium, high, xhigh, max or none")
	}

	var missing []string
	require := func(name, value string) {
		if value == "" {
			missing = append(missing, name)
		}
	}
	require("COMMITPOOL_BASE_URL", cfg.BaseURL)
	require("DATABASE_URL", cfg.DatabaseURL)
	if !cfg.FakePayPal {
		require("PAYPAL_CLIENT_ID", cfg.PayPalClientID)
		require("PAYPAL_CLIENT_SECRET", cfg.PayPalClientSecret)
	}
	switch cfg.AIProvider {
	case AIProviderBedrock:
		require("AWS_REGION", cfg.AWSRegion)
	case AIProviderFake:
	default:
		problems = append(problems, "COMMITPOOL_AI_PROVIDER must be bedrock or fake")
	}
	if len(missing) > 0 {
		problems = append(problems, "missing required environment variables: "+strings.Join(missing, ", "))
	}

	if cfg.SandboxMocks && !strings.Contains(cfg.PayPalBaseURL, "sandbox") {
		problems = append(problems, "COMMITPOOL_SANDBOX_MOCKS=1 requires PAYPAL_BASE_URL to be a PayPal sandbox URL")
	}

	if len(problems) > 0 {
		return Config{}, fmt.Errorf("config: %s", strings.Join(problems, "; "))
	}
	return cfg, nil
}
