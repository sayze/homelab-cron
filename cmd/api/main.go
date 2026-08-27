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
	"homelab-cron/internal/server"
)

func main() {
	cfg := config.Load()

	scheduler, err := cron.New(
		jobs.NewHeartbeat(),
		jobs.NewDiskUsage(cfg.HostRoot),
		jobs.NewLogSummary(filepath.Join(cfg.HostRoot, "var/log")),
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
