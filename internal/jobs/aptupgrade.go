package jobs

import (
	"context"
	"log"
	"os"
	"time"
)

// AptUpgradeCheck verifies that unattended upgrades are actually running by
// checking the age of path, which in production is
// filepath.Join(cfg.HostRoot, "var/log/apt/upgrade.log") — apt only writes
// to this file when a package upgrade occurs. If the file is missing or
// hasn't been modified within maxAge, that means upgrades haven't run
// recently, and this logs a warning so it shows up in the job's logs.
type AptUpgradeCheck struct {
	path   string
	maxAge time.Duration
}

func NewAptUpgradeCheck(path string) AptUpgradeCheck {
	return AptUpgradeCheck{path: path, maxAge: 7 * 24 * time.Hour}
}

func (AptUpgradeCheck) Name() string { return "apt-upgrade-check" }

// Schedule: every morning at 9am.
func (AptUpgradeCheck) Schedule() string { return "0 9 * * *" }

func (j AptUpgradeCheck) Run(context.Context) error {
	info, err := os.Stat(j.path)
	if os.IsNotExist(err) {
		log.Printf("apt-upgrade-check: %s does not exist — apt upgrades may not be running", j.path)
		return nil
	}
	if err != nil {
		return err
	}

	if age := time.Since(info.ModTime()); age > j.maxAge {
		log.Printf("apt-upgrade-check: %s last modified %s ago (older than %s) — apt upgrades may not be running", j.path, age.Round(time.Hour), j.maxAge)
	}
	return nil
}
