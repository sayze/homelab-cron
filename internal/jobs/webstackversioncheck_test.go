//go:build unit

package jobs

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"homelab-cron/internal/consul"
	"homelab-cron/internal/vault"
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
			name: "postgres bare major version format compares correctly",
			deps: []dependency{
				{name: "PostgreSQL", currentVersion: "16", fetchLatest: fakeLatest("18", nil)},
			},
			wantContent: true,
			wantSubstr:  []string{"PostgreSQL"},
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
		{
			name: "current version sourced from consul, major version behind",
			deps: []dependency{
				{name: "Traefik", fetchCurrent: fakeCurrent("3.6.1", nil), fetchLatest: fakeLatest("4.0.0", nil)},
			},
			wantContent: true,
			wantSubstr:  []string{"Traefik", "3.6.1", "4.0.0"},
		},
		{
			name: "current version sourced from consul, up to date",
			deps: []dependency{
				{name: "PostgreSQL", fetchCurrent: fakeCurrent("18", nil), fetchLatest: fakeLatest("18", nil)},
			},
			wantContent: false,
		},
		{
			name: "consul fetch failure is reported, not fatal",
			deps: []dependency{
				{name: "New Relic Infrastructure", fetchCurrent: fakeCurrent("", errors.New(`consul: no healthy instance of service "newrelic" registered`)), fetchLatest: fakeLatest("1.80.3", nil)},
			},
			wantContent: true,
			wantSubstr:  []string{"New Relic Infrastructure", "could not check current version", "no healthy instance"},
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
		{in: "16", want: 16},
		{in: "18", want: 18},
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

func TestConsulCurrent(t *testing.T) {
	t.Run("returns the version consul reports for the service", func(t *testing.T) {
		client := fakeConsulClient{"traefik": {version: "3.6.1"}}

		got, err := consulCurrent(client, "traefik")(context.Background())

		assert.NoError(t, err)
		assert.Equal(t, "3.6.1", got)
	})

	t.Run("propagates a consul error", func(t *testing.T) {
		client := fakeConsulClient{"newrelic": {err: errors.New("boom")}}

		_, err := consulCurrent(client, "newrelic")(context.Background())

		assert.ErrorContains(t, err, "boom")
	})
}

func TestVaultCurrent(t *testing.T) {
	t.Run("returns the version vault reports for itself", func(t *testing.T) {
		client := fakeVaultClient{version: "1.21.4"}

		got, err := vaultCurrent(client)(context.Background())

		assert.NoError(t, err)
		assert.Equal(t, "1.21.4", got)
	})

	t.Run("propagates a vault error", func(t *testing.T) {
		client := fakeVaultClient{err: errors.New("boom")}

		_, err := vaultCurrent(client)(context.Background())

		assert.ErrorContains(t, err, "boom")
	})
}

// fakeVaultClient is a vault.Client fake, used to test vaultCurrent without
// a real Vault server.
type fakeVaultClient struct {
	version string
	err     error
}

func (f fakeVaultClient) Version(context.Context) (string, error) {
	return f.version, f.err
}

var _ vault.Client = fakeVaultClient{}

// fakeCurrent returns a fetchCurrent func for use in dependency structs in
// tests, mirroring fakeLatest below.
func fakeCurrent(version string, err error) func(context.Context) (string, error) {
	return func(context.Context) (string, error) {
		return version, err
	}
}

// fakeConsulClient is a consul.Client fake keyed by service name, used to
// test consulCurrent without a real Consul server.
type fakeConsulClient map[string]struct {
	version string
	err     error
}

func (f fakeConsulClient) Version(_ context.Context, service string) (string, error) {
	entry, ok := f[service]
	if !ok {
		return "", errors.New("fakeConsulClient: no entry for " + service)
	}
	return entry.version, entry.err
}

var _ consul.Client = fakeConsulClient{}

func fakeLatest(version string, err error) func(context.Context) (string, error) {
	return func(context.Context) (string, error) {
		return version, err
	}
}
