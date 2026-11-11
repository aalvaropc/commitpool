package config

import (
	"fmt"
	"log/slog"
	netmail "net/mail"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	defaultPayPalBaseURL = "https://api-m.sandbox.paypal.com"
	defaultSeatTTLMin    = 30
	defaultDemoPaceMS    = 2000
	defaultAIHourlyCap   = 200
	defaultAIDailyCap    = 1500

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
	DemoPace             time.Duration
	SandboxMocks         bool
	SeatTTL              time.Duration
	FakePayPal           bool
	AllowLivePayPal      bool
	AIHourlyCap          int
	AIDailyCap           int
	VideoEmbedURL        string
	DevpostURL           string
	JudgeBuyerEmail      string
	JudgeBuyerPassword   string
	Payouts              bool
	DemoPayoutEmail      string
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
		AllowLivePayPal:      flag("COMMITPOOL_ALLOW_LIVE_PAYPAL"),
		Payouts:              flag("COMMITPOOL_PAYOUTS"),
		DemoPayoutEmail:      str("COMMITPOOL_DEMO_PAYOUT_EMAIL", ""),
	}
	if cfg.DemoPayoutEmail != "" {
		if a, err := netmail.ParseAddress(cfg.DemoPayoutEmail); err != nil || a.Name != "" || a.Address != cfg.DemoPayoutEmail {
			problems = append(problems, "COMMITPOOL_DEMO_PAYOUT_EMAIL must be one plain address, such as receiver@example.com")
		}
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
		} else if _, err := netmail.ParseAddress(cfg.MailFrom); err != nil {
			problems = append(problems, "COMMITPOOL_MAIL_FROM must be an address such as hello@example.com or CommitPool <hello@example.com>")
		}
	default:
		problems = append(problems, "COMMITPOOL_MAILER must be log or ses")
	}
	if secret := str("COMMITPOOL_MCP_SECRET", ""); secret != "" {
		cfg.MCPSecret = []byte(secret)
	}
	if raw := str("COMMITPOOL_VIDEO_URL", ""); raw != "" {
		if id, ok := youTubeID(raw); ok {
			cfg.VideoEmbedURL = youTubeEmbedPrefix + id
		} else {
			problems = append(problems, "COMMITPOOL_VIDEO_URL must be a YouTube video link, such as https://youtu.be/VIDEO_ID or https://www.youtube.com/watch?v=VIDEO_ID")
		}
	}
	if raw := str("COMMITPOOL_DEVPOST_URL", ""); raw != "" {
		if u, err := url.Parse(raw); err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil {
			cfg.DevpostURL = u.String()
		} else {
			problems = append(problems, "COMMITPOOL_DEVPOST_URL must be an https URL, such as https://devpost.com/software/commitpool")
		}
	}
	cfg.JudgeBuyerEmail = str("COMMITPOOL_JUDGE_BUYER_EMAIL", "")
	if password := getenv("COMMITPOOL_JUDGE_BUYER_PASSWORD"); strings.TrimSpace(password) != "" {
		cfg.JudgeBuyerPassword = password
	}
	switch {
	case cfg.JudgeBuyerEmail == "" && cfg.JudgeBuyerPassword == "":
	case cfg.JudgeBuyerEmail == "" || cfg.JudgeBuyerPassword == "":
		problems = append(problems, "COMMITPOOL_JUDGE_BUYER_EMAIL and COMMITPOOL_JUDGE_BUYER_PASSWORD go together: set both or neither")
	default:
		if a, err := netmail.ParseAddress(cfg.JudgeBuyerEmail); err != nil || a.Name != "" {
			problems = append(problems, "COMMITPOOL_JUDGE_BUYER_EMAIL must be a plain address such as buyer@example.com")
		}
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
	pace := defaultDemoPaceMS
	if raw := strings.TrimSpace(getenv("COMMITPOOL_DEMO_PACE_MS")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			problems = append(problems, "COMMITPOOL_DEMO_PACE_MS must be zero or a positive number of milliseconds")
		} else {
			pace = n
		}
	}
	cfg.DemoPace = time.Duration(pace) * time.Millisecond
	positive := func(name string, def int) int {
		raw := strings.TrimSpace(getenv(name))
		if raw == "" {
			return def
		}
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			problems = append(problems, name+" must be a positive number of model calls")
			return def
		}
		return n
	}
	cfg.AIHourlyCap = positive("COMMITPOOL_AI_HOURLY_CAP", defaultAIHourlyCap)
	cfg.AIDailyCap = positive("COMMITPOOL_AI_DAILY_CAP", defaultAIDailyCap)
	if cfg.AIHourlyCap > cfg.AIDailyCap {
		if strings.TrimSpace(getenv("COMMITPOOL_AI_HOURLY_CAP")) != "" {
			problems = append(problems, "COMMITPOOL_AI_HOURLY_CAP cannot be above COMMITPOOL_AI_DAILY_CAP")
		}
		cfg.AIHourlyCap = cfg.AIDailyCap
	}
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

	if cfg.AllowLivePayPal {
		if cfg.DemoMode {
			problems = append(problems, "COMMITPOOL_DEMO_MODE=1 runs only on the PayPal Sandbox; unset COMMITPOOL_ALLOW_LIVE_PAYPAL")
		}
		if cfg.SandboxMocks {
			problems = append(problems, "COMMITPOOL_SANDBOX_MOCKS=1 runs only on the PayPal Sandbox; unset COMMITPOOL_ALLOW_LIVE_PAYPAL")
		}
	}
	sandbox := cfg.PayPalBaseURL == defaultPayPalBaseURL
	switch {
	case sandbox:
	case cfg.SandboxMocks:
		problems = append(problems, "COMMITPOOL_SANDBOX_MOCKS=1 requires PAYPAL_BASE_URL to be "+defaultPayPalBaseURL)
	case !cfg.AllowLivePayPal:
		problems = append(problems, "PAYPAL_BASE_URL must be "+defaultPayPalBaseURL+"; set COMMITPOOL_ALLOW_LIVE_PAYPAL=1 to use another PayPal host")
	case !httpsURL(cfg.PayPalBaseURL):
		problems = append(problems, "PAYPAL_BASE_URL must be an https URL")
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
		slog.Bool("allow_live_paypal", c.AllowLivePayPal),
		slog.Int("ai_hourly_cap", c.AIHourlyCap),
		slog.Int("ai_daily_cap", c.AIDailyCap),
		slog.String("video_embed_url", c.VideoEmbedURL),
		slog.String("devpost_url", c.DevpostURL),
		slog.String("judge_buyer_email", c.JudgeBuyerEmail),
		slog.String("judge_buyer_password", redact(c.JudgeBuyerPassword)),
		slog.Bool("payouts", c.Payouts),
		slog.Bool("demo_payout_email_set", c.DemoPayoutEmail != ""),
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

func httpsURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil
}

const youTubeEmbedPrefix = "https://www.youtube-nocookie.com/embed/"

var youTubeVideoID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

func youTubeID(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil {
		return "", false
	}
	var id string
	switch host := strings.ToLower(u.Hostname()); host {
	case "youtu.be":
		id = strings.Trim(u.Path, "/")
	case "youtube.com", "www.youtube.com", "m.youtube.com", "youtube-nocookie.com", "www.youtube-nocookie.com":
		switch rest, _ := strings.CutPrefix(u.Path, "/"); {
		case rest == "watch":
			id = u.Query().Get("v")
		case strings.HasPrefix(rest, "embed/"), strings.HasPrefix(rest, "shorts/"), strings.HasPrefix(rest, "live/"):
			_, id, _ = strings.Cut(rest, "/")
			id = strings.TrimSuffix(id, "/")
		}
	}
	if !youTubeVideoID.MatchString(id) {
		return "", false
	}
	return id, true
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
