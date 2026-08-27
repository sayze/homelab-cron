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

func TestAptUpgradeCheck_Run_Fresh(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.log")
	assert.NoError(t, os.WriteFile(path, nil, 0o644))

	job := NewAptUpgradeCheck(path)

	err := job.Run(context.Background())

	assert.NoError(t, err)
	assert.True(t, job.AlertingEnabled())
	assert.Empty(t, job.EmailContent())
}

func TestAptUpgradeCheck_Run_Stale(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.log")
	assert.NoError(t, os.WriteFile(path, nil, 0o644))
	stale := time.Now().Add(-8 * 24 * time.Hour)
	assert.NoError(t, os.Chtimes(path, stale, stale))

	job := NewAptUpgradeCheck(path)

	err := job.Run(context.Background())

	assert.NoError(t, err)
	assert.NotEmpty(t, job.EmailContent())
	assert.Contains(t, job.EmailContent(), path)
}

func TestAptUpgradeCheck_Run_Missing(t *testing.T) {
	job := NewAptUpgradeCheck(filepath.Join(t.TempDir(), "does-not-exist.log"))

	err := job.Run(context.Background())

	assert.NoError(t, err)
	assert.NotEmpty(t, job.EmailContent())
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
