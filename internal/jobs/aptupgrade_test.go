//go:build unit

package jobs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestAptUpgradeCheck_Run_Fresh(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.log")
	assert.NoError(t, os.WriteFile(path, nil, 0o644))

	job := NewAptUpgradeCheck(path)

	err := job.Run(context.Background())

	assert.NoError(t, err)
}

func TestAptUpgradeCheck_Run_Stale(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.log")
	assert.NoError(t, os.WriteFile(path, nil, 0o644))
	stale := time.Now().Add(-8 * 24 * time.Hour)
	assert.NoError(t, os.Chtimes(path, stale, stale))

	job := NewAptUpgradeCheck(path)

	err := job.Run(context.Background())

	assert.NoError(t, err)
}

func TestAptUpgradeCheck_Run_Missing(t *testing.T) {
	job := NewAptUpgradeCheck(filepath.Join(t.TempDir(), "does-not-exist.log"))

	err := job.Run(context.Background())

	assert.NoError(t, err)
}
