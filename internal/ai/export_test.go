package ai

import (
	"context"

	"github.com/anthropics/anthropic-sdk-go/v2/option"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
)

func NewBedrockForTest(_ context.Context, cfg BedrockConfig, baseURL string) (*Bedrock, error) {
	awsCfg := aws.Config{Region: cfg.Region, Credentials: credentials.NewStaticCredentialsProvider("AKIDTEST", "test-secret", "")}
	return newBedrock(cfg, awsCfg, option.WithBaseURL(baseURL))
}

var DraftSystem = draftSystem
