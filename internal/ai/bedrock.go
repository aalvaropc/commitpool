package ai

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/bedrock"
	"github.com/anthropics/anthropic-sdk-go/v2/lib/betafallback"
	"github.com/anthropics/anthropic-sdk-go/v2/option"

	"github.com/aalvaropc/commitpool/internal/domain"
)

const (
	bedrockProvider = "bedrock"

	interpretMaxTokens = 2048
	explainMaxTokens   = 2048

	// maxExplanationWords caps an explanation, whatever the model writes.
	maxExplanationWords = 80
)

var (
	//go:embed prompts/interpret_system.txt
	interpretSystemPrompt string
	//go:embed prompts/explain_system.txt
	explainSystemPrompt string
)

// BedrockConfig selects Claude on Amazon Bedrock. Model and FallbackModel are
// Bedrock model IDs such as "anthropic.claude-opus-5-5"; FallbackModel
// answers a request Model declines, and an empty one disables the fallback.
// A nil Log uses slog.Default.
type BedrockConfig struct {
	Region        string
	Model         string
	FallbackModel string
	Log           *slog.Logger
}

// Bedrock is the Interpreter backed by Claude on Amazon Bedrock, through the
// Bedrock Mantle endpoint (the Messages API). Readings come back as strict
// structured output and are checked again against InterpretationSchema; the
// server, not the model, computes compatibility and the recommendation.
type Bedrock struct {
	messages anthropic.BetaMessageService
	model    string
	log      *slog.Logger
}

var _ Interpreter = (*Bedrock)(nil)

// NewBedrock builds the client for cfg.Region. AWS credentials come from the
// default chain (environment, shared config, instance role) and are resolved
// here, so a missing credential fails at startup rather than on the first
// message.
func NewBedrock(ctx context.Context, cfg BedrockConfig) (*Bedrock, error) {
	return newBedrock(ctx, cfg, bedrock.MantleClientConfig{AWSRegion: cfg.Region})
}

func newBedrock(ctx context.Context, cfg BedrockConfig, mantle bedrock.MantleClientConfig, opts ...option.RequestOption) (*Bedrock, error) {
	if cfg.Model == "" {
		return nil, errors.New("ai: bedrock needs a model id")
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	var fallbacks []anthropic.BetaFallbackParam
	if cfg.FallbackModel != "" && cfg.FallbackModel != cfg.Model {
		fallbacks = []anthropic.BetaFallbackParam{{Model: cfg.FallbackModel}}
	}
	// The refusal fallback must be the first middleware: it needs the request
	// as the Messages API shapes it.
	opts = append([]option.RequestOption{
		option.WithMiddleware(betafallback.BetaRefusalFallbackMiddleware(fallbacks)),
	}, opts...)
	client, err := bedrock.NewMantleClient(ctx, mantle, opts...)
	if err != nil {
		return nil, fmt.Errorf("ai: bedrock client: %w", err)
	}
	return &Bedrock{messages: client.Beta.Messages, model: cfg.Model, log: cfg.Log}, nil
}

// Interpret asks the model for a reading of in.Message held to
// InterpretationSchema for the workshop's dates. The system prompt carries
// the rules, the dates and the time zone; the message travels in the user
// turn as a JSON string, so it is read as data and cannot close what wraps
// it.
func (b *Bedrock) Interpret(ctx context.Context, in InterpretInput) (domain.Interpretation, Meta, error) {
	ids := make([]string, len(in.Dates))
	for i, d := range in.Dates {
		ids[i] = d.ID
	}
	if len(ids) == 0 {
		return domain.Interpretation{}, Meta{Provider: bedrockProvider, Model: b.model}, errors.New("ai: interpret: the workshop has no candidate dates")
	}
	msg, meta, err := b.send(ctx, "interpret", anthropic.BetaMessageNewParams{
		Model:     b.model,
		MaxTokens: interpretMaxTokens,
		System:    []anthropic.BetaTextBlockParam{{Text: interpretSystem(in)}},
		Messages: []anthropic.BetaMessageParam{
			anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("The participant's message, as a JSON string:\n" + jsonString(in.Message))),
		},
		OutputConfig: anthropic.BetaOutputConfigParam{
			Effort: anthropic.BetaOutputConfigEffortLow,
			Format: anthropic.BetaJSONOutputFormatParam{Schema: InterpretationSchema(ids)},
		},
	})
	if err != nil {
		return domain.Interpretation{}, meta, err
	}
	out, err := ParseInterpretation([]byte(answerText(msg)), ids)
	if err != nil {
		return domain.Interpretation{}, meta, fmt.Errorf("ai: interpret: %w", err)
	}
	return out, meta, nil
}

func interpretSystem(in InterpretInput) string {
	var sb strings.Builder
	sb.WriteString(interpretSystemPrompt)
	fmt.Fprintf(&sb, "\nWorkshop title: %s\nTime zone: %s\nCandidate dates (id: local date and time):\n", jsonString(in.WorkshopTitle), in.Timezone)
	for _, d := range in.Dates {
		fmt.Fprintf(&sb, "- %s: %s\n", d.ID, d.Label)
	}
	return sb.String()
}

// Explain asks the model to put the server's recommendation into at most 80
// words. The model sees the recommended date's label and the server's
// compatibility table, and only counts for the other dates, so it has no
// other date to name.
func (b *Bedrock) Explain(ctx context.Context, in ExplainInput) (string, Meta, error) {
	label := ""
	for _, d := range in.Dates {
		if d.ID == in.Recommendation.DateID {
			label = d.Label
		}
	}
	if label == "" {
		return PlainExplanation(in), Meta{Provider: bedrockProvider, Model: b.model}, nil
	}
	msg, meta, err := b.send(ctx, "explain", anthropic.BetaMessageNewParams{
		Model:     b.model,
		MaxTokens: explainMaxTokens,
		System:    []anthropic.BetaTextBlockParam{{Text: explainSystemPrompt}},
		Messages: []anthropic.BetaMessageParam{
			anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(ExplainFacts(in))),
		},
		OutputConfig: anthropic.BetaOutputConfigParam{Effort: anthropic.BetaOutputConfigEffortLow},
	})
	if err != nil {
		return "", meta, err
	}
	text := limitWords(answerText(msg), maxExplanationWords)
	if text == "" {
		return "", meta, errors.New("ai: explain: the answer was empty")
	}
	return text, meta, nil
}

// send makes one Messages call and logs its model, latency and token counts,
// never its content. A refused or truncated answer is an error.
func (b *Bedrock) send(ctx context.Context, op string, params anthropic.BetaMessageNewParams) (*anthropic.BetaMessage, Meta, error) {
	meta := Meta{Provider: bedrockProvider, Model: b.model}
	start := time.Now()
	// Each call is its own conversation, so each gets its own fallback state.
	msg, err := b.messages.New(ctx, params, betafallback.WithBetaFallbackState(&betafallback.BetaFallbackState{}))
	latency := time.Since(start).Milliseconds()
	if err != nil {
		err = callError(op, err)
		b.log.Warn("bedrock call failed", "op", op, "model", b.model, "latency_ms", latency, "error", err)
		return nil, meta, err
	}
	if msg.Model != "" {
		meta.Model = string(msg.Model)
	}
	meta.InputTokens = msg.Usage.InputTokens
	meta.OutputTokens = msg.Usage.OutputTokens
	for _, block := range msg.Content {
		if block.Type == "fallback" {
			meta.Fallback = true
		}
	}
	b.log.Info("bedrock call", "op", op, "model", b.model, "answered_by", meta.Model, "fallback", meta.Fallback,
		"latency_ms", latency, "input_tokens", meta.InputTokens, "output_tokens", meta.OutputTokens, "stop_reason", string(msg.StopReason))
	switch msg.StopReason {
	case anthropic.BetaStopReasonRefusal:
		return nil, meta, fmt.Errorf("ai: %s: the model declined to answer (category %q)", op, string(msg.StopDetails.Category))
	case anthropic.BetaStopReasonMaxTokens:
		return nil, meta, fmt.Errorf("ai: %s: the answer was cut off at %d tokens", op, params.MaxTokens)
	}
	return msg, meta, nil
}

// answerText joins the answer's text blocks; thinking and fallback boundary
// blocks are not part of it.
func answerText(msg *anthropic.BetaMessage) string {
	var sb strings.Builder
	for _, block := range msg.Content {
		if block.Type == "text" {
			sb.WriteString(block.Text)
		}
	}
	return strings.TrimSpace(sb.String())
}

// ProviderError is an error answer from Bedrock, reduced to what is safe to
// log: the operation, the HTTP status, the error type and the request ID.
// The answer's body is left out because it can quote the request.
type ProviderError struct {
	Op         string
	StatusCode int
	Type       string
	RequestID  string
}

func (e *ProviderError) Error() string {
	msg := fmt.Sprintf("ai: %s: bedrock answered %d", e.Op, e.StatusCode)
	if e.Type != "" {
		msg += " " + e.Type
	}
	if e.RequestID != "" {
		msg += " (request " + e.RequestID + ")"
	}
	return msg
}

// callError names the operation and keeps only the SDK's status and type, or
// the context's error; the SDK's own message is dropped, since it can carry
// the request or the answer.
func callError(op string, err error) error {
	var apiErr *anthropic.Error
	switch {
	case errors.As(err, &apiErr):
		return &ProviderError{Op: op, StatusCode: apiErr.StatusCode, Type: string(apiErr.Type()), RequestID: apiErr.RequestID}
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("ai: %s: %w", op, context.DeadlineExceeded)
	case errors.Is(err, context.Canceled):
		return fmt.Errorf("ai: %s: %w", op, context.Canceled)
	}
	// A network error names addresses only.
	var netErr net.Error
	switch {
	case errors.As(err, &netErr):
		return fmt.Errorf("ai: %s: %w", op, netErr)
	default:
		return fmt.Errorf("ai: %s: the request to bedrock failed (%T)", op, err)
	}
}

// limitWords keeps at most n words of s, on one line.
func limitWords(s string, n int) string {
	words := strings.Fields(s)
	if len(words) <= n {
		return strings.Join(words, " ")
	}
	return strings.Join(words[:n], " ") + "…"
}
