// Package api wires up the chi router: GET /health for Nomad/Consul's
// health check, GET /job/{name} to trigger a registered job on demand.
// Neither is routed through Traefik.
package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/sayze/homelab-utils/router"

	"github.com/sayze/homelab-cron/internal/cron"
	"github.com/sayze/homelab-cron/internal/mailer"
)

// New adds GET /job/{name} to router.New. jobs is keyed by Name(); an
// unknown name 404s.
func New(jobs map[string]cron.Job, m mailer.Sender) chi.Router {
	r := router.New()

	r.Get("/job/{name}", handleTriggerJob(jobs, m))

	return r
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
