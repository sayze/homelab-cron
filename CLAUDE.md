# Homelab Cron

A single Go service that schedules and runs arbitrary cron jobs, deployed to
the homelab's Nomad cluster. Every job is a Go type implementing the `Job`
interface — there is no dynamic/config-driven job loading, no scripting
layer, and no way to add a job without a code change and redeploy.

`cmd/api` serves two HTTP routes: `GET /health`, used only for
Nomad/Consul's own health check, and `GET /job/{name}`, which lets an
operator trigger a registered job to run immediately, outside its
schedule. Neither route is routed through Traefik — both are reachable
only on the homelab's internal network. All other work happens on cron
schedules inside the separate `cmd/cron` process, not in response to HTTP
requests.

## Architecture (cmd pattern)

`api` and `cron` are two independent entrypoints/composition roots, built
as two separate binaries (see **Docker** below) and deployed as two
separate Nomad tasks (see **Deployment**). Each loads its own
`config.Load()` and only reads the env vars it actually needs; unused ones
are simply ignored.

- `cmd/api/main.go` — HTTP entrypoint. Loads `config.Load()`, builds the
  same `internal/consul`/`internal/vault`/`internal/nomad`/`internal/docker`
  clients and `internal/jobs` jobs `cmd/cron` does, but only to hand them to
  `internal/api.New(jobs, m)` as a `map[string]cron.Job` keyed by each
  job's `Name()` — never to `cron.New(...)`/`cron.Scheduler`, which is
  `cmd/cron`'s alone. Starts `http.ListenAndServe` on `cfg.Addr`. On
  `SIGINT`/`SIGTERM`, shuts the HTTP server down (`srv.Shutdown`, 10s
  timeout). A `GET /job/{name}` request runs the matching job directly via
  `cron.RunJob` (see `internal/cron/scheduler.go` below) — `cmd/api` never
  talks to the `cmd/cron` process to do this; it just runs its own copy of
  the job.
- `cmd/cron/main.go` — entrypoint for the scheduler. Loads `config.Load()`,
  builds the same clients/jobs `cmd/api` does, builds a `cron.Scheduler`
  from them, starts it. Has no HTTP surface of its own. On
  `SIGINT`/`SIGTERM`, stops the scheduler (deferred `scheduler.Stop()`),
  which cancels any in-flight job's context and blocks until it returns —
  so a job mid-read of the host filesystem gets a chance to notice
  cancellation and finish cleanly before the process exits.
- `internal/config/config.go` — env var loading (`ADDR`, `HOST_ROOT`,
  `ALERT_EMAIL_FROM`, `ALERT_EMAIL_TO`, `CONSUL_ADDR`, `VAULT_ADDR`), plain
  `os.Getenv` + comma-split with defaults, no third-party config library.
  `ADDR` is read only by `cmd/api` — `cmd/cron` has no HTTP server.
  Deliberately does *not* read AWS credentials/region — those go straight
  to the AWS SDK's own env chain (see `internal/mailer`).
- `internal/api/api.go` — chi router. Middleware: chi's `RequestID`, then
  this package's own `requestLogger` and `recoverer` — JSON replacements
  for chi's `Logger`/`Recoverer`, which write plain text (see **Logging**).
  `GET /health` → `200 {"status":"ok"}`. `GET /job/{name}` → looks `name` up in the
  `map[string]cron.Job` given to `New`, runs it via `cron.RunJob` in its
  own goroutine (not waiting for it to finish), and returns `202
  {"status":"triggered","job":name}`; `name` not in the map is a `404
  {"error":"job not found"}`. `New(jobs map[string]cron.Job, m
  mailer.Sender)` takes the jobs and the mailer `RunJob` uses for a
  triggered job's alert email as explicit dependencies — no interface
  indirection, no dependency on `cron.Scheduler`. No CORS, no auth —
  nothing here is meant to be called by a browser; both routes are only
  reachable on the homelab's internal network (see **Deployment**).

### Logging (`github.com/sayze/homelab-utils/logger`)

The logger lives in the shared homelab-utils module, not in this repo.


All log output is one JSON object per line, on stdout, so upstream
(Nomad → Fluent Bit) can parse it consistently. Every line has slog's
own `time` (RFC 3339), `level`, and `msg` keys, plus `component`, plus
whatever key/value pairs the call site adds (`"job"`, `"error"`,
`"duration_ms"`, …). Backed by the standard library's `log/slog` JSON
handler (no third-party logging library).

There's one process-wide logger, built behind a `sync.Once`: each
`main.go` calls `logger.Init` once, first thing — `"homelab-cron-api"` in
`cmd/api`, `"homelab-cron"` in `cmd/cron` — which sets `component` to that
service and also routes stray
output from dependencies using the standard `log` package through it.
Every other package just calls the package-level `logger.Info`/`Warn`/
`Error(msg string, args ...any)` — no per-package logger variables.
Logging before `Init` (e.g. from unit tests) lazily builds it with
component `unknown`; only the first `Init` takes effect. Since
`component` is the service, put which package/job a line came from in the
message or a field (e.g. `"closing consul response body"`, `"job"`).
robfig/cron's own logging goes through `cronLogger` in `scheduler.go`
(errors only). Don't import the standard `log` package — keep the message
short and constant, and put variable data in key/value args rather than
formatting it into the message.

### Cron scheduling (`internal/cron/`)

- `job.go` — the `Job` interface every cron job implements:
  - `Name() string` — identifies the job in logs.
  - `Schedule() string` — a standard 5-field cron expression (minute hour
    dom month dow), e.g. `"0 * * * *"` for hourly.
  - `Run(ctx context.Context) error` — executes one occurrence. Should
    return promptly once `ctx` is cancelled.
  - `AlertingEnabled() bool` — whether this job's results should be emailed
    after a run.
  - `EmailContent() string` — the markdown body to send when
    `AlertingEnabled` is true. Sent as the plain-text body of the alert
    email, not rendered to HTML.
- `scheduler.go` — `Scheduler`, a thin wrapper around
  `github.com/robfig/cron/v3`. `New(m mailer.Mailer, jobs ...Job)` registers
  each job's `Schedule()` via `AddFunc`, returning an error if any
  expression is invalid (fails fast at startup, not at the job's next
  scheduled run). `Start()` is non-blocking. `Stop()` cancels a context
  shared by all in-flight job runs, then blocks until robfig/cron confirms
  none are still running. Each scheduled tick calls the exported
  `RunJob(ctx context.Context, m mailer.Sender, j Job)`, which logs
  start/finish/duration, recovers a panic so one broken job can't take the
  caller down, and — once `Run` returns, success or not — sends the job's
  alert email via `m` if `AlertingEnabled()` is true and `EmailContent()`
  is non-empty. The send uses its own 10s timeout independent of `ctx`, so
  a job cancelled by `Stop()` still gets a chance to alert. `RunJob` is
  exported so `internal/api` can call it directly for `GET /job/{name}` —
  on-demand triggering needs none of `Scheduler`'s robfig/cron machinery,
  just this one function.

### Email alerting (`internal/mailer/`)

AWS SES is the only provider. `Mailer` is a one-method interface
(`Send(ctx, subject, body string) error`); `scheduler.go` is the only
caller, driven by each job's `AlertingEnabled`/`EmailContent`.

- `ses.go` — `SES`, wraps `github.com/aws/aws-sdk-go-v2/service/sesv2`.
  `NewSES(ctx, from, to)` calls `awsconfig.LoadDefaultConfig(ctx)`, so AWS
  credentials and region (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`,
  `AWS_SESSION_TOKEN`, `AWS_REGION`) come from the SDK's own default env
  chain — this package never reads or stores them itself. `from`/`to` are
  this service's own `ALERT_EMAIL_FROM`/`ALERT_EMAIL_TO`. `from` must be an
  SES-verified sender address.
- `noop.go` — `Noop`, logs instead of sending.
- `factory.go` — `Config` (`From`/`To`, this package's own small config,
  not `internal/config.Config`) and `New(ctx, cfg)`, which builds the
  `Sender` both `cmd/api/main.go` and `cmd/cron/main.go` use: `SES` if both
  fields are set, otherwise `Noop`, so alerting jobs don't error out in
  local dev without AWS credentials.

### Consul client (`internal/consul/`)

`Client` is a one-method interface (`Version(ctx, service string) (string,
error)`), read by `internal/jobs.VersionCheck` to look up a
dependency's actually-deployed version live instead of a hand-maintained
baseline. `HTTPClient` is the concrete implementation, backed by Consul's
HTTP health API (`GET /v1/health/service/{name}?passing=true`); it reads
the `version` key off the first passing instance's `Service.Meta`,
retrying up to 3 times (1s apart) on failure, and errors if there's no
passing instance or no `version` meta. `NewHTTPClient(addr, client)` takes
Consul's HTTP API base URL (`cfg.ConsulAddr`, env var `CONSUL_ADDR`) and an
`*http.Client`. Tests fake the `Client` interface directly rather than
standing up a Consul server.

### Vault client (`internal/vault/`)

`Client` is a one-method interface (`Version(ctx) (string, error)`), read
by `internal/jobs.VersionCheck` to look up Vault's own
actually-deployed version live. `HTTPClient` is the concrete
implementation, backed by Vault's unauthenticated health endpoint
(`GET /v1/sys/health`); it reads the `version` key off the response body,
retrying up to 3 times (1s apart) on failure. Unlike `internal/consul`'s
`Version`, a non-200 status here isn't itself treated as a failure — Vault
responds with a status that varies with seal/standby state (e.g. 503
sealed, 429 standby), but the JSON body, including `version`, is populated
regardless. `NewHTTPClient(addr, client)` takes Vault's HTTP API base URL
(`cfg.VaultAddr`, env var `VAULT_ADDR`) and an `*http.Client`. Tests fake
the `Client` interface directly rather than standing up a Vault server.

### Nomad client (`internal/nomad/`)

`Client` is a one-method interface (`Version(ctx) (string, error)`), read
by `internal/jobs.VersionCheck` to look up Nomad's own
actually-deployed version live. `HTTPClient` is the concrete
implementation, backed by Nomad's own agent-self endpoint (`GET
/v1/agent/self`); it reads the nested `Version` key off the response
body's `config.Version` object — unlike Consul's identically-shaped
endpoint, where `Config.Version` is a bare string, Nomad's `Config.Version`
is itself an object (`Version`/`Revision`/`VersionPrerelease`/
`VersionMetadata`) — retrying up to 3 times (1s apart) on failure, same
retry shape as `internal/consul` and `internal/vault`. Unlike those two's
equivalent endpoints, Nomad's requires an ACL token once ACLs are
enabled — `NewHTTPClient(addr, token, client)` takes Nomad's HTTP API base
URL (`cfg.NomadAddr`, env var `NOMAD_ADDR`), an ACL token (`cfg.NomadToken`,
env var `NOMAD_TOKEN` — see **Required env vars** below), and an
`*http.Client`; every request carries the token as Nomad's `X-Nomad-Token`
header. Tests fake the `Client` interface directly rather than standing up
a Nomad agent.

### Docker client (`internal/docker/`)

`Client` is a one-method interface (`Version(ctx) (string, error)`), read
by `internal/jobs.VersionCheck` to look up the local Docker
daemon's own actually-deployed version live. `HTTPClient` is the concrete
implementation, backed by the Docker Engine API's `GET /version`; it reads
the `Version` key off the response body, retrying up to 3 times (1s apart)
on failure. Unlike Consul/Vault/Nomad, dockerd doesn't listen on TCP by
default, so there's no `addr`/`*http.Client` pair to inject: instead
`NewHTTPClient(sockPath string)` builds its own `*http.Client` with a
`Transport.DialContext` that always dials the Unix socket at `sockPath`
(`cfg.DockerSock`), regardless of the request URL's host. Tests fake the
`Client` interface directly for `internal/jobs`, and exercise `HTTPClient`
itself against a real Unix socket listener in a temp dir rather than a
fake host/port server, so the dialer is actually covered.

### Postgres client (`internal/postgres/`)

`Client` is a one-method interface (`Ping(ctx) error`), used by
`internal/jobs.HealthCheck` to verify the homelab database accepts
connections. `PgxClient` is the concrete implementation, backed by
`github.com/jackc/pgx/v5`; `NewPgxClient(connString)` takes a libpq-style URL
(`cfg.DatabaseURL`, env var `DATABASE_URL`). Each `Ping` opens a fresh
connection, pings, and closes it — no pool, since the check runs once a
minute and a pool would hide a database that has stopped accepting *new*
connections. An empty or unparseable connection string is a failed `Ping`,
not a startup error. pgx redacts the password from its own errors, so they're
safe to log. pgx is pinned at v5.8.0 (the newest release that supports Go
1.24, which the Dockerfile builds with) — bumping it past that needs the
Dockerfile's Go version and `go.mod`'s `go` directive bumped too. Tests use
no live database: the empty/unparseable/refused-connection paths need none,
and `internal/jobs` fakes the `Client` interface.

### Jobs (`internal/jobs/`)

Each file is one `Job` implementation, independent of the others. Copy an
existing one as the starting point for a new job — there's no shared base
type or registry: `cmd/cron/main.go` passes the same jobs to `cron.New(...)`
(for scheduling) that `cmd/api/main.go` builds a `map[string]cron.Job`
from (for on-demand triggering), and both main.go's construct them
identically, by hand. Each job exports its `Name()` string as a const
(e.g. `AptUpgradeCheckJobName`, `VersionCheckJobName`) right next
to the `Name()` method that returns it — this is the single source of
truth for that job's `GET /job/{name}` trigger value, so it can't drift
out of sync with what the scheduler actually registers the job under.

- `aptupgrade.go` — `AptUpgradeCheck`, runs every morning at 9am, checks
  that a file has been modified within the last week. Takes that file's
  path as a constructor arg (`NewAptUpgradeCheck(path string) *AptUpgradeCheck`);
  both main.go's pass `filepath.Join(cfg.HostRoot, "var/log/apt/upgrade.log")`,
  the host's real apt upgrade log — apt only writes to it when a package
  upgrade actually runs, so a missing or stale file means unattended
  upgrades have stopped running. Logs a warning in that case; otherwise
  silent. `AlertingEnabled` is always `true`; `Run` stores the warning (if
  any) on the struct behind a mutex, and `EmailContent` returns it — empty
  after a healthy run, so the scheduler only emails when there's something
  to report. Worked example of a job that reads host filesystem state and
  uses per-run alerting state — copy this one for jobs that need to
  read/tail/scan files under the host mount or conditionally alert.
- `versioncheck.go` — `VersionCheck`, runs weekly (Monday
  7am), checks Consul, Vault, Nomad, Docker, Traefik, PostgreSQL, New
  Relic Infrastructure, and Fluent Bit versions against each project's latest stable
  release, and alerts when any has fallen significantly behind: any
  major-version bump, or a same-major minor-version drift of
  `minorVersionAlertThreshold` (5) or more — e.g. 1.34.1 vs 1.35.0 is fine,
  1.34.1 vs 1.39.0 alerts. A bare major bump always alerts regardless of
  size, since Consul/Vault/Nomad rarely move their major version at all —
  most real drift for this stack shows up as a minor-version gap instead.
  Latest-version sources: HashiCorp's releases API (Consul/Vault/Nomad), a
  project's own GitHub releases (Docker via moby/moby, Traefik, New Relic
  Infrastructure, Fluent Bit via fluent/fluent-bit), and postgresql.org's published version list (PostgreSQL
  — its Docker tag is just the bare major version, e.g. `postgres:16`).
  `dockerLatest` strips a `docker-` prefix off moby/moby's release tag
  before handing it to `parseVersion` — that repo tags Docker Engine's own
  releases `docker-vX.Y.Z`, distinct from its other release trains
  (`client/vX.Y.Z`, `api/vX.Y.Z`). No dependency has a hand-maintained
  pinned baseline: every `dependency`'s current version is fetched live.
  Traefik, PostgreSQL, New Relic Infrastructure, and Fluent Bit (Consul
  service `fluent-bit`) read theirs from Consul service meta (`dependency.fetchCurrent`, built by `consulCurrent` — see
  `internal/consul`), since their Nomad jobs register their image tag as
  `version` there. Consul, Vault, Nomad, and Docker instead read their own
  current version from their own endpoints: `consulAgentVersion`,
  `vaultCurrent`, `nomadCurrent`, and `dockerCurrent` each call straight
  into their respective client package (see the sections above for each
  endpoint's shape and quirks). Latest-version checks go out over public
  HTTPS to upstream endpoints — the reason the Dockerfile carries
  `ca-certificates` into the `scratch` image — but the task runs on the
  host network (`network { mode = "host" }` in `homelab-cron.nomad.hcl`),
  so the Consul/Vault/Nomad HTTP APIs resolve at
  `127.0.0.1:8500`/`8200`/`4646`, their own local-agent addresses, same as
  `internal/config`'s own defaults. The Docker daemon isn't reachable over
  the host network the same way — dockerd doesn't listen on TCP by
  default — so reaching it live means bind-mounting its Unix socket into
  the container instead (see **Host filesystem access** below for why
  this is a deliberate exception, not a reuse of the read-only host
  mount). Worked example of injecting fetch behavior for both the current
  version (`dependency.fetchCurrent`) and the latest version
  (`dependency.fetchLatest`) for testability, instead of hitting real APIs
  in unit tests.

- `healthcheck.go` — `HealthCheck`, runs every minute (`* * * * *`), and
  checks the homelab's dependencies are up. Currently one check: PostgreSQL
  accepts a connection (`postgres.Client.Ping`). Takes its dependency as a
  constructor arg (`NewHealthCheck(pg postgres.Client)`). Holds a slice of
  named checks; add a dependency by appending an entry in `NewHealthCheck`.
  Every check runs on every occurrence, each under its own 10s timeout
  (`checkTimeout`, a var so tests can shrink it), and `Run` returns all
  failures joined, each naming its check — `RunJob` logs it. `AlertingEnabled`
  is `true`, but alerts are throttled per check to one per `alertThrottle`
  (10 min): a failing `Run` sets `EmailContent` to just the failing checks
  that weren't alerted on in the last 10 minutes (so one check alerting
  doesn't suppress another's), and leaves it empty if there are none, which
  the scheduler treats as nothing to send. Last-alert times are in memory on
  the struct (`lastAlerts`, keyed by check name), so they reset on restart
  and are per-process — `cmd/api` and `cmd/cron` throttle independently. A
  check's time is recorded when its message is set, not when SES confirms
  the send, so a failed send still starts the window. Healthy runs don't
  reset it. The clock is the `now` field so tests can control it.

## Host filesystem access

This service only ever *reads* the host filesystem — it is never given
write access, on purpose (no job should be able to modify host state; if
one genuinely needs to, that's a deliberate exception to design for
explicitly, not something to fall into by reusing this mount).

The whole host root filesystem is bind-mounted read-only into the
container at `cfg.HostRoot` (env var `HOST_ROOT`, default `/host`):

- In `homelab-cron.nomad.hcl`, the task's `config.volumes` includes
  `"/:/host:ro,rslave"` — `:ro` makes it read-only, `rslave` propagates new
  host mounts (e.g. plugging in a drive) into the container without a
  restart. No Nomad `host_volume` provisioning is needed for this — it's a
  raw Docker bind mount, so there's nothing that needs to exist ahead of
  time on the Nomad client.
- In `docker-compose.yml`, the same shape: bind-mounted from
  `HOST_ROOT_HOST_PATH` (default `/`, read-only) — override this to a
  narrower path locally if you'd rather not expose your whole dev machine
  to the container.

Jobs needing to look at some host path should build it off `cfg.HostRoot`
(e.g. `filepath.Join(cfg.HostRoot, "var/log/apt/upgrade.log")` — see
`aptupgrade.go`) rather than hardcoding an absolute path, since a bare
`/var/log` inside the container refers to the container's own (empty)
filesystem, not the host's.

The Docker Engine API's Unix socket (`internal/docker`,
`internal/jobs.VersionCheck`'s live Docker version check) is the
one deliberate exception above: it's bind-mounted separately at
`cfg.DockerSock` (env var `DOCKER_SOCK`, default `/var/run/docker.sock`),
not folded into the host-root mount. `:ro` on that mount only stops the
container from replacing/deleting the socket file itself — a process
connected to it still gets the full Docker Engine API (create/exec/mount
containers, etc.), which is root-equivalent on the host regardless of the
mount's read-only flag. `internal/docker.HTTPClient` only ever calls `GET
/version` through it, but the mount itself grants more than that.

## Adding a new job

1. Add a new file in `internal/jobs/` implementing `cron.Job` (`Name`,
   `Schedule`, `Run`) — `aptupgrade.go` is the closest template, especially
   if the job reads host filesystem state. Export the job's name as a
   const next to `Name()` (see the **Jobs** section above), so it's
   immediately usable as a `GET /job/{name}` trigger value.
2. Construct it in *both* `cmd/cron/main.go`'s `cron.New(...)` call (so it
   runs on its schedule) and `cmd/api/main.go`'s job map (so it can be
   triggered on demand) — a job left out of one still works in the other,
   just not both.
3. If it needs a new env var (a secret, an external endpoint, etc.), add it
   to `internal/config/config.go`, `.env.example`, and *both* tasks'
   `env`/`template` stanzas in `homelab-cron.nomad.hcl` — `cron` needs it
   for the scheduled run, `api` for the on-demand one.

## Required env vars

- `ADDR` — listen address for `cmd/api`'s HTTP server (`GET /health`,
  `GET /job/{name}`). Defaults to `:8080`. `cmd/cron` has no HTTP server
  and doesn't read this.
- `HOST_ROOT` — path where the host's root filesystem is mounted
  read-only. Defaults to `/host`.
- `ALERT_EMAIL_FROM` / `ALERT_EMAIL_TO` — sender and (comma-separated)
  recipient addresses for job alert emails. Both optional; if either is
  unset, `mailer.New` returns `mailer.Noop` instead of `mailer.SES` and
  alerting jobs just log. `ALERT_EMAIL_FROM` must be an SES-verified
  sender address.
- `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` / `AWS_REGION` — required
  if the above are set. Standard AWS SDK env vars, read directly by
  `aws-sdk-go-v2`'s default config chain, not by this repo's own
  `internal/config`.
- `CONSUL_ADDR` — Consul's HTTP API base URL, used by
  `internal/consul.HTTPClient`. Defaults to `http://127.0.0.1:8500`
  (Consul's default local-agent address); left unset in production too,
  since the task runs on the host network and that address already
  reaches the placement node's own Consul agent. Override only for local
  dev if Consul isn't reachable at that default.
- `VAULT_ADDR` — Vault's HTTP API base URL, used by
  `internal/vault.HTTPClient`. Defaults to `http://127.0.0.1:8200`
  (Vault's default local-agent address), same reasoning as `CONSUL_ADDR`
  above.
- `NOMAD_ADDR` — Nomad's HTTP API base URL, used by
  `internal/nomad.HTTPClient`. Defaults to `http://127.0.0.1:4646`
  (Nomad's default local-agent address), same reasoning as `CONSUL_ADDR`
  above.
- `NOMAD_TOKEN` — ACL token sent as Nomad's `X-Nomad-Token` header on every
  request `internal/nomad.HTTPClient` makes, required once Nomad's ACLs
  are enabled (agent-self needs at least `agent:read`). A secret, rendered
  into the task's env from Vault's
  `secret/data/homelab/homelab-cron#nomad_token` by
  `homelab-cron.nomad.hcl`'s `template` block. Unset in local dev just
  means that one dependency's check fails and is reported rather than
  fatal (see `versioncheck.go`'s per-dependency error handling).
- `DATABASE_URL` — PostgreSQL connection URL, used by
  `internal/postgres.PgxClient` (`internal/jobs.HealthCheck`). Contains the
  database password, so it's a secret: never defaulted, and rendered into
  both tasks' env by `homelab-cron.nomad.hcl`'s own `template` block from
  Vault's `secret/data/homelab/homelab-cron#db_password` plus the postgres
  service's Consul address (its host port is dynamic), with the password
  `urlquery`-encoded. That template uses `service "postgres|any"` rather
  than the default filter on purpose: the default drops postgres from the
  result when its own health check fails, which would re-render the
  template and restart the task (`change_mode` defaults to `restart`)
  exactly when the check should be reporting postgres as down. The user and
  database name come from the Nomad file's `db_user`/`db_name` variables
  (`homelab-cron`/`homelab`). Unset means the health check fails and logs, which isn't fatal to anything else.
- `DOCKER_SOCK` — path (inside the container) to the Docker Engine API's
  Unix socket, used by `internal/docker.HTTPClient`. Defaults to
  `/var/run/docker.sock`, matching both `homelab-cron.nomad.hcl`'s and
  `docker-compose.yml`'s socket bind mount destination — see **Host
  filesystem access** above for why this is mounted separately from
  `HOST_ROOT`. Override only for local dev if the socket is bind-mounted
  somewhere else inside the container.

## Docker

Two-stage build, producing two static binaries:
1. `golang:1.24-alpine` — installs `ca-certificates`, builds two static
   binaries (`CGO_ENABLED=0`): `/usr/local/bin/api` from `./cmd/api` and
   `/usr/local/bin/cron` from `./cmd/cron`.
2. `FROM scratch` — copies in both binaries and
   `/etc/ssl/certs/ca-certificates.crt` (needed by `cron`'s outbound HTTPS
   calls to upstream release APIs — see `versioncheck.go`). There's
   deliberately no `ENTRYPOINT`/`CMD`: the image just holds both binaries,
   and the caller (Nomad's task `config.command`, or docker-compose's
   `command:`) picks which one to run.

Build/run — `--entrypoint` selects which binary a one-off `docker run`
starts. Both binaries need the host mount and Docker socket: `api` needs
them to actually run a triggered `AptUpgradeCheck`/`VersionCheck`,
same as `cron` needs them for the scheduled run:
```
docker build -t homelab-cron:dev .
docker run --rm -p 8080:8080 -v /:/host:ro -v /var/run/docker.sock:/var/run/docker.sock --entrypoint /usr/local/bin/api homelab-cron:dev
docker run --rm -v /:/host:ro -v /var/run/docker.sock:/var/run/docker.sock --entrypoint /usr/local/bin/cron homelab-cron:dev
```
(the Docker socket mount is only needed to exercise
`internal/jobs.VersionCheck`'s live Docker version check.)

For local dev, `docker-compose.yml` builds the same image once and runs it
twice, as two services (`api` and `cron`, each with its own `command:`
picking its binary and its own env/volumes); copy `.env.example` to `.env`,
then `docker compose up --build`.

## Deployment (`homelab-cron.nomad.hcl`)

`type = "service"` with one group holding two tasks, `api` and `cron`,
generated from a single `dynamic "task"` block (so their volumes, env,
`vault`, and `template` stanzas can't drift apart), both from the same `var.image` (see **Docker** above) but selecting their
binary via the docker driver's `config.command` (`/usr/local/bin/api` or
`/usr/local/bin/cron`). The group's `network { mode = "host" }` declares
one static port, `http` (8080), which only `api` listens on (`cron` has
no HTTP server). The `service`/`check` block (a Consul `service` block
with an HTTP check against `/health` — no Traefik tags, so it's never
exposed publicly) covers that port, so `GET /job/{name}` sits on the same
address as the health check, reachable only by curling the placement node
directly on the homelab's internal network. Since triggering a job means
`api` actually running it, `api` carries the same host filesystem/Docker
socket volumes, `vault` block, and AWS SES/Nomad-token `template` block
`cron` does — both tasks need identical runtime capability, they just do
it on different triggers (a schedule vs. an HTTP request).

CI (`.github/workflows/deploy.yml`) builds/pushes `sayze/homelab-cron` on
push to `master`, then runs `nomad job run` against the homelab's Nomad
cluster, passing `-var="image=sayze/homelab-cron:sha-<short-sha>"` plus
`-var="alert_email_from=..."` and `-var="alert_email_to=..."` sourced from
the `ALERT_EMAIL_FROM`/`ALERT_EMAIL_TO` repo variables (not secrets — the
actual AWS credentials come from Vault via `homelab-cron.nomad.hcl`'s
`template` block, not CI). `aws_region` is deliberately left unpassed so
it keeps the Nomad file's own `ap-southeast-2` default rather than being
overridden with an empty string if the repo variable were ever unset —
override it directly in the Nomad file (or via a manual `-var`) if you
need a different region. Requires
`REGISTRY_USER`/`NOMAD_ADDR`/`ALERT_EMAIL_FROM`/`ALERT_EMAIL_TO`
(variables) and `REGISTRY_TOKEN`/`NOMAD_CI_TOKEN` (secrets) configured as
repo (or org) config before it will run successfully.

This job's host mount needs no ansible/Nomad `host_volume` provisioning
ahead of time, since it's a raw Docker bind mount rather than a named
`host_volume`.
