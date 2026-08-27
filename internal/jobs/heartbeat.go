// Package jobs holds concrete cron.Job implementations. Each job is its own
// type conforming to cron.Job (Name, Schedule, Run) — copy an existing job
// as a starting point for a new one.
package jobs

import (
	"context"
	"log"
)

// Heartbeat is a minimal example job with no external dependencies: it just
// logs on a fixed schedule. Useful as a smoke test that the scheduler is
// wired up and running correctly.
type Heartbeat struct{}

func NewHeartbeat() Heartbeat { return Heartbeat{} }

func (Heartbeat) Name() string { return "heartbeat" }

// Schedule: every 15 minutes.
func (Heartbeat) Schedule() string { return "*/15 * * * *" }

func (Heartbeat) Run(context.Context) error {
	log.Println("heartbeat: still alive")
	return nil
}
