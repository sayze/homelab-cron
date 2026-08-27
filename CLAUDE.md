# Homelab Cron

A single Go service that schedules and runs arbitrary cron jobs, deployed to
the homelab's Nomad cluster. Every job is a Go type implementing the `Job`
interface — there is no dynamic/config-driven job loading, no scripting
layer, and no way to add a job without a code change and redeploy.

This service exposes exactly one HTTP route, `GET /health`, used only for
Nomad/Consul's own health check. It is never routed through Traefik and has
no other API surface — all real work happens on cron schedules inside the
process, not in response to HTTP requests.

## Architecture (cmd pattern)

- `cmd/api/main.go` — entrypoint/composition root. Loads `config.Load()`,
  builds a `cron.Scheduler` from the jobs in `internal/jobs`, starts it,
  builds the router via `server.New()`, starts `http.ListenAndServe` on
  `cfg.Addr`. Listens for `SIGINT`/`SIGTERM`: on signal, shuts the HTTP
  server down first (`srv.Shutdown`, 10s timeout), then stops the scheduler
  (deferred `scheduler.Stop()`), which cancels any in-flight job's context
  and blocks until it returns — so a job mid-read of the host filesystem
  gets a chance to notice cancellation and finish cleanly before the
  process exits.
- `internal/config/config.go` — env var loading (`ADDR`, `HOST_ROOT`), plain
  `os.Getenv` with defaults, no third-party config library.
- `internal/server/server.go` — chi router. Middleware: chi's default stack
  (`RequestID`, `Logger`, `Recoverer`). One route: `GET /health` → `200
  {"status":"ok"}`. No CORS, no auth — nothing here is meant to be called by
  a browser or an external client.

### Cron scheduling (`internal/cron/`)

- `job.go` — the `Job` interface every cron job implements:
  - `Name() string` — identifies the job in logs.
  - `Schedule() string` — a standard 5-field cron expression (minute hour
    dom month dow), e.g. `"0 * * * *"` for hourly.
  - `Run(ctx context.Context) error` — executes one occurrence. Should
    return promptly once `ctx` is cancelled.
- `scheduler.go` — `Scheduler`, a thin wrapper around
  `github.com/robfig/cron/v3`. `New(jobs ...Job)` registers each job's
  `Schedule()` via `AddFunc`, returning an error if any expression is
  invalid (fails fast at startup, not at the job's next scheduled run).
  `Start()` is non-blocking. `Stop()` cancels a context shared by all
  in-flight job runs, then blocks until robfig/cron confirms none are still
  running. Each run is wrapped in `runJob`, which logs start/finish/duration
  and recovers a panic so one broken job can't take the process down or
  block other jobs' future runs.

### Jobs (`internal/jobs/`)

Each file is one `Job` implementation, independent of the others. Copy an
existing one as the starting point for a new job — there's no shared base
type or registry beyond passing the job into `cron.New(...)` in
`cmd/api/main.go`.

- `heartbeat.go` — `Heartbeat`, runs every 15 minutes, just logs. No
  dependencies; exists as a smoke test that the scheduler is wired up and
  running.
- `aptupgrade.go` — `AptUpgradeCheck`, runs every morning at 9am, checks
  that a file has been modified within the last week. Takes that file's
  path as a constructor arg (`NewAptUpgradeCheck(path string)`); `main.go`
  passes `filepath.Join(cfg.HostRoot, "var/log/apt/upgrade.log")`, i.e. the
  host's real apt upgrade log — apt only writes to it when a package
  upgrade actually runs, so a missing or stale file means unattended
  upgrades have stopped running. Logs a warning in that case; otherwise
  silent. Worked example of a job that reads host filesystem state — copy
  this one for jobs that need to read/tail/scan files under the host
  mount.

## Host filesystem access

This service only ever *reads* the host filesystem — it is never given
write access, on purpose (no job should be able to modify host state; if
one genuinely needs to, that's a deliberate exception to design for
explicitly, not something to fall into by reusing this mount).

The whole host root filesystem is bind-mounted read-only into the
container at `cfg.HostRoot` (env var `HOST_ROOT`, default `/host`) — the
same pattern the `homelab` repo's `jobs/newrelic.nomad.hcl` uses for its
infra agent:

- In `homelab-cron.nomad.hcl`, the task's `config.volumes` includes
  `"/:/host:ro,rslave"` — `:ro` makes it read-only, `rslave` propagates new
  host mounts (e.g. plugging in a drive) into the container without a
  restart. No Nomad `host_volume`/ansible provisioning is needed for
  this — it's a raw Docker bind mount, so there's nothing that needs to
  exist ahead of time on the Nomad client (unlike a named `host_volume`).
- In `docker-compose.yml`, the same shape: bind-mounted from
  `HOST_ROOT_HOST_PATH` (default `/`, read-only) — override this to a
  narrower path locally if you'd rather not expose your whole dev machine
  to the container.

Jobs needing to look at some host path should build it off `cfg.HostRoot`
(e.g. `filepath.Join(cfg.HostRoot, "var/log/apt/upgrade.log")` — see
`aptupgrade.go`) rather than hardcoding an absolute path, since a bare
`/var/log` inside the container refers to the container's own (empty)
filesystem, not the host's.

## Adding a new job

1. Add a new file in `internal/jobs/` implementing `cron.Job` (`Name`,
   `Schedule`, `Run`) — `aptupgrade.go` is the closest template if the job
   reads host filesystem state, `heartbeat.go` otherwise.
2. Register it in `cmd/api/main.go`'s `cron.New(...)` call.
3. If it needs a new env var (a secret, an external endpoint, etc.), add it
   to `internal/config/config.go`, `.env.example`, and the `env`/`template`
   stanza in `homelab-cron.nomad.hcl` (see `qotd-api.nomad.hcl`'s `template`
   block for the Vault-backed-secret pattern, if the new var is a secret).

## Required env vars

- `ADDR` — listen address for the `/health` server. Defaults to `:8080`.
- `HOST_ROOT` — path where the host's root filesystem is mounted
  read-only. Defaults to `/host`.

## Docker

Two-stage build, same shape as `qotd-api`:
1. `golang:1.24-alpine` — installs `ca-certificates`, builds a static binary
   (`CGO_ENABLED=0`).
2. `FROM scratch` — copies in only the binary and
   `/etc/ssl/certs/ca-certificates.crt` (for any future job that makes
   outbound HTTPS calls).

Build/run:
```
docker build -t homelab-cron:dev .
docker run --rm -p 8080:8080 -v /:/host:ro homelab-cron:dev
```

For local dev, `docker-compose.yml` builds and runs the same image; copy
`.env.example` to `.env`, then `docker compose up --build`.

## Deployment (`homelab-cron.nomad.hcl`)

`type = "service"` with a Consul `service` block and an HTTP check against
`/health` — no Traefik tags, so it's never exposed publicly (Traefik's
`exposedByDefault=false`, and nothing here opts in). CI (`.github/workflows/
deploy.yml`) builds/pushes `sayze/homelab-cron` on push to `master`, then
runs `nomad job run -var="image=sayze/homelab-cron:sha-<short-sha>"
homelab-cron.nomad.hcl` against the homelab's Nomad cluster — same shape as
`qotd-api`'s deploy workflow. Requires `REGISTRY_USER`/`NOMAD_ADDR`
(variables) and `REGISTRY_TOKEN`/`NOMAD_CI_TOKEN` (secrets) configured as
repo (or org) config before it will run successfully — the workflow
doesn't scope these to a GitHub Environment.

See **Host filesystem access** above — unlike `postgres-data` in the
`homelab` repo, this job's host mount needs no ansible/Nomad `host_volume`
provisioning ahead of time, since it's a raw Docker bind mount rather than
a named `host_volume`.

## Related

- `homelab` — the infra repo (Consul/Vault/Nomad/Traefik provisioning via
  Ansible). `jobs/newrelic.nomad.hcl` is the sibling Nomad job this one's
  host-mount pattern was copied from.
- `qotd-api` — sibling service this project's Makefile/Dockerfile/CI/Nomad
  conventions were copied from.
