package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"homelab-cron/internal/config"
	"homelab-cron/internal/cron"
	"homelab-cron/internal/jobs"
	"homelab-cron/internal/mailer"
	"homelab-cron/internal/server"
)

func main() {
	cfg := config.Load()

	m, err := newMailer(cfg)
	if err != nil {
		log.Fatalf("failed to build mailer: %v", err)
	}

	scheduler, err := cron.New(
		m,
		jobs.NewHeartbeat(),
		jobs.NewAptUpgradeCheck(filepath.Join(cfg.HostRoot, "var/log/apt/upgrade.log")),
	)
	if err != nil {
		log.Fatalf("failed to build scheduler: %v", err)
	}
	scheduler.Start()
	defer scheduler.Stop()

	srv := &http.Server{
		Addr:    cfg.Addr,
		Handler: server.New(),
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("homelab-cron listening on %s", cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http server: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("http server shutdown: %v", err)
	}
}

// newMailer builds the mailer used to send alerting jobs' emails. If
// ALERT_EMAIL_FROM/ALERT_EMAIL_TO aren't both set, alerting isn't
// configured and it returns a mailer.Noop that logs instead of sending —
// this keeps local dev (no AWS credentials) working without error.
func newMailer(cfg config.Config) (mailer.Mailer, error) {
	if cfg.AlertEmailFrom == "" || len(cfg.AlertEmailTo) == 0 {
		log.Println("mailer: ALERT_EMAIL_FROM/ALERT_EMAIL_TO not set, alert emails will only be logged")
		return mailer.Noop{}, nil
	}
	return mailer.NewSES(context.Background(), cfg.AlertEmailFrom, cfg.AlertEmailTo)
}
