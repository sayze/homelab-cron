//go:build unit

package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoad_Defaults(t *testing.T) {
	cfg := Load()

	assert.Equal(t, ":8080", cfg.Addr)
	assert.Equal(t, "/host", cfg.HostRoot)
}

func TestLoad_Overrides(t *testing.T) {
	t.Setenv("ADDR", ":9090")
	t.Setenv("HOST_ROOT", "/mnt/host")

	cfg := Load()

	assert.Equal(t, ":9090", cfg.Addr)
	assert.Equal(t, "/mnt/host", cfg.HostRoot)
}
