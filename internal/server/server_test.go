//go:build unit

package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNew_HealthCheck(t *testing.T) {
	srv := New()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"status": "ok"}`, rec.Body.String())
}

func TestNew_NoRoutesOutsideHealth(t *testing.T) {
	srv := New()

	for _, path := range []string{"/", "/status", "/metrics", "/jobs"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()

		srv.ServeHTTP(rec, req)

		assert.Equalf(t, http.StatusNotFound, rec.Code, "path %q should not be routed", path)
	}
}
