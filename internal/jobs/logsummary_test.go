//go:build unit

package jobs

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogSummary_Run(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "syslog"), []byte("hello"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "subdir"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "subdir", "app.log"), []byte("world!"), 0o644))

	job := NewLogSummary(dir)

	err := job.Run(context.Background())

	assert.NoError(t, err)
}

func TestLogSummary_Run_MissingDir(t *testing.T) {
	job := NewLogSummary(filepath.Join(t.TempDir(), "does-not-exist"))

	err := job.Run(context.Background())

	assert.Error(t, err)
}

func TestLogSummary_Run_RespectsCancellation(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "syslog"), []byte("hello"), 0o644))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	job := NewLogSummary(dir)

	err := job.Run(ctx)

	assert.ErrorIs(t, err, context.Canceled)
}
