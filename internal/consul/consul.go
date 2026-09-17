// Package consul is a minimal read-only client for Consul's HTTP API.
// internal/jobs.WebstackVersionCheck uses it to read a cluster service's
// actually-deployed version from Consul service meta, and Consul's own
// agent version from its health/self-info endpoint.
package consul

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// maxAttempts and retryDelay bound Version's retries against transient
// Consul/network failures: up to 3 attempts, 1s apart. retryDelay is a var
// so tests can shrink it.
const maxAttempts = 3

var retryDelay = time.Second

// Client reads a Consul-registered service's deployed version, or Consul's
// own. HTTPClient is the concrete implementation, backed by Consul's HTTP
// API; tests fake this interface directly rather than standing up a Consul
// server.
type Client interface {
	// Version returns the deployed version of service, read from the
	// "version" key of its Consul service meta on one healthy instance.
	// Returns an error if the service has no healthy instance registered,
	// or that instance's meta has no "version" key.
	Version(ctx context.Context, service string) (string, error)

	// AgentVersion returns this Consul agent's own version, read from
	// GET /v1/agent/self's Config.Version.
	AgentVersion(ctx context.Context) (string, error)
}

// HTTPClient is a Client backed by Consul's HTTP catalog API
// (https://developer.hashicorp.com/consul/api-docs/health).
type HTTPClient struct {
	addr   string
	client *http.Client
}

// NewHTTPClient builds an HTTPClient against Consul's HTTP API at addr
// (e.g. "http://127.0.0.1:8500" — homelab-cron's own CONSUL_ADDR config).
func NewHTTPClient(addr string, client *http.Client) *HTTPClient {
	return &HTTPClient{addr: strings.TrimRight(addr, "/"), client: client}
}

// serviceHealthEntry is the subset of one entry in Consul's
// /v1/health/service/{name} response this package needs.
type serviceHealthEntry struct {
	Service struct {
		Meta map[string]string `json:"Meta"`
	} `json:"Service"`
}

// Version implements Client by querying Consul's health endpoint for
// service's passing instances and reading "version" off the first one's
// meta, retrying up to maxAttempts times on failure.
func (c *HTTPClient) Version(ctx context.Context, service string) (string, error) {
	return retryFetch(ctx, fmt.Sprintf("query service %q", service), func(ctx context.Context) (string, error) {
		return c.version(ctx, service)
	})
}

// AgentVersion implements Client by querying Consul's own /v1/agent/self
// endpoint and reading Config.Version off the response, retrying up to
// maxAttempts times on failure.
func (c *HTTPClient) AgentVersion(ctx context.Context) (string, error) {
	return retryFetch(ctx, "query agent self", c.agentVersion)
}

// retryFetch retries fetch up to maxAttempts times, retryDelay apart, so a
// single transient Consul/network failure doesn't fail the whole job run.
// Returns the last error if every attempt fails.
func retryFetch(ctx context.Context, what string, fetch func(context.Context) (string, error)) (string, error) {
	var err error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		var version string
		if version, err = fetch(ctx); err == nil {
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
	return "", fmt.Errorf("consul: %s failed after %d attempts: %w", what, maxAttempts, err)
}

func (c *HTTPClient) version(ctx context.Context, service string) (string, error) {
	url := fmt.Sprintf("%s/v1/health/service/%s?passing=true", c.addr, service)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("consul: build request for service %q: %w", service, err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("consul: query service %q: %w", service, err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			log.Printf("consul: closing response body for service %q: %v", service, cerr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("consul: unexpected status %d querying service %q", resp.StatusCode, service)
	}

	var entries []serviceHealthEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return "", fmt.Errorf("consul: decode response for service %q: %w", service, err)
	}
	if len(entries) == 0 {
		return "", fmt.Errorf("consul: no healthy instance of service %q registered", service)
	}

	version, ok := entries[0].Service.Meta["version"]
	if !ok || version == "" {
		return "", fmt.Errorf("consul: service %q has no %q meta", service, "version")
	}
	return version, nil
}

// agentSelf is the subset of Consul's /v1/agent/self response this package
// needs.
type agentSelf struct {
	Config struct {
		Version string `json:"Version"`
	} `json:"Config"`
}

func (c *HTTPClient) agentVersion(ctx context.Context) (string, error) {
	url := fmt.Sprintf("%s/v1/agent/self", c.addr)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("consul: build request for agent self: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("consul: query agent self: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			log.Printf("consul: closing response body for agent self: %v", cerr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("consul: unexpected status %d querying agent self", resp.StatusCode)
	}

	var body agentSelf
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("consul: decode agent self response: %w", err)
	}
	if body.Config.Version == "" {
		return "", fmt.Errorf("consul: agent self response has no Config.Version")
	}
	return body.Config.Version, nil
}
