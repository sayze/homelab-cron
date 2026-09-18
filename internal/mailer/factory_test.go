//go:build unit

package mailer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNew_NoopWhenAlertingUnconfigured(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{"both unset", Config{}},
		{"only from set", Config{From: "cron@example.com"}},
		{"only to set", Config{To: []string{"you@example.com"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := New(context.Background(), tt.cfg)
			assert.NoError(t, err)
			assert.IsType(t, Noop{}, m)
		})
	}
}
