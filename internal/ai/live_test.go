package ai_test

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/ai"
	"github.com/aalvaropc/commitpool/internal/config"
)

func TestBedrock_Live(t *testing.T) {
	if os.Getenv("COMMITPOOL_LIVE_AI") != "1" {
		t.Skip("set COMMITPOOL_LIVE_AI=1, AWS_REGION and AWS credentials to call Bedrock for real")
	}
	region := os.Getenv("AWS_REGION")
	if region == "" {
		t.Fatal("COMMITPOOL_LIVE_AI=1 needs AWS_REGION (and AWS credentials) to reach Bedrock")
	}
	model := envOr("COMMITPOOL_BEDROCK_MODEL", config.DefaultBedrockModel)
	fallback := envOr("COMMITPOOL_BEDROCK_FALLBACK_MODEL", config.DefaultBedrockFallbackModel)
	effort := envOr("COMMITPOOL_BEDROCK_EFFORT", "none")
	if effort == "none" {
		effort = ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	b, err := ai.NewBedrock(ctx, ai.BedrockConfig{
		Region: region, Model: model, FallbackModel: fallback, Effort: effort,
		Log: slog.New(slog.NewTextHandler(os.Stderr, nil)),
	})
	if err != nil {
		t.Fatalf("NewBedrock: %v", err)
	}

	in, meta, err := b.Interpret(ctx, ai.InterpretInput{
		WorkshopTitle: "Go for operators",
		Timezone:      "America/Lima",
		Dates:         dates,
		Message:       "Saturday works if we finish before six. Sunday mornings are impossible for me.",
	})
	if err != nil {
		t.Fatalf("Interpret: %v", err)
	}
	t.Logf("interpret meta: %+v", meta)
	if len(in.PerDate) != len(dates) {
		t.Fatalf("per_date has %d verdicts, want one per candidate date (%d): %+v", len(in.PerDate), len(dates), in.PerDate)
	}
	seen := map[string]bool{}
	for _, v := range in.PerDate {
		seen[v.CandidateDateID] = true
	}
	for _, d := range dates {
		if !seen[d.ID] {
			t.Errorf("no verdict for date %s: %+v", d.ID, in.PerDate)
		}
	}
	t.Logf("reading: %+v", in)

	text, meta, err := b.Explain(ctx, explainInput())
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	t.Logf("explain meta: %+v", meta)
	if text == "" || len(strings.Fields(text)) > 60 {
		t.Errorf("explanation %q, want 1 to 60 words", text)
	}
	if strings.Contains(text, "Nov 15") || strings.Contains(text, "Sun") {
		t.Errorf("explanation %q names a date the server did not recommend", text)
	}
	t.Logf("explanation: %s", text)
}

func envOr(name, def string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return def
}
