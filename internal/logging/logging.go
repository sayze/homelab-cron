// Package logging is the one place homelab-cron's log output is shaped, so
// everything upstream (Nomad → Fluent Bit) sees the same JSON structure:
//
//	{"timestamp":"...","level":"INFO","message":"...","component":"cron",...}
//
// plus any key/value pairs passed alongside the message. It wraps the
// standard library's log/slog JSON handler rather than a third-party
// logging library.
//
// There's a single process-wide logger: each main.go calls Init once with
// its component, and every other package logs through the package-level
// Info/Warn/Error functions.
package logging

import (
	"io"
	"log"
	"log/slog"
	"os"
	"sync"
)

// Keys every log line carries.
const (
	TimestampKey = "timestamp"
	MessageKey   = "message"
	ComponentKey = "component"
)

// defaultComponent tags lines logged before (or without) Init — e.g. from
// unit tests, which never run a main.go.
const defaultComponent = "homelab-cron"

var (
	once sync.Once
	std  *slog.Logger
	out  io.Writer = os.Stdout
)

// Init builds the process-wide logger, tagging every line with component
// (the service it's logging for, e.g. "api" or "cron"), and routes output
// from the standard library's log package (used by some dependencies)
// through it too. Call it once, first thing in main. Only the first call
// (including the implicit one the first Info/Warn/Error makes) has any
// effect.
func Init(component string) {
	once.Do(func() {
		h := slog.NewJSONHandler(out, &slog.HandlerOptions{ReplaceAttr: renameKeys})
		std = slog.New(h).With(ComponentKey, component)
		slog.SetDefault(std)
		log.SetFlags(0)
	})
}

// get returns the process-wide logger, building it with defaultComponent
// if Init hasn't been called yet.
func get() *slog.Logger {
	Init(defaultComponent)
	return std
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
func Info(msg string, args ...any) { get().Info(msg, args...) }

// Warn logs msg at warn level; see Info for args.
func Warn(msg string, args ...any) { get().Warn(msg, args...) }

// Error logs msg at error level; see Info for args. Pass the error itself
// as an "error" arg — it's rendered as its Error() string.
func Error(msg string, args ...any) { get().Error(msg, args...) }
