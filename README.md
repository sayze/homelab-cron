# Homelab Cron

A single Go service that schedules and runs arbitrary cron jobs against the
homelab's Nomad cluster. Every job is a Go type implementing a small `Job`
interface — there's no dynamic/config-driven job loading, so adding a job
means writing Go code and redeploying, not editing a config file.

`api` serves two HTTP routes: `GET /health`, used only for Nomad/Consul's
own health check, and `GET /job/{name}`, letting an operator trigger a
registered job to run immediately, outside its schedule. Neither route is
routed through Traefik — both are reachable only on the homelab's
internal network. All other work happens on cron schedules inside the
separate `cron` process.

## Running locally

Requires Go 1.24+.

```
go run ./cmd/api    # /health, /job/{name}
go run ./cmd/cron   # scheduler, no HTTP
```

Or via Docker Compose (copy `.env.example` to `.env` first), which runs
both as separate services from the same image:

```
docker compose up --build
```

`api`'s `/health`/`/job/{name}` listen on `:8080` by default (`ADDR` env
var); `cron` has no listen address, since it has no HTTP server.

## Configuration

All env vars are optional with working local-dev defaults except AWS
credentials, which are only required once alerting is turned on. See
`.env.example` and [CLAUDE.md](./CLAUDE.md) for the full list and
rationale:

| Var | Default | Purpose |
| --- | --- | --- |
| `ADDR` | `:8080` | `cmd/api`'s `/health`/`/job/{name}` listen address (unused by `cmd/cron`) |
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
read-only bind mount of the entire host root, never write access. Jobs
build paths off `cfg.HostRoot` (env var `HOST_ROOT`, default `/host`)
rather than hardcoding one. See [CLAUDE.md](./CLAUDE.md) for the full
mount setup, in both `docker-compose.yml` and `homelab-cron.nomad.hcl`.

## Deployment

`homelab-cron.nomad.hcl` deploys this as a Nomad service with no Traefik
tags, so it's never exposed publicly. CI (`.github/workflows/deploy.yml`)
builds/pushes the image on push to `master`, then runs the Nomad job
against the homelab cluster. See [CLAUDE.md](./CLAUDE.md) for the required
repo variables/secrets.
