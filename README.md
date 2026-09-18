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
roots, and a thin scheduler wrapping
[`robfig/cron`](https://github.com/robfig/cron):

```
cmd/api/main.go           entrypoint: /health HTTP server only
cmd/cron/main.go          entrypoint: builds and runs the cron scheduler
internal/config            env var configuration
internal/server              chi router (GET /health only)
internal/cron                  the Job interface + Scheduler
internal/jobs                    concrete cron.Job implementations
internal/mailer                  alert email delivery (AWS SES, or a Noop in local dev)
internal/consul                  reads live dependency versions from Consul
internal/vault                   reads Vault's own live version
internal/nomad                   reads Nomad's own live version
internal/docker                  reads the local Docker daemon's own live version
```

`api` and `cron` are separate processes (and, in the built Docker image,
separate binaries) — `api` only ever serves `/health`; `cron` does all the
actual work and has no HTTP surface. `cron.Scheduler` depends only on the
`cron.Job` interface, not on any concrete job, so jobs are added by writing
a new type in `internal/jobs/` and registering it in `cmd/cron/main.go` —
nothing else needs to change. Each job also declares whether it wants
alerting (`AlertingEnabled`/`EmailContent`); the scheduler emails the
result via `internal/mailer` after every run when enabled. See
[CLAUDE.md](./CLAUDE.md) for the full design rationale.

## Running locally

Requires Go 1.24+.

```
go run ./cmd/api    # /health server
go run ./cmd/cron   # scheduler
```

Or via Docker Compose (copy `.env.example` to `.env` first), which runs
both as separate services from the same image:

```
docker compose up --build
```

The API listens on `:8080` by default (`ADDR` env var).

## Configuration

All env vars are optional with working local-dev defaults except AWS
credentials, which are only required once alerting is turned on. See
`.env.example` and [CLAUDE.md](./CLAUDE.md) for the full list and
rationale:

| Var | Default | Purpose |
| --- | --- | --- |
| `ADDR` | `:8080` | `/health` listen address |
| `HOST_ROOT` | `/host` | read-only host filesystem mount, for jobs like `AptUpgradeCheck` |
| `ALERT_EMAIL_FROM` / `ALERT_EMAIL_TO` | unset | alert email sender/recipients; unset means `mailer.Noop` (log-only) |
| `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` / `AWS_REGION` | — | required if the above are set; read by the AWS SDK's own env chain, not this repo's config |
| `CONSUL_ADDR` | `http://127.0.0.1:8500` | Consul HTTP API, for live dependency versions |
| `VAULT_ADDR` | `http://127.0.0.1:8200` | Vault HTTP API, for Vault's own live version |
| `NOMAD_ADDR` | `http://127.0.0.1:4646` | Nomad HTTP API, for Nomad's own live version |
| `NOMAD_TOKEN` | unset | ACL token for `NOMAD_ADDR`, required once Nomad's ACLs are enabled; a secret, rendered from Vault in production |
| `DOCKER_SOCK` | `/var/run/docker.sock` | Docker Engine API Unix socket, for the daemon's own live version |

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
