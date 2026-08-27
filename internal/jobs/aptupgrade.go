package jobs

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

// AptUpgradeCheck verifies that unattended upgrades are actually running by
// checking the age of path, which in production is
// filepath.Join(cfg.HostRoot, "var/log/apt/upgrade.log") — apt only writes
// to this file when a package upgrade occurs. If the file is missing or
// hasn't been modified within maxAge, that means upgrades haven't run
// recently: this logs a warning and, since AlertingEnabled is always true,
// the scheduler emails it too. EmailContent is empty after a healthy run,
// so the scheduler sends nothing when there's nothing to report.
type AptUpgradeCheck struct {
	path   string
	maxAge time.Duration

	mu      sync.Mutex
	message string
}

func NewAptUpgradeCheck(path string) *AptUpgradeCheck {
	return &AptUpgradeCheck{path: path, maxAge: 7 * 24 * time.Hour}
}

func (*AptUpgradeCheck) Name() string { return "apt-upgrade-check" }

// Schedule: every morning at 9am.
func (*AptUpgradeCheck) Schedule() string { return "0 9 * * *" }

func (j *AptUpgradeCheck) Run(context.Context) error {
	info, err := os.Stat(j.path)
	if os.IsNotExist(err) {
		msg := fmt.Sprintf("%s does not exist — apt upgrades may not be running", j.path)
		log.Printf("apt-upgrade-check: %s", msg)
		j.setMessage(msg)
		return nil
	}
	if err != nil {
		return err
	}

	if age := time.Since(info.ModTime()); age > j.maxAge {
		msg := fmt.Sprintf("%s last modified %s ago (older than %s) — apt upgrades may not be running", j.path, age.Round(time.Hour), j.maxAge)
		log.Printf("apt-upgrade-check: %s", msg)
		j.setMessage(msg)
		return nil
	}

	j.setMessage("")
	return nil
}

func (j *AptUpgradeCheck) setMessage(msg string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.message = msg
}

func (*AptUpgradeCheck) AlertingEnabled() bool { return true }

// EmailContent returns the warning from the most recent Run, or "" if that
// run found nothing wrong — the scheduler treats an empty EmailContent as
// nothing to send.
func (j *AptUpgradeCheck) EmailContent() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.message
}
