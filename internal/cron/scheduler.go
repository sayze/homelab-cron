package cron

import (
	"context"
	"fmt"
	"log"
	"time"

	robfigcron "github.com/robfig/cron/v3"
)

// Scheduler runs a fixed set of Jobs, each on its own cron schedule.
type Scheduler struct {
	c      *robfigcron.Cron
	cancel context.CancelFunc
}

// New builds a Scheduler with jobs registered against their schedules. It
// returns an error if any job's Schedule() is not a valid cron expression.
// Call Start to begin running jobs.
func New(jobs ...Job) (*Scheduler, error) {
	c := robfigcron.New()
	ctx, cancel := context.WithCancel(context.Background())

	for _, j := range jobs {
		j := j
		if _, err := c.AddFunc(j.Schedule(), func() { runJob(ctx, j) }); err != nil {
			cancel()
			return nil, fmt.Errorf("register job %q: %w", j.Name(), err)
		}
	}

	return &Scheduler{c: c, cancel: cancel}, nil
}

// Start begins running scheduled jobs. Non-blocking.
func (s *Scheduler) Start() {
	s.c.Start()
}

// Stop cancels the context passed to any in-flight job's Run, then blocks
// until the scheduler confirms no job is still running.
func (s *Scheduler) Stop() {
	s.cancel()
	<-s.c.Stop().Done()
}

// runJob executes j.Run, logging its outcome and recovering from a panic so
// one broken job can't take down the scheduler.
func runJob(ctx context.Context, j Job) {
	start := time.Now()
	log.Printf("cron: %s starting", j.Name())

	defer func() {
		if r := recover(); r != nil {
			log.Printf("cron: %s panicked after %s: %v", j.Name(), time.Since(start), r)
		}
	}()

	if err := j.Run(ctx); err != nil {
		log.Printf("cron: %s failed after %s: %v", j.Name(), time.Since(start), err)
		return
	}

	log.Printf("cron: %s finished in %s", j.Name(), time.Since(start))
}
