//go:build unit

package docker

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	retryDelay = time.Millisecond
}

// newTestSocket starts an HTTP server listening on a Unix socket in a temp
// dir, mirroring how a real Docker daemon serves its API, and returns the
// socket's path.
func newTestSocket(t *testing.T, handler http.Handler) string {
	t.Helper()

	sockPath := filepath.Join(t.TempDir(), "docker.sock")
	l, err := net.Listen("unix", sockPath)
	require.NoError(t, err)

	srv := &http.Server{Handler: handler}
	go func() {
		_ = srv.Serve(l)
	}()
	t.Cleanup(func() {
		_ = srv.Close()
	})

	return sockPath
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
			name:        "healthy daemon",
			respStatus:  http.StatusOK,
			respBody:    map[string]any{"Version": "28.5.2"},
			wantVersion: "28.5.2",
		},
		{
			name:       "unexpected status",
			respStatus: http.StatusInternalServerError,
			respBody:   map[string]any{"message": "boom"},
			wantErr:    "unexpected status 500",
		},
		{
			name:       "response missing version",
			respStatus: http.StatusOK,
			respBody:   map[string]any{"ApiVersion": "1.51"},
			wantErr:    "has no Version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sockPath := newTestSocket(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/version", r.URL.Path)
				w.WriteHeader(tt.respStatus)
				require.NoError(t, json.NewEncoder(w).Encode(tt.respBody))
			}))

			client := NewHTTPClient(sockPath)

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

func TestHTTPClient_Version_RetriesOnFailure(t *testing.T) {
	var requests atomic.Int32
	sockPath := newTestSocket(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) < maxAttempts {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"Version": "28.5.2"}))
	}))

	client := NewHTTPClient(sockPath)

	version, err := client.Version(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "28.5.2", version)
	assert.EqualValues(t, maxAttempts, requests.Load())
}

func TestHTTPClient_Version_GivesUpAfterMaxAttempts(t *testing.T) {
	var requests atomic.Int32
	sockPath := newTestSocket(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))

	client := NewHTTPClient(sockPath)

	_, err := client.Version(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed after 3 attempts")
	assert.EqualValues(t, maxAttempts, requests.Load())
}

func TestHTTPClient_Version_NoSocket(t *testing.T) {
	client := NewHTTPClient(filepath.Join(t.TempDir(), "does-not-exist.sock"))

	_, err := client.Version(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed after 3 attempts")
}
