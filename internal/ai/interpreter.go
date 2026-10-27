// Package ai turns a participant's free-text availability into a structured
// domain.Interpretation and explains a date recommendation in plain words.
// The message is data, never instructions: the server, not the model,
// computes compatibility, amounts and states from the interpretation.
package ai

import (
	"context"

	"github.com/aalvaropc/commitpool/internal/domain"
)

// DateOption is one candidate date as the interpreter sees it: its ID and a
// label on the workshop's local clock, such as
// "Sat 14 Nov 2026, 15:00–18:00 America/Lima".
type DateOption struct {
	ID    string
	Label string
}

type InterpretInput struct {
	WorkshopTitle string
	Timezone      string
	Dates         []DateOption
	Message       string
}

// Meta describes the call that produced a result. Fallback is true when the
// provider could not answer and a conservative reading was used instead.
type Meta struct {
	Provider     string `json:"provider"`
	Model        string `json:"model"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	Fallback     bool   `json:"fallback"`
}

// ExplainInput is a recommendation to put into words. Names maps participant
// IDs to display names.
type ExplainInput struct {
	WorkshopTitle  string
	Dates          []DateOption
	Recommendation domain.Recommendation
	Names          map[string]string
	Minimum        int
}

type Interpreter interface {
	Interpret(ctx context.Context, in InterpretInput) (domain.Interpretation, Meta, error)
	Explain(ctx context.Context, in ExplainInput) (string, Meta, error)
}
