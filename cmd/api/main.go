// Command api is homelab-cron's HTTP entrypoint: GET /health for
// Nomad/Consul's health check, and GET /job/{name} to run a registered
// job on demand (see internal/api). cmd/cron owns actual scheduling.
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

	m, err := mailer.New(context.Background(), cfg)
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
