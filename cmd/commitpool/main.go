package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"sync"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/aalvaropc/commitpool/internal/ai"
	"github.com/aalvaropc/commitpool/internal/clock"
	"github.com/aalvaropc/commitpool/internal/config"
	"github.com/aalvaropc/commitpool/internal/demo"
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
	if len(os.Args) == 2 && os.Args[1] == "check-config" {
		if err := checkConfig(os.Getenv, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(log); err != nil {
		log.Error("commitpool stopped", "error", err)
		os.Exit(1)
	}
}

func checkConfig(getenv func(string) string, out io.Writer) error {
	if _, err := config.Load(getenv); err != nil {
		return err
	}
	_, err := fmt.Fprintln(out, "config: ok")
	return err
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
	var background sync.WaitGroup
	srv := &http.Server{
		Addr: ":" + cfg.Port,
		Handler: httpapi.New(httpapi.Deps{
			Store: st, Worker: runner, PayPal: pp, AI: interpreter, Clock: clk, Cfg: cfg, Log: log, Dist: dist, Background: &background,
			MCP: mcpserver.New(mcpserver.Config{Log: log, Version: version()}),
		}),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      90 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

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
	if err := waitFor(shutdownCtx, &background); err != nil {
		log.Warn("shutdown: demo answers still arriving at the deadline were cut", "error", err)
	}
	if err := runner.WaitInline(shutdownCtx); err != nil {
		log.Warn("shutdown: inline jobs still running at the deadline; their results reconcile at the next start", "error", err)
	}
	log.Info("stopped")
	return nil
}

func waitFor(ctx context.Context, wg *sync.WaitGroup) error {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

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
		log.Info("using Claude on Amazon Bedrock", "region", cfg.AWSRegion, "model", cfg.BedrockModel, "fallback_model", cfg.BedrockFallbackModel, "effort", cfg.BedrockEffort,
			"hourly_cap", cfg.AIHourlyCap, "daily_cap", cfg.AIDailyCap, "monthly_cap", cfg.AIMonthlyCap)
		return ai.NewMemo(ai.NewBudget(b, ai.BudgetConfig{Hourly: cfg.AIHourlyCap, Daily: cfg.AIDailyCap, Monthly: cfg.AIMonthlyCap, Clock: clock.Real{}}), ai.MemoSize), nil
	case config.AIProviderFake:
		log.Warn("using the deterministic fake interpreter, scripted for the demo cast")
		return ai.Fake{Script: demo.FakeScript()}, nil
	default:
		return nil, fmt.Errorf("ai: unknown provider %q; set COMMITPOOL_AI_PROVIDER to bedrock or fake", cfg.AIProvider)
	}
}

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
