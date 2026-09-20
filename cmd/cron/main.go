// Command cron is homelab-cron's scheduler entrypoint: it builds and runs
// the service's cron jobs. It has no HTTP surface of its own — see cmd/api
// for /health and GET /job/{name} (the latter runs a job on demand,
// outside its schedule, without going through this process at all).
package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
	_ "time/tzdata"

	"homelab-cron/internal/config"
	"homelab-cron/internal/consul"
	"homelab-cron/internal/cron"
	"homelab-cron/internal/docker"
	"homelab-cron/internal/jobs"
	"homelab-cron/internal/mailer"
	"homelab-cron/internal/nomad"
	"homelab-cron/internal/postgres"
	"homelab-cron/internal/vault"
)

func main() {
	cfg := config.Load()

	m, err := mailer.New(context.Background(), mailer.Config{From: cfg.AlertEmailFrom, To: cfg.AlertEmailTo})
	if err != nil {
		log.Fatalf("failed to build mailer: %v", err)
	}

	consulClient, vaultClient, nomadClient, dockerClient := consul.NewHTTPClient(cfg.ConsulAddr, &http.Client{Timeout: 10 * time.Second}),
		vault.NewHTTPClient(cfg.VaultAddr, &http.Client{Timeout: 10 * time.Second}),
		nomad.NewHTTPClient(cfg.NomadAddr, cfg.NomadToken, &http.Client{Timeout: 10 * time.Second}),
		docker.NewHTTPClient(cfg.DockerSock)

	scheduler, err := cron.New(
		m,
		jobs.NewAptUpgradeCheck(filepath.Join(cfg.HostRoot, "var/log/apt/upgrade.log")),
		jobs.NewWebstackVersionCheck(consulClient, vaultClient, nomadClient, dockerClient),
		jobs.NewHealthCheck(postgres.NewPgxClient(cfg.DatabaseURL)),
	)
	if err != nil {
		log.Fatalf("failed to build scheduler: %v", err)
	}
	scheduler.Start()
	defer scheduler.Stop()

	log.Println("homelab-cron scheduler running")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	<-ctx.Done()
	log.Println("shutting down")
}
