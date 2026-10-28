package ai

import (
	"context"

	"github.com/anthropics/anthropic-sdk-go/v2/bedrock"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
)

// NewBedrockForTest builds the Bedrock interpreter as NewBedrock does, but
// sends to baseURL with the bearer API key "test" instead of AWS SigV4.
func NewBedrockForTest(ctx context.Context, cfg BedrockConfig, baseURL string) (*Bedrock, error) {
	return newBedrock(ctx, cfg, bedrock.MantleClientConfig{APIKey: "test", AWSRegion: cfg.Region}, option.WithBaseURL(baseURL))
}
