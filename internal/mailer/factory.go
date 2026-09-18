package mailer

import (
	"context"
	"log"
)

// Config configures New. It's this package's own small config, not
// homelab-cron/internal/config.Config — mailer only needs these two
// fields, so it doesn't depend on the whole service's config.
type Config struct {
	// From is the SES-verified sender address. To is the list of
	// recipient addresses. Both must be set for New to return an SES
	// sender — otherwise it returns a Noop.
	From string
	To   []string
}

// New builds the Sender cmd/api and cmd/cron both use to send a job's
// alert email. If cfg.From/cfg.To aren't both set, alerting isn't
// configured and it returns a Noop that logs instead of sending — this
// keeps local dev (no AWS credentials) working without error.
func New(ctx context.Context, cfg Config) (Sender, error) {
	if cfg.From == "" || len(cfg.To) == 0 {
		log.Println("mailer: ALERT_EMAIL_FROM/ALERT_EMAIL_TO not set, alert emails will only be logged")
		return Noop{}, nil
	}
	return NewSES(ctx, cfg.From, cfg.To)
}
