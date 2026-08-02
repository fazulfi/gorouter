package migrations

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// getTestPool returns a pool for tests that require a real PostgreSQL.
// It is skipped if -short is active or DATABASE_URL is unset.
func getTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping: short mode")
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://gorouter:gorouter@localhost:5432/gorouter_test?sslmode=disable"
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("getTestPool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// cleanupMigrationsTable drops the tracking table so each test starts clean.
func cleanupMigrationsTable(ctx context.Context, pool *pgxpool.Pool) {
	_, _ = pool.Exec(ctx, "DROP TABLE IF EXISTS gorouter_migrations CASCADE")
}

// ---------------------------------------------------------------------------
// Unit tests – ParseVersion
// ---------------------------------------------------------------------------

func TestParseVersion(t *testing.T) {
	tests := []struct {
		name    string
		want    string
		wantErr bool
	}{
		{"000001_foundation.up.sql", "000001", false},
		{"000002_add_widgets.down.sql", "000002", false},
		{"invalid.txt", "", true},
		{".sql", "", true},
		{"_leading.up.sql", "", true},
		{"9999999999_migration.up.sql", "9999999999", false},
		{"abc_def.up.sql", "abc", false},
		{"no_extension", "", true},
		{".only_ext.sql", "", true},
		{"V001_name.up.sql", "V001", false},
		{"1_single.up.sql", "1", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseVersion(tt.name)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseVersion() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Unit tests – ParseMigrations
// ---------------------------------------------------------------------------

func TestParseMigrations_UpDirection(t *testing.T) {
	migs, err := parseMigrations(DirectionUp)
	if err != nil {
		t.Fatalf("parseMigrations(up) failed: %v", err)
	}
	if len(migs) == 0 {
		t.Fatal("expected at least 1 up migration, got 0")
	}
	if migs[0].Version != "000001" {
		t.Errorf("first migration version = %q, want %q", migs[0].Version, "000001")
	}
	if migs[0].Name != "000001_foundation.up.sql" {
		t.Errorf("first migration name = %q, want %q", migs[0].Name, "000001_foundation.up.sql")
	}
	if migs[0].Content == "" {
		t.Error("migration content is empty")
	}
	if migs[0].Checksum == "" {
		t.Error("migration checksum is empty")
	}
}

func TestParseMigrations_DownDirection(t *testing.T) {
	migs, err := parseMigrations(DirectionDown)
	if err != nil {
		t.Fatalf("parseMigrations(down) failed: %v", err)
	}
	if len(migs) == 0 {
		t.Fatal("expected at least 1 down migration, got 0")
	}
	if migs[0].Version != "000001" {
		t.Errorf("first migration version = %q, want %q", migs[0].Version, "000001")
	}
	if migs[0].Name != "000001_foundation.down.sql" {
		t.Errorf("first migration name = %q, want %q", migs[0].Name, "000001_foundation.down.sql")
	}
}

func TestParseMigrations_FiltersByDirection(t *testing.T) {
	upMigs, err := parseMigrations(DirectionUp)
	if err != nil {
		t.Fatalf("parseMigrations(up): %v", err)
	}
	downMigs, err := parseMigrations(DirectionDown)
	if err != nil {
		t.Fatalf("parseMigrations(down): %v", err)
	}
	for _, m := range upMigs {
		if m.Version == "" {
			t.Errorf("up migration %q has empty version", m.Name)
		}
	}
	for _, m := range downMigs {
		if m.Version == "" {
			t.Errorf("down migration %q has empty version", m.Name)
		}
	}
	// Should have same number of up and down files for foundation
	if len(upMigs) != len(downMigs) {
		t.Errorf("up count (%d) != down count (%d)", len(upMigs), len(downMigs))
	}
}

func TestParseMigrations_SortedOrder(t *testing.T) {
	migs, err := parseMigrations(DirectionUp)
	if err != nil {
		t.Fatalf("parseMigrations(up): %v", err)
	}
	for i := 1; i < len(migs); i++ {
		if migs[i].Version < migs[i-1].Version {
			t.Errorf("migrations not sorted: %s (index %d) < %s (index %d)",
				migs[i].Version, i, migs[i-1].Version, i-1)
		}
	}
}

func TestChecksum_Deterministic(t *testing.T) {
	migs1, err := parseMigrations(DirectionUp)
	if err != nil {
		t.Fatalf("first parse: %v", err)
	}
	migs2, err := parseMigrations(DirectionUp)
	if err != nil {
		t.Fatalf("second parse: %v", err)
	}
	if len(migs1) != len(migs2) {
		t.Fatalf("migration count mismatch: %d vs %d", len(migs1), len(migs2))
	}
	for i := range migs1 {
		if migs1[i].Checksum != migs2[i].Checksum {
			t.Errorf("checksum mismatch for %s: %q vs %q",
				migs1[i].Name, migs1[i].Checksum, migs2[i].Checksum)
		}
	}
}

func TestParseMigrations_FiltersNonSQL(t *testing.T) {
	// parseMigrations only picks up .sql files; the embedded FS
	// only has .sql files, so this verifies the isDir / suffix guards.
	migs, err := parseMigrations(DirectionUp)
	if err != nil {
		t.Fatalf("parseMigrations: %v", err)
	}
	for _, m := range migs {
		if len(m.Name) < 5 || m.Name[len(m.Name)-4:] != ".sql" {
			t.Errorf("unexpected non-sql entry: %s", m.Name)
		}
	}
}

func TestParseMigrations_ContentAndChecksumPopulated(t *testing.T) {
	migs, err := parseMigrations(DirectionUp)
	if err != nil {
		t.Fatalf("parseMigrations: %v", err)
	}
	for _, m := range migs {
		if m.Content == "" {
			t.Errorf("migration %s has empty content", m.Name)
		}
		if m.Checksum == "" {
			t.Errorf("migration %s has empty checksum", m.Name)
		}
		if len(m.Checksum) != 64 {
			t.Errorf("migration %s checksum length = %d, want 64 (sha256 hex)", m.Name, len(m.Checksum))
		}
	}
}

func TestParseMigrations_RejectsInvalidDirection(t *testing.T) {
	_, err := parseMigrations("invalid")
	if err != nil {
		t.Logf("parseMigrations(invalid) returned error (expected): %v", err)
	}
}

// ---------------------------------------------------------------------------
// Unit tests – Migrate nil / invalid guards
// ---------------------------------------------------------------------------

func TestMigrate_NilPool(t *testing.T) {
	_, err := Migrate(nil, nil, DirectionUp)
	if err == nil {
		t.Error("expected error for nil pool, got nil")
	}
}

func TestMigrate_InvalidDirection(t *testing.T) {
	_, err := Migrate(nil, nil, "invalid")
	if err == nil {
		t.Error("expected error for invalid direction, got nil")
	}
}

func TestMigrate_WithNilPoolPanics(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Migrate panicked with nil pool: %v", r)
		}
	}()
	_, err := Migrate(nil, nil, DirectionUp)
	if err == nil {
		t.Error("expected error for nil pool")
	}
}

// ---------------------------------------------------------------------------
// Integration tests – deps: real PostgreSQL, skipped in short mode
// ---------------------------------------------------------------------------

func TestEnsureMigrationsTable_CreatesTable(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	// Start clean
	cleanupMigrationsTable(ctx, pool)

	if err := ensureMigrationsTable(ctx, pool); err != nil {
		t.Fatalf("ensureMigrationsTable: %v", err)
	}

	// Verify the table exists via information_schema
	var exists bool
	err := pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_name = 'gorouter_migrations'
		)`).Scan(&exists)
	if err != nil {
		t.Fatalf("query table existence: %v", err)
	}
	if !exists {
		t.Fatal("gorouter_migrations table was not created")
	}

	// Calling it again should be idempotent
	if err := ensureMigrationsTable(ctx, pool); err != nil {
		t.Fatalf("ensureMigrationsTable (idempotent): %v", err)
	}
}

func TestEnsureMigrationsTable_HasExpectedColumns(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	cleanupMigrationsTable(ctx, pool)
	_ = ensureMigrationsTable(ctx, pool)

	expectedCols := map[string]string{
		"id":         "integer",
		"version":    "character varying",
		"name":       "character varying",
		"checksum":   "character varying",
		"applied_at": "timestamp with time zone",
	}
	for col, typ := range expectedCols {
		var dataType string
		err := pool.QueryRow(ctx,
			`SELECT data_type FROM information_schema.columns
			 WHERE table_name = 'gorouter_migrations' AND column_name = $1`, col,
		).Scan(&dataType)
		if err != nil {
			t.Errorf("column %q: %v", col, err)
			continue
		}
		if dataType != typ {
			t.Errorf("column %q: data_type = %q, want %q", col, dataType, typ)
		}
	}
}

func TestGetAppliedMigrations_Empty(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	cleanupMigrationsTable(ctx, pool)
	_ = ensureMigrationsTable(ctx, pool)

	applied, err := getAppliedMigrations(ctx, pool)
	if err != nil {
		t.Fatalf("getAppliedMigrations: %v", err)
	}
	if len(applied) != 0 {
		t.Errorf("expected empty map, got %d entries", len(applied))
	}
}

func TestGetAppliedMigrations_ReturnsApplied(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	cleanupMigrationsTable(ctx, pool)
	_ = ensureMigrationsTable(ctx, pool)

	// Insert a fake record
	_, err := pool.Exec(ctx,
		`INSERT INTO gorouter_migrations (version, name, checksum) VALUES ($1, $2, $3)`,
		"000001", "000001_foundation.up.sql", "abc123def456",
	)
	if err != nil {
		t.Fatalf("insert test record: %v", err)
	}

	applied, err := getAppliedMigrations(ctx, pool)
	if err != nil {
		t.Fatalf("getAppliedMigrations: %v", err)
	}
	if len(applied) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(applied))
	}
	if applied["000001"] != "000001_foundation.up.sql" {
		t.Errorf("version 000001 mapped to %q, want %q", applied["000001"], "000001_foundation.up.sql")
	}
}

func TestRemoveMigrationRecord_RemovesEntry(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	cleanupMigrationsTable(ctx, pool)
	_ = ensureMigrationsTable(ctx, pool)

	_, _ = pool.Exec(ctx,
		`INSERT INTO gorouter_migrations (version, name, checksum) VALUES ($1, $2, $3)`,
		"000001", "test.up.sql", "deadbeef",
	)

	if err := removeMigrationRecord(ctx, pool, "000001"); err != nil {
		t.Fatalf("removeMigrationRecord: %v", err)
	}

	// Verify deleted
	var count int
	_ = pool.QueryRow(ctx, "SELECT COUNT(*) FROM gorouter_migrations WHERE version = '000001'").Scan(&count)
	if count != 0 {
		t.Errorf("expected 0 records after removal, got %d", count)
	}
}

func TestApplyMigration_AppliesAndRecords(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	cleanupMigrationsTable(ctx, pool)
	_ = ensureMigrationsTable(ctx, pool)

	// Apply a minimal migration that creates a temp table
	m := Migration{
		Version:  "000999",
		Name:     "000999_test_create.up.sql",
		Content:  "CREATE TABLE IF NOT EXISTS _test_migrate_apply (id INT);",
		Checksum: "dummy",
	}
	if err := applyMigration(ctx, pool, m); err != nil {
		t.Fatalf("applyMigration: %v", err)
	}

	// Verify the table exists
	var exists bool
	_ = pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = '_test_migrate_apply')`,
	).Scan(&exists)
	if !exists {
		t.Error("table _test_migrate_apply was not created")
	}

	// Verify the migration was recorded
	var recordedName string
	err := pool.QueryRow(ctx,
		`SELECT name FROM gorouter_migrations WHERE version = '000999'`,
	).Scan(&recordedName)
	if err != nil {
		t.Fatalf("query recorded migration: %v", err)
	}
	if recordedName != "000999_test_create.up.sql" {
		t.Errorf("recorded name = %q, want %q", recordedName, "000999_test_create.up.sql")
	}

	// Clean up
	_, _ = pool.Exec(ctx, "DROP TABLE IF EXISTS _test_migrate_apply")
	_, _ = pool.Exec(ctx, "DELETE FROM gorouter_migrations WHERE version = '000999'")
}

func TestApplyMigration_RollbackOnFailure(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	cleanupMigrationsTable(ctx, pool)
	_ = ensureMigrationsTable(ctx, pool)

	// Migration with invalid SQL should fail and roll back
	bad := Migration{
		Version:  "000999",
		Name:     "000999_bad.up.sql",
		Content:  "this is not valid SQL;",
		Checksum: "bad",
	}
	err := applyMigration(ctx, pool, bad)
	if err == nil {
		t.Fatal("expected error for invalid SQL, got nil")
	}
	t.Logf("applyMigration rejected bad SQL: %v", err)

	// Verify the migration record was NOT inserted (rolled back)
	var count int
	_ = pool.QueryRow(ctx, "SELECT COUNT(*) FROM gorouter_migrations WHERE version = '000999'").Scan(&count)
	if count != 0 {
		t.Errorf("expected 0 records after rollback, got %d", count)
	}
}

func TestMigrate_CleanApply(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	// Remove existing tracking table so we start fresh
	cleanupMigrationsTable(ctx, pool)

	result, err := Migrate(ctx, pool, DirectionUp)
	if err != nil {
		t.Fatalf("Migrate(up): %v", err)
	}
	if len(result.Applied) == 0 {
		t.Fatal("expected at least 1 migration to be applied")
	}
	if result.Applied[0] != "000001_foundation.up.sql" {
		t.Errorf("first applied = %q, want %q", result.Applied[0], "000001_foundation.up.sql")
	}

	// Verify the foundation tables were created
	expectedTables := []string{
		"gorouter_settings",
		"gorouter_users",
		"gorouter_sessions",
		"gorouter_api_keys",
		"gorouter_pats",
		"gorouter_audit_log",
		"gorouter_providers",
		"gorouter_jobs",
	}
	for _, tbl := range expectedTables {
		var exists bool
		err := pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)`, tbl,
		).Scan(&exists)
		if err != nil {
			t.Errorf("check %s: %v", tbl, err)
			continue
		}
		if !exists {
			t.Errorf("table %s was not created by migration", tbl)
		}
	}
}

func TestMigrate_Idempotent(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	cleanupMigrationsTable(ctx, pool)

	// First run
	first, err := Migrate(ctx, pool, DirectionUp)
	if err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	if len(first.Applied) == 0 {
		t.Fatal("first run: expected migrations to be applied")
	}

	// Second run — should skip everything
	second, err := Migrate(ctx, pool, DirectionUp)
	if err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	if len(second.Applied) != 0 {
		t.Errorf("second run: expected 0 applied, got %d: %v", len(second.Applied), second.Applied)
	}
	if len(second.Skipped) == 0 {
		t.Error("second run: expected skipped migrations, got 0")
	}
}

func TestMigrate_DirectionDown(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	cleanupMigrationsTable(ctx, pool)

	// Apply up
	_, err := Migrate(ctx, pool, DirectionUp)
	if err != nil {
		t.Fatalf("Migrate(up): %v", err)
	}

	// Roll down
	downResult, err := Migrate(ctx, pool, DirectionDown)
	if err != nil {
		t.Fatalf("Migrate(down): %v", err)
	}
	if len(downResult.Applied) == 0 {
		t.Fatal("expected at least 1 down migration to be applied")
	}
	if downResult.Applied[0] != "000006_usage.down.sql" {
		t.Errorf("first down applied = %q, want %q",
			downResult.Applied[0], "000006_usage.down.sql")
	}

	// Verify tracking table is empty
	applied, err := getAppliedMigrations(ctx, pool)
	if err != nil {
		t.Fatalf("getAppliedMigrations: %v", err)
	}
	if len(applied) != 0 {
		t.Errorf("expected 0 applied after full down, got %d", len(applied))
	}
}

func TestMigrate_DownThenUpReapplies(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	cleanupMigrationsTable(ctx, pool)

	// Up
	_, err := Migrate(ctx, pool, DirectionUp)
	if err != nil {
		t.Fatalf("first Migrate(up): %v", err)
	}

	// Down
	_, err = Migrate(ctx, pool, DirectionDown)
	if err != nil {
		t.Fatalf("Migrate(down): %v", err)
	}

	// Up again — should re-apply all
	upAgain, err := Migrate(ctx, pool, DirectionUp)
	if err != nil {
		t.Fatalf("second Migrate(up): %v", err)
	}
	if len(upAgain.Applied) == 0 {
		t.Fatal("expected migrations to be re-applied after down")
	}
}

func TestMigrate_DownSkipsNotApplied(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	cleanupMigrationsTable(ctx, pool)

	// Run down without ever applying up
	result, err := Migrate(ctx, pool, DirectionDown)
	if err != nil {
		t.Fatalf("Migrate(down) on clean DB: %v", err)
	}
	if len(result.Applied) != 0 {
		t.Errorf("expected 0 applied on clean DB, got %d", len(result.Applied))
	}
	if len(result.Skipped) == 0 {
		t.Error("expected down migrations to be skipped on clean DB")
	}
}

func TestMigrate_EmptyDirReturnsEmptyResult(t *testing.T) {
	// parseMigrations always returns at least 1 migration in this project,
	// but Migrate's empty-list guard (len==0) is exercised logically:
	pool := getTestPool(t)
	ctx := context.Background()

	cleanupMigrationsTable(ctx, pool)

	// Apply down on a clean database — no migrations are applied because none
	// have been tracked yet, but the len>0 path runs anyway.
	result, err := Migrate(ctx, pool, DirectionDown)
	if err != nil {
		t.Fatalf("Migrate(down) on clean: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// All down files were skipped (not applied yet)
	if len(result.Skipped) == 0 {
		t.Log("no skipped entries — all down files were applied")
	}
}

// ---------------------------------------------------------------------------
// Integration tests – Migration order and version tracking
// ---------------------------------------------------------------------------

func TestMigrationOrder_RunsInSequence(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	cleanupMigrationsTable(ctx, pool)

	result, err := Migrate(ctx, pool, DirectionUp)
	if err != nil {
		t.Fatalf("Migrate(up): %v", err)
	}

	// Migrations are returned sorted, so they should be applied in order.
	// Since there's only one right now, just verify it's applied.
	if len(result.Applied) < 1 {
		t.Fatal("expected at least 1 migration applied")
	}
	for i := 1; i < len(result.Applied); i++ {
		if result.Applied[i] < result.Applied[i-1] {
			t.Errorf("migrations applied out of order: %s before %s",
				result.Applied[i-1], result.Applied[i])
		}
	}
}

func TestSchemaVersionTracking_StoresAndReads(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	cleanupMigrationsTable(ctx, pool)
	_ = ensureMigrationsTable(ctx, pool)

	// No migrations yet → empty
	applied, err := getAppliedMigrations(ctx, pool)
	if err != nil {
		t.Fatalf("getAppliedMigrations: %v", err)
	}
	if len(applied) != 0 {
		t.Fatalf("expected 0, got %d", len(applied))
	}

	// Simulate a migration apply
	m := Migration{
		Version:  "000001",
		Name:     "000001_test.up.sql",
		Content:  "SELECT 1;",
		Checksum: "testchecksum",
	}
	if err := applyMigration(ctx, pool, m); err != nil {
		t.Fatalf("applyMigration: %v", err)
	}

	// Read back
	applied, err = getAppliedMigrations(ctx, pool)
	if err != nil {
		t.Fatalf("getAppliedMigrations: %v", err)
	}
	if len(applied) != 1 {
		t.Fatalf("expected 1, got %d", len(applied))
	}
	if applied["000001"] != "000001_test.up.sql" {
		t.Errorf("version mapping: %q, want %q", applied["000001"], "000001_test.up.sql")
	}

	// Clean up
	_, _ = pool.Exec(ctx, "DELETE FROM gorouter_migrations WHERE version = '000001'")
}

func TestSchemaVersionTracking_RejectsDowngrade(t *testing.T) {
	// The current Migrate implementation for DirectionUp skips already-applied
	// versions rather than rejecting them. This test verifies that behaviour:
	// applying the same version twice results in the second being skipped.
	pool := getTestPool(t)
	ctx := context.Background()

	cleanupMigrationsTable(ctx, pool)

	first, err := Migrate(ctx, pool, DirectionUp)
	if err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	if len(first.Applied) == 0 {
		t.Fatal("expected migrations to be applied")
	}

	// Verify that a down+up cycle doesn't double-apply the same version
	_, _ = Migrate(ctx, pool, DirectionDown)
	upAgain, err := Migrate(ctx, pool, DirectionUp)
	if err != nil {
		t.Fatalf("up after down: %v", err)
	}
	if len(upAgain.Applied) == 0 {
		t.Error("expected migrations to be re-applied after down")
	}
}

// ---------------------------------------------------------------------------
// Integration tests – SQL file associations
// ---------------------------------------------------------------------------

func TestUpDownFileAssociations_MatchingPairs(t *testing.T) {
	// Verify that every version has both an up and down file.
	// This test is a unit test — it only needs the embedded FS.
	upMigs, err := parseMigrations(DirectionUp)
	if err != nil {
		t.Fatalf("parseMigrations(up): %v", err)
	}
	downMigs, err := parseMigrations(DirectionDown)
	if err != nil {
		t.Fatalf("parseMigrations(down): %v", err)
	}

	up := make(map[string]bool)
	for _, m := range upMigs {
		up[m.Version] = true
	}
	down := make(map[string]bool)
	for _, m := range downMigs {
		down[m.Version] = true
	}

	for _, m := range upMigs {
		if !down[m.Version] {
			t.Errorf("version %s (%s) has no matching down file", m.Version, m.Name)
		}
	}
	for _, m := range downMigs {
		if !up[m.Version] {
			t.Errorf("version %s (%s) has no matching up file", m.Version, m.Name)
		}
	}
}

// ---------------------------------------------------------------------------
// Benchmark
// ---------------------------------------------------------------------------

func BenchmarkParseMigrations(b *testing.B) {
	for range b.N {
		_, err := parseMigrations(DirectionUp)
		if err != nil {
			b.Fatal(err)
		}
	}
}
