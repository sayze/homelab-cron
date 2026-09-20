package jobs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"homelab-cron/internal/postgres"
)

// checkTimeout bounds each individual health check, so one hung dependency
// can't delay the others or overlap into the next minute's run. It's a var
// so tests can shrink it.
var checkTimeout = 10 * time.Second

// healthCheckEntry is one named dependency check run by HealthCheck.
type healthCheckEntry struct {
	name string
	run  func(ctx context.Context) error
}

// HealthCheck verifies the homelab's dependencies are up, once a minute.
// Currently that's one check: PostgreSQL accepts a connection. Every check
// runs on every occurrence even if an earlier one fails, and Run returns all
// failures joined, so RunJob logs every dependency that's down, not just the
// first.
//
// AlertingEnabled is false: a per-minute schedule would email once a minute
// for as long as something stays down, so failures are only logged.
type HealthCheck struct {
	checks []healthCheckEntry
}

// NewHealthCheck builds a HealthCheck that pings pg.
func NewHealthCheck(pg postgres.Client) *HealthCheck {
	return &HealthCheck{checks: []healthCheckEntry{
		{name: "postgres", run: pg.Ping},
	}}
}

// HealthCheckJobName is this job's Name() — also the {name} path param value
// for triggering it via GET /job/{name} (see internal/api).
const HealthCheckJobName = "health-check"

// Name identifies this job in logs.
func (*HealthCheck) Name() string { return HealthCheckJobName }

// Schedule runs every minute.
func (*HealthCheck) Schedule() string { return "* * * * *" }

// Run executes every check and returns the failures, if any, joined into one
// error, which RunJob logs. Each failure names its check.
func (j *HealthCheck) Run(ctx context.Context) error {
	var errs []error
	for _, c := range j.checks {
		if err := runCheck(ctx, c); err != nil {
			errs = append(errs, fmt.Errorf("check %q: %w", c.name, err))
		}
	}
	return errors.Join(errs...)
}

func runCheck(ctx context.Context, c healthCheckEntry) error {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	return c.run(ctx)
}

// AlertingEnabled is always false for this job — see HealthCheck.
func (*HealthCheck) AlertingEnabled() bool { return false }

// EmailContent is always empty, since alerting is disabled.
func (*HealthCheck) EmailContent() string { return "" }
