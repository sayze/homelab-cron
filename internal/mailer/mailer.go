// Package mailer sends the alert emails cron jobs request via
// cron.Job.AlertingEnabled/EmailContent. AWS SES is the only provider today
// (see SES); Noop is used when alerting isn't configured, e.g. local dev
// without AWS credentials.
package mailer

import "context"

// Mailer sends a single alert email for a completed job run.
type Mailer interface {
	// Send delivers an email with the given subject and plain-text body.
	Send(ctx context.Context, subject, body string) error
}
