// Package api wires up the chi router: GET /health for Nomad/Consul's
// health check, GET /job/{name} to trigger a registered job on demand.
// Neither is routed through Traefik.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/sayze/homelab-cron/internal/cron"
	"github.com/sayze/homelab-cron/internal/logger"
	"github.com/sayze/homelab-cron/internal/mailer"
)

// New builds the chi router. jobs (keyed by Name()) and m back GET
// /job/{name} — see handleTriggerJob. Both may be nil/empty, in which
// case every /job/{name} request 404s.
func New(jobs map[string]cron.Job, m mailer.Sender) chi.Router {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(requestLogger)
	r.Use(recoverer)

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

// requestLogger logs one JSON line per request once it completes. It
// replaces chi's middleware.Logger, which writes plain text.
func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		defer func() {
			logger.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", middleware.GetReqID(r.Context()),
				"remote_addr", r.RemoteAddr,
			)
		}()

		next.ServeHTTP(ww, r)
	})
}

// recoverer turns a handler panic into a 500 and a JSON error log line. It
// replaces chi's middleware.Recoverer, which prints a plain-text stack
// trace straight to stderr.
func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			if err, ok := rec.(error); ok && errors.Is(err, http.ErrAbortHandler) {
				// net/http's own signal to abort the response; let it through.
				panic(rec)
			}
			logger.Error("handler panicked",
				"panic", fmt.Sprint(rec),
				"stack", string(debug.Stack()),
				"request_id", middleware.GetReqID(r.Context()),
			)
			w.WriteHeader(http.StatusInternalServerError)
		}()

		next.ServeHTTP(w, r)
	})
}
