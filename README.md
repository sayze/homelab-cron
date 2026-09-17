# Homelab Cron

A single Go service that schedules and runs arbitrary cron jobs against the
homelab's Nomad cluster. Every job is a Go type implementing a small `Job`
interface — there's no dynamic/config-driven job loading, so adding a job
means writing Go code and redeploying, not editing a config file.

The service exposes exactly one HTTP route, `GET /health`, used only for
Nomad/Consul's own health check — it's never routed through Traefik and has
no other API surface. All real work happens on cron schedules inside the
process.

## Architecture

Go, [chi](https://github.com/go-chi/chi) router, cmd-pattern composition
root, and a thin scheduler wrapping
[`robfig/cron`](https://github.com/robfig/cron):

```
cmd/api/main.go          entrypoint / composition root
internal/config            env var configuration
internal/server              chi router (GET /health only)
internal/cron                  the Job interface + Scheduler
internal/jobs                    concrete cron.Job implementations
```

`cron.Scheduler` depends only on the `cron.Job` interface, not on any
concrete job, so jobs are added by writing a new type in `internal/jobs/`
and registering it in `main.go` — nothing else needs to change. See
[CLAUDE.md](./CLAUDE.md) for the full design rationale.

## Running locally

Requires Go 1.24+.

```
go run ./cmd/api
```

Or via Docker Compose (copy `.env.example` to `.env` first):

```
docker compose up --build
```

The API listens on `:8080` by default (`ADDR` env var).

## Testing

```
make unit
```

Runs the full unit test suite (`go test --tags unit ./...`) through
[tparse](https://github.com/mfridman/tparse) for readable output. Run a
single package or test with `make unit path=internal/cron` or
`make unit path=internal/cron test=TestNew_InvalidSchedule`.

## Jobs

See [CLAUDE.md](./CLAUDE.md) for how to add a new one.

## Host filesystem access

This service only ever *reads* the host filesystem — it's given a
read-only bind mount of the entire host root (the same pattern the
`homelab` repo's New Relic infra agent job uses), never write access. Jobs
build paths off `cfg.HostRoot` (env var `HOST_ROOT`, default `/host`)
rather than hardcoding one. See [CLAUDE.md](./CLAUDE.md) for the full
mount setup, in both `docker-compose.yml` and `homelab-cron.nomad.hcl`.

## Deployment

`homelab-cron.nomad.hcl` deploys this as a Nomad service with no Traefik
tags, so it's never exposed publicly. CI (`.github/workflows/deploy.yml`)
builds/pushes the image on push to `master`, then runs the Nomad job
against the homelab cluster. See [CLAUDE.md](./CLAUDE.md) for the required
repo variables/secrets.

## TODO

### Hygiene

- **Source `webstack-version-check`'s current versions from the live
  stack instead of a hardcoded list.** `internal/jobs/webstackversioncheck.go`
  currently compares hand-maintained version strings (copied from
  `homelab`'s Ansible defaults and `jobs/*.nomad.hcl` image tags) against
  upstream latest-release APIs — nothing here actually asks Consul, Vault,
  or Nomad what version they're running, so the baseline silently goes
  stale unless someone remembers to update it by hand alongside `homelab`.
  To fix properly:
  - This job's Nomad task currently uses the default bridge network
    (`homelab-cron.nomad.hcl`'s `network` block has no `mode = "host"`),
    so it can't reach `127.0.0.1:8500`/`8200`/`4646` — the container's
    loopback isn't the host's. Switching to `mode = "host"` (same as
    `jobs/traefik.nomad.hcl`/`jobs/newrelic.nomad.hcl`) would fix that,
    but is a real deployment change (host port binding) worth its own
    review, not a drive-by edit.
  - Consul (`GET /v1/agent/self`, `Config.Version`) and Vault
    (`GET /v1/sys/health`, `version`) are unauthenticated on this stack
    (no Consul ACLs; Vault's health endpoint doesn't require a token), so
    those two are straightforward once host networking is in place.
  - Nomad has ACLs enabled (`acl.enabled = true` in `nomad.hcl.j2`), so
    its `/v1/agent/self` needs a token. Would need a new read-only Nomad
    ACL policy/token provisioned via `homelab`'s Ansible (same pattern as
    the existing `ci-deploy` token in `provisioning/ansible/vars/defaults.yml`),
    delivered to this service as a Vault-templated secret like the AWS SES
    credentials already are.
  - Docker has no such HTTP API without mounting `/var/run/docker.sock`
    (as `jobs/newrelic.nomad.hcl` does) and calling the Engine API's
    `/version` — broader access than this service currently needs, so
    worth weighing separately.
  - Traefik/PostgreSQL/New Relic Infrastructure are Docker image tags
    pinned in `jobs/*.nomad.hcl` in the `homelab` repo, not services this
    job could query directly either way — keeping those hardcoded (or
    reading them out of the deployed Nomad job specs via the Nomad API)
    is a separate question from the Consul/Vault/Nomad one above.
