// Command api is homelab-cron's HTTP entrypoint: GET /health for
// Nomad/Consul's health check, and GET /job/{name} to run a registered
// job on demand (see internal/api). cmd/cron owns actual scheduling.
package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/sayze/homelab-cron/internal/api"
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
	logger.Init("homelab-cron-api")

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

	triggerable := []cron.Job{
		jobs.NewAptUpgradeCheck(filepath.Join(cfg.HostRoot, "var/log/apt/upgrade.log")),
		jobs.NewVersionCheck(consulClient, vaultClient, nomadClient, dockerClient),
		jobs.NewHealthCheck(postgres.NewPgxClient(cfg.DatabaseURL)),
	}
	jobsByName := make(map[string]cron.Job, len(triggerable))
	for _, j := range triggerable {
		jobsByName[j.Name()] = j
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           api.New(jobsByName, m),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("homelab-cron api listening", "addr", cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server failed", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("http server shutdown failed", "error", err)
	}
}
