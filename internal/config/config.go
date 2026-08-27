package config

import (
	"os"
	"strings"
)

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
}

func Load() Config {
	return Config{
		Addr:           getEnv("ADDR", ":8080"),
		HostRoot:       getEnv("HOST_ROOT", "/host"),
		AlertEmailFrom: os.Getenv("ALERT_EMAIL_FROM"),
		AlertEmailTo:   getEnvList("ALERT_EMAIL_TO"),
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
