package cron

import (
	"context"
	"fmt"
	"time"

	robfigcron "github.com/robfig/cron/v3"

	"github.com/sayze/homelab-cron/internal/logger"
	"github.com/sayze/homelab-cron/internal/mailer"
)

// alertTimeout bounds how long sending a job's alert email may take. It's
// deliberately independent of the job's own (cancellable-on-shutdown) ctx,
// so a job cancelled mid-run by Stop() still gets a chance to send its
// alert rather than having the send fail immediately alongside it.
const alertTimeout = 10 * time.Second

// Scheduler runs a fixed set of Jobs, each on its own cron schedule.
type Scheduler struct {
	c      *robfigcron.Cron
	cancel context.CancelFunc
}

const schedulerLocation = "Australia/Brisbane"

// New builds a Scheduler with jobs registered against their schedules,
// interpreted in schedulerLocation. m is used to send the alert email for
// any job whose AlertingEnabled() returns true after it runs. It returns an
// error if any job's Schedule() is not a valid cron expression, or if
// schedulerLocation can't be loaded. Call Start to begin running jobs.
func New(m mailer.Sender, jobs ...Job) (*Scheduler, error) {
	loc, err := time.LoadLocation(schedulerLocation)
	if err != nil {
		return nil, fmt.Errorf("load scheduler location %q: %w", schedulerLocation, err)
	}

	c := robfigcron.New(robfigcron.WithLocation(loc), robfigcron.WithLogger(cronLogger{}))
	ctx, cancel := context.WithCancel(context.Background())

	for _, j := range jobs {
		if _, err := c.AddFunc(j.Schedule(), func() { RunJob(ctx, m, j) }); err != nil {
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

// RunJob executes j.Run, logging its outcome and recovering from a panic
// so one broken job can't take the caller down. If j.AlertingEnabled(), it
// sends j.EmailContent() as an alert email via m once Run returns,
// regardless of whether Run succeeded. Exported so internal/api can call
// it directly for GET /job/{name}, without going through Scheduler.
func RunJob(ctx context.Context, m mailer.Sender, j Job) {
	start := time.Now()
	logger.Info("job starting", "job", j.Name())

	defer func() {
		if r := recover(); r != nil {
			logger.Error("job panicked", "job", j.Name(), "duration_ms", time.Since(start).Milliseconds(), "panic", fmt.Sprint(r))
		}
	}()

	err := j.Run(ctx)
	if err != nil {
		logger.Error("job failed", "job", j.Name(), "duration_ms", time.Since(start).Milliseconds(), "error", err)
	} else {
		logger.Info("job finished", "job", j.Name(), "duration_ms", time.Since(start).Milliseconds())
	}

	sendAlert(m, j)
}

// sendAlert sends j's alert email if it has one to send. It runs on its
// own bounded timeout rather than the job's ctx, so a job cancelled by
// Stop() still gets a chance to alert.
func sendAlert(m mailer.Sender, j Job) {
	if !j.AlertingEnabled() {
		return
	}
	body := j.EmailContent()
	if body == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), alertTimeout)
	defer cancel()

	subject := fmt.Sprintf("homelab-cron: %s alert", j.Name())
	if err := m.Send(ctx, subject, body); err != nil {
		logger.Error("alert email failed", "job", j.Name(), "error", err)
	}
}

// cronLogger adapts robfig/cron's own logging to this service's JSON
// logging — robfig's default writes plain text to stdout. Like robfig's
// default (non-verbose) logger, it drops Info: those are per-tick
// "wake"/"run" noise, and RunJob already logs each run.
type cronLogger struct{}

func (cronLogger) Info(string, ...any) {}

func (cronLogger) Error(err error, msg string, keysAndValues ...any) {
	logger.Error(msg, append(keysAndValues, "error", err)...)
}
