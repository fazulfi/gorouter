package migrations

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestRoundTrip000008 verifies that migration 000008 applies and rolls back
// cleanly, leaving the database in a state equivalent to pre-migration.
func TestRoundTrip000008(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	// Start clean: drop tracking table and any 000008 data tables from prior
	// test runs that may have left them behind.
	cleanupMigrationsTable(ctx, pool)
	_, _ = pool.Exec(ctx, "DROP TABLE IF EXISTS gorouter_password_resets CASCADE")
	if err := ensureMigrationsTable(ctx, pool); err != nil {
		t.Fatalf("ensure migrations table: %v", err)
	}

	// Apply only foundation through console migrations (000001-000007).
	upMigrations, err := parseMigrations(DirectionUp)
	if err != nil {
		t.Fatalf("parse up migrations: %v", err)
	}
	for _, m := range upMigrations {
		if m.Version == "000008" {
			break // Stop before applying 000008.
		}
		if err := applyMigration(ctx, pool, m); err != nil {
			t.Fatalf("apply %s: %v", m.Name, err)
		}
	}

	// Snapshot tables present after 000001-000007.
	preTables := listTables(ctx, pool)
	if len(preTables) == 0 {
		t.Fatal("expected at least some tables after 000001-000007 migrations")
	}

	// Verify 000008 objects do NOT exist before migration.
	if preTables["gorouter_password_resets"] {
		t.Error("table gorouter_password_resets should not exist before migration 000008")
	}

	// ── Apply 000008 ──
	var m008 Migration
	for _, m := range upMigrations {
		if m.Version == "000008" {
			m008 = m
			break
		}
	}
	if m008.Version != "000008" {
		t.Fatal("migration 000008 not found in embedded files")
	}
	if err := applyMigration(ctx, pool, m008); err != nil {
		t.Fatalf("apply 000008: %v", err)
	}

	// Verify the 000008 table now exists.
	postUpTables := listTables(ctx, pool)
	if !postUpTables["gorouter_password_resets"] {
		t.Error("table gorouter_password_resets was not created by migration 000008")
	}

	// Verify column types.
	verifyColumnType(ctx, pool, t, "gorouter_password_resets", "id", "uuid")
	verifyColumnType(ctx, pool, t, "gorouter_password_resets", "user_id", "uuid")
	verifyColumnType(ctx, pool, t, "gorouter_password_resets", "token_hash", "text")
	verifyColumnType(ctx, pool, t, "gorouter_password_resets", "requested_by", "text")
	verifyColumnType(ctx, pool, t, "gorouter_password_resets", "requested_at", "timestamp with time zone")
	verifyColumnType(ctx, pool, t, "gorouter_password_resets", "completed_at", "timestamp with time zone")
	verifyColumnType(ctx, pool, t, "gorouter_password_resets", "revoked_at", "timestamp with time zone")

	// Verify NOT NULL: token_hash (the only explicitly NOT NULL column; the
	// id primary key is implicitly NOT NULL).
	verifyColumnNotNull(ctx, pool, t, "gorouter_password_resets", "id")
	verifyColumnNotNull(ctx, pool, t, "gorouter_password_resets", "token_hash")

	// Verify the user FK is declared.
	verifyForeignKey(ctx, pool, t, "gorouter_password_resets", "user_id", "gorouter_users")

	// Security contract: the schema stores an opaque token_hash only. There
	// must be no plaintext token column on this table.
	verifyNoColumn(ctx, pool, t, "gorouter_password_resets", "token")

	// ── Rollback 000008 ──
	downMigrations, err := parseMigrations(DirectionDown)
	if err != nil {
		t.Fatalf("parse down migrations: %v", err)
	}
	var m008down Migration
	for _, m := range downMigrations {
		if m.Version == "000008" {
			m008down = m
			break
		}
	}
	if m008down.Version != "000008" {
		t.Fatal("migration 000008 down not found in embedded files")
	}
	if err := applyMigration(ctx, pool, m008down); err != nil {
		t.Fatalf("apply 000008 down: %v", err)
	}
	if err := removeMigrationRecord(ctx, pool, "000008"); err != nil {
		t.Fatalf("remove 000008 record: %v", err)
	}

	// After rollback, the 000008 table should be gone.
	postDownTables := listTables(ctx, pool)
	if postDownTables["gorouter_password_resets"] {
		t.Error("table gorouter_password_resets should not exist after down migration 000008")
	}

	// Pre-existing tables should survive.
	for tbl := range preTables {
		if !postDownTables[tbl] {
			t.Errorf("pre-existing table %s is missing after rollback", tbl)
		}
	}
}

// verifyForeignKey asserts that a foreign key constraint from the given
// column references the given table.
func verifyForeignKey(ctx context.Context, pool *pgxpool.Pool, t *testing.T, table, column, refTable string) {
	t.Helper()
	rows, err := pool.Query(ctx,
		`SELECT tc.constraint_name
		 FROM information_schema.table_constraints tc
		 JOIN information_schema.key_column_usage kcu
		   ON tc.constraint_name = kcu.constraint_name
		  AND tc.constraint_schema = kcu.constraint_schema
		 JOIN information_schema.constraint_column_usage ccu
		   ON tc.constraint_name = ccu.constraint_name
		  AND tc.constraint_schema = ccu.constraint_schema
		 WHERE tc.constraint_type = 'FOREIGN KEY'
		   AND tc.table_name = $1 AND kcu.column_name = $2
		   AND ccu.table_name = $3`, table, column, refTable)
	if err != nil {
		t.Errorf("query foreign key %s.%s -> %s: %v", table, column, refTable, err)
		return
	}
	defer rows.Close()
	if !rows.Next() {
		t.Errorf("foreign key %s(%s) -> %s not found", table, column, refTable)
	}
}

// verifyNoColumn asserts that the given table has no column with the given
// name. Used to prove the schema stores token_hash only and exposes no
// plaintext token storage column.
func verifyNoColumn(ctx context.Context, pool *pgxpool.Pool, t *testing.T, table, column string) {
	t.Helper()
	rows, err := pool.Query(ctx,
		`SELECT column_name FROM information_schema.columns
		 WHERE table_name = $1 AND column_name = $2`, table, column)
	if err != nil {
		t.Errorf("query column %s on %s: %v", column, table, err)
		return
	}
	defer rows.Close()
	if rows.Next() {
		t.Errorf("unexpected plaintext column %q on %s (only token_hash is allowed)", column, table)
	}
}
