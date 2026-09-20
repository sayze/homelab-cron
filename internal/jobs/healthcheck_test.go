//go:build unit

package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakePostgres struct {
	err error

	gotDeadline bool
}

func (f *fakePostgres) Ping(ctx context.Context) error {
	_, f.gotDeadline = ctx.Deadline()
	return f.err
}

func TestHealthCheck_Metadata(t *testing.T) {
	job := NewHealthCheck(&fakePostgres{})

	assert.Equal(t, "health-check", job.Name())
	assert.Equal(t, HealthCheckJobName, job.Name())
	assert.Equal(t, "* * * * *", job.Schedule())
	assert.False(t, job.AlertingEnabled())
	assert.Empty(t, job.EmailContent())
}

func TestHealthCheck_Run_Healthy(t *testing.T) {
	pg := &fakePostgres{}

	err := NewHealthCheck(pg).Run(context.Background())

	assert.NoError(t, err)
}

func TestHealthCheck_Run_PostgresDown(t *testing.T) {
	pg := &fakePostgres{err: errors.New("connection refused")}

	err := NewHealthCheck(pg).Run(context.Background())

	require.Error(t, err)
	assert.ErrorIs(t, err, pg.err)
	assert.Contains(t, err.Error(), "postgres")
}

func TestHealthCheck_Run_BoundsEachCheckWithATimeout(t *testing.T) {
	pg := &fakePostgres{}

	require.NoError(t, NewHealthCheck(pg).Run(context.Background()))

	assert.True(t, pg.gotDeadline, "check should run under a timeout so a hung dependency can't stall the job")
}

func TestHealthCheck_Run_RunsEveryCheckAndJoinsFailures(t *testing.T) {
	errA, errB := errors.New("a is down"), errors.New("b is down")
	var ranB bool
	job := &HealthCheck{checks: []healthCheckEntry{
		{name: "a", run: func(context.Context) error { return errA }},
		{name: "b", run: func(context.Context) error { ranB = true; return errB }},
	}}

	err := job.Run(context.Background())

	assert.True(t, ranB, "a failing check must not stop later checks")
	assert.ErrorIs(t, err, errA)
	assert.ErrorIs(t, err, errB)
}

func TestHealthCheck_Run_CheckTimeoutIsEnforced(t *testing.T) {
	orig := checkTimeout
	checkTimeout = 50 * time.Millisecond
	t.Cleanup(func() { checkTimeout = orig })

	job := &HealthCheck{checks: []healthCheckEntry{
		{name: "hung", run: func(ctx context.Context) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
				return nil
			}
		}},
	}}

	start := time.Now()
	err := job.Run(context.Background())

	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), 2*time.Second)
}
