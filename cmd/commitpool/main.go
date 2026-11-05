package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"
	// The workshop time zone must load in a minimal container image too.
	_ "time/tzdata"

	"github.com/aalvaropc/commitpool/internal/ai"
	"github.com/aalvaropc/commitpool/internal/clock"
	"github.com/aalvaropc/commitpool/internal/config"
	"github.com/aalvaropc/commitpool/internal/httpapi"
	"github.com/aalvaropc/commitpool/internal/mail"
	"github.com/aalvaropc/commitpool/internal/mcpserver"
	"github.com/aalvaropc/commitpool/internal/paypal"
	"github.com/aalvaropc/commitpool/internal/store"
	"github.com/aalvaropc/commitpool/internal/worker"
	"github.com/aalvaropc/commitpool/web"
)

const shutdownTimeout = 30 * time.Second

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(log); err != nil {
		log.Error("commitpool stopped", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := store.Migrate(ctx, st.Pool); err != nil {
		return err
	}

	pp, closePayPal, err := newPayPal(&cfg, log)
	if err != nil {
		return err
	}
	defer closePayPal()

	interpreter, err := newInterpreter(ctx, cfg, log)
	if err != nil {
		return err
	}

	dist, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		return err
	}

	clk := clock.Real{}
	var mailer mail.Mailer = mail.Log{Log: log}
	if cfg.Mailer == "ses" {
		ses, err := mail.NewSES(ctx, cfg.AWSRegion, cfg.MailFrom)
		if err != nil {
			return err
		}
		mailer = ses
	}
	runner := worker.New(worker.Deps{Store: st, PayPal: pp, Clock: clk, Cfg: cfg, Log: log, Mailer: mailer})
	srv := &http.Server{
		Addr: ":" + cfg.Port,
		Handler: httpapi.New(httpapi.Deps{
			Store: st, Worker: runner, PayPal: pp, AI: interpreter, Clock: clk, Cfg: cfg, Log: log, Dist: dist,
			MCP: mcpserver.New(mcpserver.Config{Secret: cfg.MCPSecret, Clock: clk, Log: log, Version: version()}),
		}),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		// The longest handler is /paypal/return: an OAuth token fetch, GET
		// order and the authorize, each bounded by the PayPal client's 20 s
		// timeout, about 60 s at worst; an interpretation waits up to 30 s.
		WriteTimeout: 90 * time.Second,
		IdleTimeout:  2 * time.Minute,
	}

	// The worker runs on its own context. On shutdown it stops claiming jobs
	// while the HTTP server drains (requests run their jobs inline through
	// RunKey, not through Run), and the process waits for the job it is
	// running, which Run bounds. Run also enqueues the seat releases once a
	// minute and reconciles and heals every two.
	workerCtx, stopWorker := context.WithCancel(context.Background())
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		if err := runner.Run(workerCtx); err != nil && !errors.Is(err, context.Canceled) {
			log.Error("worker stopped", "error", err)
		}
	}()
	defer func() {
		stopWorker()
		<-workerDone
	}()

	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Info("commitpool listening", "addr", srv.Addr, "config", cfg)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	stopWorker()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Warn("shutdown: requests still running at the deadline were cut", "error", err)
	}
	// Jobs requests ran inline finish before the database closes.
	if err := runner.WaitInline(shutdownCtx); err != nil {
		log.Warn("shutdown: inline jobs still running at the deadline; their results reconcile at the next start", "error", err)
	}
	log.Info("stopped")
	return nil
}

// newPayPal returns the PayPal client. With E2E_FAKE_PAYPAL=1 it first starts
// the in-process fake and points the configuration at it; the returned func
// stops the fake. Only a binary built with -tags e2e carries the fake.
func newPayPal(cfg *config.Config, log *slog.Logger) (paypal.API, func(), error) {
	ppCfg := paypal.Config{
		BaseURL:      cfg.PayPalBaseURL,
		ClientID:     cfg.PayPalClientID,
		ClientSecret: cfg.PayPalClientSecret,
		WebhookID:    cfg.PayPalWebhookID,
		SandboxMocks: cfg.SandboxMocks,
	}
	closeFake := func() {}
	if cfg.FakePayPal {
		url, stop, err := startFakePayPal()
		if err != nil {
			return nil, nil, err
		}
		closeFake = stop
		cfg.PayPalBaseURL = url
		ppCfg.BaseURL = url
		if ppCfg.ClientID == "" {
			ppCfg.ClientID = "fake-client"
		}
		if ppCfg.ClientSecret == "" {
			ppCfg.ClientSecret = "fake-secret"
		}
		log.Warn("using the in-process fake PayPal server", "url", url)
	}
	client, err := paypal.New(ppCfg)
	if err != nil {
		closeFake()
		return nil, nil, err
	}
	return client, closeFake, nil
}

// newInterpreter returns the configured interpreter: Claude on Amazon Bedrock
// or the deterministic fake. Any other provider fails at startup.
func newInterpreter(ctx context.Context, cfg config.Config, log *slog.Logger) (ai.Interpreter, error) {
	switch cfg.AIProvider {
	case config.AIProviderBedrock:
		b, err := ai.NewBedrock(ctx, ai.BedrockConfig{
			Region:        cfg.AWSRegion,
			Model:         cfg.BedrockModel,
			FallbackModel: cfg.BedrockFallbackModel,
			Effort:        cfg.BedrockEffort,
			Log:           log,
		})
		if err != nil {
			return nil, err
		}
		log.Info("using Claude on Amazon Bedrock", "region", cfg.AWSRegion, "model", cfg.BedrockModel, "fallback_model", cfg.BedrockFallbackModel, "effort", cfg.BedrockEffort)
		return b, nil
	case config.AIProviderFake:
		log.Warn("using the deterministic fake interpreter")
		return ai.Fake{}, nil
	default:
		return nil, fmt.Errorf("ai: unknown provider %q; set COMMITPOOL_AI_PROVIDER to bedrock or fake", cfg.AIProvider)
	}
}

// version is the build's VCS revision, or "dev".
func version() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" && len(setting.Value) >= 7 {
				return setting.Value[:7]
			}
		}
	}
	return "dev"
}
