//go:build unit

package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name           string
		env            map[string]string
		wantAddr       string
		wantHostRoot   string
		wantAlertFrom  string
		wantAlertTo    []string
		wantConsulAddr string
		wantVaultAddr  string
	}{
		{
			name:           "defaults",
			env:            nil,
			wantAddr:       ":8080",
			wantHostRoot:   "/host",
			wantAlertFrom:  "",
			wantAlertTo:    nil,
			wantConsulAddr: "http://127.0.0.1:8500",
			wantVaultAddr:  "http://127.0.0.1:8200",
		},
		{
			name: "overrides",
			env: map[string]string{
				"ADDR":             ":9090",
				"HOST_ROOT":        "/mnt/host",
				"ALERT_EMAIL_FROM": "cron@example.com",
				"ALERT_EMAIL_TO":   "alerts@example.com, oncall@example.com",
				"CONSUL_ADDR":      "http://consul.service.consul:8500",
				"VAULT_ADDR":       "http://vault.service.consul:8200",
			},
			wantAddr:       ":9090",
			wantHostRoot:   "/mnt/host",
			wantAlertFrom:  "cron@example.com",
			wantAlertTo:    []string{"alerts@example.com", "oncall@example.com"},
			wantConsulAddr: "http://consul.service.consul:8500",
			wantVaultAddr:  "http://vault.service.consul:8200",
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
			assert.Equal(t, tt.wantConsulAddr, cfg.ConsulAddr)
			assert.Equal(t, tt.wantVaultAddr, cfg.VaultAddr)
		})
	}
}
