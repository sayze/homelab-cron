//go:build unit

package jobs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAptUpgradeCheck_Run(t *testing.T) {
	tests := []struct {
		name          string
		setup         func(t *testing.T) string // returns the log path to check
		wantContent   bool
		wantPathInMsg bool
	}{
		{
			name: "fresh log",
			setup: func(t *testing.T) string {
				path := filepath.Join(t.TempDir(), "upgrade.log")
				require.NoError(t, os.WriteFile(path, nil, 0o644))
				return path
			},
			wantContent: false,
		},
		{
			name: "stale log",
			setup: func(t *testing.T) string {
				path := filepath.Join(t.TempDir(), "upgrade.log")
				require.NoError(t, os.WriteFile(path, nil, 0o644))
				stale := time.Now().Add(-8 * 24 * time.Hour)
				require.NoError(t, os.Chtimes(path, stale, stale))
				return path
			},
			wantContent:   true,
			wantPathInMsg: true,
		},
		{
			name: "missing log",
			setup: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "does-not-exist.log")
			},
			wantContent: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := tt.setup(t)
			job := NewAptUpgradeCheck(path)

			err := job.Run(context.Background())

			assert.NoError(t, err)
			assert.True(t, job.AlertingEnabled())
			if tt.wantContent {
				assert.NotEmpty(t, job.EmailContent())
			} else {
				assert.Empty(t, job.EmailContent())
			}
			if tt.wantPathInMsg {
				assert.Contains(t, job.EmailContent(), path)
			}
		})
	}
}

func TestAptUpgradeCheck_Run_ClearsPreviousAlert(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.log")
	assert.NoError(t, os.WriteFile(path, nil, 0o644))
	stale := time.Now().Add(-8 * 24 * time.Hour)
	assert.NoError(t, os.Chtimes(path, stale, stale))

	job := NewAptUpgradeCheck(path)
	assert.NoError(t, job.Run(context.Background()))
	require.NotEmpty(t, job.EmailContent())

	fresh := time.Now()
	assert.NoError(t, os.Chtimes(path, fresh, fresh))
	assert.NoError(t, job.Run(context.Background()))

	assert.Empty(t, job.EmailContent())
}
