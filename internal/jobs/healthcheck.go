package jobs

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"homelab-cron/internal/postgres"
)

// checkTimeout bounds each individual health check, so one hung dependency
// can't delay the others or overlap into the next minute's run. It's a var
// so tests can shrink it.
var checkTimeout = 10 * time.Second

// alertThrottle is the minimum gap between two alert emails from HealthCheck,
// so a dependency that stays down doesn't send one email per minute.
const alertThrottle = 10 * time.Minute

// healthCheckEntry is one named dependency check run by HealthCheck.
type healthCheckEntry struct {
	name string
	run  func(ctx context.Context) error
}

// HealthCheck verifies the homelab's dependencies are up, once a minute.
type HealthCheck struct {
	checks []healthCheckEntry
	now    func() time.Time // a field so tests can control the clock

	mu        sync.Mutex
	message   string
	lastAlert time.Time // when EmailContent last got a message for the scheduler to send; in memory only
}

// NewHealthCheck builds a HealthCheck that pings pg.
func NewHealthCheck(pg postgres.Client) *HealthCheck {
	return &HealthCheck{
		checks: []healthCheckEntry{
			{name: "postgres", run: pg.Ping},
		},
		now: time.Now,
	}
}

// HealthCheckJobName is this job's Name() — also the {name} path param value
// for triggering it via GET /job/{name} (see internal/api).
const HealthCheckJobName = "health-check"

// Name identifies this job in logs.
func (*HealthCheck) Name() string { return HealthCheckJobName }

// Schedule runs every minute.
func (*HealthCheck) Schedule() string { return "* * * * *" }

// Run executes every check and returns the failures, if any, joined into one
// error, which RunJob logs. Each failure names its check. If there are
// failures and no alert went out in the last alertThrottle, it also records
// them as the email to send; otherwise it records nothing to send.
func (j *HealthCheck) Run(ctx context.Context) error {
	var errs []error
	for _, c := range j.checks {
		if err := runCheck(ctx, c); err != nil {
			errs = append(errs, fmt.Errorf("check %q: %w", c.name, err))
		}
	}
	err := errors.Join(errs...)
	j.recordAlert(err)
	return err
}

// recordAlert sets the pending alert message from err, throttled to one per
// alertThrottle. A healthy run, or a failing one inside the throttle window,
// leaves the message empty, so the scheduler sends nothing.
func (j *HealthCheck) recordAlert(err error) {
	j.mu.Lock()
	defer j.mu.Unlock()

	j.message = ""
	if err == nil {
		return
	}
	now := j.now()
	if !j.lastAlert.IsZero() && now.Sub(j.lastAlert) < alertThrottle {
		return
	}
	j.lastAlert = now
	j.message = "Health check failed:\n\n" + err.Error()
}

func runCheck(ctx context.Context, c healthCheckEntry) error {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	return c.run(ctx)
}

// AlertingEnabled is always true; throttling is done by leaving EmailContent
// empty (see recordAlert), which the scheduler treats as nothing to send.
func (*HealthCheck) AlertingEnabled() bool { return true }

// EmailContent returns the failures from the most recent Run, or "" if that
// run was healthy or an alert was already sent within alertThrottle.
func (j *HealthCheck) EmailContent() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.message
}
