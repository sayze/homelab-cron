// Package config loads homelab-cron's configuration from environment
// variables.
package config

import (
	"os"
	"strings"
)

// Config holds homelab-cron's runtime configuration, loaded from
// environment variables by Load.
type Config struct {
	// Addr is the listen address for the /health HTTP server.
	Addr string

	// HostRoot is the path (inside the container) where the host's root
	// filesystem is mounted, read-only. In production this is a raw Docker
	// bind mount (see homelab-cron.nomad.hcl: "/:/host:ro,rslave", the same
	// pattern jobs/newrelic.nomad.hcl uses); in docker-compose it's a bind
	// mount too. Jobs that need to observe host state should read under
	// this path (e.g. filepath.Join(cfg.HostRoot, "var/log")) — this
	// service only ever reads the host, never writes to it.
	HostRoot string

	// AlertEmailFrom is the SES-verified sender address used for job alert
	// emails. AlertEmailTo is the list of recipient addresses. Both must
	// be set for alerting to be enabled — otherwise main wires up a no-op
	// mailer and alerting jobs just log instead of sending. AWS
	// credentials and region (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY,
	// AWS_REGION, ...) are read directly by the AWS SDK's own default env
	// chain (see internal/mailer.NewSES) rather than duplicated here.
	AlertEmailFrom string
	AlertEmailTo   []string

	ConsulAddr string
	VaultAddr  string

	// DockerSock is the path (inside the container) to the Docker Engine
	// API's Unix socket, used by internal/docker.HTTPClient to read the
	// local daemon's own deployed version (see
	// internal/jobs/webstackversioncheck.go). Unlike ConsulAddr/VaultAddr,
	// this isn't reachable via host networking alone — dockerd doesn't
	// listen on TCP by default — so the socket itself must be bind-mounted
	// into the container (see homelab-cron.nomad.hcl).
	DockerSock string
}

// Load reads homelab-cron's configuration from environment variables,
// applying defaults for anything unset.
func Load() Config {
	return Config{
		Addr:           getEnv("ADDR", ":8080"),
		HostRoot:       getEnv("HOST_ROOT", "/host"),
		AlertEmailFrom: os.Getenv("ALERT_EMAIL_FROM"),
		AlertEmailTo:   getEnvList("ALERT_EMAIL_TO"),
		ConsulAddr:     getEnv("CONSUL_ADDR", "http://127.0.0.1:8500"),
		VaultAddr:      getEnv("VAULT_ADDR", "http://127.0.0.1:8200"),
		DockerSock:     getEnv("DOCKER_SOCK", "/var/run/docker.sock"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// getEnvList parses a comma-separated env var into a trimmed, non-empty
// slice. Returns nil if the var is unset or empty.
func getEnvList(key string) []string {
	raw := os.Getenv(key)
	if raw == "" {
		return nil
	}

	var out []string
	for _, part := range strings.Split(raw, ",") {
		if v := strings.TrimSpace(part); v != "" {
			out = append(out, v)
		}
	}
	return out
}
