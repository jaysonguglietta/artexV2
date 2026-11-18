package server

import (
	"database/sql"
	"os"
	"testing"

	"github.com/Autumn-27/artex/db"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestMain acquires a PostgreSQL advisory lock (7337741002) for the entire
// server test suite so cross-package DELETE cleanup races with db/agent
// packages are avoided when running `go test ./...`.
func TestMain(m *testing.M) {
	os.Setenv("ARTEX_ALLOW_HOST_EXECUTION", "1") // Trusted test fixtures explicitly enable execution.
	// Disable the background notification loop suite-wide: fixtures drive realtime/digest steps directly, and a leaked three-second loop from another server could claim their rows and corrupt deterministic attempt accounting. Dispatch behavior remains covered through direct calls.
	os.Setenv(notifyBackgroundDisabledEnv, "1")
	// The server suite verifies the Korean localization of user-facing output,
	// so pin the default output language to Korean for the whole test binary.
	// Production defaults to English (config.DefaultLanguage); individual tests
	// that need another language set it explicitly (t.Setenv / the settings table).
	os.Setenv("ARTEX_LANG", "en")

	dsn, _, err := db.DSN()
	if err != nil {
		os.Exit(m.Run())
	}
	conn, err := sql.Open("pgx", dsn)
	if err != nil || conn.Ping() != nil {
		os.Exit(m.Run())
	}
	defer conn.Close()
	if _, err := conn.Exec(`SELECT pg_advisory_lock(7337741002)`); err != nil {
		os.Exit(m.Run())
	}
	defer conn.Exec(`SELECT pg_advisory_unlock(7337741002)`) //nolint:errcheck
	os.Exit(m.Run())
}
