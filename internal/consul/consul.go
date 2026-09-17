// Package consul is a minimal read-only client for Consul's HTTP catalog
// API. internal/jobs.WebstackVersionCheck uses it to read a cluster
// service's actually-deployed version from Consul service meta.
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

// Client reads a Consul-registered service's deployed version. HTTPClient
// is the concrete implementation, backed by Consul's HTTP API; tests fake
// this interface directly rather than standing up a Consul server.
type Client interface {
	// Version returns the deployed version of service, read from the
	// "version" key of its Consul service meta on one healthy instance.
	// Returns an error if the service has no healthy instance registered,
	// or that instance's meta has no "version" key.
	Version(ctx context.Context, service string) (string, error)
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
	var err error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		var version string
		if version, err = c.version(ctx, service); err == nil {
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
	return "", fmt.Errorf("consul: query service %q failed after %d attempts: %w", service, maxAttempts, err)
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
