//go:build unit

package logging

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogger_JSONShape(t *testing.T) {
	tests := []struct {
		name  string
		log   func(*Logger)
		level string
	}{
		{"info", func(l *Logger) { l.Info("hello", "job", "x") }, "INFO"},
		{"warn", func(l *Logger) { l.Warn("hello", "job", "x") }, "WARN"},
		{"error", func(l *Logger) { l.Error("hello", "job", "x") }, "ERROR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			tt.log(newLogger(&buf, "test-component"))

			var got map[string]any
			require.NoError(t, json.Unmarshal(buf.Bytes(), &got))

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

func TestLogger_ErrorRenderedAsString(t *testing.T) {
	var buf bytes.Buffer
	newLogger(&buf, "c").Error("failed", "error", errors.New("boom"))

	var got map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	assert.Equal(t, "boom", got["error"])
}
