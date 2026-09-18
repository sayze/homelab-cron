// Command api is homelab-cron's HTTP entrypoint: it serves GET /health,
// used only for Nomad/Consul's own health check, and GET /job/{name},
// which runs a registered cron.Job immediately, outside its normal
// schedule (see internal/api). It builds the same jobs cmd/cron
// schedules, but only ever runs one on demand, via cron.RunOnce — never
// through cron.Scheduler, which is cmd/cron's alone.
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

	"homelab-cron/internal/api"
	"homelab-cron/internal/config"
	"homelab-cron/internal/consul"
	"homelab-cron/internal/cron"
	"homelab-cron/internal/docker"
	"homelab-cron/internal/jobs"
	"homelab-cron/internal/mailer"
	"homelab-cron/internal/nomad"
	"homelab-cron/internal/vault"
)

func main() {
	cfg := config.Load()

	m, err := newMailer(cfg)
	if err != nil {
		log.Fatalf("failed to build mailer: %v", err)
	}

	consulClient, vaultClient, nomadClient, dockerClient := consul.NewHTTPClient(cfg.ConsulAddr, &http.Client{Timeout: 10 * time.Second}),
		vault.NewHTTPClient(cfg.VaultAddr, &http.Client{Timeout: 10 * time.Second}),
		nomad.NewHTTPClient(cfg.NomadAddr, cfg.NomadToken, &http.Client{Timeout: 10 * time.Second}),
		docker.NewHTTPClient(cfg.DockerSock)

	triggerable := []cron.Job{
		jobs.NewAptUpgradeCheck(filepath.Join(cfg.HostRoot, "var/log/apt/upgrade.log")),
		jobs.NewWebstackVersionCheck(consulClient, vaultClient, nomadClient, dockerClient),
	}
	byName := make(map[string]cron.Job, len(triggerable))
	for _, j := range triggerable {
		byName[j.Name()] = j
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           api.New(byName, m),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("homelab-cron api listening on %s", cfg.Addr)
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

// newMailer builds the mailer used to send a triggered job's alert email,
// if it has one (see internal/api). If ALERT_EMAIL_FROM/ALERT_EMAIL_TO
// aren't both set, alerting isn't configured and it returns a mailer.Noop
// that logs instead of sending — this keeps local dev (no AWS
// credentials) working without error.
func newMailer(cfg config.Config) (mailer.Sender, error) {
	if cfg.AlertEmailFrom == "" || len(cfg.AlertEmailTo) == 0 {
		log.Println("mailer: ALERT_EMAIL_FROM/ALERT_EMAIL_TO not set, alert emails will only be logged")
		return mailer.Noop{}, nil
	}
	return mailer.NewSES(context.Background(), cfg.AlertEmailFrom, cfg.AlertEmailTo)
}
