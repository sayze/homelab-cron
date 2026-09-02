//go:build unit

package cron

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"homelab-cron/internal/mailer"
)

// testJob is a minimal Job implementation for exercising the scheduler
// without waiting on real cron ticks.
type testJob struct {
	name         string
	schedule     string
	run          func(ctx context.Context) error
	alerting     bool
	emailContent string
}

func (j testJob) Name() string                  { return j.name }
func (j testJob) Schedule() string              { return j.schedule }
func (j testJob) Run(ctx context.Context) error { return j.run(ctx) }
func (j testJob) AlertingEnabled() bool         { return j.alerting }
func (j testJob) EmailContent() string          { return j.emailContent }

// recordingMailer captures every Send call for assertions, instead of
// actually sending anything.
type recordingMailer struct {
	mu      sync.Mutex
	sent    []sentEmail
	sendErr error
}

type sentEmail struct {
	subject string
	body    string
}

func (m *recordingMailer) Send(_ context.Context, subject, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, sentEmail{subject: subject, body: body})
	return m.sendErr
}

func noopRun(context.Context) error { return nil }

func TestNew(t *testing.T) {
	tests := []struct {
		name     string
		schedule string
		wantErr  bool
	}{
		{"invalid schedule", "not-a-cron", true},
		{"valid schedule", "0 0 1 1 *", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := New(mailer.Noop{}, testJob{name: "job", schedule: tt.schedule, run: noopRun})

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			s.Start()
			s.Stop() // must return promptly; no job is running yet
		})
	}
}

func TestRunJob_RecoversPanic(t *testing.T) {
	j := testJob{name: "panics", run: func(context.Context) error { panic("boom") }}

	assert.NotPanics(t, func() {
		runJob(context.Background(), mailer.Noop{}, j)
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

	runJob(ctx, mailer.Noop{}, j)
	assert.True(t, sawCancel.Load())
}

func TestRunJob_Alerting(t *testing.T) {
	tests := []struct {
		name         string
		alerting     bool
		emailContent string
		runErr       error
		wantSent     bool
	}{
		{"sends alert when enabled", true, "something happened", nil, true},
		{"sends alert on job error", true, "it broke", assert.AnError, true},
		{"no alert when disabled", false, "", nil, false},
		{"no alert when email content empty", true, "", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &recordingMailer{}
			j := testJob{
				name:         "job-under-test",
				alerting:     tt.alerting,
				emailContent: tt.emailContent,
				run:          func(context.Context) error { return tt.runErr },
			}

			runJob(context.Background(), m, j)

			if !tt.wantSent {
				assert.Empty(t, m.sent)
				return
			}

			require.Len(t, m.sent, 1)
			assert.Contains(t, m.sent[0].subject, j.name)
			assert.Equal(t, tt.emailContent, m.sent[0].body)
		})
	}
}
