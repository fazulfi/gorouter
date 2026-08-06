package migrations

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestRoundTrip000007 verifies that migration 000007 applies and rolls back
// cleanly, leaving the database in a state equivalent to pre-migration.
func TestRoundTrip000007(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	// Start clean: drop tracking table and any 000007 data tables from prior
	// test runs that may have left them behind.
	cleanupMigrationsTable(ctx, pool)
	_, _ = pool.Exec(ctx, "DROP TABLE IF EXISTS gorouter_console_logs CASCADE")
	if err := ensureMigrationsTable(ctx, pool); err != nil {
		t.Fatalf("ensure migrations table: %v", err)
	}

	// Apply only foundation through usage migrations (000001-000006).
	upMigrations, err := parseMigrations(DirectionUp)
	if err != nil {
		t.Fatalf("parse up migrations: %v", err)
	}
	for _, m := range upMigrations {
		if m.Version == "000007" {
			break // Stop before applying 000007.
		}
		if err := applyMigration(ctx, pool, m); err != nil {
			t.Fatalf("apply %s: %v", m.Name, err)
		}
	}

	// Snapshot tables present after 000001-000006.
	preTables := listTables(ctx, pool)
	if len(preTables) == 0 {
		t.Fatal("expected at least some tables after 000001-000006 migrations")
	}

	// Verify 000007 objects do NOT exist before migration.
	if preTables["gorouter_console_logs"] {
		t.Error("table gorouter_console_logs should not exist before migration 000007")
	}

	// ── Apply 000007 ──
	var m007 Migration
	for _, m := range upMigrations {
		if m.Version == "000007" {
			m007 = m
			break
		}
	}
	if m007.Version != "000007" {
		t.Fatal("migration 000007 not found in embedded files")
	}
	if err := applyMigration(ctx, pool, m007); err != nil {
		t.Fatalf("apply 000007: %v", err)
	}

	// Verify the 000007 table and index now exist.
	postUpTables := listTables(ctx, pool)
	if !postUpTables["gorouter_console_logs"] {
		t.Error("table gorouter_console_logs was not created by migration 000007")
	}
	verifyIndexExists(ctx, pool, t, "idx_console_logs_seq", "gorouter_console_logs")

	// Verify column types.
	verifyColumnType(ctx, pool, t, "gorouter_console_logs", "id", "bigint")
	verifyColumnType(ctx, pool, t, "gorouter_console_logs", "seq", "bigint")
	verifyColumnType(ctx, pool, t, "gorouter_console_logs", "level", "text")
	verifyColumnType(ctx, pool, t, "gorouter_console_logs", "message", "text")
	verifyColumnType(ctx, pool, t, "gorouter_console_logs", "redacted_message", "text")
	verifyColumnType(ctx, pool, t, "gorouter_console_logs", "occurred_at", "timestamp with time zone")
	verifyColumnType(ctx, pool, t, "gorouter_console_logs", "retention_until", "timestamp with time zone")

	// Verify NOT NULL columns: seq (the ordering/retention key) and
	// redacted_message (the authoritative console payload).
	verifyColumnNotNull(ctx, pool, t, "gorouter_console_logs", "seq")
	verifyColumnNotNull(ctx, pool, t, "gorouter_console_logs", "redacted_message")

	// ── Rollback 000007 ──
	downMigrations, err := parseMigrations(DirectionDown)
	if err != nil {
		t.Fatalf("parse down migrations: %v", err)
	}
	var m007down Migration
	for _, m := range downMigrations {
		if m.Version == "000007" {
			m007down = m
			break
		}
	}
	if m007down.Version != "000007" {
		t.Fatal("migration 000007 down not found in embedded files")
	}
	if err := applyMigration(ctx, pool, m007down); err != nil {
		t.Fatalf("apply 000007 down: %v", err)
	}
	if err := removeMigrationRecord(ctx, pool, "000007"); err != nil {
		t.Fatalf("remove 000007 record: %v", err)
	}

	// After rollback, the 000007 table should be gone.
	postDownTables := listTables(ctx, pool)
	if postDownTables["gorouter_console_logs"] {
		t.Error("table gorouter_console_logs should not exist after down migration 000007")
	}

	// Pre-existing tables should survive.
	for tbl := range preTables {
		if !postDownTables[tbl] {
			t.Errorf("pre-existing table %s is missing after rollback", tbl)
		}
	}
}

// verifyIndexExists asserts an index with the given name exists and is
// defined over the given table.
func verifyIndexExists(ctx context.Context, pool *pgxpool.Pool, t *testing.T, indexName, table string) {
	t.Helper()
	rows, err := pool.Query(ctx,
		`SELECT indexname FROM pg_indexes
		 WHERE indexname = $1 AND tablename = $2`, indexName, table)
	if err != nil {
		t.Errorf("query index %s: %v", indexName, err)
		return
	}
	defer rows.Close()
	if !rows.Next() {
		t.Errorf("index %s on %s not found", indexName, table)
	}
}
