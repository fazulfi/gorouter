package migrations

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestRoundTrip000009 verifies that migration 000009 applies and rolls back
// cleanly, leaving the database in a state equivalent to pre-migration.
func TestRoundTrip000009(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	// Start clean: drop tracking table and any 000009 objects from prior
	// test runs that may have left them behind.
	cleanupMigrationsTable(ctx, pool)
	_, _ = pool.Exec(ctx, "DROP TABLE IF EXISTS gorouter_backups CASCADE")
	_, _ = pool.Exec(ctx, "DROP INDEX IF EXISTS idx_audit_log_job_provenance")
	_, _ = pool.Exec(ctx, "DROP INDEX IF EXISTS idx_audit_log_actor_time")
	_, _ = pool.Exec(ctx, "DROP INDEX IF EXISTS idx_audit_log_resource_time")
	_, _ = pool.Exec(ctx, "ALTER TABLE gorouter_audit_log DROP CONSTRAINT IF EXISTS chk_audit_actor_kind")
	_, _ = pool.Exec(ctx, "ALTER TABLE gorouter_audit_log DROP COLUMN IF EXISTS job_id")
	_, _ = pool.Exec(ctx, "ALTER TABLE gorouter_audit_log DROP COLUMN IF EXISTS actor_kind")
	if err := ensureMigrationsTable(ctx, pool); err != nil {
		t.Fatalf("ensure migrations table: %v", err)
	}

	// Apply only foundation through password resets (000001-000008).
	upMigrations, err := parseMigrations(DirectionUp)
	if err != nil {
		t.Fatalf("parse up migrations: %v", err)
	}
	for _, m := range upMigrations {
		if m.Version == "000009" {
			break // Stop before applying 000009.
		}
		if err := applyMigration(ctx, pool, m); err != nil {
			t.Fatalf("apply %s: %v", m.Name, err)
		}
	}

	// Snapshot tables present after 000001-000008.
	preTables := listTables(ctx, pool)
	if len(preTables) == 0 {
		t.Fatal("expected at least some tables after 000001-000008 migrations")
	}

	// Verify 000009 objects do NOT exist before migration.
	if preTables["gorouter_backups"] {
		t.Error("table gorouter_backups should not exist before migration 000009")
	}
	verifyIndexAbsent(ctx, pool, t, "idx_audit_log_job_provenance")
	verifyConstraintAbsent(ctx, pool, t, "chk_audit_actor_kind")

	// ── Apply 000009 ──
	var m009 Migration
	for _, m := range upMigrations {
		if m.Version == "000009" {
			m009 = m
			break
		}
	}
	if m009.Version != "000009" {
		t.Fatal("migration 000009 not found in embedded files")
	}
	if err := applyMigration(ctx, pool, m009); err != nil {
		t.Fatalf("apply 000009: %v", err)
	}

	// Verify the 000009 table now exists.
	postUpTables := listTables(ctx, pool)
	if !postUpTables["gorouter_backups"] {
		t.Error("table gorouter_backups was not created by migration 000009")
	}

	// Verify column types on gorouter_backups.
	verifyColumnType(ctx, pool, t, "gorouter_backups", "id", "uuid")
	verifyColumnType(ctx, pool, t, "gorouter_backups", "path", "text")
	verifyColumnType(ctx, pool, t, "gorouter_backups", "sha256", "text")
	verifyColumnType(ctx, pool, t, "gorouter_backups", "bytes", "bigint")
	verifyColumnType(ctx, pool, t, "gorouter_backups", "generated_by", "text")
	verifyColumnType(ctx, pool, t, "gorouter_backups", "verified_at", "timestamp with time zone")
	verifyColumnType(ctx, pool, t, "gorouter_backups", "restore_verified_at", "timestamp with time zone")
	verifyColumnType(ctx, pool, t, "gorouter_backups", "created_at", "timestamp with time zone")

	// Verify NOT NULL: id (PK), path, sha256 and bytes.
	verifyColumnNotNull(ctx, pool, t, "gorouter_backups", "id")
	verifyColumnNotNull(ctx, pool, t, "gorouter_backups", "path")
	verifyColumnNotNull(ctx, pool, t, "gorouter_backups", "sha256")
	verifyColumnNotNull(ctx, pool, t, "gorouter_backups", "bytes")

	// Verify the audit query indexes and the job provenance index.
	verifyIndexExists(ctx, pool, t, "idx_audit_log_actor_time", "gorouter_audit_log")
	verifyIndexExists(ctx, pool, t, "idx_audit_log_resource_time", "gorouter_audit_log")
	verifyIndexExists(ctx, pool, t, "idx_audit_log_job_provenance", "gorouter_audit_log")

	// The job provenance index is partial: it covers only job rows.
	verifyPartialIndexPredicate(ctx, pool, t, "idx_audit_log_job_provenance")

	// Verify the audit provenance columns and the integrity constraint.
	verifyColumnType(ctx, pool, t, "gorouter_audit_log", "job_id", "uuid")
	verifyColumnType(ctx, pool, t, "gorouter_audit_log", "actor_kind", "character varying")
	verifyColumnNotNull(ctx, pool, t, "gorouter_audit_log", "actor_kind")
	verifyConstraintExists(ctx, pool, t, "chk_audit_actor_kind", "gorouter_audit_log")

	// ── Rollback 000009 ──
	downMigrations, err := parseMigrations(DirectionDown)
	if err != nil {
		t.Fatalf("parse down migrations: %v", err)
	}
	var m009down Migration
	for _, m := range downMigrations {
		if m.Version == "000009" {
			m009down = m
			break
		}
	}
	if m009down.Version != "000009" {
		t.Fatal("migration 000009 down not found in embedded files")
	}
	if err := applyMigration(ctx, pool, m009down); err != nil {
		t.Fatalf("apply 000009 down: %v", err)
	}
	if err := removeMigrationRecord(ctx, pool, "000009"); err != nil {
		t.Fatalf("remove 000009 record: %v", err)
	}

	// After rollback, the 000009 objects should be gone.
	postDownTables := listTables(ctx, pool)
	if postDownTables["gorouter_backups"] {
		t.Error("table gorouter_backups should not exist after down migration 000009")
	}
	verifyIndexAbsent(ctx, pool, t, "idx_audit_log_job_provenance")
	verifyIndexAbsent(ctx, pool, t, "idx_audit_log_actor_time")
	verifyIndexAbsent(ctx, pool, t, "idx_audit_log_resource_time")
	verifyConstraintAbsent(ctx, pool, t, "chk_audit_actor_kind")
	verifyNoColumn(ctx, pool, t, "gorouter_audit_log", "job_id")
	verifyNoColumn(ctx, pool, t, "gorouter_audit_log", "actor_kind")

	// Pre-existing tables should survive.
	for tbl := range preTables {
		if !postDownTables[tbl] {
			t.Errorf("pre-existing table %s is missing after rollback", tbl)
		}
	}
}

// verifyIndexAbsent asserts that the given index does not exist.
func verifyIndexAbsent(ctx context.Context, pool *pgxpool.Pool, t *testing.T, indexName string) {
	t.Helper()
	rows, err := pool.Query(ctx,
		`SELECT indexname FROM pg_indexes WHERE indexname = $1`, indexName)
	if err != nil {
		t.Errorf("query index %s: %v", indexName, err)
		return
	}
	defer rows.Close()
	if rows.Next() {
		t.Errorf("index %s still exists", indexName)
	}
}

// verifyConstraintExists asserts that the given CHECK constraint exists on
// the given table.
func verifyConstraintExists(ctx context.Context, pool *pgxpool.Pool, t *testing.T, constraintName, table string) {
	t.Helper()
	rows, err := pool.Query(ctx,
		`SELECT conname FROM pg_constraint c
		 JOIN pg_class r ON r.oid = c.conrelid
		 WHERE c.conname = $1 AND r.relname = $2 AND c.contype = 'c'`,
		constraintName, table)
	if err != nil {
		t.Errorf("query constraint %s on %s: %v", constraintName, table, err)
		return
	}
	defer rows.Close()
	if !rows.Next() {
		t.Errorf("constraint %s on %s not found", constraintName, table)
	}
}

// verifyConstraintAbsent asserts that the given constraint does not exist.
func verifyConstraintAbsent(ctx context.Context, pool *pgxpool.Pool, t *testing.T, constraintName string) {
	t.Helper()
	rows, err := pool.Query(ctx,
		`SELECT conname FROM pg_constraint WHERE conname = $1`, constraintName)
	if err != nil {
		t.Errorf("query constraint %s: %v", constraintName, err)
		return
	}
	defer rows.Close()
	if rows.Next() {
		t.Errorf("constraint %s still exists", constraintName)
	}
}

// verifyPartialIndexPredicate asserts that the index is partial (has a
// WHERE predicate) and that the predicate restricts to job rows. The
// predicate text is compared structurally because PostgreSQL normalizes
// boolean casts in index predicates across versions.
func verifyPartialIndexPredicate(ctx context.Context, pool *pgxpool.Pool, t *testing.T, indexName string) {
	t.Helper()
	var indexdef string
	var isPartial bool
	err := pool.QueryRow(ctx,
		`SELECT pg_get_indexdef(i.indexrelid), i.indpred IS NOT NULL FROM pg_index i
		 JOIN pg_class c ON c.oid = i.indexrelid
		 WHERE c.relname = $1`, indexName).Scan(&indexdef, &isPartial)
	if err != nil {
		t.Errorf("query index definition %s: %v", indexName, err)
		return
	}
	if !isPartial {
		t.Errorf("index %s is not partial (no WHERE predicate)", indexName)
	}
	if !strings.Contains(indexdef, "WHERE") || !strings.Contains(indexdef, "actor_kind") || !strings.Contains(indexdef, "'job'") {
		t.Errorf("index %s definition %q does not restrict to actor_kind = 'job'", indexName, indexdef)
	}
}
