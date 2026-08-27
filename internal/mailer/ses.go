package mailer

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/aws-sdk-go-v2/service/sesv2/types"
)

// SES sends alert emails via AWS SES. Credentials and region are resolved
// through the AWS SDK's own default env-var chain (AWS_ACCESS_KEY_ID,
// AWS_SECRET_ACCESS_KEY, AWS_SESSION_TOKEN, AWS_REGION) — this package
// never reads or stores them itself. from/to come from this service's own
// config (ALERT_EMAIL_FROM/ALERT_EMAIL_TO).
type SES struct {
	client *sesv2.Client
	from   string
	to     []string
}

// NewSES builds an SES mailer. from must be an SES-verified sender
// address; to is the list of recipient addresses. Returns an error if the
// AWS SDK can't load a default config (e.g. no credentials present).
func NewSES(ctx context.Context, from string, to []string) (*SES, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("mailer: load aws config: %w", err)
	}

	return &SES{
		client: sesv2.NewFromConfig(cfg),
		from:   from,
		to:     to,
	}, nil
}

func (s *SES) Send(ctx context.Context, subject, body string) error {
	_, err := s.client.SendEmail(ctx, &sesv2.SendEmailInput{
		FromEmailAddress: aws.String(s.from),
		Destination: &types.Destination{
			ToAddresses: s.to,
		},
		Content: &types.EmailContent{
			Simple: &types.Message{
				Subject: &types.Content{Data: aws.String(subject)},
				Body: &types.Body{
					Text: &types.Content{Data: aws.String(body)},
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("mailer: ses send email: %w", err)
	}
	return nil
}
