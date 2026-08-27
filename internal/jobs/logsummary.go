package jobs

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"path/filepath"
)

// LogSummary is an example job exercising read-only host log access: it
// walks dir and logs how many regular files it found and their combined
// size. In production dir is filepath.Join(cfg.HostRoot, "var/log") — the
// host's /var/log, reachable read-only through the same bind mount
// DiskUsage uses (see homelab-cron.nomad.hcl). Template for jobs that need
// to observe host logs without ever writing to them.
type LogSummary struct {
	dir string
}

func NewLogSummary(dir string) LogSummary { return LogSummary{dir: dir} }

func (LogSummary) Name() string { return "log-summary" }

// Schedule: every 5 minutes.
func (LogSummary) Schedule() string { return "*/5 * * * *" }

func (j LogSummary) Run(ctx context.Context) error {
	var (
		files int
		bytes int64
	)

	err := filepath.WalkDir(j.dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.Type().IsRegular() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			files++
			bytes += info.Size()
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk %s: %w", j.dir, err)
	}

	log.Printf("log-summary: %s has %d files totalling %d bytes", j.dir, files, bytes)
	return nil
}
