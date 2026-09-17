//go:build unit

package consul

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	retryDelay = time.Millisecond
}

func TestHTTPClient_Version(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		respStatus  int
		respBody    any
		wantVersion string
		wantErr     string
	}{
		{
			name:        "healthy instance with version meta",
			path:        "/v1/health/service/traefik",
			respStatus:  http.StatusOK,
			respBody:    []map[string]any{{"Service": map[string]any{"Meta": map[string]string{"image": "traefik", "version": "3.6.1"}}}},
			wantVersion: "3.6.1",
		},
		{
			name:       "first of multiple instances wins",
			path:       "/v1/health/service/postgres",
			respStatus: http.StatusOK,
			respBody: []map[string]any{
				{"Service": map[string]any{"Meta": map[string]string{"version": "16"}}},
				{"Service": map[string]any{"Meta": map[string]string{"version": "15"}}},
			},
			wantVersion: "16",
		},
		{
			name:       "no healthy instances",
			path:       "/v1/health/service/newrelic",
			respStatus: http.StatusOK,
			respBody:   []map[string]any{},
			wantErr:    `no healthy instance of service "newrelic" registered`,
		},
		{
			name:       "instance missing version meta",
			path:       "/v1/health/service/traefik",
			respStatus: http.StatusOK,
			respBody:   []map[string]any{{"Service": map[string]any{"Meta": map[string]string{"image": "traefik"}}}},
			wantErr:    `service "traefik" has no "version" meta`,
		},
		{
			name:       "non-200 status",
			path:       "/v1/health/service/traefik",
			respStatus: http.StatusInternalServerError,
			respBody:   nil,
			wantErr:    "unexpected status 500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, tt.path, r.URL.Path)
				assert.Equal(t, "true", r.URL.Query().Get("passing"))
				w.WriteHeader(tt.respStatus)
				if tt.respBody != nil {
					require.NoError(t, json.NewEncoder(w).Encode(tt.respBody))
				}
			}))
			defer srv.Close()

			client := NewHTTPClient(srv.URL, srv.Client())
			service := tt.path[len("/v1/health/service/"):]

			version, err := client.Version(context.Background(), service)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantVersion, version)
		})
	}
}

func TestHTTPClient_Version_RetriesOnFailure(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) < maxAttempts {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		require.NoError(t, json.NewEncoder(w).Encode([]map[string]any{
			{"Service": map[string]any{"Meta": map[string]string{"version": "3.6.1"}}},
		}))
	}))
	defer srv.Close()

	client := NewHTTPClient(srv.URL, srv.Client())

	version, err := client.Version(context.Background(), "traefik")

	require.NoError(t, err)
	assert.Equal(t, "3.6.1", version)
	assert.EqualValues(t, maxAttempts, requests.Load())
}

func TestHTTPClient_Version_GivesUpAfterMaxAttempts(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewHTTPClient(srv.URL, srv.Client())

	_, err := client.Version(context.Background(), "traefik")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed after 3 attempts")
	assert.EqualValues(t, maxAttempts, requests.Load())
}

func TestNewHTTPClient_TrimsTrailingSlash(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		require.NoError(t, json.NewEncoder(w).Encode([]map[string]any{
			{"Service": map[string]any{"Meta": map[string]string{"version": "1.0.0"}}},
		}))
	}))
	defer srv.Close()

	client := NewHTTPClient(srv.URL+"/", srv.Client())

	_, err := client.Version(context.Background(), "traefik")

	require.NoError(t, err)
	assert.Equal(t, "/v1/health/service/traefik", gotPath)
}
