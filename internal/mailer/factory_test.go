//go:build unit

package mailer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"homelab-cron/internal/config"
)

func TestNew_NoopWhenAlertingUnconfigured(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.Config
	}{
		{"both unset", config.Config{}},
		{"only from set", config.Config{AlertEmailFrom: "cron@example.com"}},
		{"only to set", config.Config{AlertEmailTo: []string{"you@example.com"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := New(context.Background(), tt.cfg)
			assert.NoError(t, err)
			assert.IsType(t, Noop{}, m)
		})
	}
}
