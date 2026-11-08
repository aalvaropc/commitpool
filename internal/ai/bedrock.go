package ai

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/bedrock"
	"github.com/anthropics/anthropic-sdk-go/v2/lib/betafallback"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"

	"github.com/aalvaropc/commitpool/internal/domain"
)

const (
	bedrockProvider = "bedrock"

	bedrockAPIVersion = "bedrock-2023-05-31"

	credentialTimeout = 10 * time.Second

	interpretMaxTokens = 2048
	explainMaxTokens   = 2048
	draftMaxTokens     = 4096

	maxExplanationWords = 60
)

var (
	//go:embed prompts/interpret_system.txt
	interpretSystemPrompt string
	//go:embed prompts/explain_system.txt
	explainSystemPrompt string
	//go:embed prompts/draft_system.txt
	draftSystemPrompt string
)

type BedrockConfig struct {
	Region        string
	Model         string
	FallbackModel string
	Effort        string
	Log           *slog.Logger
}

type Bedrock struct {
	messages anthropic.BetaMessageService
	model    string
	effort   anthropic.BetaOutputConfigEffort
	log      *slog.Logger
}

var _ Interpreter = (*Bedrock)(nil)

func NewBedrock(ctx context.Context, cfg BedrockConfig) (*Bedrock, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.Region))
	if err != nil {
		return nil, fmt.Errorf("ai: bedrock client: %w", err)
	}
	if os.Getenv("AWS_BEARER_TOKEN_BEDROCK") == "" {
		rctx, cancel := context.WithTimeout(ctx, credentialTimeout)
		defer cancel()
		if _, err := awsCfg.Credentials.Retrieve(rctx); err != nil {
			return nil, fmt.Errorf("ai: bedrock client: failed to resolve AWS credentials: %w", err)
		}
	}
	return newBedrock(cfg, awsCfg)
}

func newBedrock(cfg BedrockConfig, awsCfg aws.Config, opts ...option.RequestOption) (*Bedrock, error) {
	if cfg.Model == "" {
		return nil, errors.New("ai: bedrock needs a model id")
	}
	switch cfg.Effort {
	case "", "low", "medium", "high", "xhigh", "max":
	default:
		return nil, fmt.Errorf("ai: bedrock effort %q is not low, medium, high, xhigh or max", cfg.Effort)
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	var fallbacks []anthropic.BetaFallbackParam
	if cfg.FallbackModel != "" && cfg.FallbackModel != cfg.Model {
		fallbacks = []anthropic.BetaFallbackParam{{Model: cfg.FallbackModel}}
	}
	opts = append([]option.RequestOption{
		option.WithMiddleware(betafallback.BetaRefusalFallbackMiddleware(fallbacks)),
		bedrock.WithConfig(awsCfg),
		option.WithJSONSet("anthropic_version", bedrockAPIVersion),
	}, opts...)
	client := anthropic.NewClient(opts...)
	return &Bedrock{messages: client.Beta.Messages, model: cfg.Model, effort: anthropic.BetaOutputConfigEffort(cfg.Effort), log: cfg.Log}, nil
}

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
			anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(interpretUserTurn(in))),
		},
		OutputConfig: anthropic.BetaOutputConfigParam{
			Effort: b.effort,
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

func interpretUserTurn(in InterpretInput) string {
	text := "The participant's message, as a JSON string:\n" + jsonString(in.Message)
	if in.Earlier == nil {
		return text
	}
	text += "\n\nIt follows their earlier message, as a JSON string:\n" + jsonString(in.Earlier.Message)
	text += "\n\nand the questions we asked them, as a JSON list:\n" + jsonList(in.Earlier.Questions)
	return text
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
		OutputConfig: anthropic.BetaOutputConfigParam{Effort: b.effort},
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

func (b *Bedrock) Draft(ctx context.Context, in DraftInput) (Draft, Meta, error) {
	msg, meta, err := b.send(ctx, "draft", anthropic.BetaMessageNewParams{
		Model:     b.model,
		MaxTokens: draftMaxTokens,
		System:    []anthropic.BetaTextBlockParam{{Text: draftSystem(in)}},
		Messages: []anthropic.BetaMessageParam{
			anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("The organizer's description, as a JSON string:\n" + jsonString(in.Text))),
		},
		OutputConfig: anthropic.BetaOutputConfigParam{
			Effort: b.effort,
			Format: anthropic.BetaJSONOutputFormatParam{Schema: DraftSchema()},
		},
	})
	if err != nil {
		return Draft{}, meta, err
	}
	out, err := ParseDraft([]byte(answerText(msg)))
	if err != nil {
		return Draft{}, meta, fmt.Errorf("ai: draft: %w", err)
	}
	return out, meta, nil
}

func (b *Bedrock) send(ctx context.Context, op string, params anthropic.BetaMessageNewParams) (*anthropic.BetaMessage, Meta, error) {
	meta := Meta{Provider: bedrockProvider, Model: b.model}
	start := time.Now()
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

func answerText(msg *anthropic.BetaMessage) string {
	var sb strings.Builder
	for _, block := range msg.Content {
		if block.Type == "text" {
			sb.WriteString(block.Text)
		}
	}
	return strings.TrimSpace(sb.String())
}

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
	var netErr net.Error
	switch {
	case errors.As(err, &netErr):
		return fmt.Errorf("ai: %s: %w", op, netErr)
	default:
		return fmt.Errorf("ai: %s: the request to bedrock failed (%T)", op, err)
	}
}

func limitWords(s string, n int) string {
	words := strings.Fields(s)
	if len(words) <= n {
		return strings.Join(words, " ")
	}
	return strings.Join(words[:n], " ") + "…"
}
