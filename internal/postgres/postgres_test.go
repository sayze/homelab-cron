//go:build unit

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPgxClient_Ping_EmptyConnString(t *testing.T) {
	err := NewPgxClient("").Ping(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestPgxClient_Ping_UnparseableConnStringDoesNotLeakPassword(t *testing.T) {
	err := NewPgxClient("postgres://ops:s3cr3t-pw@localhost:notaport/homelab").Ping(context.Background())

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "s3cr3t-pw")
}

func TestPgxClient_Ping_Unreachable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Port 1 on loopback: nothing listens there, so the connection is refused.
	err := NewPgxClient("postgres://ops:s3cr3t-pw@127.0.0.1:1/homelab?sslmode=disable").Ping(ctx)

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "s3cr3t-pw")
}
