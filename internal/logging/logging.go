// Package logging is the one place homelab-cron's log output is shaped, so
// everything upstream (Nomad → Fluent Bit) sees the same JSON structure:
//
//	{"timestamp":"...","level":"INFO","message":"...","component":"consul",...}
//
// plus any key/value pairs passed alongside the message. It wraps the
// standard library's log/slog JSON handler rather than a third-party
// logging library.
package logging

import (
	"io"
	"log"
	"log/slog"
	"os"
)

// Keys every log line carries.
const (
	TimestampKey = "timestamp"
	MessageKey   = "message"
	ComponentKey = "component"
)

// Logger writes JSON log lines tagged with a fixed component.
type Logger struct {
	l *slog.Logger
}

// New returns a Logger that tags every line with component — the service
// or package it's logging for (e.g. "consul", "http"). Safe to call from
// a package-level var: it doesn't depend on any setup in main.
func New(component string) *Logger {
	return newLogger(os.Stdout, component)
}

func newLogger(w io.Writer, component string) *Logger {
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{ReplaceAttr: renameKeys})
	return &Logger{l: slog.New(h).With(ComponentKey, component)}
}

// renameKeys swaps slog's default "time"/"msg" keys for this service's
// "timestamp"/"message".
func renameKeys(groups []string, a slog.Attr) slog.Attr {
	if len(groups) > 0 {
		return a
	}
	switch a.Key {
	case slog.TimeKey:
		a.Key = TimestampKey
	case slog.MessageKey:
		a.Key = MessageKey
	}
	return a
}

// Info logs msg with optional key/value pairs, e.g.
// Info("job finished", "job", name, "duration_ms", 12).
func (lg *Logger) Info(msg string, args ...any) { lg.l.Info(msg, args...) }

// Warn logs msg at warn level; see Info for args.
func (lg *Logger) Warn(msg string, args ...any) { lg.l.Warn(msg, args...) }

// Error logs msg at error level; see Info for args. Pass the error itself
// as an "error" arg — it's rendered as its Error() string.
func (lg *Logger) Error(msg string, args ...any) { lg.l.Error(msg, args...) }

// CaptureStdlib routes output from the standard library's log package
// (used by some dependencies) through lg, at info level, so nothing
// reaches stdout/stderr as unstructured text.
func (lg *Logger) CaptureStdlib() {
	slog.SetDefault(lg.l)
	log.SetFlags(0)
}
