package main

import (
	"context"
	"io"
	"log/slog"
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
