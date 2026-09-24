// Package docker is a minimal read-only client for the Docker Engine API.
// internal/jobs.VersionCheck uses it to read the Docker daemon's own
// actually-deployed version.
package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"homelab-cron/internal/logging"
)

// maxAttempts and retryDelay bound Version's retries against transient
// daemon/socket failures: up to 3 attempts, 1s apart. retryDelay is a var so
// tests can shrink it.
const maxAttempts = 3

var retryDelay = time.Second

// Client reads the local Docker daemon's own version. HTTPClient is the
// concrete implementation, backed by the Docker Engine API over its Unix
// socket; tests fake this interface directly rather than standing up a real
// daemon.
type Client interface {
	// Version returns the Docker Engine's own version, read from the
	// "Version" key of GET /version's response body.
	Version(ctx context.Context) (string, error)
}

// HTTPClient is a Client backed by the Docker Engine API
// (https://docs.docker.com/engine/api/), reached over a Unix socket rather
// than TCP — dockerd doesn't listen on TCP by default, only the socket at
// sockPath (homelab-cron's own DOCKER_SOCK config, see internal/config).
// The request URL's host is a dummy value: DialContext ignores it and always
// connects to sockPath.
type HTTPClient struct {
	client *http.Client
}

// NewHTTPClient builds an HTTPClient that talks to the Docker Engine API
// over the Unix socket at sockPath (e.g. "/var/run/docker.sock" —
// homelab-cron's own DOCKER_SOCK config).
func NewHTTPClient(sockPath string) *HTTPClient {
	return &HTTPClient{
		client: &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", sockPath)
				},
			},
			Timeout: 10 * time.Second,
		},
	}
}

// Version implements Client by querying the Docker Engine API's /version
// endpoint and reading "Version" off the response body, retrying up to
// maxAttempts times on failure.
func (c *HTTPClient) Version(ctx context.Context) (string, error) {
	var err error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		var version string
		if version, err = c.version(ctx); err == nil {
			return version, nil
		}
		if attempt == maxAttempts {
			break
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(retryDelay):
		}
	}
	return "", fmt.Errorf("docker: query version failed after %d attempts: %w", maxAttempts, err)
}

func (c *HTTPClient) version(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/version", nil)
	if err != nil {
		return "", fmt.Errorf("docker: build request for version: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("docker: query version: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			logging.Error("closing docker response body", "endpoint", "version", "error", cerr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("docker: unexpected status %d querying version", resp.StatusCode)
	}

	var body struct {
		Version string `json:"Version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("docker: decode version response: %w", err)
	}
	if body.Version == "" {
		return "", fmt.Errorf("docker: version response has no Version")
	}
	return body.Version, nil
}
