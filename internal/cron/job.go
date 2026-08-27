// Package cron schedules and runs Jobs. It has no knowledge of what any
// given Job actually does — see internal/jobs for concrete jobs.
package cron

import "context"

// Job is implemented by any task the scheduler can run.
type Job interface {
	// Name identifies the job in logs. Should be unique among registered
	// jobs, but this isn't enforced.
	Name() string

	// Schedule is a standard 5-field cron expression (minute hour dom
	// month dow), e.g. "0 * * * *" for hourly. See
	// https://pkg.go.dev/github.com/robfig/cron/v3#hdr-CRON_Expression_Format.
	Schedule() string

	// Run executes one occurrence of the job. It should return promptly
	// after ctx is cancelled so the scheduler can shut down without delay.
	Run(ctx context.Context) error
}
