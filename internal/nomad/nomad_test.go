//go:build unit

package nomad

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
		respStatus  int
		respBody    any
		wantVersion string
		wantErr     string
	}{
		{
			name:        "agent self with nomad version",
			respStatus:  http.StatusOK,
			respBody:    map[string]any{"config": map[string]any{"Version": "1.11.3"}},
			wantVersion: "1.11.3",
		},
		{
			name:       "config missing version",
			respStatus: http.StatusOK,
			respBody:   map[string]any{"config": map[string]any{}},
			wantErr:    "agent self response has no config.Version",
		},
		{
			name:       "non-200 status",
			respStatus: http.StatusForbidden,
			respBody:   nil,
			wantErr:    "unexpected status 403",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/v1/agent/self", r.URL.Path)
				w.WriteHeader(tt.respStatus)
				if tt.respBody != nil {
					require.NoError(t, json.NewEncoder(w).Encode(tt.respBody))
				}
			}))
			defer srv.Close()

			client := NewHTTPClient(srv.URL, "test-token", srv.Client())

			version, err := client.Version(context.Background())

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

func TestHTTPClient_Version_SendsToken(t *testing.T) {
	var gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-Nomad-Token")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"config": map[string]any{"Version": "1.11.3"}}))
	}))
	defer srv.Close()

	client := NewHTTPClient(srv.URL, "super-secret-token", srv.Client())

	_, err := client.Version(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "super-secret-token", gotToken)
}

func TestHTTPClient_Version_RetriesOnFailure(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) < maxAttempts {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"config": map[string]any{"Version": "1.11.3"}}))
	}))
	defer srv.Close()

	client := NewHTTPClient(srv.URL, "test-token", srv.Client())

	version, err := client.Version(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "1.11.3", version)
	assert.EqualValues(t, maxAttempts, requests.Load())
}

func TestHTTPClient_Version_GivesUpAfterMaxAttempts(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewHTTPClient(srv.URL, "test-token", srv.Client())

	_, err := client.Version(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed after 3 attempts")
	assert.EqualValues(t, maxAttempts, requests.Load())
}

func TestNewHTTPClient_TrimsTrailingSlash(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"config": map[string]any{"Version": "1.11.3"}}))
	}))
	defer srv.Close()

	client := NewHTTPClient(srv.URL+"/", "test-token", srv.Client())

	_, err := client.Version(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "/v1/agent/self", gotPath)
}
