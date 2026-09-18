// Command api is homelab-cron's HTTP entrypoint: it serves a single
// /health route, used only for Nomad/Consul's own health check. The
// service's actual work — running cron jobs — happens in the separate
// cmd/cron entrypoint; this process does none of that.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"homelab-cron/internal/config"
	"homelab-cron/internal/server"
)

func main() {
	cfg := config.Load()

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           server.New(),
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
