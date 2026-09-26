// Package vault is a minimal read-only client for Vault's HTTP health API.
// internal/jobs.VersionCheck uses it to read Vault's own
// actually-deployed version.
package vault

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sayze/homelab-utils/logger"
)

// maxAttempts and retryDelay bound Version's retries against transient
// Vault/network failures: up to 3 attempts, 1s apart. retryDelay is a var
// so tests can shrink it.
const maxAttempts = 3

var retryDelay = time.Second

// Client reads Vault's own deployed version. HTTPClient is the concrete
// implementation, backed by Vault's HTTP API; tests fake this interface
// directly rather than standing up a Vault server.
type Client interface {
	// Version returns Vault's own version, read from the "version" key of
	// GET /v1/sys/health's response body.
	Version(ctx context.Context) (string, error)
}

// HTTPClient is a Client backed by Vault's HTTP health API
// (https://developer.hashicorp.com/vault/api-docs/system/health).
type HTTPClient struct {
	addr   string
	client *http.Client
}

// NewHTTPClient builds an HTTPClient against Vault's HTTP API at addr
// (e.g. "http://127.0.0.1:8200" — homelab-cron's own VAULT_ADDR config).
func NewHTTPClient(addr string, client *http.Client) *HTTPClient {
	return &HTTPClient{addr: strings.TrimRight(addr, "/"), client: client}
}

// Version implements Client by querying Vault's health endpoint and reading
// "version" off the response body, retrying up to maxAttempts times on
// failure.
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
	return "", fmt.Errorf("vault: query sys/health failed after %d attempts: %w", maxAttempts, err)
}

func (c *HTTPClient) version(ctx context.Context) (string, error) {
	url := fmt.Sprintf("%s/v1/sys/health", c.addr)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("vault: build request for sys/health: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("vault: query sys/health: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			logger.Error("closing vault response body", "endpoint", "sys/health", "error", cerr)
		}
	}()

	// Unlike Consul's endpoints, Vault's sys/health responds with a status
	// code that varies with seal/standby state (e.g. 503 sealed, 429
	// standby) rather than always 200 — but its JSON body, including
	// "version", is populated regardless, so a non-200 status here isn't
	// itself treated as a failure.
	var body struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("vault: decode sys/health response (status %d): %w", resp.StatusCode, err)
	}
	if body.Version == "" {
		return "", fmt.Errorf("vault: sys/health response (status %d) has no version", resp.StatusCode)
	}
	return body.Version, nil
}
