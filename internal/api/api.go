// Package api wires up the chi router: GET /health for Nomad/Consul's
// health check, GET /job/{name} to trigger a registered job on demand.
// Neither is routed through Traefik.
package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"homelab-cron/internal/cron"
	"homelab-cron/internal/mailer"
)

// New builds the chi router. jobs (keyed by Name()) and m back GET
// /job/{name} — see handleTriggerJob. Both may be nil/empty, in which
// case every /job/{name} request 404s.
func New(jobs map[string]cron.Job, m mailer.Sender) chi.Router {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/health", handleHealth)
	r.Get("/job/{name}", handleTriggerJob(jobs, m))

	return r
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleTriggerJob runs the job named by the {name} path param via
// cron.RunJob, in its own goroutine, and returns immediately without
// waiting for it to finish. name must match one of the const job names in
// internal/jobs (e.g. jobs.AptUpgradeCheckJobName) — anything else is a
// 404.
func handleTriggerJob(jobs map[string]cron.Job, m mailer.Sender) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")

		w.Header().Set("Content-Type", "application/json")

		j, ok := jobs[name]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "job not found"})
			return
		}

		go cron.RunJob(context.Background(), m, j)

		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "triggered", "job": name})
	}
}
