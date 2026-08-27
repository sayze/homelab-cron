//go:build unit

package jobs

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDiskUsage_Run(t *testing.T) {
	job := NewDiskUsage(t.TempDir())

	err := job.Run(context.Background())

	assert.NoError(t, err)
}

func TestDiskUsage_Run_MissingDir(t *testing.T) {
	job := NewDiskUsage(filepath.Join(t.TempDir(), "does-not-exist"))

	err := job.Run(context.Background())

	assert.Error(t, err)
}
