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
)

// dependency is one component of the homelab stack this job tracks: the
// version currently pinned in the homelab repo's Ansible role defaults
// (provisioning/ansible/roles/*/defaults/main.yml), and how to fetch the
// latest stable upstream release to compare it against.
type dependency struct {
	name           string
	currentVersion string
	fetchLatest    func(ctx context.Context) (string, error)
}

// WebstackVersionCheck compares the versions of Consul, Vault, Nomad, and
// Docker pinned in the homelab repo against each project's latest stable
// release, and alerts when any has fallen a major version behind. The
// pinned versions below are a hand-maintained snapshot — update them
// whenever the homelab repo's Ansible defaults change (see homelab's
// UPGRADE.md).
//
// This never touches the actually-running stack: homelab-cron isn't on the
// host network and can't reach Consul/Vault/Nomad's local APIs from inside
// its container, so this only compares hardcoded baselines against public
// upstream version endpoints.
type WebstackVersionCheck struct {
	deps []dependency

	mu      sync.Mutex
	message string
}

// NewWebstackVersionCheck builds the check against HashiCorp's public
// releases API (Consul/Vault/Nomad) and moby/moby's GitHub releases
// (Docker Engine).
func NewWebstackVersionCheck() *WebstackVersionCheck {
	client := &http.Client{Timeout: 10 * time.Second}
	return newWebstackVersionCheck([]dependency{
		{name: "Consul", currentVersion: "1.22.2", fetchLatest: hashiCorpLatest(client, "consul")},
		{name: "Vault", currentVersion: "1.18.3", fetchLatest: hashiCorpLatest(client, "vault")},
		{name: "Nomad", currentVersion: "1.8.4", fetchLatest: hashiCorpLatest(client, "nomad")},
		{name: "Docker", currentVersion: "5:28.5.2-1~ubuntu.24.04~noble", fetchLatest: dockerLatest(client)},
	})
}

func newWebstackVersionCheck(deps []dependency) *WebstackVersionCheck {
	return &WebstackVersionCheck{deps: deps}
}

// Name identifies this job in logs.
func (*WebstackVersionCheck) Name() string { return "webstack-version-check" }

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
		latest, err := d.fetchLatest(ctx)
		if err != nil {
			log.Printf("webstack-version-check: %s: failed to fetch latest version: %v", d.name, err)
			lines = append(lines, fmt.Sprintf("- %s: could not check latest version (%v)", d.name, err))
			continue
		}

		currentMajor, err := majorVersion(d.currentVersion)
		if err != nil {
			log.Printf("webstack-version-check: %s: bad pinned version %q: %v", d.name, d.currentVersion, err)
			continue
		}
		latestMajor, err := majorVersion(latest)
		if err != nil {
			log.Printf("webstack-version-check: %s: bad latest version %q: %v", d.name, latest, err)
			continue
		}

		if latestMajor > currentMajor {
			lines = append(lines, fmt.Sprintf("- %s: pinned %s is a major version behind latest stable %s", d.name, d.currentVersion, latest))
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
// moby/moby's latest GitHub release tag (e.g. "v29.8.1").
func dockerLatest(client *http.Client) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		var body struct {
			TagName string `json:"tag_name"`
		}
		if err := getJSON(ctx, client, "https://api.github.com/repos/moby/moby/releases/latest", &body); err != nil {
			return "", err
		}
		if body.TagName == "" {
			return "", fmt.Errorf("docker: no tag_name in response")
		}
		return body.TagName, nil
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
	defer resp.Body.Close()

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
