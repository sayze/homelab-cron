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

// alertThrottle is the minimum gap between two alert emails for the same
// check, so a dependency that stays down doesn't send one email per minute.
// It's tracked per check: one check alerting doesn't suppress another's.
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

	mu         sync.Mutex
	message    string
	lastAlerts map[string]time.Time // check name -> when it was last put in an alert; in memory only
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
// error, which RunJob logs. Each failure names its check. Failing checks that
// haven't been alerted on in the last alertThrottle are also recorded as the
// email to send; if there are none, nothing is recorded to send.
func (j *HealthCheck) Run(ctx context.Context) error {
	var errs []error
	var failed []healthCheckEntry
	for _, c := range j.checks {
		if err := runCheck(ctx, c); err != nil {
			errs = append(errs, fmt.Errorf("check %q: %w", c.name, err))
			failed = append(failed, c)
		}
	}
	j.recordAlert(failed, errs)
	return errors.Join(errs...)
}

// recordAlert sets the pending alert message from the failing checks,
// throttled per check: a check is included only if it wasn't included in the
// last alertThrottle. failed[i] and errs[i] are the same failure. If every
// failure is inside its check's throttle window (or there are none), the
// message is empty and the scheduler sends nothing.
func (j *HealthCheck) recordAlert(failed []healthCheckEntry, errs []error) {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.lastAlerts == nil {
		j.lastAlerts = map[string]time.Time{}
	}
	now := j.now()
	var toAlert []error
	for i, c := range failed {
		if last, ok := j.lastAlerts[c.name]; ok && now.Sub(last) < alertThrottle {
			continue
		}
		j.lastAlerts[c.name] = now
		toAlert = append(toAlert, errs[i])
	}

	j.message = ""
	if len(toAlert) > 0 {
		j.message = "Health check failed:\n\n" + errors.Join(toAlert...).Error()
	}
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
// run was healthy or every failure was already alerted within alertThrottle.
func (j *HealthCheck) EmailContent() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.message
}
