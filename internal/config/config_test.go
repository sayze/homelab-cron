//go:build unit

package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name          string
		env           map[string]string
		wantAddr      string
		wantHostRoot  string
		wantAlertFrom string
		wantAlertTo   []string
	}{
		{
			name:          "defaults",
			env:           nil,
			wantAddr:      ":8080",
			wantHostRoot:  "/host",
			wantAlertFrom: "",
			wantAlertTo:   nil,
		},
		{
			name: "overrides",
			env: map[string]string{
				"ADDR":             ":9090",
				"HOST_ROOT":        "/mnt/host",
				"ALERT_EMAIL_FROM": "cron@example.com",
				"ALERT_EMAIL_TO":   "alerts@example.com, oncall@example.com",
			},
			wantAddr:      ":9090",
			wantHostRoot:  "/mnt/host",
			wantAlertFrom: "cron@example.com",
			wantAlertTo:   []string{"alerts@example.com", "oncall@example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			cfg := Load()

			assert.Equal(t, tt.wantAddr, cfg.Addr)
			assert.Equal(t, tt.wantHostRoot, cfg.HostRoot)
			assert.Equal(t, tt.wantAlertFrom, cfg.AlertEmailFrom)
			assert.Equal(t, tt.wantAlertTo, cfg.AlertEmailTo)
		})
	}
}
