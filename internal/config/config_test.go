//go:build unit

package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoad_Defaults(t *testing.T) {
	cfg := Load()

	assert.Equal(t, ":8080", cfg.Addr)
	assert.Equal(t, "/host", cfg.HostRoot)
	assert.Equal(t, "", cfg.AlertEmailFrom)
	assert.Nil(t, cfg.AlertEmailTo)
}

func TestLoad_Overrides(t *testing.T) {
	t.Setenv("ADDR", ":9090")
	t.Setenv("HOST_ROOT", "/mnt/host")
	t.Setenv("ALERT_EMAIL_FROM", "cron@example.com")
	t.Setenv("ALERT_EMAIL_TO", "alerts@example.com, oncall@example.com")

	cfg := Load()

	assert.Equal(t, ":9090", cfg.Addr)
	assert.Equal(t, "/mnt/host", cfg.HostRoot)
	assert.Equal(t, "cron@example.com", cfg.AlertEmailFrom)
	assert.Equal(t, []string{"alerts@example.com", "oncall@example.com"}, cfg.AlertEmailTo)
}
