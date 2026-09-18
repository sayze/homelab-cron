//go:build unit

package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"homelab-cron/internal/cron"
	"homelab-cron/internal/mailer"
)

// testJob is a minimal cron.Job for exercising the router without a real
// job implementation.
type testJob struct {
	name string
	run  func(ctx context.Context) error
}

func (j testJob) Name() string                  { return j.name }
func (testJob) Schedule() string                { return "0 0 1 1 *" }
func (j testJob) Run(ctx context.Context) error { return j.run(ctx) }
func (testJob) AlertingEnabled() bool           { return false }
func (testJob) EmailContent() string            { return "" }

func TestNew_NoJobs(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		wantStatus int
		wantBody   string // JSON-compared when non-empty
	}{
		{"health check", "/health", http.StatusOK, `{"status": "ok"}`},
		{"root not routed", "/", http.StatusNotFound, ""},
		{"status not routed", "/status", http.StatusNotFound, ""},
		{"metrics not routed", "/metrics", http.StatusNotFound, ""},
		{"unregistered job is a 404", "/job/apt-upgrade-check", http.StatusNotFound, `{"error": "job not found"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := New(nil, mailer.Noop{})

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()

			srv.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
			if tt.wantBody != "" {
				assert.JSONEq(t, tt.wantBody, rec.Body.String())
			}
		})
	}
}

func TestNew_TriggerJob(t *testing.T) {
	t.Run("runs the matching job without waiting for it to finish", func(t *testing.T) {
		ran := make(chan struct{})
		jobs := map[string]cron.Job{
			"on-demand": testJob{name: "on-demand", run: func(context.Context) error {
				close(ran)
				return nil
			}},
		}

		srv := New(jobs, mailer.Noop{})

		req := httptest.NewRequest(http.MethodGet, "/job/on-demand", nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusAccepted, rec.Code)
		assert.JSONEq(t, `{"status": "triggered", "job": "on-demand"}`, rec.Body.String())

		select {
		case <-ran:
		case <-time.After(time.Second):
			t.Fatal("triggered job did not run")
		}
	})

	t.Run("unknown job name is a 404", func(t *testing.T) {
		jobs := map[string]cron.Job{
			"on-demand": testJob{name: "on-demand", run: func(context.Context) error { return nil }},
		}

		srv := New(jobs, mailer.Noop{})

		req := httptest.NewRequest(http.MethodGet, "/job/does-not-exist", nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.JSONEq(t, `{"error": "job not found"}`, rec.Body.String())
	})
}
