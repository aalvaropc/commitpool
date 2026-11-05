package mail

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/aws-sdk-go-v2/service/sesv2/types"
	"github.com/aws/smithy-go"
)

// SES sends through Amazon SES v2 with the default AWS credential chain (the
// VM's instance role in the hosted demo) from a verified sender identity.
type SES struct {
	client *sesv2.Client
	from   string
}

// NewSES builds the SES mailer for region, sending as from.
func NewSES(ctx context.Context, region, from string) (*SES, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("mail: ses client: %w", err)
	}
	return &SES{client: sesv2.NewFromConfig(cfg), from: from}, nil
}

func (s *SES) Send(ctx context.Context, m Message) (string, error) {
	out, err := s.client.SendEmail(ctx, &sesv2.SendEmailInput{
		FromEmailAddress: aws.String(s.from),
		Destination:      &types.Destination{ToAddresses: []string{m.To}},
		Content: &types.EmailContent{Simple: &types.Message{
			Subject: &types.Content{Data: aws.String(m.Subject), Charset: aws.String("UTF-8")},
			Body:    &types.Body{Text: &types.Content{Data: aws.String(m.Text), Charset: aws.String("UTF-8")}},
		}},
	})
	if err != nil {
		var api smithy.APIError
		if errors.As(err, &api) {
			switch api.ErrorCode() {
			case "MessageRejected", "MailFromDomainNotVerifiedException", "AccountSuspendedException", "SendingPausedException", "BadRequestException":
				return "", fmt.Errorf("%w: %s", ErrRejected, api.ErrorCode())
			}
		}
		return "", fmt.Errorf("mail: ses send: %w", err)
	}
	return aws.ToString(out.MessageId), nil
}
