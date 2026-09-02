//go:build unit

package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNew(t *testing.T) {
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
		{"jobs not routed", "/jobs", http.StatusNotFound, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := New()

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
