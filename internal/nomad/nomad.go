// Package nomad is a minimal read-only client for Nomad's HTTP API.
// internal/jobs.VersionCheck uses it to read Nomad's own
// actually-deployed version.
package nomad

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"homelab-cron/internal/logging"
)

var log = logging.New("nomad")

// maxAttempts and retryDelay bound Version's retries against transient
// Nomad/network failures: up to 3 attempts, 1s apart. retryDelay is a var
// so tests can shrink it.
const maxAttempts = 3

var retryDelay = time.Second

// Client reads Nomad's own deployed version. HTTPClient is the concrete
// implementation, backed by Nomad's HTTP API; tests fake this interface
// directly rather than standing up a Nomad agent.
type Client interface {
	// Version returns this Nomad agent's own version, read from the
	// nested "Version" key of GET /v1/agent/self's Config.Version object.
	Version(ctx context.Context) (string, error)
}

// HTTPClient is a Client backed by Nomad's HTTP agent API
// (https://developer.hashicorp.com/nomad/api-docs/agent#read-agent-configuration).
// Unlike internal/consul and internal/vault's equivalent endpoints, Nomad's
// requires an ACL token with at least agent:read once ACLs are enabled.
type HTTPClient struct {
	addr   string
	token  string
	client *http.Client
}

// NewHTTPClient builds an HTTPClient against Nomad's HTTP API at addr (e.g.
// "http://127.0.0.1:4646" — homelab-cron's own NOMAD_ADDR config),
// authenticating every request with token (homelab-cron's own NOMAD_TOKEN
// config) via Nomad's X-Nomad-Token header.
func NewHTTPClient(addr, token string, client *http.Client) *HTTPClient {
	return &HTTPClient{addr: strings.TrimRight(addr, "/"), token: token, client: client}
}

// Version implements Client by querying Nomad's own /v1/agent/self endpoint
// and reading Config.Version.Version off the response, retrying up to
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
	return "", fmt.Errorf("nomad: query agent self failed after %d attempts: %w", maxAttempts, err)
}

// agentSelfResponse is the subset of Nomad's /v1/agent/self response this
// package needs. Nomad's "config" object serializes its Go struct directly
// (PascalCase keys), unlike "stats", whose metrics are snake_case and don't
// include a version at all — stats.nomad only has server/leader/bootstrap/
// known_regions fields. Unlike Consul's identically-shaped endpoint, where
// Config.Version is a bare string, Nomad's Config.Version is itself a
// version.VersionInfo object (Version/Revision/VersionPrerelease/
// VersionMetadata) — this package only needs the nested Version field.
type agentSelfResponse struct {
	Config struct {
		Version struct {
			Version string `json:"Version"`
		} `json:"Version"`
	} `json:"config"`
}

func (c *HTTPClient) version(ctx context.Context) (string, error) {
	url := fmt.Sprintf("%s/v1/agent/self", c.addr)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("nomad: build request for agent self: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Nomad-Token", c.token)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("nomad: query agent self: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			log.Error("closing response body", "endpoint", "agent/self", "error", cerr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("nomad: unexpected status %d querying agent self", resp.StatusCode)
	}

	var body agentSelfResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("nomad: decode agent self response: %w", err)
	}
	if body.Config.Version.Version == "" {
		return "", fmt.Errorf("nomad: agent self response has no config.Version.Version")
	}
	return body.Config.Version.Version, nil
}
