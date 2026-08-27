package jobs

import (
	"context"
	"fmt"
	"log"
	"syscall"
)

// DiskUsage is an example job exercising host filesystem access: it reports
// free space on the volume mounted at dir. In production dir is
// cfg.HostRoot, the read-only bind mount of the host's root filesystem (see
// homelab-cron.nomad.hcl). Template for jobs that need to read host
// filesystem state.
type DiskUsage struct {
	dir string
}

func NewDiskUsage(dir string) DiskUsage { return DiskUsage{dir: dir} }

func (DiskUsage) Name() string { return "disk-usage" }

// Schedule: every hour, on the hour.
func (DiskUsage) Schedule() string { return "0 * * * *" }

func (j DiskUsage) Run(context.Context) error {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(j.dir, &stat); err != nil {
		return fmt.Errorf("statfs %s: %w", j.dir, err)
	}

	freeBytes := stat.Bavail * uint64(stat.Bsize)
	totalBytes := stat.Blocks * uint64(stat.Bsize)
	log.Printf("disk-usage: %s has %d/%d bytes free", j.dir, freeBytes, totalBytes)
	return nil
}
