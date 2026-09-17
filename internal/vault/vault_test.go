//go:build unit

package vault

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
			name:        "healthy active node",
			respStatus:  http.StatusOK,
			respBody:    map[string]any{"version": "1.21.4"},
			wantVersion: "1.21.4",
		},
		{
			name:        "sealed node still reports version",
			respStatus:  http.StatusServiceUnavailable,
			respBody:    map[string]any{"sealed": true, "version": "1.21.4"},
			wantVersion: "1.21.4",
		},
		{
			name:        "standby node still reports version",
			respStatus:  http.StatusTooManyRequests,
			respBody:    map[string]any{"standby": true, "version": "1.21.4"},
			wantVersion: "1.21.4",
		},
		{
			name:       "response missing version",
			respStatus: http.StatusOK,
			respBody:   map[string]any{"sealed": false},
			wantErr:    "has no version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/v1/sys/health", r.URL.Path)
				w.WriteHeader(tt.respStatus)
				require.NoError(t, json.NewEncoder(w).Encode(tt.respBody))
			}))
			defer srv.Close()

			client := NewHTTPClient(srv.URL, srv.Client())

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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A non-200 status doesn't trigger a retry here (Vault's body is
		// valid JSON in every documented health state), so simulate a
		// retryable failure with a malformed body instead.
		if requests.Add(1) < maxAttempts {
			_, _ = w.Write([]byte("not json"))
			return
		}
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"version": "1.21.4"}))
	}))
	defer srv.Close()

	client := NewHTTPClient(srv.URL, srv.Client())

	version, err := client.Version(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "1.21.4", version)
	assert.EqualValues(t, maxAttempts, requests.Load())
}

func TestHTTPClient_Version_GivesUpAfterMaxAttempts(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	client := NewHTTPClient(srv.URL, srv.Client())

	_, err := client.Version(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed after 3 attempts")
	assert.EqualValues(t, maxAttempts, requests.Load())
}

func TestNewHTTPClient_TrimsTrailingSlash(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"version": "1.21.4"}))
	}))
	defer srv.Close()

	client := NewHTTPClient(srv.URL+"/", srv.Client())

	_, err := client.Version(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "/v1/sys/health", gotPath)
}
