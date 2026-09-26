package mailer

import (
	"context"

	"github.com/sayze/homelab-utils/logger"
)

// Noop logs alert emails instead of sending them. Used when SES isn't
// configured (ALERT_EMAIL_FROM/ALERT_EMAIL_TO unset) — e.g. local dev
// without AWS credentials — so alerting jobs still run without error.
type Noop struct{}

// Send logs the email instead of sending it.
func (Noop) Send(_ context.Context, subject, body string) error {
	logger.Info("alerting not configured, dropping email", "subject", subject, "bytes", len(body))
	return nil
}
