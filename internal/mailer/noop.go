package mailer

import (
	"context"
	"log"
)

// Noop logs alert emails instead of sending them. Used when SES isn't
// configured (ALERT_EMAIL_FROM/ALERT_EMAIL_TO unset) — e.g. local dev
// without AWS credentials — so alerting jobs still run without error.
type Noop struct{}

func (Noop) Send(_ context.Context, subject, body string) error {
	log.Printf("mailer: alerting not configured, dropping email %q (%d bytes)", subject, len(body))
	return nil
}
