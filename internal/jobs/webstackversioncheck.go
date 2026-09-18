package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"homelab-cron/internal/consul"
	"homelab-cron/internal/docker"
	"homelab-cron/internal/nomad"
	"homelab-cron/internal/vault"
)

// dependency is one component of the homelab stack this job tracks: its
// current version (or how to fetch it) and how to fetch the latest stable
// release to compare it against.
type dependency struct {
	name string

	// currentVersion is a hand-maintained pinned baseline, used when
	// fetchCurrent is nil. Dependencies that can report their own deployed
	// version live use fetchCurrent instead, so the baseline can't drift
	// out of sync with what's actually running — see consulCurrent (via
	// Consul service meta), vaultCurrent (via Vault's own health endpoint),
	// nomadCurrent (via Nomad's own agent-self endpoint), and dockerCurrent
	// (via the local Docker daemon's own Engine API).
	currentVersion string
	fetchCurrent   func(ctx context.Context) (string, error)

	fetchLatest func(ctx context.Context) (string, error)
}

// WebstackVersionCheck compares pinned versions of the homelab stack
// against each project's latest stable release and alerts when any has
// fallen a major version behind.
type WebstackVersionCheck struct {
	deps []dependency

	mu      sync.Mutex
	message string
}

// NewWebstackVersionCheck builds the check against HashiCorp's public
// releases API (Consul/Vault/Nomad), moby/moby's GitHub releases (Docker
// Engine), each image's own GitHub releases (Traefik, New Relic
// Infrastructure), and postgresql.org's published version list (PostgreSQL,
// whose Docker tag is just the bare major version).
func NewWebstackVersionCheck(
	consulClient consul.Client,
	vaultClient vault.Client,
	nomadClient nomad.Client,
	dockerClient docker.Client,
) *WebstackVersionCheck {
	client := &http.Client{Timeout: 10 * time.Second}
	return newWebstackVersionCheck([]dependency{
		{
			name:         "Consul",
			fetchCurrent: consulAgentVersion(consulClient),
			fetchLatest:  hashiCorpLatest(client, "consul"),
		},
		{
			name:         "Vault",
			fetchCurrent: vaultCurrent(vaultClient),
			fetchLatest:  hashiCorpLatest(client, "vault"),
		},
		{
			name:         "Nomad",
			fetchCurrent: nomadCurrent(nomadClient),
			fetchLatest:  hashiCorpLatest(client, "nomad"),
		},
		{
			name:         "Docker",
			fetchCurrent: dockerCurrent(dockerClient),
			fetchLatest:  dockerLatest(client),
		},
		{
			name:         "Traefik",
			fetchCurrent: consulCurrent(consulClient, "traefik"),
			fetchLatest:  githubLatestTag(client, "traefik", "traefik"),
		},
		{
			name:         "PostgreSQL",
			fetchCurrent: consulCurrent(consulClient, "postgres"),
			fetchLatest:  postgresLatestMajor(client),
		},
		{
			name:         "New Relic Infrastructure",
			fetchCurrent: consulCurrent(consulClient, "newrelic"),
			fetchLatest:  githubLatestTag(client, "newrelic", "infrastructure-agent"),
		},
	})
}

func newWebstackVersionCheck(deps []dependency) *WebstackVersionCheck {
	return &WebstackVersionCheck{deps: deps}
}

// WebstackVersionCheckJobName is this job's Name() — also the {name} path
// param value for triggering it via GET /job/{name} (see internal/api).
const WebstackVersionCheckJobName = "webstack-version-check"

// Name identifies this job in logs.
func (*WebstackVersionCheck) Name() string { return WebstackVersionCheckJobName }

// Schedule runs once a week, Monday at 7am — version drift moves slowly, so
// there's no need to check more often.
func (*WebstackVersionCheck) Schedule() string { return "0 7 * * 1" }

// Run fetches the latest stable release for each tracked dependency and
// records any that are a major version behind their pinned version. A
// dependency whose latest-version fetch fails is reported rather than
// failing the whole run, so one flaky upstream API can't hide a real
// version gap in another dependency.
func (j *WebstackVersionCheck) Run(ctx context.Context) error {
	var lines []string

	for _, d := range j.deps {
		current := d.currentVersion
		if d.fetchCurrent != nil {
			v, err := d.fetchCurrent(ctx)
			if err != nil {
				log.Printf("webstack-version-check: %s: failed to fetch current version: %v", d.name, err)
				lines = append(lines, fmt.Sprintf("- %s: could not check current version (%v)", d.name, err))
				continue
			}
			current = v
		}

		latest, err := d.fetchLatest(ctx)
		if err != nil {
			log.Printf("webstack-version-check: %s: failed to fetch latest version: %v", d.name, err)
			lines = append(lines, fmt.Sprintf("- %s: could not check latest version (%v)", d.name, err))
			continue
		}

		currentMajor, err := majorVersion(current)
		if err != nil {
			log.Printf("webstack-version-check: %s: bad current version %q: %v", d.name, current, err)
			continue
		}
		latestMajor, err := majorVersion(latest)
		if err != nil {
			log.Printf("webstack-version-check: %s: bad latest version %q: %v", d.name, latest, err)
			continue
		}

		if latestMajor > currentMajor {
			lines = append(lines, fmt.Sprintf("- %s: current %s is a major version behind latest stable %s", d.name, current, latest))
		}
	}

	j.setMessage(strings.Join(lines, "\n"))
	return nil
}

func (j *WebstackVersionCheck) setMessage(msg string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.message = msg
}

// AlertingEnabled is always true for this job.
func (*WebstackVersionCheck) AlertingEnabled() bool { return true }

// EmailContent returns the dependencies found to be a major version behind
// on the most recent Run, or "" if none were — the scheduler treats an
// empty EmailContent as nothing to send.
func (j *WebstackVersionCheck) EmailContent() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.message
}

// consulCurrent returns a fetchCurrent func that reads service's
// actually-deployed version live from Consul, via consulClient — see
// internal/consul
func consulCurrent(consulClient consul.Client, service string) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		return consulClient.Version(ctx, service)
	}
}

// consulAgentVersion returns a fetchCurrent func that reads Consul's own
// deployed version live from its /v1/agent/self endpoint, via
// consulClient — see internal/consul.
func consulAgentVersion(consulClient consul.Client) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		return consulClient.AgentVersion(ctx)
	}
}

// vaultCurrent returns a fetchCurrent func that reads Vault's own
// deployed version live from its health endpoint, via vaultClient — see
// internal/vault.
func vaultCurrent(vaultClient vault.Client) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		return vaultClient.Version(ctx)
	}
}

// nomadCurrent returns a fetchCurrent func that reads Nomad's own deployed
// version live from its agent-self endpoint, via nomadClient — see
// internal/nomad.
func nomadCurrent(nomadClient nomad.Client) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		return nomadClient.Version(ctx)
	}
}

// dockerCurrent returns a fetchCurrent func that reads the local Docker
// daemon's own deployed version live from its Engine API, via
// dockerClient — see internal/docker.
func dockerCurrent(dockerClient docker.Client) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		return dockerClient.Version(ctx)
	}
}

// hashiCorpLatest returns a fetchLatest func for a HashiCorp product,
// backed by the public releases API (no auth required).
func hashiCorpLatest(client *http.Client, product string) func(context.Context) (string, error) {
	url := fmt.Sprintf("https://api.releases.hashicorp.com/v1/releases/%s/latest", product)
	return func(ctx context.Context) (string, error) {
		var body struct {
			Version string `json:"version"`
		}
		if err := getJSON(ctx, client, url, &body); err != nil {
			return "", err
		}
		if body.Version == "" {
			return "", fmt.Errorf("%s: no version in response", product)
		}
		return body.Version, nil
	}
}

// dockerLatest returns a fetchLatest func for Docker Engine, backed by
// moby/moby's latest GitHub release tag.
func dockerLatest(client *http.Client) func(context.Context) (string, error) {
	fetchTag := githubLatestTag(client, "moby", "moby")
	return func(ctx context.Context) (string, error) {
		tag, err := fetchTag(ctx)
		if err != nil {
			return "", err
		}
		return dockerTagVersion(tag), nil
	}
}

// dockerTagVersion strips the "docker-" prefix moby/moby puts on Docker
// Engine's own release tags (e.g. "docker-v29.8.1"), distinguishing them
// from that repo's other release trains published from the same repo
// (e.g. "client/v0.6.0", "api/v1.56.0"), so majorVersion sees a plain
// "vX.Y.Z" tag, same shape as Traefik/New Relic's tags.
func dockerTagVersion(tag string) string {
	return strings.TrimPrefix(tag, "docker-")
}

// githubLatestTag returns a fetchLatest func backed by a GitHub repo's
// latest release tag (e.g. Traefik's "v3.7.13", New Relic's "1.80.3").
func githubLatestTag(client *http.Client, owner, repo string) func(context.Context) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo)
	return func(ctx context.Context) (string, error) {
		var body struct {
			TagName string `json:"tag_name"`
		}
		if err := getJSON(ctx, client, url, &body); err != nil {
			return "", err
		}
		if body.TagName == "" {
			return "", fmt.Errorf("%s/%s: no tag_name in response", owner, repo)
		}
		return body.TagName, nil
	}
}

// postgresLatestMajor returns a fetchLatest func for PostgreSQL, backed by
// postgresql.org's published version list. PostgreSQL's Docker tags (e.g.
// "postgres:16") are just the bare major version, so this returns whichever
// entry is currently marked "current" (e.g. "18") rather than a full
// semver release.
func postgresLatestMajor(client *http.Client) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		var versions []struct {
			Major   string `json:"major"`
			Current bool   `json:"current"`
		}
		if err := getJSON(ctx, client, "https://www.postgresql.org/versions.json", &versions); err != nil {
			return "", err
		}
		for _, v := range versions {
			if v.Current {
				return v.Major, nil
			}
		}
		return "", fmt.Errorf("postgresql: no current version found in versions.json")
	}
}

func getJSON(ctx context.Context, client *http.Client, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "homelab-cron")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			log.Printf("webstack-version-check: closing response body from %s: %v", url, cerr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d from %s", resp.StatusCode, url)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// majorVersion extracts the leading major version number from a version
// string, tolerating a "v" prefix (GitHub release tags), a leading
// "epoch:" (Debian/apt package versions, e.g. Docker's
// "5:28.5.2-1~ubuntu.24.04~noble"), and any "-"/"~"/"+" suffix.
func majorVersion(v string) (int, error) {
	v = strings.TrimPrefix(v, "v")
	if i := strings.LastIndex(v, ":"); i != -1 {
		v = v[i+1:]
	}
	if i := strings.IndexAny(v, "-~+"); i != -1 {
		v = v[:i]
	}

	major, _, _ := strings.Cut(v, ".")
	n, err := strconv.Atoi(major)
	if err != nil {
		return 0, fmt.Errorf("parse major version from %q: %w", v, err)
	}
	return n, nil
}
