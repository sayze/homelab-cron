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
