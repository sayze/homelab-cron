//go:build unit

package cron

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testJob is a minimal Job implementation for exercising the scheduler
// without waiting on real cron ticks.
type testJob struct {
	name     string
	schedule string
	run      func(ctx context.Context) error
}

func (j testJob) Name() string                  { return j.name }
func (j testJob) Schedule() string              { return j.schedule }
func (j testJob) Run(ctx context.Context) error { return j.run(ctx) }
func (j testJob) AlertingEnabled() bool         { return false }
func (j testJob) EmailContent() string          { return "" }

func TestNew_InvalidSchedule(t *testing.T) {
	_, err := New(testJob{name: "bad", schedule: "not-a-cron", run: func(context.Context) error { return nil }})
	assert.Error(t, err)
}

func TestNew_ValidSchedule_StartStop(t *testing.T) {
	s, err := New(testJob{name: "ok", schedule: "0 0 1 1 *", run: func(context.Context) error { return nil }})
	require.NoError(t, err)

	s.Start()
	s.Stop() // must return promptly; no job is running yet
}

func TestRunJob_RecoversPanic(t *testing.T) {
	j := testJob{name: "panics", run: func(context.Context) error { panic("boom") }}

	assert.NotPanics(t, func() {
		runJob(context.Background(), j)
	})
}

func TestRunJob_PassesContextThrough(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var sawCancel atomic.Bool
	j := testJob{name: "ctx-aware", run: func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			sawCancel.Store(true)
		case <-time.After(time.Second):
		}
		return nil
	}}

	runJob(ctx, j)
	assert.True(t, sawCancel.Load())
}
