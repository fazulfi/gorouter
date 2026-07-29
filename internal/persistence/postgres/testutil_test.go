package postgres

import (
	"context"
	"os"
	"testing"

	"gorouter/internal/persistence/postgres/migrations"

	"github.com/jackc/pgx/v5/pgxpool"
)

// foundationTables lists all tables created by the foundation migration.
// Used by teardownFoundationSchema for clean-up.
var foundationTables = []string{
	"gorouter_settings",
	"gorouter_users",
	"gorouter_sessions",
	"gorouter_api_keys",
	"gorouter_pats",
	"gorouter_audit_log",
	"gorouter_providers",
	"gorouter_jobs",
	"gorouter_migrations",
}

// getTestDSN returns the PostgreSQL DSN for integration tests.
// It checks POSTGRES_TEST_DSN first, then DATABASE_URL, then a default.
func getTestDSN() string {
	dsn := os.Getenv("POSTGRES_TEST_DSN")
	if dsn != "" {
		return dsn
	}
	dsn = os.Getenv("DATABASE_URL")
	if dsn != "" {
		return dsn
	}
	return "postgres://postgres:postgres@localhost:5432/postgres"
}

// newIntegrationPool creates a pgxpool.Pool for integration testing.
// The pool is closed via t.Cleanup. Tests are skipped in short mode.
func newIntegrationPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	dsn := getTestDSN()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse test DSN: %v", err)
	}
	cfg.MaxConns = 5
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("create test pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// setupFoundationSchema runs the foundation migration to create all tables.
// It returns a cleanup function that drops every foundation table and closes
// the migration tracking table. Tests that only need a pool (not the schema)
// can use newIntegrationPool directly instead.
func setupFoundationSchema(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	result, err := migrations.Migrate(ctx, pool, migrations.DirectionUp)
	if err != nil {
		t.Fatalf("setup foundation schema: %v", err)
	}
	if len(result.Applied) == 0 && len(result.Skipped) == 0 {
		t.Fatal("setup foundation schema: no migrations were applied or skipped")
	}
	t.Logf("foundation schema ready: applied=%d skipped=%d",
		len(result.Applied), len(result.Skipped))
}

// teardownFoundationSchema drops all foundation tables and the migration
// tracking table. It is safe to call even if the tables do not exist.
// This is intentionally not registered via t.Cleanup so callers control
// teardown timing (e.g., between test cases that share a pool).
func teardownFoundationSchema(ctx context.Context, pool *pgxpool.Pool) {
	for _, tbl := range foundationTables {
		_, _ = pool.Exec(ctx, "DROP TABLE IF EXISTS "+tbl+" CASCADE")
	}
}
