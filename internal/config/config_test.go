package config_test

import (
	"fmt"
	"log/slog"
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
	if cfg.BedrockModel != "us.anthropic.claude-sonnet-4-5-20250929-v1:0" {
		t.Errorf("BedrockModel = %q, want Claude Sonnet 4.5 through the US inference profile", cfg.BedrockModel)
	}
	if cfg.BedrockFallbackModel != "" {
		t.Errorf("BedrockFallbackModel = %q, want no fallback by default", cfg.BedrockFallbackModel)
	}
	if cfg.BedrockEffort != "" {
		t.Errorf("BedrockEffort = %q, want none by default: Sonnet 4.5 takes no effort", cfg.BedrockEffort)
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

func TestLoad_BedrockEffort(t *testing.T) {
	for value, want := range map[string]string{"high": "high", "MAX": "max", "none": "", "low": "low", "medium": "medium", "xhigh": "xhigh"} {
		env := baseEnv()
		env["COMMITPOOL_BEDROCK_EFFORT"] = value
		cfg, err := load(env)
		if err != nil || cfg.BedrockEffort != want {
			t.Errorf("COMMITPOOL_BEDROCK_EFFORT=%s: effort %q, %v; want %q", value, cfg.BedrockEffort, err, want)
		}
	}
	env := baseEnv()
	env["COMMITPOOL_BEDROCK_EFFORT"] = "extreme"
	if _, err := load(env); err == nil || !strings.Contains(err.Error(), "COMMITPOOL_BEDROCK_EFFORT") {
		t.Errorf("unknown effort: err = %v, want an error naming COMMITPOOL_BEDROCK_EFFORT", err)
	}
}

func TestConfig_LogValueAndStringRedactSecrets(t *testing.T) {
	cases := []struct {
		name, databaseURL string
		secrets, visible  []string
	}{
		{"url with password in user info and query",
			"postgres://commitpool:db-password-1@db.internal:5432/commitpool?sslmode=disable&password=db-password-2",
			[]string{"db-password-1", "db-password-2"}, []string{"db.internal"}},
		{"plain url", "postgresql://user:db-password-5@db.plain/commitpool",
			[]string{"db-password-5"}, []string{"db.plain"}},
		{"keyword dsn", "host=db.internal user=commitpool password=db-password-4 dbname=commitpool",
			[]string{"db-password-4"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := load(map[string]string{
				"COMMITPOOL_BASE_URL":  "https://commitpool.example",
				"DATABASE_URL":         tc.databaseURL,
				"PAYPAL_CLIENT_ID":     "client-id-visible",
				"PAYPAL_CLIENT_SECRET": "paypal-secret-3",
				"AWS_REGION":           "us-east-1",
			})
			if err != nil {
				t.Fatal(err)
			}
			var logged strings.Builder
			slog.New(slog.NewJSONHandler(&logged, nil)).Info("starting", "config", cfg)
			renderings := map[string]string{
				"slog": logged.String(),
				"%v":   fmt.Sprintf("%v", cfg),
				"%+v":  fmt.Sprintf("%+v", cfg),
				"%#v":  fmt.Sprintf("%#v", cfg),
				//lint:ignore S1025 the test checks what every fmt verb prints
				"%s":    fmt.Sprintf("%s", cfg),
				"Print": fmt.Sprint(cfg),
			}
			for name, out := range renderings {
				for _, secret := range append([]string{"paypal-secret-3"}, tc.secrets...) {
					if strings.Contains(out, secret) {
						t.Errorf("%s rendering contains %q: %s", name, secret, out)
					}
				}
				for _, visible := range append([]string{"https://commitpool.example", "client-id-visible"}, tc.visible...) {
					if !strings.Contains(out, visible) {
						t.Errorf("%s rendering lacks %q: %s", name, visible, out)
					}
				}
			}
		})
	}
}

func TestLoad_RefusesFakePayPalWithRealCredentialsOrAPublicURL(t *testing.T) {
	env := baseEnv()
	env["E2E_FAKE_PAYPAL"] = "1"
	if _, err := load(env); err == nil || !strings.Contains(err.Error(), "E2E_FAKE_PAYPAL") {
		t.Errorf("fake PayPal next to real credentials: err = %v, want a refusal naming E2E_FAKE_PAYPAL", err)
	}

	delete(env, "PAYPAL_CLIENT_ID")
	delete(env, "PAYPAL_CLIENT_SECRET")
	env["COMMITPOOL_BASE_URL"] = "https://demo.example.com"
	if _, err := load(env); err == nil || !strings.Contains(err.Error(), "E2E_FAKE_PAYPAL") {
		t.Errorf("fake PayPal on a public URL: err = %v, want a refusal naming E2E_FAKE_PAYPAL", err)
	}

	env["COMMITPOOL_BASE_URL"] = "http://localhost:18080"
	if _, err := load(env); err != nil {
		t.Errorf("fake PayPal on localhost: %v", err)
	}
}

func TestLoad_RequiresAnAbsoluteHTTPBaseURL(t *testing.T) {
	for _, bad := range []string{"localhost:8080", "/relative", "ftp://example.com"} {
		env := baseEnv()
		env["COMMITPOOL_BASE_URL"] = bad
		if _, err := load(env); err == nil || !strings.Contains(err.Error(), "COMMITPOOL_BASE_URL") {
			t.Errorf("COMMITPOOL_BASE_URL=%q: err = %v, want a refusal", bad, err)
		}
	}
}

func TestLoad_RateLimit(t *testing.T) {
	for value, want := range map[string]int{"": 120, "60": 60, "off": 0, "0": 0} {
		env := baseEnv()
		if value != "" {
			env["COMMITPOOL_RATE_LIMIT"] = value
		}
		cfg, err := load(env)
		if err != nil || cfg.RateLimitPerMin != want {
			t.Errorf("COMMITPOOL_RATE_LIMIT=%q: %d, %v; want %d", value, cfg.RateLimitPerMin, err, want)
		}
	}
	env := baseEnv()
	env["COMMITPOOL_RATE_LIMIT"] = "lots"
	if _, err := load(env); err == nil || !strings.Contains(err.Error(), "COMMITPOOL_RATE_LIMIT") {
		t.Errorf("COMMITPOOL_RATE_LIMIT=lots: err = %v, want a refusal", err)
	}
}

func TestLoad_Mailer(t *testing.T) {
	env := baseEnv()
	if cfg, err := load(env); err != nil || cfg.Mailer != "log" {
		t.Errorf("default mailer = %q, %v; want log", cfg.Mailer, err)
	}
	env["COMMITPOOL_MAILER"] = "ses"
	if _, err := load(env); err == nil || !strings.Contains(err.Error(), "COMMITPOOL_MAIL_FROM") {
		t.Errorf("ses without a sender: err = %v, want a refusal naming COMMITPOOL_MAIL_FROM", err)
	}
	env["COMMITPOOL_MAIL_FROM"] = "CommitPool <hello@example.com>"
	if cfg, err := load(env); err != nil || cfg.Mailer != "ses" || cfg.MailFrom != "CommitPool <hello@example.com>" {
		t.Errorf("ses with a sender = %+v, %v", cfg.Mailer, err)
	}
	env["COMMITPOOL_MAIL_FROM"] = "CommitPool hello at example"
	if _, err := load(env); err == nil || !strings.Contains(err.Error(), "COMMITPOOL_MAIL_FROM must be an address") {
		t.Errorf("a malformed sender: err = %v", err)
	}
}

func TestLoad_DemoPace(t *testing.T) {
	cfg, err := load(baseEnv())
	if err != nil || cfg.DemoPace != 2*time.Second {
		t.Fatalf("default demo pace = %v, %v; want 2s", cfg.DemoPace, err)
	}
	env := baseEnv()
	env["COMMITPOOL_DEMO_PACE_MS"] = "0"
	if cfg, err := load(env); err != nil || cfg.DemoPace != 0 {
		t.Errorf("COMMITPOOL_DEMO_PACE_MS=0: pace %v, %v; want no pause", cfg.DemoPace, err)
	}
	env["COMMITPOOL_DEMO_PACE_MS"] = "-5"
	if _, err := load(env); err == nil || !strings.Contains(err.Error(), "COMMITPOOL_DEMO_PACE_MS") {
		t.Errorf("negative pace: err = %v, want an error naming COMMITPOOL_DEMO_PACE_MS", err)
	}
}

func TestLoad_PayPalIsExactlyTheSandboxUnlessLiveIsAllowed(t *testing.T) {
	cases := []struct {
		name, url    string
		live, mocks  bool
		refusalNames string
	}{
		{"the default", "", false, false, ""},
		{"the sandbox", "https://api-m.sandbox.paypal.com", false, true, ""},
		{"the sandbox with a trailing slash", "https://api-m.sandbox.paypal.com/", false, true, ""},
		{"a host that only starts like the sandbox", "https://api-m.sandbox.paypal.com.evil.test", false, false, "COMMITPOOL_ALLOW_LIVE_PAYPAL"},
		{"mocks on a host that only starts like the sandbox", "https://api-m.sandbox.paypal.com.evil.test", false, true, "COMMITPOOL_SANDBOX_MOCKS"},
		{"the sandbox over plain http", "http://api-m.sandbox.paypal.com", false, false, "COMMITPOOL_ALLOW_LIVE_PAYPAL"},
		{"plain http even when live is allowed", "http://api-m.paypal.com", true, false, "https"},
		{"live without the flag", "https://api-m.paypal.com", false, false, "COMMITPOOL_ALLOW_LIVE_PAYPAL"},
		{"live with the flag", "https://api-m.paypal.com", true, false, ""},
		{"live with the flag and mocks", "https://api-m.paypal.com", true, true, "COMMITPOOL_SANDBOX_MOCKS"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := baseEnv()
			if tc.url != "" {
				env["PAYPAL_BASE_URL"] = tc.url
			}
			if tc.live {
				env["COMMITPOOL_ALLOW_LIVE_PAYPAL"] = "1"
			}
			if tc.mocks {
				env["COMMITPOOL_SANDBOX_MOCKS"] = "1"
			}
			cfg, err := load(env)
			if tc.refusalNames != "" {
				if err == nil || !strings.Contains(err.Error(), "PAYPAL_BASE_URL") || !strings.Contains(err.Error(), tc.refusalNames) {
					t.Fatalf("PAYPAL_BASE_URL=%q: err = %v, want a refusal naming PAYPAL_BASE_URL and %s", tc.url, err, tc.refusalNames)
				}
				return
			}
			if err != nil {
				t.Fatalf("PAYPAL_BASE_URL=%q: %v", tc.url, err)
			}
			if cfg.AllowLivePayPal != tc.live {
				t.Errorf("AllowLivePayPal = %v, want %v", cfg.AllowLivePayPal, tc.live)
			}
			if want := strings.TrimSuffix(tc.url, "/"); want != "" && cfg.PayPalBaseURL != want {
				t.Errorf("PayPalBaseURL = %q, want %q", cfg.PayPalBaseURL, want)
			}
		})
	}
}

func TestLoad_AICaps(t *testing.T) {
	cfg, err := load(baseEnv())
	if err != nil || cfg.AIHourlyCap != 200 || cfg.AIDailyCap != 1500 {
		t.Fatalf("default caps = %d an hour, %d a day, %v; want 200 and 1500", cfg.AIHourlyCap, cfg.AIDailyCap, err)
	}
	env := baseEnv()
	env["COMMITPOOL_AI_HOURLY_CAP"] = "50"
	env["COMMITPOOL_AI_DAILY_CAP"] = "400"
	if cfg, err := load(env); err != nil || cfg.AIHourlyCap != 50 || cfg.AIDailyCap != 400 {
		t.Errorf("caps = %d, %d, %v; want 50 and 400", cfg.AIHourlyCap, cfg.AIDailyCap, err)
	}
	for _, name := range []string{"COMMITPOOL_AI_HOURLY_CAP", "COMMITPOOL_AI_DAILY_CAP"} {
		for _, bad := range []string{"0", "-3", "lots", "1.5"} {
			env := baseEnv()
			env[name] = bad
			if _, err := load(env); err == nil || !strings.Contains(err.Error(), name) {
				t.Errorf("%s=%s: err = %v, want a refusal naming it", name, bad, err)
			}
		}
	}
	env = baseEnv()
	env["COMMITPOOL_AI_HOURLY_CAP"] = "300"
	env["COMMITPOOL_AI_DAILY_CAP"] = "250"
	if _, err := load(env); err == nil || !strings.Contains(err.Error(), "COMMITPOOL_AI_HOURLY_CAP cannot be above COMMITPOOL_AI_DAILY_CAP") {
		t.Errorf("an hourly cap set above the daily one: err = %v", err)
	}
	env = baseEnv()
	env["COMMITPOOL_AI_HOURLY_CAP"] = "2000"
	if _, err := load(env); err == nil || !strings.Contains(err.Error(), "COMMITPOOL_AI_HOURLY_CAP") {
		t.Errorf("an hourly cap set above the default daily one: err = %v", err)
	}
	env = baseEnv()
	env["COMMITPOOL_AI_DAILY_CAP"] = "120"
	if cfg, err := load(env); err != nil || cfg.AIHourlyCap != 120 || cfg.AIDailyCap != 120 {
		t.Errorf("only a daily cap of 120 = %d an hour, %d a day, %v; want the hourly default clamped to 120", cfg.AIHourlyCap, cfg.AIDailyCap, err)
	}
	env["COMMITPOOL_AI_DAILY_CAP"] = "800"
	if cfg, err := load(env); err != nil || cfg.AIHourlyCap != 200 || cfg.AIDailyCap != 800 {
		t.Errorf("only a daily cap of 800 = %d an hour, %d a day, %v; want the hourly default", cfg.AIHourlyCap, cfg.AIDailyCap, err)
	}
}

func TestLoad_DemoAndMocksAreSandboxOnly(t *testing.T) {
	for _, flag := range []string{"COMMITPOOL_DEMO_MODE", "COMMITPOOL_SANDBOX_MOCKS"} {
		for _, url := range []string{"", "https://api-m.paypal.com"} {
			env := baseEnv()
			env["COMMITPOOL_ALLOW_LIVE_PAYPAL"] = "1"
			env[flag] = "1"
			if url != "" {
				env["PAYPAL_BASE_URL"] = url
			}
			if _, err := load(env); err == nil || !strings.Contains(err.Error(), flag) || !strings.Contains(err.Error(), "COMMITPOOL_ALLOW_LIVE_PAYPAL") {
				t.Errorf("%s=1 with live PayPal allowed and PAYPAL_BASE_URL=%q: err = %v, want a refusal naming both", flag, url, err)
			}
		}
	}
	env := baseEnv()
	env["COMMITPOOL_DEMO_MODE"] = "1"
	env["COMMITPOOL_SANDBOX_MOCKS"] = "1"
	if _, err := load(env); err != nil {
		t.Errorf("demo and mocks on the Sandbox: %v", err)
	}
}

func TestLoad_PayoutsAreOffUnlessSwitchedOn(t *testing.T) {
	cfg, err := load(baseEnv())
	if err != nil || cfg.Payouts || cfg.DemoPayoutEmail != "" {
		t.Fatalf("default payouts = %v, demo email %q, %v; want off and none", cfg.Payouts, cfg.DemoPayoutEmail, err)
	}
	env := baseEnv()
	env["COMMITPOOL_PAYOUTS"] = "1"
	env["COMMITPOOL_DEMO_PAYOUT_EMAIL"] = " receiver@example.com "
	cfg, err = load(env)
	if err != nil || !cfg.Payouts || cfg.DemoPayoutEmail != "receiver@example.com" {
		t.Fatalf("switched on = %v, %q, %v", cfg.Payouts, cfg.DemoPayoutEmail, err)
	}
	if got := cfg.String(); strings.Contains(got, "receiver@example.com") || !strings.Contains(got, "payouts=true") {
		t.Errorf("config log %q names the demo payout email or hides the switch", got)
	}
	for _, bad := range []string{"receiver", "Iris <receiver@example.com>", "a@example.com, b@example.com"} {
		env["COMMITPOOL_DEMO_PAYOUT_EMAIL"] = bad
		if _, err := load(env); err == nil || !strings.Contains(err.Error(), "COMMITPOOL_DEMO_PAYOUT_EMAIL") {
			t.Errorf("COMMITPOOL_DEMO_PAYOUT_EMAIL=%q: err = %v, want refused", bad, err)
		}
	}
	env["COMMITPOOL_DEMO_PAYOUT_EMAIL"] = ""
	env["COMMITPOOL_PAYOUTS"] = "yes"
	if _, err := load(env); err == nil || !strings.Contains(err.Error(), "COMMITPOOL_PAYOUTS") {
		t.Errorf("COMMITPOOL_PAYOUTS=yes: err = %v, want refused", err)
	}
}
