// Package postgres is a minimal connectivity client for PostgreSQL.
// internal/jobs.HealthCheck uses it to verify the homelab database accepts
// connections.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"homelab-cron/internal/logging"
)

// closeTimeout bounds how long Ping waits for the connection to close
// cleanly once the check itself is done.
const closeTimeout = time.Second

// Client checks that a PostgreSQL database is reachable. PgxClient is the
// concrete implementation; tests fake this interface directly rather than
// standing up a database.
type Client interface {
	// Ping opens a connection, verifies the server responds, and closes it.
	Ping(ctx context.Context) error
}

// PgxClient is a Client backed by github.com/jackc/pgx/v5.
type PgxClient struct {
	connString string
}

// NewPgxClient builds a PgxClient for connString, a libpq-style URL or
// key/value DSN (homelab-cron's own DATABASE_URL config). It's parsed
// lazily by Ping, so a bad or empty connString surfaces as a failed check
// rather than a startup failure.
func NewPgxClient(connString string) *PgxClient {
	return &PgxClient{connString: connString}
}

// Ping implements Client. It opens a fresh connection on every call rather
// than holding a pool: the health check runs once a minute, and a pool would
// hide exactly the failure this is meant to catch (a database that no longer
// accepts new connections).
//
// pgx redacts the password from its own connect and parse errors, so the
// returned error is safe to log.
func (c *PgxClient) Ping(ctx context.Context) error {
	if c.connString == "" {
		return errors.New("postgres: connection string is empty")
	}

	conn, err := pgx.Connect(ctx, c.connString)
	if err != nil {
		return fmt.Errorf("postgres: connect: %w", err)
	}
	defer func() {
		// ctx may already be cancelled or expired, so closing gets its own
		// short timeout.
		closeCtx, cancel := context.WithTimeout(context.Background(), closeTimeout)
		defer cancel()
		if cerr := conn.Close(closeCtx); cerr != nil {
			logging.Error("closing postgres connection", "error", cerr)
		}
	}()

	if err := conn.Ping(ctx); err != nil {
		return fmt.Errorf("postgres: ping: %w", err)
	}
	return nil
}
