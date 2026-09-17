//go:build unit

package jobs

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWebstackVersionCheck_Run(t *testing.T) {
	tests := []struct {
		name        string
		deps        []dependency
		wantContent bool
		wantSubstr  []string
	}{
		{
			name: "up to date",
			deps: []dependency{
				{name: "Consul", currentVersion: "2.0.4", fetchLatest: fakeLatest("2.0.4", nil)},
			},
			wantContent: false,
		},
		{
			name: "major version behind",
			deps: []dependency{
				{name: "Consul", currentVersion: "1.22.2", fetchLatest: fakeLatest("2.0.4", nil)},
			},
			wantContent: true,
			wantSubstr:  []string{"Consul", "1.22.2", "2.0.4"},
		},
		{
			name: "minor/patch behind is not alerted",
			deps: []dependency{
				{name: "Nomad", currentVersion: "1.8.4", fetchLatest: fakeLatest("1.9.0", nil)},
			},
			wantContent: false,
		},
		{
			name: "docker apt version format compares correctly",
			deps: []dependency{
				{name: "Docker", currentVersion: "5:28.5.2-1~ubuntu.24.04~noble", fetchLatest: fakeLatest("v29.8.1", nil)},
			},
			wantContent: true,
			wantSubstr:  []string{"Docker"},
		},
		{
			name: "fetch failure is reported, not fatal",
			deps: []dependency{
				{name: "Vault", currentVersion: "1.18.3", fetchLatest: fakeLatest("", errors.New("boom"))},
			},
			wantContent: true,
			wantSubstr:  []string{"Vault", "could not check"},
		},
		{
			name: "only the dependency that's behind is reported",
			deps: []dependency{
				{name: "Consul", currentVersion: "2.0.4", fetchLatest: fakeLatest("2.0.4", nil)},
				{name: "Nomad", currentVersion: "1.8.4", fetchLatest: fakeLatest("2.0.6", nil)},
			},
			wantContent: true,
			wantSubstr:  []string{"Nomad"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := newWebstackVersionCheck(tt.deps)

			err := job.Run(context.Background())

			assert.NoError(t, err)
			assert.True(t, job.AlertingEnabled())
			if tt.wantContent {
				assert.NotEmpty(t, job.EmailContent())
				for _, substr := range tt.wantSubstr {
					assert.Contains(t, job.EmailContent(), substr)
				}
			} else {
				assert.Empty(t, job.EmailContent())
			}
		})
	}
}

func TestWebstackVersionCheck_Run_DoesNotLeakPreviousAlert(t *testing.T) {
	deps := []dependency{
		{name: "Consul", currentVersion: "1.22.2", fetchLatest: fakeLatest("2.0.4", nil)},
	}
	job := newWebstackVersionCheck(deps)

	assert.NoError(t, job.Run(context.Background()))
	assert.NotEmpty(t, job.EmailContent())

	job.deps[0].currentVersion = "2.0.4"
	assert.NoError(t, job.Run(context.Background()))
	assert.Empty(t, job.EmailContent())
}

func TestMajorVersion(t *testing.T) {
	tests := []struct {
		in      string
		want    int
		wantErr bool
	}{
		{in: "1.22.2", want: 1},
		{in: "2.0.4", want: 2},
		{in: "v29.8.1", want: 29},
		{in: "5:28.5.2-1~ubuntu.24.04~noble", want: 28},
		{in: "not-a-version", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := majorVersion(tt.in)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func fakeLatest(version string, err error) func(context.Context) (string, error) {
	return func(context.Context) (string, error) {
		return version, err
	}
}
