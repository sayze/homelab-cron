//go:build unit

package logging

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reset points the singleton at a fresh buffer and clears it, so the next
// Init (explicit or implicit) rebuilds it.
func reset(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	once, std, out = sync.Once{}, nil, &buf
	return &buf
}

func decodeLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var got map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &got))
		lines = append(lines, got)
	}
	return lines
}

func TestLog_JSONShape(t *testing.T) {
	tests := []struct {
		name  string
		log   func(string, ...any)
		level string
	}{
		{"info", Info, "INFO"},
		{"warn", Warn, "WARN"},
		{"error", Error, "ERROR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := reset(t)
			Init("test-component")
			tt.log("hello", "job", "x")

			got := decodeLines(t, buf)[0]
			assert.Equal(t, "test-component", got[ComponentKey])
			assert.Equal(t, "hello", got[MessageKey])
			assert.Equal(t, tt.level, got["level"])
			assert.Equal(t, "x", got["job"])
			assert.NotContains(t, got, "msg")
			assert.NotContains(t, got, "time")

			ts, ok := got[TimestampKey].(string)
			require.True(t, ok, "timestamp should be a string")
			_, err := time.Parse(time.RFC3339Nano, ts)
			assert.NoError(t, err)
		})
	}
}

func TestLog_ErrorRenderedAsString(t *testing.T) {
	buf := reset(t)
	Error("failed", "error", errors.New("boom"))

	assert.Equal(t, "boom", decodeLines(t, buf)[0]["error"])
}

func TestInit_OnlyFirstCallTakesEffect(t *testing.T) {
	buf := reset(t)
	Init("first")
	Init("second")
	Info("hello")

	assert.Equal(t, "first", decodeLines(t, buf)[0][ComponentKey])
}

func TestLog_WithoutInitUsesDefaultComponent(t *testing.T) {
	buf := reset(t)
	Info("hello")

	assert.Equal(t, defaultComponent, decodeLines(t, buf)[0][ComponentKey])
}
