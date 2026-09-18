package mailer

import (
	"context"
	"log"

	"homelab-cron/internal/config"
)

// New builds the Sender cmd/api and cmd/cron both use to send a job's
// alert email. If cfg.AlertEmailFrom/AlertEmailTo aren't both set,
// alerting isn't configured and it returns a Noop that logs instead of
// sending — this keeps local dev (no AWS credentials) working without
// error.
func New(ctx context.Context, cfg config.Config) (Sender, error) {
	if cfg.AlertEmailFrom == "" || len(cfg.AlertEmailTo) == 0 {
		log.Println("mailer: ALERT_EMAIL_FROM/ALERT_EMAIL_TO not set, alert emails will only be logged")
		return Noop{}, nil
	}
	return NewSES(ctx, cfg.AlertEmailFrom, cfg.AlertEmailTo)
}
