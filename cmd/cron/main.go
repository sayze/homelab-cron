// Command cron is homelab-cron's scheduler entrypoint: it builds and runs
// the service's cron jobs. It has no HTTP surface of its own — see cmd/api
// for /health and GET /job/{name} (the latter runs a job on demand,
// outside its schedule, without going through this process at all).
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/sayze/homelab-cron/internal/config"
	"github.com/sayze/homelab-cron/internal/consul"
	"github.com/sayze/homelab-cron/internal/cron"
	"github.com/sayze/homelab-cron/internal/docker"
	"github.com/sayze/homelab-cron/internal/jobs"
	"github.com/sayze/homelab-cron/internal/logger"
	"github.com/sayze/homelab-cron/internal/mailer"
	"github.com/sayze/homelab-cron/internal/nomad"
	"github.com/sayze/homelab-cron/internal/postgres"
	"github.com/sayze/homelab-cron/internal/vault"
)

func main() {
	logger.Init("homelab-cron")

	cfg := config.Load()

	m, err := mailer.New(context.Background(), mailer.Config{From: cfg.AlertEmailFrom, To: cfg.AlertEmailTo})
	if err != nil {
		logger.Error("failed to build mailer", "error", err)
		os.Exit(1)
	}

	consulClient, vaultClient, nomadClient, dockerClient := consul.NewHTTPClient(cfg.ConsulAddr, &http.Client{Timeout: 10 * time.Second}),
		vault.NewHTTPClient(cfg.VaultAddr, &http.Client{Timeout: 10 * time.Second}),
		nomad.NewHTTPClient(cfg.NomadAddr, cfg.NomadToken, &http.Client{Timeout: 10 * time.Second}),
		docker.NewHTTPClient(cfg.DockerSock)

	scheduler, err := cron.New(
		m,
		jobs.NewAptUpgradeCheck(filepath.Join(cfg.HostRoot, "var/log/apt/upgrade.log")),
		jobs.NewVersionCheck(consulClient, vaultClient, nomadClient, dockerClient),
		jobs.NewHealthCheck(postgres.NewPgxClient(cfg.DatabaseURL)),
	)
	if err != nil {
		logger.Error("failed to build scheduler", "error", err)
		os.Exit(1)
	}
	scheduler.Start()
	defer scheduler.Stop()

	logger.Info("homelab-cron scheduler running")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	<-ctx.Done()
	logger.Info("shutting down")
}
