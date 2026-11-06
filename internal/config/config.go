package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultPayPalBaseURL = "https://api-m.sandbox.paypal.com"
	defaultSeatTTLMin    = 30

	DefaultBedrockModel         = "us.anthropic.claude-sonnet-4-5-20250929-v1:0"
	DefaultBedrockFallbackModel = ""
	defaultBedrockEffort        = "none"

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
	BedrockEffort        string
	MCPSecret            []byte
	Mailer, MailFrom     string
	RateLimitPerMin      int
	DemoMode             bool
	SandboxMocks         bool
	SeatTTL              time.Duration
	FakePayPal           bool
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
	switch raw := strings.ToLower(strings.TrimSpace(str("COMMITPOOL_RATE_LIMIT", "120"))); raw {
	case "off", "0":
	default:
		if n, err := strconv.Atoi(raw); err != nil || n < 1 {
			problems = append(problems, "COMMITPOOL_RATE_LIMIT must be a number of writes a minute, or off")
		} else {
			cfg.RateLimitPerMin = n
		}
	}
	cfg.Mailer = strings.ToLower(str("COMMITPOOL_MAILER", "log"))
	cfg.MailFrom = str("COMMITPOOL_MAIL_FROM", "")
	switch cfg.Mailer {
	case "log":
	case "ses":
		if cfg.MailFrom == "" {
			problems = append(problems, "COMMITPOOL_MAILER=ses needs COMMITPOOL_MAIL_FROM, a verified SES sender")
		}
	default:
		problems = append(problems, "COMMITPOOL_MAILER must be log or ses")
	}
	if secret := str("COMMITPOOL_MCP_SECRET", ""); secret != "" {
		cfg.MCPSecret = []byte(secret)
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

	if cfg.BaseURL != "" {
		if u, err := url.Parse(cfg.BaseURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			problems = append(problems, "COMMITPOOL_BASE_URL must be an absolute http(s) URL, such as https://commitpool.example")
		}
	}
	if cfg.FakePayPal {
		if cfg.PayPalClientID != "" || cfg.PayPalClientSecret != "" {
			problems = append(problems, "E2E_FAKE_PAYPAL=1 cannot run next to real PayPal credentials")
		}
		if !localURL(cfg.BaseURL) {
			problems = append(problems, "E2E_FAKE_PAYPAL=1 is only for a local COMMITPOOL_BASE_URL (localhost)")
		}
	}

	if cfg.SandboxMocks && !strings.Contains(cfg.PayPalBaseURL, "sandbox") {
		problems = append(problems, "COMMITPOOL_SANDBOX_MOCKS=1 requires PAYPAL_BASE_URL to be a PayPal sandbox URL")
	}

	if len(problems) > 0 {
		return Config{}, fmt.Errorf("config: %s", strings.Join(problems, "; "))
	}
	return cfg, nil
}

const redactedValue = "[REDACTED]"

func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("port", c.Port),
		slog.String("base_url", c.BaseURL),
		slog.String("database_url", redactURL(c.DatabaseURL)),
		slog.String("paypal_base_url", c.PayPalBaseURL),
		slog.String("paypal_client_id", c.PayPalClientID),
		slog.String("paypal_client_secret", redact(c.PayPalClientSecret)),
		slog.String("paypal_webhook_id", c.PayPalWebhookID),
		slog.String("aws_region", c.AWSRegion),
		slog.String("ai_provider", c.AIProvider),
		slog.String("bedrock_model", c.BedrockModel),
		slog.String("bedrock_fallback_model", c.BedrockFallbackModel),
		slog.String("bedrock_effort", c.BedrockEffort),
		slog.Bool("demo_mode", c.DemoMode),
		slog.Bool("sandbox_mocks", c.SandboxMocks),
		slog.Duration("seat_ttl", c.SeatTTL),
		slog.Bool("fake_paypal", c.FakePayPal),
	)
}

func (c Config) String() string { return c.LogValue().String() }

func (c Config) GoString() string { return c.String() }

func redact(secret string) string {
	if secret == "" {
		return ""
	}
	return redactedValue
}

func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return redact(raw)
	}
	if q := u.Query(); q.Has("password") {
		q.Set("password", redactedValue)
		u.RawQuery = q.Encode()
	}
	return u.Redacted()
}

func localURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	switch h := u.Hostname(); {
	case h == "localhost", h == "127.0.0.1", h == "::1", strings.HasSuffix(h, ".localhost"):
		return true
	}
	return false
}
