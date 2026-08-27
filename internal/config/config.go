package config

import "os"

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
}

func Load() Config {
	return Config{
		Addr:     getEnv("ADDR", ":8080"),
		HostRoot: getEnv("HOST_ROOT", "/host"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
