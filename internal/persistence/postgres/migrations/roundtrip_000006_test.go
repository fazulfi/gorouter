package migrations

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestRoundTrip000006 verifies that migration 000006 applies and rolls back
// cleanly, leaving the database in a state equivalent to pre-migration.
func TestRoundTrip000006(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	// Start clean: drop tracking table and any 000006 data tables from prior
	// test runs that may have left them behind.
	cleanupMigrationsTable(ctx, pool)
	for _, tbl := range []string{
		"gorouter_usage_daily",
		"gorouter_request_details",
		"gorouter_request_history",
	} {
		_, _ = pool.Exec(ctx, "DROP TABLE IF EXISTS "+tbl+" CASCADE")
	}
	if err := ensureMigrationsTable(ctx, pool); err != nil {
		t.Fatalf("ensure migrations table: %v", err)
	}

	// Apply only foundation through proxy pools migrations (000001-000005).
	upMigrations, err := parseMigrations(DirectionUp)
	if err != nil {
		t.Fatalf("parse up migrations: %v", err)
	}
	for _, m := range upMigrations {
		if m.Version == "000006" {
			break // Stop before applying 000006.
		}
		if err := applyMigration(ctx, pool, m); err != nil {
			t.Fatalf("apply %s: %v", m.Name, err)
		}
	}

	// Snapshot tables present after 000001-000005.
	preTables := listTables(ctx, pool)
	if len(preTables) == 0 {
		t.Fatal("expected at least some tables after 000001-000005 migrations")
	}

	// Verify 000006 objects do NOT exist before migration.
	for _, tbl := range []string{
		"gorouter_usage_daily",
		"gorouter_request_details",
		"gorouter_request_history",
	} {
		if preTables[tbl] {
			t.Errorf("table %s should not exist before migration 000006", tbl)
		}
	}

	// ── Apply 000006 ──
	var m006 Migration
	for _, m := range upMigrations {
		if m.Version == "000006" {
			m006 = m
			break
		}
	}
	if m006.Version != "000006" {
		t.Fatal("migration 000006 not found in embedded files")
	}
	if err := applyMigration(ctx, pool, m006); err != nil {
		t.Fatalf("apply 000006: %v", err)
	}

	// Verify 000006 tables now exist.
	postUpTables := listTables(ctx, pool)
	for _, tbl := range []string{
		"gorouter_usage_daily",
		"gorouter_request_details",
		"gorouter_request_history",
	} {
		if !postUpTables[tbl] {
			t.Errorf("table %s was not created by migration 000006", tbl)
		}
	}

	// Verify column types on gorouter_usage_daily.
	verifyColumnType(ctx, pool, t, "gorouter_usage_daily", "day", "date")
	verifyColumnType(ctx, pool, t, "gorouter_usage_daily", "provider_id", "uuid")
	verifyColumnType(ctx, pool, t, "gorouter_usage_daily", "model_id", "text")
	verifyColumnType(ctx, pool, t, "gorouter_usage_daily", "requests", "integer")
	verifyColumnType(ctx, pool, t, "gorouter_usage_daily", "prompt_tokens", "bigint")
	verifyColumnType(ctx, pool, t, "gorouter_usage_daily", "completion_tokens", "bigint")
	verifyColumnType(ctx, pool, t, "gorouter_usage_daily", "cost", "numeric")

	// Verify the UNIQUE (day, provider_id, model_id) constraint.
	verifyUniqueConstraint(ctx, pool, t, "gorouter_usage_daily")

	// Verify request details columns, nullability and the debug_opt_in default.
	verifyColumnType(ctx, pool, t, "gorouter_request_details", "id", "uuid")
	verifyColumnType(ctx, pool, t, "gorouter_request_details", "request_id", "uuid")
	verifyColumnType(ctx, pool, t, "gorouter_request_details", "model", "text")
	verifyColumnType(ctx, pool, t, "gorouter_request_details", "status", "text")
	verifyColumnType(ctx, pool, t, "gorouter_request_details", "occurred_at", "timestamp with time zone")
	verifyColumnType(ctx, pool, t, "gorouter_request_details", "debug_opt_in", "boolean")
	verifyColumnDefault(ctx, pool, t, "gorouter_request_details", "debug_opt_in", "false")
	verifyColumnNotNull(ctx, pool, t, "gorouter_request_details", "debug_opt_in")

	// Verify request history columns.
	verifyColumnType(ctx, pool, t, "gorouter_request_history", "id", "uuid")
	verifyColumnType(ctx, pool, t, "gorouter_request_history", "model", "text")
	verifyColumnType(ctx, pool, t, "gorouter_request_history", "provider", "text")
	verifyColumnType(ctx, pool, t, "gorouter_request_history", "prompt_tokens", "integer")
	verifyColumnType(ctx, pool, t, "gorouter_request_history", "completion_tokens", "integer")
	verifyColumnType(ctx, pool, t, "gorouter_request_history", "status", "text")
	verifyColumnType(ctx, pool, t, "gorouter_request_history", "occurred_at", "timestamp with time zone")

	// ── Rollback 000006 ──
	downMigrations, err := parseMigrations(DirectionDown)
	if err != nil {
		t.Fatalf("parse down migrations: %v", err)
	}
	var m006down Migration
	for _, m := range downMigrations {
		if m.Version == "000006" {
			m006down = m
			break
		}
	}
	if m006down.Version != "000006" {
		t.Fatal("migration 000006 down not found in embedded files")
	}
	if err := applyMigration(ctx, pool, m006down); err != nil {
		t.Fatalf("apply 000006 down: %v", err)
	}
	if err := removeMigrationRecord(ctx, pool, "000006"); err != nil {
		t.Fatalf("remove 000006 record: %v", err)
	}

	// After rollback, 000006 tables should be gone.
	postDownTables := listTables(ctx, pool)
	for _, tbl := range []string{
		"gorouter_usage_daily",
		"gorouter_request_details",
		"gorouter_request_history",
	} {
		if postDownTables[tbl] {
			t.Errorf("table %s should not exist after down migration 000006", tbl)
		}
	}

	// Pre-existing tables should survive.
	for tbl := range preTables {
		if !postDownTables[tbl] {
			t.Errorf("pre-existing table %s is missing after rollback", tbl)
		}
	}
}

// verifyUniqueConstraint asserts a UNIQUE constraint exists on the given table.
func verifyUniqueConstraint(ctx context.Context, pool *pgxpool.Pool, t *testing.T, table string) {
	t.Helper()
	rows, err := pool.Query(ctx,
		`SELECT constraint_name FROM information_schema.table_constraints
		 WHERE table_name = $1 AND constraint_type = 'UNIQUE'`, table)
	if err != nil {
		t.Errorf("query unique constraint %s: %v", table, err)
		return
	}
	defer rows.Close()
	if !rows.Next() {
		t.Errorf("missing UNIQUE constraint on %s", table)
	}
}

// verifyColumnNotNull asserts the given column is NOT NULL.
func verifyColumnNotNull(ctx context.Context, pool *pgxpool.Pool, t *testing.T, table, col string) {
	t.Helper()
	rows, err := pool.Query(ctx,
		`SELECT is_nullable FROM information_schema.columns
		 WHERE table_name = $1 AND column_name = $2`, table, col)
	if err != nil {
		t.Errorf("query nullability %s.%s: %v", table, col, err)
		return
	}
	defer rows.Close()
	if !rows.Next() {
		t.Errorf("column %s.%s not found", table, col)
		return
	}
	var nullable string
	if err := rows.Scan(&nullable); err != nil {
		t.Errorf("scan nullability %s.%s: %v", table, col, err)
		return
	}
	if nullable != "NO" {
		t.Errorf("column %s.%s: is_nullable = %q, want NO", table, col, nullable)
	}
}
