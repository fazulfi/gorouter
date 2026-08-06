package migrations

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestRoundTrip000005 verifies that migration 000005 applies and rolls back
// cleanly, leaving the database in a state equivalent to pre-migration.
func TestRoundTrip000005(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	// Start clean: drop tracking table and any 000005 data tables from prior
	// test runs that may have left them behind.
	cleanupMigrationsTable(ctx, pool)
	for _, tbl := range []string{
		"gorouter_proxy_pool_members",
		"gorouter_proxy_pools",
		"gorouter_provider_nodes",
	} {
		_, _ = pool.Exec(ctx, "DROP TABLE IF EXISTS "+tbl+" CASCADE")
	}
	if err := ensureMigrationsTable(ctx, pool); err != nil {
		t.Fatalf("ensure migrations table: %v", err)
	}

	// Apply only foundation + engine migrations (000001-000004).
	// We parse migrations and apply each one individually, stopping before 000005.
	upMigrations, err := parseMigrations(DirectionUp)
	if err != nil {
		t.Fatalf("parse up migrations: %v", err)
	}
	for _, m := range upMigrations {
		if m.Version == "000005" {
			break // Stop before applying 000005.
		}
		if err := applyMigration(ctx, pool, m); err != nil {
			t.Fatalf("apply %s: %v", m.Name, err)
		}
	}

	// Snapshot tables present after 000001-000004.
	preTables := listTables(ctx, pool)
	if len(preTables) == 0 {
		t.Fatal("expected at least some tables after 000001-000004 migrations")
	}

	// Verify 000005 objects do NOT exist before migration.
	for _, tbl := range []string{
		"gorouter_proxy_pools",
		"gorouter_proxy_pool_members",
		"gorouter_provider_nodes",
	} {
		if preTables[tbl] {
			t.Errorf("table %s should not exist before migration 000005", tbl)
		}
	}

	// ── Apply 000005 ──
	var m005 Migration
	for _, m := range upMigrations {
		if m.Version == "000005" {
			m005 = m
			break
		}
	}
	if m005.Version != "000005" {
		t.Fatal("migration 000005 not found in embedded files")
	}
	if err := applyMigration(ctx, pool, m005); err != nil {
		t.Fatalf("apply 000005: %v", err)
	}

	// Verify 000005 tables now exist.
	postUpTables := listTables(ctx, pool)
	for _, tbl := range []string{
		"gorouter_proxy_pools",
		"gorouter_proxy_pool_members",
		"gorouter_provider_nodes",
	} {
		if !postUpTables[tbl] {
			t.Errorf("table %s was not created by migration 000005", tbl)
		}
	}

	// Verify column types on gorouter_proxy_pools.
	verifyColumnType(ctx, pool, t, "gorouter_proxy_pools", "id", "uuid")
	verifyColumnType(ctx, pool, t, "gorouter_proxy_pools", "name", "text")
	verifyColumnType(ctx, pool, t, "gorouter_proxy_pools", "description", "text")

	// Verify FK constraints on gorouter_proxy_pool_members.
	verifyFKTo(ctx, pool, t, "gorouter_proxy_pool_members", "pool_id", "gorouter_proxy_pools")
	verifyFKTo(ctx, pool, t, "gorouter_proxy_pool_members", "proxy_config_id", "gorouter_proxy_configs")

	// Verify defaults on gorouter_provider_nodes.
	verifyColumnDefault(ctx, pool, t, "gorouter_provider_nodes", "priority", "0")
	verifyColumnDefault(ctx, pool, t, "gorouter_provider_nodes", "is_active", "true")

	// ── Rollback 000005 ──
	// Apply the down migration for 000005, then remove its tracking record.
	downMigrations, err := parseMigrations(DirectionDown)
	if err != nil {
		t.Fatalf("parse down migrations: %v", err)
	}
	var m005down Migration
	for _, m := range downMigrations {
		if m.Version == "000005" {
			m005down = m
			break
		}
	}
	if m005down.Version != "000005" {
		t.Fatal("migration 000005 down not found in embedded files")
	}
	if err := applyMigration(ctx, pool, m005down); err != nil {
		t.Fatalf("apply 000005 down: %v", err)
	}
	if err := removeMigrationRecord(ctx, pool, "000005"); err != nil {
		t.Fatalf("remove 000005 record: %v", err)
	}

	// After rollback, 000005 tables should be gone.
	postDownTables := listTables(ctx, pool)
	for _, tbl := range []string{
		"gorouter_proxy_pools",
		"gorouter_proxy_pool_members",
		"gorouter_provider_nodes",
	} {
		if postDownTables[tbl] {
			t.Errorf("table %s should not exist after down migration 000005", tbl)
		}
	}

	// Pre-existing tables should survive.
	for tbl := range preTables {
		if !postDownTables[tbl] {
			t.Errorf("pre-existing table %s is missing after rollback", tbl)
		}
	}
}

func listTables(ctx context.Context, pool *pgxpool.Pool) map[string]bool {
	res := make(map[string]bool)
	rows, err := pool.Query(ctx,
		`SELECT table_name FROM information_schema.tables
		 WHERE table_schema = 'public' AND table_type = 'BASE TABLE'
		   AND table_name LIKE 'gorouter_%'
		 ORDER BY table_name`)
	if err != nil {
		return res
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		res[name] = true
	}
	return res
}

func verifyColumnType(ctx context.Context, pool *pgxpool.Pool, t *testing.T, table, col, expectedType string) {
	t.Helper()
	var dataType string
	err := pool.QueryRow(ctx,
		`SELECT data_type FROM information_schema.columns
		 WHERE table_name = $1 AND column_name = $2`, table, col).Scan(&dataType)
	if err != nil {
		t.Errorf("query column %s.%s: %v", table, col, err)
		return
	}
	if dataType != expectedType {
		t.Errorf("column %s.%s: data_type = %q, want %q", table, col, dataType, expectedType)
	}
}

func verifyFKTo(ctx context.Context, pool *pgxpool.Pool, t *testing.T, table, column, refTable string) {
	t.Helper()
	rows, err := pool.Query(ctx,
		`SELECT 1 FROM information_schema.table_constraints tc
		 JOIN information_schema.key_column_usage kcu
		   ON tc.constraint_name = kcu.constraint_name
		   AND tc.table_schema = kcu.table_schema
		 JOIN information_schema.constraint_column_usage ccu
		   ON tc.constraint_name = ccu.constraint_name
		   AND tc.table_schema = ccu.table_schema
		 WHERE tc.constraint_type = 'FOREIGN KEY'
		   AND tc.table_name = $1
		   AND kcu.column_name = $2
		   AND ccu.table_name = $3`, table, column, refTable)
	if err != nil {
		t.Errorf("query FK %s.%s: %v", table, column, err)
		return
	}
	defer rows.Close()
	if !rows.Next() {
		t.Errorf("missing FK constraint on %s.%s -> %s", table, column, refTable)
	}
}

func verifyColumnDefault(ctx context.Context, pool *pgxpool.Pool, t *testing.T, table, col, expectedDefault string) {
	t.Helper()
	rows, err := pool.Query(ctx,
		`SELECT column_default FROM information_schema.columns
		 WHERE table_name = $1 AND column_name = $2`, table, col)
	if err != nil {
		t.Errorf("query default %s.%s: %v", table, col, err)
		return
	}
	defer rows.Close()
	if !rows.Next() {
		t.Errorf("column %s.%s not found", table, col)
		return
	}
	var def *string
	if err := rows.Scan(&def); err != nil {
		t.Errorf("scan default %s.%s: %v", table, col, err)
		return
	}
	if def == nil || *def != expectedDefault {
		got := "<nil>"
		if def != nil {
			got = *def
		}
		t.Errorf("column %s.%s: default = %q, want %q", table, col, got, expectedDefault)
	}
}
