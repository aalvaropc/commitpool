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

type SES struct {
	client *sesv2.Client
	from   string
}

func NewSES(ctx context.Context, region, from string) (*SES, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("mail: ses client: %w", err)
	}
	return &SES{client: sesv2.NewFromConfig(cfg), from: from}, nil
}

func (s *SES) Send(ctx context.Context, m Message) (string, error) {
	out, err := s.client.SendEmail(ctx, sendInput(s.from, m))
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

func sendInput(from string, m Message) *sesv2.SendEmailInput {
	utf8 := func(s string) *types.Content {
		return &types.Content{Data: aws.String(s), Charset: aws.String("UTF-8")}
	}
	body := &types.Body{Text: utf8(m.Text)}
	if m.HTML != "" {
		body.Html = utf8(m.HTML)
	}
	msg := &types.Message{Subject: utf8(m.Subject), Body: body}
	for _, a := range m.Attachments {
		msg.Attachments = append(msg.Attachments, types.Attachment{
			FileName:                aws.String(a.Name),
			ContentType:             aws.String(a.ContentType),
			RawContent:              a.Data,
			ContentDisposition:      types.AttachmentContentDispositionAttachment,
			ContentTransferEncoding: types.AttachmentContentTransferEncodingBase64,
		})
	}
	return &sesv2.SendEmailInput{
		FromEmailAddress: aws.String(from),
		Destination:      &types.Destination{ToAddresses: []string{m.To}},
		Content:          &types.EmailContent{Simple: msg},
	}
}
