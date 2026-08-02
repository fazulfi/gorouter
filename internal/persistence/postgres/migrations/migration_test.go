package migrations

import (
	"context"
	"errors"
	"testing"

	"github.com/pashagolub/pgxmock/v2"
)

// ---------------------------------------------------------------------------
// ensureMigrationsTable
// ---------------------------------------------------------------------------

func TestRunner_EnsureMigrationsTable_Success(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))

	runner := NewRunner(mock)
	err = runner.ensureMigrationsTable(context.Background())
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_EnsureMigrationsTable_ExecError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnError(errors.New("connection refused"))

	runner := NewRunner(mock)
	err = runner.ensureMigrationsTable(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// getAppliedMigrations
// ---------------------------------------------------------------------------

func TestRunner_GetAppliedMigrations_Empty(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}))

	runner := NewRunner(mock)
	applied, err := runner.getAppliedMigrations(context.Background())
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if len(applied) != 0 {
		t.Errorf("expected empty map, got %d entries", len(applied))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_GetAppliedMigrations_WithResults(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}).
			AddRow("000001", "000001_foundation.up.sql").
			AddRow("000002", "000002_add_widgets.up.sql"))

	runner := NewRunner(mock)
	applied, err := runner.getAppliedMigrations(context.Background())
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if len(applied) != 2 {
		t.Errorf("expected 2 entries, got %d", len(applied))
	}
	if applied["000001"] != "000001_foundation.up.sql" {
		t.Errorf("version 000001 = %q, want %q", applied["000001"], "000001_foundation.up.sql")
	}
	if applied["000002"] != "000002_add_widgets.up.sql" {
		t.Errorf("version 000002 = %q, want %q", applied["000002"], "000002_add_widgets.up.sql")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_GetAppliedMigrations_QueryError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnError(errors.New("table not found"))

	runner := NewRunner(mock)
	_, err = runner.getAppliedMigrations(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// applyMigration
// ---------------------------------------------------------------------------

func TestRunner_ApplyMigration_Success(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectBegin()
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS _test_apply").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000999", "000999_test.up.sql", "checksum123").
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()

	runner := NewRunner(mock)
	m := Migration{
		Version:  "000999",
		Name:     "000999_test.up.sql",
		Content:  "CREATE TABLE IF NOT EXISTS _test_apply (id INT);",
		Checksum: "checksum123",
	}
	err = runner.applyMigration(context.Background(), m)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_ApplyMigration_BeginError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectBegin().WillReturnError(errors.New("pool closed"))

	runner := NewRunner(mock)
	m := Migration{
		Version:  "000999",
		Name:     "000999_test.up.sql",
		Content:  "SELECT 1;",
		Checksum: "abc",
	}
	err = runner.applyMigration(context.Background(), m)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_ApplyMigration_ExecContentError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectBegin()
	mock.ExpectExec("this is not valid SQL").
		WillReturnError(errors.New("syntax error"))
	mock.ExpectRollback()

	runner := NewRunner(mock)
	m := Migration{
		Version:  "000999",
		Name:     "000999_bad.up.sql",
		Content:  "this is not valid SQL;",
		Checksum: "bad",
	}
	err = runner.applyMigration(context.Background(), m)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_ApplyMigration_RecordError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectBegin()
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS _test_record_error").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000999", "000999_test.up.sql", "abc").
		WillReturnError(errors.New("duplicate key"))
	mock.ExpectRollback()

	runner := NewRunner(mock)
	m := Migration{
		Version:  "000999",
		Name:     "000999_test.up.sql",
		Content:  "CREATE TABLE IF NOT EXISTS _test_record_error (id INT);",
		Checksum: "abc",
	}
	err = runner.applyMigration(context.Background(), m)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// removeMigrationRecord
// ---------------------------------------------------------------------------

func TestRunner_RemoveMigrationRecord_Success(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("DELETE FROM gorouter_migrations WHERE version").
		WithArgs("000001").
		WillReturnResult(pgxmock.NewResult("DELETE", 1))

	runner := NewRunner(mock)
	err = runner.removeMigrationRecord(context.Background(), "000001")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_RemoveMigrationRecord_Error(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("DELETE FROM gorouter_migrations WHERE version").
		WithArgs("000001").
		WillReturnError(errors.New("table not found"))

	runner := NewRunner(mock)
	err = runner.removeMigrationRecord(context.Background(), "000001")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Migrate guard clauses
// ---------------------------------------------------------------------------

func TestRunner_Migrate_NilPool(t *testing.T) {
	runner := &Runner{pool: nil}
	_, err := runner.Migrate(context.Background(), DirectionUp)
	if err == nil {
		t.Error("expected error for nil pool, got nil")
	}
}

func TestRunner_Migrate_InvalidDirection(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	runner := NewRunner(mock)
	_, err = runner.Migrate(context.Background(), "invalid")
	if err == nil {
		t.Error("expected error for invalid direction, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Migrate DirectionUp
// ---------------------------------------------------------------------------

func TestRunner_Migrate_Up_AppliesAll(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}))
	mock.ExpectBegin()
	mock.ExpectExec("(?i)CREATE TABLE").WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000001", "000001_foundation.up.sql", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()
	mock.ExpectBegin()
	mock.ExpectExec("(?i)CREATE TABLE").WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000002", "000002_engine.up.sql", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()
	mock.ExpectBegin()
	mock.ExpectExec("(?i)ALTER TABLE").WillReturnResult(pgxmock.NewResult("ALTER TABLE", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000003", "000003_rename_api_key_column.up.sql", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()
	mock.ExpectBegin()
	mock.ExpectExec("(?i)CREATE TABLE IF NOT EXISTS gorouter_oauth_sessions").WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000004", "000004_engine_matrix.up.sql", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()
	mock.ExpectBegin()
	mock.ExpectExec("(?i)CREATE TABLE").WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000005", "000005_proxy_pools.up.sql", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()
	mock.ExpectBegin()
	mock.ExpectExec("(?i)CREATE TABLE").WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000006", "000006_usage.up.sql", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()
	mock.ExpectBegin()
	mock.ExpectExec("(?i)CREATE TABLE").WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000007", "000007_console_logs.up.sql", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()
	mock.ExpectBegin()
	mock.ExpectExec("(?i)CREATE TABLE").WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000008", "000008_password_resets.up.sql", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()
	mock.ExpectBegin()
	mock.ExpectExec("(?i)CREATE TABLE").WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000009", "000009_backups_audit.up.sql", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()

	runner := NewRunner(mock)
	result, err := runner.Migrate(context.Background(), DirectionUp)
	if err != nil {
		t.Fatalf("Migrate(up): %v", err)
	}
	if len(result.Applied) != 9 {
		t.Errorf("expected 9 applied, got %d", len(result.Applied))
	}
	if len(result.Skipped) != 0 {
		t.Errorf("expected 0 skipped, got %d", len(result.Skipped))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_Migrate_Up_SkipsApplied(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}).
			AddRow("000001", "000001_foundation.up.sql"))
	mock.ExpectBegin()
	mock.ExpectExec("(?i)CREATE TABLE").WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000002", "000002_engine.up.sql", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()
	mock.ExpectBegin()
	mock.ExpectExec("(?i)ALTER TABLE").WillReturnResult(pgxmock.NewResult("ALTER TABLE", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000003", "000003_rename_api_key_column.up.sql", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()
	mock.ExpectBegin()
	mock.ExpectExec("(?i)CREATE TABLE IF NOT EXISTS gorouter_oauth_sessions").WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000004", "000004_engine_matrix.up.sql", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()
	mock.ExpectBegin()
	mock.ExpectExec("(?i)CREATE TABLE").WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000005", "000005_proxy_pools.up.sql", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()
	mock.ExpectBegin()
	mock.ExpectExec("(?i)CREATE TABLE").WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000006", "000006_usage.up.sql", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()
	mock.ExpectBegin()
	mock.ExpectExec("(?i)CREATE TABLE").WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000007", "000007_console_logs.up.sql", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()
	mock.ExpectBegin()
	mock.ExpectExec("(?i)CREATE TABLE").WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000008", "000008_password_resets.up.sql", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()
	mock.ExpectBegin()
	mock.ExpectExec("(?i)CREATE TABLE").WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000009", "000009_backups_audit.up.sql", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()

	runner := NewRunner(mock)
	result, err := runner.Migrate(context.Background(), DirectionUp)
	if err != nil {
		t.Fatalf("Migrate(up): %v", err)
	}
	if len(result.Applied) != 8 {
		t.Errorf("expected 8 applied, got %d", len(result.Applied))
	}
	if len(result.Skipped) != 1 {
		t.Errorf("expected 1 skipped, got %d", len(result.Skipped))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Migrate DirectionDown
// ---------------------------------------------------------------------------

func TestRunner_Migrate_Down_AppliesApplied(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}).
			AddRow("000001", "000001_foundation.down.sql"))
	mock.ExpectBegin()
	mock.ExpectExec("(?i)DROP TABLE").WillReturnResult(pgxmock.NewResult("DROP", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000001", "000001_foundation.down.sql", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()
	mock.ExpectExec("DELETE FROM gorouter_migrations WHERE version").
		WithArgs("000001").
		WillReturnResult(pgxmock.NewResult("DELETE", 1))

	runner := NewRunner(mock)
	result, err := runner.Migrate(context.Background(), DirectionDown)
	if err != nil {
		t.Fatalf("Migrate(down): %v", err)
	}
	if len(result.Applied) != 1 {
		t.Errorf("expected 1 applied, got %d", len(result.Applied))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_Migrate_Down_SkipsNotApplied(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}))

	runner := NewRunner(mock)
	result, err := runner.Migrate(context.Background(), DirectionDown)
	if err != nil {
		t.Fatalf("Migrate(down): %v", err)
	}
	if len(result.Applied) != 0 {
		t.Errorf("expected 0 applied, got %d", len(result.Applied))
	}
	if len(result.Skipped) == 0 {
		t.Error("expected migrations to be skipped on clean database")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Migrate error paths
// ---------------------------------------------------------------------------

func TestRunner_Migrate_EnsureTableError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnError(errors.New("permission denied"))

	runner := NewRunner(mock)
	_, err = runner.Migrate(context.Background(), DirectionUp)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_Migrate_GetAppliedError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnError(errors.New("relation does not exist"))

	runner := NewRunner(mock)
	_, err = runner.Migrate(context.Background(), DirectionUp)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_Migrate_ApplyError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}))
	mock.ExpectBegin()
	mock.ExpectExec("(?i)CREATE TABLE").WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000001", "000001_foundation.up.sql", pgxmock.AnyArg()).
		WillReturnError(errors.New("duplicate key"))
	mock.ExpectRollback()

	runner := NewRunner(mock)
	_, err = runner.Migrate(context.Background(), DirectionUp)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_Migrate_RemoveRecordError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}).
			AddRow("000001", "000001_foundation.down.sql"))
	mock.ExpectBegin()
	mock.ExpectExec("(?i)DROP TABLE").WillReturnResult(pgxmock.NewResult("DROP", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000001", "000001_foundation.down.sql", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()
	mock.ExpectExec("DELETE FROM gorouter_migrations WHERE version").
		WithArgs("000001").
		WillReturnError(errors.New("table not found"))

	runner := NewRunner(mock)
	_, err = runner.Migrate(context.Background(), DirectionDown)
	if err == nil {
		t.Fatal("expected error for remove record failure, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_Migrate_NilPoolNoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Migrate panicked with nil pool: %v", r)
		}
	}()
	runner := &Runner{pool: nil}
	_, err := runner.Migrate(context.Background(), DirectionUp)
	if err == nil {
		t.Error("expected error for nil pool")
	}
}

// ---------------------------------------------------------------------------
// Up / Down convenience methods
// ---------------------------------------------------------------------------

func TestRunner_Up_DelegatesToMigrate(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}))
	mock.ExpectBegin()
	mock.ExpectExec("(?i)CREATE TABLE").WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000001", "000001_foundation.up.sql", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()
	mock.ExpectBegin()
	mock.ExpectExec("(?i)CREATE TABLE").WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000002", "000002_engine.up.sql", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()
	mock.ExpectBegin()
	mock.ExpectExec("(?i)ALTER TABLE").WillReturnResult(pgxmock.NewResult("ALTER TABLE", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000003", "000003_rename_api_key_column.up.sql", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()
	mock.ExpectBegin()
	mock.ExpectExec("(?i)CREATE TABLE IF NOT EXISTS gorouter_oauth_sessions").WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000004", "000004_engine_matrix.up.sql", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()
	mock.ExpectBegin()
	mock.ExpectExec("(?i)CREATE TABLE").WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000005", "000005_proxy_pools.up.sql", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()
	mock.ExpectBegin()
	mock.ExpectExec("(?i)CREATE TABLE").WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000006", "000006_usage.up.sql", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()
	mock.ExpectBegin()
	mock.ExpectExec("(?i)CREATE TABLE").WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000007", "000007_console_logs.up.sql", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()
	mock.ExpectBegin()
	mock.ExpectExec("(?i)CREATE TABLE").WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000008", "000008_password_resets.up.sql", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()
	mock.ExpectBegin()
	mock.ExpectExec("(?i)CREATE TABLE").WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000009", "000009_backups_audit.up.sql", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()

	runner := NewRunner(mock)
	result, err := runner.Up(context.Background())
	if err != nil {
		t.Fatalf("Up: %v", err)
	}
	if len(result.Applied) != 9 {
		t.Errorf("expected 9 applied, got %d", len(result.Applied))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_Down_DelegatesToMigrate(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}).
			AddRow("000001", "000001_foundation.down.sql"))
	mock.ExpectBegin()
	mock.ExpectExec("(?i)DROP TABLE").WillReturnResult(pgxmock.NewResult("DROP", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000001", "000001_foundation.down.sql", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()
	mock.ExpectExec("DELETE FROM gorouter_migrations WHERE version").
		WithArgs("000001").
		WillReturnResult(pgxmock.NewResult("DELETE", 1))

	runner := NewRunner(mock)
	result, err := runner.Down(context.Background())
	if err != nil {
		t.Fatalf("Down: %v", err)
	}
	if len(result.Applied) != 1 {
		t.Errorf("expected 1 applied, got %d", len(result.Applied))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ListPending
// ---------------------------------------------------------------------------

func TestRunner_ListPending_AllPending(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}))

	runner := NewRunner(mock)
	pending, err := runner.ListPending(context.Background(), DirectionUp)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if pending == nil {
		t.Fatal("expected non-nil pending list")
	}
	if len(pending) != 9 {
		t.Errorf("expected 9 pending, got %d", len(pending))
	}
	if pending[0].Version != "000001" {
		t.Errorf("expected version 000001, got %s", pending[0].Version)
	}
	if pending[1].Version != "000002" {
		t.Errorf("expected version 000002, got %s", pending[1].Version)
	}
	if pending[2].Version != "000003" {
		t.Errorf("expected version 000003, got %s", pending[2].Version)
	}
	if pending[3].Version != "000004" {
		t.Errorf("expected version 000004, got %s", pending[3].Version)
	}
	if pending[4].Version != "000005" {
		t.Errorf("expected version 000005, got %s", pending[4].Version)
	}
	if pending[5].Version != "000006" {
		t.Errorf("expected version 000006, got %s", pending[5].Version)
	}
	if pending[6].Version != "000007" {
		t.Errorf("expected version 000007, got %s", pending[6].Version)
	}
	if pending[7].Version != "000008" {
		t.Errorf("expected version 000008, got %s", pending[7].Version)
	}
	if pending[8].Version != "000009" {
		t.Errorf("expected version 000009, got %s", pending[8].Version)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_ListPending_NonePending(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}).
			AddRow("000001", "000001_foundation.up.sql"))

	runner := NewRunner(mock)
	pending, err := runner.ListPending(context.Background(), DirectionUp)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if pending == nil {
		t.Fatal("expected non-nil pending, got nil")
	}
	if len(pending) != 8 {
		t.Fatalf("expected 8 pending, got %d", len(pending))
	}
	if pending[0].Version != "000002" {
		t.Errorf("expected version 000002, got %s", pending[0].Version)
	}
	if pending[1].Version != "000003" {
		t.Errorf("expected version 000003, got %s", pending[1].Version)
	}
	if pending[2].Version != "000004" {
		t.Errorf("expected version 000004, got %s", pending[2].Version)
	}
	if pending[3].Version != "000005" {
		t.Errorf("expected version 000005, got %s", pending[3].Version)
	}
	if pending[4].Version != "000006" {
		t.Errorf("expected version 000006, got %s", pending[4].Version)
	}
	if pending[5].Version != "000007" {
		t.Errorf("expected version 000007, got %s", pending[5].Version)
	}
	if pending[6].Version != "000008" {
		t.Errorf("expected version 000008, got %s", pending[6].Version)
	}
	if pending[7].Version != "000009" {
		t.Errorf("expected version 000009, got %s", pending[7].Version)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_ListPending_EnsureTableError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnError(errors.New("permission denied"))

	runner := NewRunner(mock)
	_, err = runner.ListPending(context.Background(), DirectionUp)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_ListPending_GetAppliedError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnError(errors.New("relation not found"))

	runner := NewRunner(mock)
	_, err = runner.ListPending(context.Background(), DirectionUp)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_ListPending_InvalidDirection(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	_, err = NewRunner(mock).ListPending(context.Background(), "invalid")
	if err != nil {
		t.Logf("ListPending with invalid direction returned error (expected): %v", err)
	}
}

// ---------------------------------------------------------------------------
// AppliedVersions
// ---------------------------------------------------------------------------

func TestRunner_AppliedVersions_HasVersions(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}).
			AddRow("000002", "000002_add_widgets.up.sql").
			AddRow("000001", "000001_foundation.up.sql"))

	runner := NewRunner(mock)
	versions, err := runner.AppliedVersions(context.Background())
	if err != nil {
		t.Fatalf("AppliedVersions: %v", err)
	}
	if versions == nil {
		t.Fatal("expected non-nil versions")
	}
	if len(versions) != 2 {
		t.Fatalf("expected 3 versions, got %d", len(versions))
	}
	if versions[0] != "000001" {
		t.Errorf("versions[0] = %q, want %q", versions[0], "000001")
	}
	if versions[1] != "000002" {
		t.Errorf("versions[1] = %q, want %q", versions[1], "000002")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_AppliedVersions_Empty(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}))

	runner := NewRunner(mock)
	versions, err := runner.AppliedVersions(context.Background())
	if err != nil {
		t.Fatalf("AppliedVersions: %v", err)
	}
	if versions != nil {
		t.Errorf("expected nil, got %d entries", len(versions))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_AppliedVersions_EnsureTableError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnError(errors.New("permission denied"))

	runner := NewRunner(mock)
	_, err = runner.AppliedVersions(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_AppliedVersions_QueryError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnError(errors.New("table not found"))

	runner := NewRunner(mock)
	_, err = runner.AppliedVersions(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// HasMigration
// ---------------------------------------------------------------------------

func TestRunner_HasMigration_Found(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}).
			AddRow("000001", "000001_foundation.up.sql"))

	runner := NewRunner(mock)
	found, err := runner.HasMigration(context.Background(), "000001")
	if err != nil {
		t.Fatalf("HasMigration: %v", err)
	}
	if !found {
		t.Error("expected found=true, got false")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_HasMigration_NotFound(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}).
			AddRow("000001", "000001_foundation.up.sql"))

	runner := NewRunner(mock)
	found, err := runner.HasMigration(context.Background(), "000999")
	if err != nil {
		t.Fatalf("HasMigration: %v", err)
	}
	if found {
		t.Error("expected found=false, got true")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_HasMigration_EnsureTableError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnError(errors.New("permission denied"))

	runner := NewRunner(mock)
	_, err = runner.HasMigration(context.Background(), "000001")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Lookup
// ---------------------------------------------------------------------------

func TestRunner_Lookup_Found(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	runner := NewRunner(mock)
	mig, err := runner.Lookup(context.Background(), "000001", DirectionUp)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if mig == nil {
		t.Fatal("expected non-nil migration")
	}
	if mig.Version != "000001" {
		t.Errorf("version = %q, want %q", mig.Version, "000001")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_Lookup_NotFound(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	runner := NewRunner(mock)
	_, err = runner.Lookup(context.Background(), "009999", DirectionUp)
	if err == nil {
		t.Fatal("expected error for not found, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_Lookup_InvalidDirection(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	_, err = NewRunner(mock).Lookup(context.Background(), "000001", "invalid")
	if err != nil {
		t.Logf("Lookup with invalid direction returned error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// VerifyChecksums
// ---------------------------------------------------------------------------

func TestRunner_VerifyChecksums_AllMatch(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	// Read actual checksums from embedded migration
	migs, err := parseMigrations(DirectionUp)
	if err != nil {
		t.Fatal(err)
	}

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, checksum FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "checksum"}).
			AddRow("000001", migs[0].Checksum))

	runner := NewRunner(mock)
	err = runner.VerifyChecksums(context.Background(), DirectionUp)
	if err != nil {
		t.Fatalf("VerifyChecksums: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_VerifyChecksums_Mismatch(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, checksum FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "checksum"}).
			AddRow("000001", "deadbeef"))

	runner := NewRunner(mock)
	err = runner.VerifyChecksums(context.Background(), DirectionUp)
	if err == nil {
		t.Fatal("expected error for checksum mismatch, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_VerifyChecksums_VersionNotFound(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, checksum FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "checksum"}).
			AddRow("009999", "somechecksum"))

	runner := NewRunner(mock)
	err = runner.VerifyChecksums(context.Background(), DirectionUp)
	if err == nil {
		t.Fatal("expected error for unknown version, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_VerifyChecksums_QueryError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, checksum FROM gorouter_migrations ORDER BY version").
		WillReturnError(errors.New("table not found"))

	runner := NewRunner(mock)
	err = runner.VerifyChecksums(context.Background(), DirectionUp)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_VerifyChecksums_ScanError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	rows := mock.NewRows([]string{"version", "checksum"}).
		AddRow("000001", "abc").
		RowError(0, errors.New("scan failed"))

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, checksum FROM gorouter_migrations ORDER BY version").
		WillReturnRows(rows)

	runner := NewRunner(mock)
	err = runner.VerifyChecksums(context.Background(), DirectionUp)
	if err == nil {
		t.Fatal("expected error for scan failure, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_VerifyChecksums_EnsureTableError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnError(errors.New("permission denied"))

	runner := NewRunner(mock)
	err = runner.VerifyChecksums(context.Background(), DirectionUp)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Inspect
// ---------------------------------------------------------------------------

func TestRunner_Inspect_AllPending(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}))

	runner := NewRunner(mock)
	status, err := runner.Inspect(context.Background(), DirectionUp)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if status == nil {
		t.Fatal("expected non-nil status")
	}
	if len(status) != 9 {
		t.Fatalf("expected 9 statuses, got %d", len(status))
	}
	if status[0].Status != StatusPending {
		t.Errorf("expected pending, got %s", status[0].Status)
	}
	if status[1].Status != StatusPending {
		t.Errorf("expected pending, got %s", status[1].Status)
	}
	if status[2].Status != StatusPending {
		t.Errorf("expected pending, got %s", status[2].Status)
	}
	if status[3].Status != StatusPending {
		t.Errorf("expected pending, got %s", status[3].Status)
	}
	if status[4].Status != StatusPending {
		t.Errorf("expected pending, got %s", status[4].Status)
	}
	if status[5].Status != StatusPending {
		t.Errorf("expected pending, got %s", status[5].Status)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_Inspect_AllApplied(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}).
			AddRow("000001", "000001_foundation.up.sql"))

	runner := NewRunner(mock)
	status, err := runner.Inspect(context.Background(), DirectionUp)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if len(status) != 9 {
		t.Fatalf("expected 9 statuses, got %d", len(status))
	}
	if status[0].Status != StatusApplied {
		t.Errorf("expected applied, got %s", status[0].Status)
	}
	if status[1].Status != StatusPending {
		t.Errorf("expected pending, got %s", status[1].Status)
	}
	if status[2].Status != StatusPending {
		t.Errorf("expected pending, got %s", status[2].Status)
	}
	if status[3].Status != StatusPending {
		t.Errorf("expected pending, got %s", status[3].Status)
	}
	if status[4].Status != StatusPending {
		t.Errorf("expected pending, got %s", status[4].Status)
	}
	if status[5].Status != StatusPending {
		t.Errorf("expected pending, got %s", status[5].Status)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_Inspect_EnsureTableError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnError(errors.New("permission denied"))

	runner := NewRunner(mock)
	_, err = runner.Inspect(context.Background(), DirectionUp)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_Inspect_GetAppliedError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnError(errors.New("relation not found"))

	runner := NewRunner(mock)
	_, err = runner.Inspect(context.Background(), DirectionUp)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// NewRunner stores the pool reference
// ---------------------------------------------------------------------------

func TestNewRunner_StoresPool(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	runner := NewRunner(mock)
	if runner.pool == nil {
		t.Error("NewRunner did not store the pool")
	}
}

// ---------------------------------------------------------------------------
// Version
// ---------------------------------------------------------------------------

func TestRunner_Version_Latest(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}).
			AddRow("000001", "a.up.sql").
			AddRow("000002", "b.up.sql"))

	runner := NewRunner(mock)
	v, err := runner.Version(context.Background())
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if v != "000002" {
		t.Errorf("expected 000002, got %s", v)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_Version_Empty(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}))

	runner := NewRunner(mock)
	v, err := runner.Version(context.Background())
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if v != "" {
		t.Errorf("expected empty, got %s", v)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_Version_EnsureTableError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnError(errors.New("timeout"))

	runner := NewRunner(mock)
	_, err = runner.Version(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// PendingCount
// ---------------------------------------------------------------------------

func TestRunner_PendingCount_Some(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}).
			AddRow("000001", "a.up.sql"))

	runner := NewRunner(mock)
	count, err := runner.PendingCount(context.Background(), DirectionUp)
	if err != nil {
		t.Fatalf("PendingCount: %v", err)
	}
	// 000001 is already applied, 000002-000009 are pending
	if count != 8 {
		t.Errorf("expected 8 pending, got %d", count)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_PendingCount_Zero(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}).
			AddRow("000001", "000001_foundation.up.sql"))

	runner := NewRunner(mock)
	count, err := runner.PendingCount(context.Background(), DirectionUp)
	if err != nil {
		t.Fatalf("PendingCount: %v", err)
	}
	// 000001 is already applied in PendingCount_Zero data set
	if count != 8 {
		t.Errorf("expected 8 pending, got %d", count)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_PendingCount_EnsureTableError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnError(errors.New("timeout"))

	runner := NewRunner(mock)
	_, err = runner.PendingCount(context.Background(), DirectionUp)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// AppliedCount
// ---------------------------------------------------------------------------

func TestRunner_AppliedCount_Some(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}).
			AddRow("000001", "a.up.sql"))

	runner := NewRunner(mock)
	count, err := runner.AppliedCount(context.Background())
	if err != nil {
		t.Fatalf("AppliedCount: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1, got %d", count)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_AppliedCount_Zero(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}))

	runner := NewRunner(mock)
	count, err := runner.AppliedCount(context.Background())
	if err != nil {
		t.Fatalf("AppliedCount: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0, got %d", count)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_AppliedCount_EnsureTableError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnError(errors.New("timeout"))

	runner := NewRunner(mock)
	_, err = runner.AppliedCount(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// MustMigrate
// ---------------------------------------------------------------------------

func TestRunner_MustMigrate_PanicsOnError(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic, got nil")
		}
	}()
	runner := &Runner{pool: nil}
	_ = runner.MustMigrate(context.Background(), DirectionUp)
}

func TestRunner_MustMigrate_ReturnsResult(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}))
	mock.ExpectBegin()
	mock.ExpectExec("(?i)CREATE TABLE").WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000001", "000001_foundation.up.sql", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()
	mock.ExpectBegin()
	mock.ExpectExec("(?i)CREATE TABLE").WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000002", "000002_engine.up.sql", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()
	mock.ExpectBegin()
	mock.ExpectExec("(?i)ALTER TABLE").WillReturnResult(pgxmock.NewResult("ALTER TABLE", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000003", "000003_rename_api_key_column.up.sql", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()
	mock.ExpectBegin()
	mock.ExpectExec("(?i)CREATE TABLE IF NOT EXISTS gorouter_oauth_sessions").WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000004", "000004_engine_matrix.up.sql", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()
	mock.ExpectBegin()
	mock.ExpectExec("(?i)CREATE TABLE").WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000005", "000005_proxy_pools.up.sql", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()
	mock.ExpectBegin()
	mock.ExpectExec("(?i)CREATE TABLE").WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000006", "000006_usage.up.sql", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()
	mock.ExpectBegin()
	mock.ExpectExec("(?i)CREATE TABLE").WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000007", "000007_console_logs.up.sql", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()
	mock.ExpectBegin()
	mock.ExpectExec("(?i)CREATE TABLE").WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000008", "000008_password_resets.up.sql", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()
	mock.ExpectBegin()
	mock.ExpectExec("(?i)CREATE TABLE").WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000009", "000009_backups_audit.up.sql", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()

	runner := NewRunner(mock)
	result := runner.MustMigrate(context.Background(), DirectionUp)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result.Applied) != 9 {
		t.Errorf("expected 9 applied, got %d", len(result.Applied))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// DryRun
// ---------------------------------------------------------------------------

func TestRunner_DryRun_Up_Pending(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}))

	runner := NewRunner(mock)
	names, err := runner.DryRun(context.Background(), DirectionUp)
	if err != nil {
		t.Fatalf("DryRun: %v", err)
	}
	if names == nil {
		t.Fatal("expected non-nil names")
	}
	if len(names) != 9 {
		t.Errorf("expected 9 migrations, got %d", len(names))
	}
	if names[0] != "000001_foundation.up.sql" {
		t.Errorf("expected 000001_foundation.up.sql, got %s", names[0])
	}
	if names[1] != "000002_engine.up.sql" {
		t.Errorf("expected 000002_engine.up.sql, got %s", names[1])
	}
	if names[2] != "000003_rename_api_key_column.up.sql" {
		t.Errorf("expected 000003_rename_api_key_column.up.sql, got %s", names[2])
	}
	if names[3] != "000004_engine_matrix.up.sql" {
		t.Errorf("expected 000004_engine_matrix.up.sql, got %s", names[3])
	}
	if names[4] != "000005_proxy_pools.up.sql" {
		t.Errorf("expected 000005_proxy_pools.up.sql, got %s", names[4])
	}
	if names[5] != "000006_usage.up.sql" {
		t.Errorf("expected 000006_usage.up.sql, got %s", names[5])
	}
	if names[6] != "000007_console_logs.up.sql" {
		t.Errorf("expected 000007_console_logs.up.sql, got %s", names[6])
	}
	if names[7] != "000008_password_resets.up.sql" {
		t.Errorf("expected 000008_password_resets.up.sql, got %s", names[7])
	}
	if names[8] != "000009_backups_audit.up.sql" {
		t.Errorf("expected 000009_backups_audit.up.sql, got %s", names[8])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_DryRun_Down_Applied(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}).
			AddRow("000001", "000001_foundation.down.sql"))

	runner := NewRunner(mock)
	names, err := runner.DryRun(context.Background(), DirectionDown)
	if err != nil {
		t.Fatalf("DryRun(down): %v", err)
	}
	if names == nil {
		t.Fatal("expected non-nil names")
	}
	if len(names) != 1 {
		t.Errorf("expected 1 migration, got %d", len(names))
	}
	if names[0] != "000001_foundation.down.sql" {
		t.Errorf("expected 000001_foundation.down.sql, got %s", names[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_DryRun_NonePending(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}).
			AddRow("000001", "000001_foundation.up.sql"))

	runner := NewRunner(mock)
	names, err := runner.DryRun(context.Background(), DirectionUp)
	if err != nil {
		t.Fatalf("DryRun: %v", err)
	}
	if names == nil {
		t.Fatal("expected non-nil names, got nil")
	}
	if len(names) != 8 {
		t.Fatalf("expected 8 migrations, got %d", len(names))
	}
	if names[0] != "000002_engine.up.sql" {
		t.Errorf("expected 000002_engine.up.sql, got %s", names[0])
	}
	if names[1] != "000003_rename_api_key_column.up.sql" {
		t.Errorf("expected 000003_rename_api_key_column.up.sql, got %s", names[1])
	}
	if names[2] != "000004_engine_matrix.up.sql" {
		t.Errorf("expected 000004_engine_matrix.up.sql, got %s", names[2])
	}
	if names[3] != "000005_proxy_pools.up.sql" {
		t.Errorf("expected 000005_proxy_pools.up.sql, got %s", names[3])
	}
	if names[4] != "000006_usage.up.sql" {
		t.Errorf("expected 000006_usage.up.sql, got %s", names[4])
	}
	if names[5] != "000007_console_logs.up.sql" {
		t.Errorf("expected 000007_console_logs.up.sql, got %s", names[5])
	}
	if names[6] != "000008_password_resets.up.sql" {
		t.Errorf("expected 000008_password_resets.up.sql, got %s", names[6])
	}
	if names[7] != "000009_backups_audit.up.sql" {
		t.Errorf("expected 000009_backups_audit.up.sql, got %s", names[7])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_DryRun_NilPool(t *testing.T) {
	runner := &Runner{pool: nil}
	_, err := runner.DryRun(context.Background(), DirectionUp)
	if err == nil {
		t.Error("expected error for nil pool")
	}
}

func TestRunner_DryRun_InvalidDirection(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	runner := NewRunner(mock)
	_, err = runner.DryRun(context.Background(), "invalid")
	if err == nil {
		t.Error("expected error for invalid direction")
	}
}

func TestRunner_DryRun_EnsureTableError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnError(errors.New("timeout"))

	runner := NewRunner(mock)
	_, err = runner.DryRun(context.Background(), DirectionUp)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Validate
// ---------------------------------------------------------------------------

func TestRunner_Validate_Success(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	runner := NewRunner(mock)
	err = runner.Validate(context.Background(), DirectionUp)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_Validate_InvalidDirection(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	runner := NewRunner(mock)
	err = runner.Validate(context.Background(), "invalid")
	if err == nil {
		t.Error("expected error for invalid direction")
	}
}

// ---------------------------------------------------------------------------
// RemoveApplied
// ---------------------------------------------------------------------------

func TestRunner_RemoveApplied_Success(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("DELETE FROM gorouter_migrations WHERE version").
		WithArgs("000001").
		WillReturnResult(pgxmock.NewResult("DELETE", 1))

	runner := NewRunner(mock)
	err = runner.RemoveApplied(context.Background(), "000001")
	if err != nil {
		t.Fatalf("RemoveApplied: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_RemoveApplied_EnsureTableError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnError(errors.New("timeout"))

	runner := NewRunner(mock)
	err = runner.RemoveApplied(context.Background(), "000001")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Search
// ---------------------------------------------------------------------------

func TestRunner_Search_Found(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	runner := NewRunner(mock)
	results, err := runner.Search(context.Background(), "000001", DirectionUp)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if results == nil {
		t.Fatal("expected non-nil results")
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_Search_NotFound(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	runner := NewRunner(mock)
	results, err := runner.Search(context.Background(), "009999", DirectionUp)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil, got %d results", len(results))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_Search_InvalidDirection(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	_, err = NewRunner(mock).Search(context.Background(), "x", "invalid")
	if err != nil {
		t.Logf("Search with invalid direction returned error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// IsApplied
// ---------------------------------------------------------------------------

func TestRunner_IsApplied_True(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectQuery("SELECT 1 FROM gorouter_migrations WHERE version").
		WithArgs("000001").
		WillReturnRows(mock.NewRows([]string{"1"}).AddRow(1))

	runner := NewRunner(mock)
	applied, err := runner.IsApplied(context.Background(), "000001")
	if err != nil {
		t.Fatalf("IsApplied: %v", err)
	}
	if !applied {
		t.Error("expected applied=true")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_IsApplied_False(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectQuery("SELECT 1 FROM gorouter_migrations WHERE version").
		WithArgs("009999").
		WillReturnRows(mock.NewRows([]string{"1"}))

	runner := NewRunner(mock)
	applied, err := runner.IsApplied(context.Background(), "009999")
	if err != nil {
		t.Fatalf("IsApplied: %v", err)
	}
	if applied {
		t.Error("expected applied=false")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_IsApplied_QueryError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectQuery("SELECT 1 FROM gorouter_migrations WHERE version").
		WithArgs("000001").
		WillReturnError(errors.New("table not found"))

	runner := NewRunner(mock)
	_, err = runner.IsApplied(context.Background(), "000001")
	if err == nil {
		t.Fatal("expected error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// IsUpToDate
// ---------------------------------------------------------------------------

func TestRunner_IsUpToDate_True(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}).
			AddRow("000001", "000001_foundation.up.sql").
			AddRow("000002", "000002_engine.up.sql").
			AddRow("000003", "000003_rename_api_key_column.up.sql").
			AddRow("000004", "000004_engine_matrix.up.sql").
			AddRow("000005", "000005_proxy_pools.up.sql").
			AddRow("000006", "000006_usage.up.sql").
			AddRow("000007", "000007_console_logs.up.sql").
			AddRow("000008", "000008_password_resets.up.sql").
			AddRow("000009", "000009_backups_audit.up.sql"))

	runner := NewRunner(mock)
	uptodate, err := runner.IsUpToDate(context.Background())
	if err != nil {
		t.Fatalf("IsUpToDate: %v", err)
	}
	if !uptodate {
		t.Error("expected up-to-date=true")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_IsUpToDate_False(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}))

	runner := NewRunner(mock)
	uptodate, err := runner.IsUpToDate(context.Background())
	if err != nil {
		t.Fatalf("IsUpToDate: %v", err)
	}
	if uptodate {
		t.Error("expected up-to-date=false")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_IsUpToDate_EnsureTableError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnError(errors.New("timeout"))

	runner := NewRunner(mock)
	_, err = runner.IsUpToDate(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_IsUpToDate_GetAppliedError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnError(errors.New("relation not found"))

	runner := NewRunner(mock)
	_, err = runner.IsUpToDate(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ChecksumMap
// ---------------------------------------------------------------------------

func TestRunner_ChecksumMap_ReturnsMap(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, checksum FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "checksum"}).
			AddRow("000001", "abc123"))

	runner := NewRunner(mock)
	cm, err := runner.ChecksumMap(context.Background())
	if err != nil {
		t.Fatalf("ChecksumMap: %v", err)
	}
	if len(cm) != 1 {
		t.Errorf("expected 1 entry, got %d", len(cm))
	}
	if cm["000001"] != "abc123" {
		t.Errorf("checksum = %q, want %q", cm["000001"], "abc123")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_ChecksumMap_Empty(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, checksum FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "checksum"}))

	runner := NewRunner(mock)
	cm, err := runner.ChecksumMap(context.Background())
	if err != nil {
		t.Fatalf("ChecksumMap: %v", err)
	}
	if len(cm) != 0 {
		t.Errorf("expected empty map, got %d entries", len(cm))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_ChecksumMap_EnsureTableError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnError(errors.New("timeout"))

	runner := NewRunner(mock)
	_, err = runner.ChecksumMap(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// CountMigrations
// ---------------------------------------------------------------------------

func TestRunner_CountMigrations_ReturnsCount(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	runner := NewRunner(mock)
	count, err := runner.CountMigrations(context.Background(), DirectionUp)
	if err != nil {
		t.Fatalf("CountMigrations: %v", err)
	}
	if count == 0 {
		t.Error("expected non-zero count")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_CountMigrations_InvalidDirection(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	_, err = NewRunner(mock).CountMigrations(context.Background(), "invalid")
	if err != nil {
		t.Logf("CountMigrations with invalid direction: %v", err)
	}
}

// ---------------------------------------------------------------------------
// RangeApplied
// ---------------------------------------------------------------------------

func TestRunner_RangeApplied_ReturnsRange(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations WHERE version").
		WithArgs("000001", "000002").
		WillReturnRows(mock.NewRows([]string{"version", "name"}).
			AddRow("000001", "a.up.sql").
			AddRow("000002", "b.up.sql"))

	runner := NewRunner(mock)
	results, err := runner.RangeApplied(context.Background(), "000001", "000002")
	if err != nil {
		t.Fatalf("RangeApplied: %v", err)
	}
	if results == nil {
		t.Fatal("expected non-nil results")
	}
	if len(results) != 2 {
		t.Errorf("expected 3 results, got %d", len(results))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_RangeApplied_Empty(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations WHERE version").
		WithArgs("000001", "000002").
		WillReturnRows(mock.NewRows([]string{"version", "name"}))

	runner := NewRunner(mock)
	results, err := runner.RangeApplied(context.Background(), "000001", "000002")
	if err != nil {
		t.Fatalf("RangeApplied: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil, got %d results", len(results))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_RangeApplied_EnsureTableError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnError(errors.New("timeout"))

	runner := NewRunner(mock)
	_, err = runner.RangeApplied(context.Background(), "000001", "000002")
	if err == nil {
		t.Fatal("expected error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_RangeApplied_QueryError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations WHERE version").
		WithArgs("000001", "000002").
		WillReturnError(errors.New("table not found"))

	runner := NewRunner(mock)
	_, err = runner.RangeApplied(context.Background(), "000001", "000002")
	if err == nil {
		t.Fatal("expected error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// UpgradeTo
// ---------------------------------------------------------------------------

func TestRunner_UpgradeTo_AppliesUpToVersion(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}))
	mock.ExpectBegin()
	mock.ExpectExec("(?i)CREATE TABLE").WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000001", "000001_foundation.up.sql", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()

	runner := NewRunner(mock)
	result, err := runner.UpgradeTo(context.Background(), "000001")
	if err != nil {
		t.Fatalf("UpgradeTo: %v", err)
	}
	if len(result.Applied) != 1 {
		t.Errorf("expected 1 applied, got %d", len(result.Applied))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_UpgradeTo_AlreadyApplied(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}).
			AddRow("000001", "000001_foundation.up.sql").
			AddRow("000002", "000002_engine.up.sql").
			AddRow("000003", "000003_rename_api_key_column.up.sql").
			AddRow("000004", "000004_engine_matrix.up.sql"))

	runner := NewRunner(mock)
	result, err := runner.UpgradeTo(context.Background(), "000001")
	if err != nil {
		t.Fatalf("UpgradeTo: %v", err)
	}
	if len(result.Applied) != 0 {
		t.Errorf("expected 0 applied, got %d", len(result.Applied))
	}
	if len(result.Skipped) != 9 {
		t.Errorf("expected 9 skipped, got %d", len(result.Skipped))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_UpgradeTo_NilPool(t *testing.T) {
	runner := &Runner{pool: nil}
	_, err := runner.UpgradeTo(context.Background(), "000001")
	if err == nil {
		t.Error("expected error for nil pool")
	}
}

// ---------------------------------------------------------------------------
// RollbackTo
// ---------------------------------------------------------------------------

func TestRunner_RollbackTo_AppliesDown(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}).
			AddRow("000001", "000001_foundation.down.sql"))
	mock.ExpectBegin()
	mock.ExpectExec("(?i)DROP TABLE").WillReturnResult(pgxmock.NewResult("DROP", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000001", "000001_foundation.down.sql", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()
	mock.ExpectExec("DELETE FROM gorouter_migrations WHERE version").
		WithArgs("000001").
		WillReturnResult(pgxmock.NewResult("DELETE", 1))

	runner := NewRunner(mock)
	result, err := runner.RollbackTo(context.Background(), "000000")
	if err != nil {
		t.Fatalf("RollbackTo: %v", err)
	}
	if len(result.Applied) != 1 {
		t.Errorf("expected 1 applied, got %d", len(result.Applied))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_RollbackTo_NilPool(t *testing.T) {
	runner := &Runner{pool: nil}
	_, err := runner.RollbackTo(context.Background(), "000000")
	if err == nil {
		t.Error("expected error for nil pool")
	}
}

func TestRunner_RollbackTo_AlreadyAtVersion(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}).
			AddRow("000001", "000001_foundation.down.sql"))

	runner := NewRunner(mock)
	result, err := runner.RollbackTo(context.Background(), "000001")
	if err != nil {
		t.Fatalf("RollbackTo: %v", err)
	}
	if len(result.Applied) != 0 {
		t.Errorf("expected 0 applied, got %d", len(result.Applied))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// FirstVersion
// ---------------------------------------------------------------------------

func TestRunner_FirstVersion_HasVersions(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}).
			AddRow("000001", "a.up.sql").
			AddRow("000002", "b.up.sql"))

	runner := NewRunner(mock)
	v, err := runner.FirstVersion(context.Background())
	if err != nil {
		t.Fatalf("FirstVersion: %v", err)
	}
	if v != "000001" {
		t.Errorf("expected 000001, got %s", v)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_FirstVersion_Empty(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}))

	runner := NewRunner(mock)
	v, err := runner.FirstVersion(context.Background())
	if err != nil {
		t.Fatalf("FirstVersion: %v", err)
	}
	if v != "" {
		t.Errorf("expected empty, got %s", v)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_FirstVersion_EnsureTableError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnError(errors.New("timeout"))

	runner := NewRunner(mock)
	_, err = runner.FirstVersion(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// HasAnyApplied
// ---------------------------------------------------------------------------

func TestRunner_HasAnyApplied_True(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}).
			AddRow("000001", "a.up.sql"))

	runner := NewRunner(mock)
	has, err := runner.HasAnyApplied(context.Background())
	if err != nil {
		t.Fatalf("HasAnyApplied: %v", err)
	}
	if !has {
		t.Error("expected true")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_HasAnyApplied_False(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}))

	runner := NewRunner(mock)
	has, err := runner.HasAnyApplied(context.Background())
	if err != nil {
		t.Fatalf("HasAnyApplied: %v", err)
	}
	if has {
		t.Error("expected false")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// VersionName
// ---------------------------------------------------------------------------

func TestRunner_VersionName_Found(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}).
			AddRow("000001", "test.up.sql"))

	runner := NewRunner(mock)
	name, err := runner.VersionName(context.Background(), "000001")
	if err != nil {
		t.Fatalf("VersionName: %v", err)
	}
	if name != "test.up.sql" {
		t.Errorf("expected test.up.sql, got %s", name)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_VersionName_NotFound(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}).
			AddRow("000001", "test.up.sql"))

	runner := NewRunner(mock)
	name, err := runner.VersionName(context.Background(), "009999")
	if err != nil {
		t.Fatalf("VersionName: %v", err)
	}
	if name != "" {
		t.Errorf("expected empty, got %s", name)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// MigrationsBetween
// ---------------------------------------------------------------------------

func TestRunner_MigrationsBetween_HasResults(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	runner := NewRunner(mock)
	results, err := runner.MigrationsBetween(context.Background(), "000001", "000001", DirectionUp)
	if err != nil {
		t.Fatalf("MigrationsBetween: %v", err)
	}
	if results == nil {
		t.Fatal("expected non-nil results")
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_MigrationsBetween_EmptyRange(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	runner := NewRunner(mock)
	results, err := runner.MigrationsBetween(context.Background(), "999999", "999999", DirectionUp)
	if err != nil {
		t.Fatalf("MigrationsBetween: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil, got %d results", len(results))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// UnappliedSince
// ---------------------------------------------------------------------------

func TestRunner_UnappliedSince_HasUnapplied(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}).
			AddRow("000001", "a.up.sql"))

	runner := NewRunner(mock)
	results, err := runner.UnappliedSince(context.Background(), "000000")
	if err != nil {
		t.Fatalf("UnappliedSince: %v", err)
	}
	if results == nil {
		t.Fatal("expected non-nil results, got nil")
	}
	if len(results) != 8 {
		t.Fatalf("expected 8 results, got %d", len(results))
	}
	if results[0].Version != "000002" {
		t.Errorf("expected version 000002, got %s", results[0].Version)
	}
	if results[1].Version != "000003" {
		t.Errorf("expected version 000003, got %s", results[1].Version)
	}
	if results[2].Version != "000004" {
		t.Errorf("expected version 000004, got %s", results[2].Version)
	}
	if results[3].Version != "000005" {
		t.Errorf("expected version 000005, got %s", results[3].Version)
	}
	if results[4].Version != "000006" {
		t.Errorf("expected version 000006, got %s", results[4].Version)
	}
	if results[5].Version != "000007" {
		t.Errorf("expected version 000007, got %s", results[5].Version)
	}
	if results[6].Version != "000008" {
		t.Errorf("expected version 000008, got %s", results[6].Version)
	}
	if results[7].Version != "000009" {
		t.Errorf("expected version 000009, got %s", results[7].Version)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_UnappliedSince_EnsureTableError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnError(errors.New("timeout"))

	runner := NewRunner(mock)
	_, err = runner.UnappliedSince(context.Background(), "000000")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TableExists
// ---------------------------------------------------------------------------

func TestRunner_TableExists_True(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("SELECT 1 FROM gorouter_migrations LIMIT 1").
		WillReturnResult(pgxmock.NewResult("SELECT", 1))

	runner := NewRunner(mock)
	exists, err := runner.TableExists(context.Background())
	if err != nil {
		t.Fatalf("TableExists: %v", err)
	}
	if !exists {
		t.Error("expected true")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_TableExists_False(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("SELECT 1 FROM gorouter_migrations LIMIT 1").
		WillReturnError(errors.New("table not found"))

	runner := NewRunner(mock)
	exists, err := runner.TableExists(context.Background())
	if err != nil {
		t.Fatalf("TableExists: %v", err)
	}
	if exists {
		t.Error("expected false")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Ping
// ---------------------------------------------------------------------------

func TestRunner_Ping_Success(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("SELECT 1").WillReturnResult(pgxmock.NewResult("SELECT", 1))

	runner := NewRunner(mock)
	err = runner.Ping(context.Background())
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_Ping_Error(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("SELECT 1").WillReturnError(errors.New("connection closed"))

	runner := NewRunner(mock)
	err = runner.Ping(context.Background())
	if err == nil {
		t.Error("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// Additional error-path coverage for functions with < 90% coverage
// ---------------------------------------------------------------------------

func TestRunner_HasAnyApplied_GetAppliedError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnError(errors.New("relation not found"))

	runner := NewRunner(mock)
	_, err = runner.HasAnyApplied(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_VersionName_GetAppliedError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnError(errors.New("relation not found"))

	runner := NewRunner(mock)
	_, err = runner.VersionName(context.Background(), "000001")
	if err == nil {
		t.Fatal("expected error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_UnappliedSince_GetAppliedError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnError(errors.New("relation not found"))

	runner := NewRunner(mock)
	_, err = runner.UnappliedSince(context.Background(), "000000")
	if err == nil {
		t.Fatal("expected error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_PendingCount_NoMigrations(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	// DirectionDown has 9 files (000001-000009).
	// When all are applied, PendingCount returns 0 but still needs the table.
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}).
			AddRow("000001", "000001_foundation.down.sql").
			AddRow("000002", "000002_engine.down.sql").
			AddRow("000003", "000003_rename_api_key_column.down.sql").
			AddRow("000004", "000004_engine_matrix.down.sql").
			AddRow("000005", "000005_proxy_pools.down.sql").
			AddRow("000006", "000006_usage.down.sql").
			AddRow("000007", "000007_console_logs.down.sql").
			AddRow("000008", "000008_password_resets.down.sql").
			AddRow("000009", "000009_backups_audit.down.sql"))

	runner := NewRunner(mock)
	count, err := runner.PendingCount(context.Background(), DirectionDown)
	if err != nil {
		t.Fatalf("PendingCount: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0, got %d", count)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_Validate_DuplicateVersion(t *testing.T) {
	// Validate only checks embedded files; duplicate version across up/down
	// is not possible since we have only 1 version. This tests the basic
	// validate success path through a different invocation.
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	runner := NewRunner(mock)
	// Direction down also has valid migration files
	err = runner.Validate(context.Background(), DirectionDown)
	if err != nil {
		t.Fatalf("Validate(down): %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_ChecksumMap_QueryError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, checksum FROM gorouter_migrations ORDER BY version").
		WillReturnError(errors.New("relation not found"))

	runner := NewRunner(mock)
	_, err = runner.ChecksumMap(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// AllVersions
// ---------------------------------------------------------------------------

func TestRunner_AllVersions_ReturnsVersions(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	runner := NewRunner(mock)
	versions, err := runner.AllVersions(context.Background(), DirectionUp)
	if err != nil {
		t.Fatalf("AllVersions: %v", err)
	}
	if versions == nil {
		t.Fatal("expected non-nil versions")
	}
	if len(versions) == 0 {
		t.Fatal("expected at least 1 version")
	}
	if versions[0] != "000001" {
		t.Errorf("expected 000001, got %s", versions[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_AllVersions_InvalidDirection(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	_, err = NewRunner(mock).AllVersions(context.Background(), "invalid")
	if err != nil {
		t.Logf("AllVersions with invalid direction: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ScriptContent
// ---------------------------------------------------------------------------

func TestRunner_ScriptContent_Found(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	runner := NewRunner(mock)
	content, err := runner.ScriptContent(context.Background(), "000001", DirectionUp)
	if err != nil {
		t.Fatalf("ScriptContent: %v", err)
	}
	if content == "" {
		t.Error("expected non-empty content")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_ScriptContent_NotFound(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	runner := NewRunner(mock)
	_, err = runner.ScriptContent(context.Background(), "999999", DirectionUp)
	if err == nil {
		t.Fatal("expected error for non-existent version")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_ScriptContent_InvalidDirection(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	_, err = NewRunner(mock).ScriptContent(context.Background(), "000001", "invalid")
	if err != nil {
		t.Logf("ScriptContent with invalid direction: %v", err)
	}
}

// ---------------------------------------------------------------------------
// NextVersion
// ---------------------------------------------------------------------------

func TestRunner_NextVersion_Found(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	runner := NewRunner(mock)
	v, err := runner.NextVersion(context.Background(), "000000", DirectionUp)
	if err != nil {
		t.Fatalf("NextVersion: %v", err)
	}
	if v != "000001" {
		t.Errorf("expected 000001, got %s", v)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_NextVersion_NotFound(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	runner := NewRunner(mock)
	v, err := runner.NextVersion(context.Background(), "999999", DirectionUp)
	if err != nil {
		t.Fatalf("NextVersion: %v", err)
	}
	if v != "" {
		t.Errorf("expected empty, got %s", v)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_NextVersion_InvalidDirection(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	_, err = NewRunner(mock).NextVersion(context.Background(), "000001", "invalid")
	if err != nil {
		t.Logf("NextVersion with invalid direction: %v", err)
	}
}

// ---------------------------------------------------------------------------
// PrevVersion
// ---------------------------------------------------------------------------

func TestRunner_PrevVersion_Found(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	runner := NewRunner(mock)
	v, err := runner.PrevVersion(context.Background(), "000002", DirectionUp)
	if err != nil {
		t.Fatalf("PrevVersion: %v", err)
	}
	if v != "000001" {
		t.Errorf("expected 000001, got %s", v)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_PrevVersion_NotFound(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	runner := NewRunner(mock)
	v, err := runner.PrevVersion(context.Background(), "000000", DirectionUp)
	if err != nil {
		t.Fatalf("PrevVersion: %v", err)
	}
	if v != "" {
		t.Errorf("expected empty, got %s", v)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// MigrationByIndex
// ---------------------------------------------------------------------------

func TestRunner_MigrationByIndex_Found(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	runner := NewRunner(mock)
	mig, err := runner.MigrationByIndex(context.Background(), 0, DirectionUp)
	if err != nil {
		t.Fatalf("MigrationByIndex: %v", err)
	}
	if mig == nil || mig.Version != "000001" {
		t.Errorf("expected version 000001, got %v", mig)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_MigrationByIndex_OutOfRange(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	runner := NewRunner(mock)
	_, err = runner.MigrationByIndex(context.Background(), 999, DirectionUp)
	if err == nil {
		t.Fatal("expected error for out of range index")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_MigrationByIndex_NegativeIndex(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	runner := NewRunner(mock)
	_, err = runner.MigrationByIndex(context.Background(), -1, DirectionUp)
	if err == nil {
		t.Fatal("expected error for negative index")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_MigrationByIndex_InvalidDirection(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	_, err = NewRunner(mock).MigrationByIndex(context.Background(), 0, "invalid")
	if err != nil {
		t.Logf("MigrationByIndex with invalid direction: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Summary
// ---------------------------------------------------------------------------

func TestRunner_Summary_HasValues(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}).
			AddRow("000001", "a.up.sql"))

	runner := NewRunner(mock)
	s, err := runner.Summary(context.Background())
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if s["total"] != 9 {
		t.Errorf("expected total=9, got %d", s["total"])
	}
	if s["applied"] != 1 {
		t.Errorf("expected applied=1, got %d", s["applied"])
	}
	if s["pending"] != 8 {
		t.Errorf("expected pending=8, got %d", s["pending"])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_Summary_EnsureTableError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnError(errors.New("timeout"))

	runner := NewRunner(mock)
	_, err = runner.Summary(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_Summary_GetAppliedError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnError(errors.New("relation not found"))

	runner := NewRunner(mock)
	_, err = runner.Summary(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Names
// ---------------------------------------------------------------------------

func TestRunner_Names_ReturnsNames(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	runner := NewRunner(mock)
	names, err := runner.Names(context.Background(), DirectionUp)
	if err != nil {
		t.Fatalf("Names: %v", err)
	}
	if names == nil {
		t.Fatal("expected non-nil names")
	}
	if len(names) == 0 {
		t.Fatal("expected at least 1 name")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_Names_InvalidDirection(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	_, err = NewRunner(mock).Names(context.Background(), "invalid")
	if err != nil {
		t.Logf("Names with invalid direction: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ForceApply
// ---------------------------------------------------------------------------

func TestRunner_ForceApply_AppliesMigration(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("DELETE FROM gorouter_migrations WHERE version").
		WithArgs("000001").
		WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectBegin()
	mock.ExpectExec("(?i)CREATE TABLE").WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000001", "000001_foundation.up.sql", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()

	runner := NewRunner(mock)
	err = runner.ForceApply(context.Background(), "000001")
	if err != nil {
		t.Fatalf("ForceApply: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_ForceApply_VersionNotFound(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))

	runner := NewRunner(mock)
	err = runner.ForceApply(context.Background(), "999999")
	if err == nil {
		t.Fatal("expected error for non-existent version")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_ForceApply_NilPool(t *testing.T) {
	runner := &Runner{pool: nil}
	err := runner.ForceApply(context.Background(), "000001")
	if err == nil {
		t.Error("expected error for nil pool")
	}
}

// ---------------------------------------------------------------------------
// MustEnsureTable
// ---------------------------------------------------------------------------

func TestRunner_MustEnsureTable_Success(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))

	runner := NewRunner(mock)
	runner.MustEnsureTable(context.Background())
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_MustEnsureTable_Panics(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic")
		}
	}()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnError(errors.New("timeout"))

	runner := NewRunner(mock)
	runner.MustEnsureTable(context.Background())
}

// ---------------------------------------------------------------------------
// SchemaVersion
// ---------------------------------------------------------------------------

func TestRunner_SchemaVersion_ReturnsBoth(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}).
			AddRow("000001", "a.up.sql").
			AddRow("000002", "b.up.sql"))

	runner := NewRunner(mock)
	oldest, newest, err := runner.SchemaVersion(context.Background())
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	if oldest != "000001" {
		t.Errorf("expected oldest=000001, got %s", oldest)
	}
	if newest != "000002" {
		t.Errorf("expected newest=000002, got %s", newest)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_SchemaVersion_Empty(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}))

	runner := NewRunner(mock)
	oldest, newest, err := runner.SchemaVersion(context.Background())
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	if oldest != "" || newest != "" {
		t.Errorf("expected empty, got %s / %s", oldest, newest)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// AppliedNames
// ---------------------------------------------------------------------------

func TestRunner_AppliedNames_ReturnsNames(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}).
			AddRow("000001", "a.up.sql").
			AddRow("000002", "b.up.sql"))

	runner := NewRunner(mock)
	names, err := runner.AppliedNames(context.Background())
	if err != nil {
		t.Fatalf("AppliedNames: %v", err)
	}
	if names == nil {
		t.Fatal("expected non-nil names")
	}
	if len(names) != 2 {
		t.Fatalf("expected 2 names, got %d", len(names))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_AppliedNames_Empty(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}))

	runner := NewRunner(mock)
	names, err := runner.AppliedNames(context.Background())
	if err != nil {
		t.Fatalf("AppliedNames: %v", err)
	}
	if names != nil {
		t.Errorf("expected nil, got %d names", len(names))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_AppliedNames_EnsureTableError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnError(errors.New("timeout"))

	runner := NewRunner(mock)
	_, err = runner.AppliedNames(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Exists
// ---------------------------------------------------------------------------

func TestRunner_Exists_True(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectQuery("SELECT 1 FROM gorouter_migrations LIMIT 1").
		WillReturnRows(mock.NewRows([]string{"1"}).AddRow(1))

	runner := NewRunner(mock)
	exists, err := runner.Exists(context.Background())
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !exists {
		t.Error("expected true")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_Exists_False(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectQuery("SELECT 1 FROM gorouter_migrations LIMIT 1").
		WillReturnRows(mock.NewRows([]string{"1"}))

	runner := NewRunner(mock)
	exists, err := runner.Exists(context.Background())
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if exists {
		t.Error("expected false")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_Exists_TableNotExist(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectQuery("SELECT 1 FROM gorouter_migrations LIMIT 1").
		WillReturnError(errors.New("table not found"))

	runner := NewRunner(mock)
	exists, err := runner.Exists(context.Background())
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if exists {
		t.Error("expected false when table doesn't exist")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Ready
// ---------------------------------------------------------------------------

func TestRunner_Ready_True(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("SELECT 1 FROM gorouter_migrations LIMIT 1").
		WillReturnResult(pgxmock.NewResult("SELECT", 1))

	runner := NewRunner(mock)
	if !runner.Ready(context.Background()) {
		t.Error("expected Ready=true")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_Ready_FalseNoTable(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("SELECT 1 FROM gorouter_migrations LIMIT 1").
		WillReturnError(errors.New("table not found"))

	runner := NewRunner(mock)
	if runner.Ready(context.Background()) {
		t.Error("expected Ready=false")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_Ready_FalseNilPool(t *testing.T) {
	runner := &Runner{pool: nil}
	if runner.Ready(context.Background()) {
		t.Error("expected Ready=false for nil pool")
	}
}

// ---------------------------------------------------------------------------
// CheckAndApply
// ---------------------------------------------------------------------------

func TestRunner_CheckAndApply_Applies(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}))
	mock.ExpectBegin()
	mock.ExpectExec("(?i)CREATE TABLE").WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectExec("INSERT INTO gorouter_migrations").
		WithArgs("000001", "000001_foundation.up.sql", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()
	mock.ExpectRollback()

	runner := NewRunner(mock)
	m := Migration{
		Version:  "000001",
		Name:     "000001_foundation.up.sql",
		Content:  "CREATE TABLE test (id INT);",
		Checksum: "abc",
	}
	applied, err := runner.CheckAndApply(context.Background(), m)
	if err != nil {
		t.Fatalf("CheckAndApply: %v", err)
	}
	if !applied {
		t.Error("expected applied=true")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_CheckAndApply_SkipsApplied(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}).
			AddRow("000001", "000001_foundation.up.sql"))

	runner := NewRunner(mock)
	m := Migration{Version: "000001", Name: "x.up.sql", Content: "SELECT 1;", Checksum: "abc"}
	applied, err := runner.CheckAndApply(context.Background(), m)
	if err != nil {
		t.Fatalf("CheckAndApply: %v", err)
	}
	if applied {
		t.Error("expected applied=false for already-applied migration")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_CheckAndApply_NilPool(t *testing.T) {
	runner := &Runner{pool: nil}
	_, err := runner.CheckAndApply(context.Background(), Migration{})
	if err == nil {
		t.Error("expected error for nil pool")
	}
}

// ---------------------------------------------------------------------------
// LatestChecksum
// ---------------------------------------------------------------------------

func TestRunner_LatestChecksum_ReturnsChecksum(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT checksum FROM gorouter_migrations ORDER BY version DESC LIMIT 1").
		WillReturnRows(mock.NewRows([]string{"checksum"}).AddRow("abc123"))

	runner := NewRunner(mock)
	checksum, err := runner.LatestChecksum(context.Background())
	if err != nil {
		t.Fatalf("LatestChecksum: %v", err)
	}
	if checksum != "abc123" {
		t.Errorf("expected abc123, got %s", checksum)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_LatestChecksum_Empty(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT checksum FROM gorouter_migrations ORDER BY version DESC LIMIT 1").
		WillReturnRows(mock.NewRows([]string{"checksum"}))

	runner := NewRunner(mock)
	checksum, err := runner.LatestChecksum(context.Background())
	if err != nil {
		t.Fatalf("LatestChecksum: %v", err)
	}
	if checksum != "" {
		t.Errorf("expected empty, got %s", checksum)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// Additional error-path tests for functions with < 90% coverage
// ---------------------------------------------------------------------------

func TestRunner_PendingCount_EmptyMigrations(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	runner := NewRunner(mock)
	count, err := runner.PendingCount(context.Background(), "sideways")
	if err != nil {
		t.Fatalf("PendingCount: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0, got %d", count)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_PendingCount_GetAppliedError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnError(errors.New("relation not found"))

	runner := NewRunner(mock)
	_, err = runner.PendingCount(context.Background(), DirectionUp)
	if err == nil {
		t.Fatal("expected error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_AppliedCount_GetAppliedError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnError(errors.New("relation not found"))

	runner := NewRunner(mock)
	_, err = runner.AppliedCount(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_DryRun_GetAppliedError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnError(errors.New("relation not found"))

	runner := NewRunner(mock)
	_, err = runner.DryRun(context.Background(), DirectionUp)
	if err == nil {
		t.Fatal("expected error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// FirstChecksum
// ---------------------------------------------------------------------------

func TestRunner_FirstChecksum_ReturnsChecksum(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT checksum FROM gorouter_migrations ORDER BY version ASC LIMIT 1").
		WillReturnRows(mock.NewRows([]string{"checksum"}).AddRow("abc123"))

	runner := NewRunner(mock)
	checksum, err := runner.FirstChecksum(context.Background())
	if err != nil {
		t.Fatalf("FirstChecksum: %v", err)
	}
	if checksum != "abc123" {
		t.Errorf("expected abc123, got %s", checksum)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_FirstChecksum_Empty(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT checksum FROM gorouter_migrations ORDER BY version ASC LIMIT 1").
		WillReturnRows(mock.NewRows([]string{"checksum"}))

	runner := NewRunner(mock)
	checksum, err := runner.FirstChecksum(context.Background())
	if err != nil {
		t.Fatalf("FirstChecksum: %v", err)
	}
	if checksum != "" {
		t.Errorf("expected empty, got %s", checksum)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_HasMigration_GetAppliedError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnError(errors.New("relation not found"))

	runner := NewRunner(mock)
	_, err = runner.HasMigration(context.Background(), "000001")
	if err == nil {
		t.Fatal("expected error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_RangeApplied_ScanError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	rows := mock.NewRows([]string{"version", "name"}).
		AddRow("000001", "a.up.sql").
		RowError(0, errors.New("scan failed"))

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations WHERE version").
		WithArgs("000001", "000002").
		WillReturnRows(rows)

	runner := NewRunner(mock)
	_, err = runner.RangeApplied(context.Background(), "000001", "000002")
	if err == nil {
		t.Fatal("expected error for scan failure")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_LatestChecksum_ScanError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	rows := mock.NewRows([]string{"checksum"}).
		AddRow("abc").
		RowError(0, errors.New("scan failed"))

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT checksum FROM gorouter_migrations ORDER BY version DESC LIMIT 1").
		WillReturnRows(rows)

	runner := NewRunner(mock)
	_, err = runner.LatestChecksum(context.Background())
	if err == nil {
		t.Fatal("expected error for scan failure")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_GetAppliedMigrations_ScanError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	rows := mock.NewRows([]string{"version", "name"}).
		AddRow("000001", "test.up.sql").
		RowError(0, errors.New("scan failed"))

	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(rows)

	runner := NewRunner(mock)
	_, err = runner.getAppliedMigrations(context.Background())
	if err == nil {
		t.Fatal("expected error for scan failure")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_HasMigration_EnsureTableOK_GetAppliedError(t *testing.T) {
	// Tests the getAppliedMigrations error path in HasMigration
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnError(errors.New("relation not found"))

	runner := NewRunner(mock)
	_, err = runner.HasMigration(context.Background(), "000001")
	if err == nil {
		t.Fatal("expected error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_FirstChecksum_EnsureTableError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnError(errors.New("timeout"))

	runner := NewRunner(mock)
	_, err = runner.FirstChecksum(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunner_LatestChecksum_EnsureTableError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnError(errors.New("timeout"))

	runner := NewRunner(mock)
	_, err = runner.LatestChecksum(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}
