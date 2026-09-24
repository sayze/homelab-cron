//go:build unit

package jobs

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/sayze/homelab-cron/internal/consul"
	"github.com/sayze/homelab-cron/internal/docker"
	"github.com/sayze/homelab-cron/internal/nomad"
	"github.com/sayze/homelab-cron/internal/vault"
)

func TestVersionCheck_Run(t *testing.T) {
	tests := []struct {
		name        string
		deps        []dependency
		wantContent bool
		wantSubstr  []string
	}{
		{
			name: "up to date",
			deps: []dependency{
				{name: "Consul", fetchCurrent: fakeCurrent("2.0.4", nil), fetchLatest: fakeLatest("2.0.4", nil)},
			},
			wantContent: false,
		},
		{
			name: "major version behind",
			deps: []dependency{
				{name: "Consul", fetchCurrent: fakeCurrent("1.22.2", nil), fetchLatest: fakeLatest("2.0.4", nil)},
			},
			wantContent: true,
			wantSubstr:  []string{"Consul", "1.22.2", "2.0.4"},
		},
		{
			name: "minor/patch behind under the threshold is not alerted",
			deps: []dependency{
				{name: "Nomad", fetchCurrent: fakeCurrent("1.8.4", nil), fetchLatest: fakeLatest("1.9.0", nil)},
			},
			wantContent: false,
		},
		{
			name: "minor version behind by exactly the threshold is alerted",
			deps: []dependency{
				{name: "Nomad", fetchCurrent: fakeCurrent("1.34.1", nil), fetchLatest: fakeLatest("1.39.0", nil)},
			},
			wantContent: true,
			wantSubstr:  []string{"Nomad", "1.34.1", "1.39.0"},
		},
		{
			name: "minor version behind by one less than the threshold is not alerted",
			deps: []dependency{
				{name: "Nomad", fetchCurrent: fakeCurrent("1.34.1", nil), fetchLatest: fakeLatest("1.38.0", nil)},
			},
			wantContent: false,
		},
		{
			name: "docker apt version format compares correctly",
			deps: []dependency{
				{name: "Docker", fetchCurrent: fakeCurrent("5:28.5.2-1~ubuntu.24.04~noble", nil), fetchLatest: fakeLatest("v29.8.1", nil)},
			},
			wantContent: true,
			wantSubstr:  []string{"Docker"},
		},
		{
			name: "postgres bare major version format compares correctly",
			deps: []dependency{
				{name: "PostgreSQL", fetchCurrent: fakeCurrent("16", nil), fetchLatest: fakeLatest("18", nil)},
			},
			wantContent: true,
			wantSubstr:  []string{"PostgreSQL"},
		},
		{
			name: "fetch failure is reported, not fatal",
			deps: []dependency{
				{name: "Vault", fetchCurrent: fakeCurrent("1.18.3", nil), fetchLatest: fakeLatest("", errors.New("boom"))},
			},
			wantContent: true,
			wantSubstr:  []string{"Vault", "could not check"},
		},
		{
			name: "unparseable current version is reported",
			deps: []dependency{
				{name: "Traefik", fetchCurrent: fakeCurrent("latest", nil), fetchLatest: fakeLatest("v3.7.13", nil)},
			},
			wantContent: true,
			wantSubstr:  []string{"Traefik", "could not parse current version", "latest"},
		},
		{
			name: "unparseable latest version is reported",
			deps: []dependency{
				{name: "Traefik", fetchCurrent: fakeCurrent("v3.7.13", nil), fetchLatest: fakeLatest("nightly", nil)},
			},
			wantContent: true,
			wantSubstr:  []string{"Traefik", "could not parse latest version", "nightly"},
		},
		{
			name: "only the dependency that's behind is reported",
			deps: []dependency{
				{name: "Consul", fetchCurrent: fakeCurrent("2.0.4", nil), fetchLatest: fakeLatest("2.0.4", nil)},
				{name: "Nomad", fetchCurrent: fakeCurrent("1.8.4", nil), fetchLatest: fakeLatest("2.0.6", nil)},
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
		{
			name: "current version sourced from docker, major version behind",
			deps: []dependency{
				{name: "Docker", fetchCurrent: fakeCurrent("28.5.2", nil), fetchLatest: fakeLatest("v29.8.1", nil)},
			},
			wantContent: true,
			wantSubstr:  []string{"Docker", "28.5.2", "v29.8.1"},
		},
		{
			name: "docker fetch failure is reported, not fatal",
			deps: []dependency{
				{name: "Docker", fetchCurrent: fakeCurrent("", errors.New("docker: query version failed after 3 attempts: dial unix: no such file or directory")), fetchLatest: fakeLatest("v29.8.1", nil)},
			},
			wantContent: true,
			wantSubstr:  []string{"Docker", "could not check current version", "no such file or directory"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := newVersionCheck(tt.deps)

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

func TestVersionCheck_Run_DoesNotLeakPreviousAlert(t *testing.T) {
	deps := []dependency{
		{name: "Consul", fetchCurrent: fakeCurrent("1.22.2", nil), fetchLatest: fakeLatest("2.0.4", nil)},
	}
	job := newVersionCheck(deps)

	assert.NoError(t, job.Run(context.Background()))
	assert.NotEmpty(t, job.EmailContent())

	job.deps[0].fetchCurrent = fakeCurrent("2.0.4", nil)
	assert.NoError(t, job.Run(context.Background()))
	assert.Empty(t, job.EmailContent())
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		in        string
		wantMajor int
		wantMinor int
		wantErr   bool
	}{
		{in: "1.22.2", wantMajor: 1, wantMinor: 22},
		{in: "2.0.4", wantMajor: 2, wantMinor: 0},
		{in: "v29.8.1", wantMajor: 29, wantMinor: 8},
		{in: "5:28.5.2-1~ubuntu.24.04~noble", wantMajor: 28, wantMinor: 5},
		{in: "16", wantMajor: 16, wantMinor: 0},
		{in: "18", wantMajor: 18, wantMinor: 0},
		{in: "not-a-version", wantErr: true},
		{in: "1.not-a-number", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			gotMajor, gotMinor, err := parseVersion(tt.in)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.wantMajor, gotMajor)
			assert.Equal(t, tt.wantMinor, gotMinor)
		})
	}
}

func TestDockerTagVersion(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "docker-v29.8.1", want: "v29.8.1"},
		{in: "v29.8.1", want: "v29.8.1"},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.want, dockerTagVersion(tt.in))
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

func TestConsulAgentVersion(t *testing.T) {
	t.Run("returns consul's own agent version", func(t *testing.T) {
		client := fakeConsulClient{agentSelfKey: {version: "1.22.2"}}

		got, err := consulAgentVersion(client)(context.Background())

		assert.NoError(t, err)
		assert.Equal(t, "1.22.2", got)
	})

	t.Run("propagates a consul error", func(t *testing.T) {
		client := fakeConsulClient{agentSelfKey: {err: errors.New("boom")}}

		_, err := consulAgentVersion(client)(context.Background())

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

// fakeNomadClient is a nomad.Client fake, used to test nomadCurrent without
// a real Nomad agent.
type fakeNomadClient struct {
	version string
	err     error
}

func (f fakeNomadClient) Version(context.Context) (string, error) {
	return f.version, f.err
}

var _ nomad.Client = fakeNomadClient{}

func TestNomadCurrent(t *testing.T) {
	t.Run("returns nomad's own agent version", func(t *testing.T) {
		client := fakeNomadClient{version: "1.11.3"}

		got, err := nomadCurrent(client)(context.Background())

		assert.NoError(t, err)
		assert.Equal(t, "1.11.3", got)
	})

	t.Run("propagates a nomad error", func(t *testing.T) {
		client := fakeNomadClient{err: errors.New("boom")}

		_, err := nomadCurrent(client)(context.Background())

		assert.ErrorContains(t, err, "boom")
	})
}

// fakeDockerClient is a docker.Client fake, used to test dockerCurrent
// without a real Docker daemon.
type fakeDockerClient struct {
	version string
	err     error
}

func (f fakeDockerClient) Version(context.Context) (string, error) {
	return f.version, f.err
}

var _ docker.Client = fakeDockerClient{}

// fakeCurrent returns a fetchCurrent func for use in dependency structs in
// tests, mirroring fakeLatest below.
func fakeCurrent(version string, err error) func(context.Context) (string, error) {
	return func(context.Context) (string, error) {
		return version, err
	}
}

// agentSelfKey is the fakeConsulClient key used for AgentVersion, which
// (unlike Version) isn't keyed by a service name.
const agentSelfKey = "self"

func TestDockerCurrent(t *testing.T) {
	t.Run("returns the version the local docker daemon reports for itself", func(t *testing.T) {
		client := fakeDockerClient{version: "28.5.2"}

		got, err := dockerCurrent(client)(context.Background())

		assert.NoError(t, err)
		assert.Equal(t, "28.5.2", got)
	})

	t.Run("propagates a docker error", func(t *testing.T) {
		client := fakeDockerClient{err: errors.New("boom")}

		_, err := dockerCurrent(client)(context.Background())

		assert.ErrorContains(t, err, "boom")
	})
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

func (f fakeConsulClient) AgentVersion(_ context.Context) (string, error) {
	entry, ok := f[agentSelfKey]
	if !ok {
		return "", errors.New("fakeConsulClient: no entry for agent self")
	}
	return entry.version, entry.err
}

var _ consul.Client = fakeConsulClient{}

func fakeLatest(version string, err error) func(context.Context) (string, error) {
	return func(context.Context) (string, error) {
		return version, err
	}
}
