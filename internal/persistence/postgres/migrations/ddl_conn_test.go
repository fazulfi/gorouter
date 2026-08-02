package migrations

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/pashagolub/pgxmock/v2"
)

// TestDDLRoleUserExplicit pins the DDL role name used for migration batches:
// role selection is explicit and no production role is mutated.
func TestDDLRoleUserExplicit(t *testing.T) {
	if DDLRoleUser != "gorouter_ddl" {
		t.Fatalf("DDLRoleUser = %q, want %q", DDLRoleUser, "gorouter_ddl")
	}
}

// TestFromRuntimeDSN_SelectsDDLRole proves the DDL connection config is
// derived from the runtime DSN with the DDL role substituted for the
// runtime role and the runtime credential dropped.
func TestFromRuntimeDSN_SelectsDDLRole(t *testing.T) {
	cfg, err := FromRuntimeDSN("postgres://gorouter:sekrit@127.0.0.1:5432/gorouter?application_name=gorouter-runtime")
	if err != nil {
		t.Fatalf("FromRuntimeDSN: %v", err)
	}
	if cfg.User != DDLRoleUser {
		t.Errorf("User = %q, want DDL role %q", cfg.User, DDLRoleUser)
	}
	if cfg.Host != "127.0.0.1" || cfg.Port != 5432 || cfg.Database != "gorouter" {
		t.Errorf("transport = %s:%d/%s, want 127.0.0.1:5432/gorouter", cfg.Host, cfg.Port, cfg.Database)
	}
	if got := cfg.RuntimeParams["application_name"]; got != "gorouter-runtime" {
		t.Errorf("application_name = %q, want gorouter-runtime", got)
	}
	if s := fmt.Sprintf("%#v", cfg); strings.Contains(s, "sekrit") {
		t.Error("DDL config leaks the runtime password")
	}
}

// TestFromRuntimeDSN_SocketPeerAuth proves a Unix socket runtime DSN keeps
// the socket directory so the DDL connection authenticates via peer auth.
func TestFromRuntimeDSN_SocketPeerAuth(t *testing.T) {
	cfg, err := FromRuntimeDSN("postgres://gorouter@/gorouter?host=/var/run/postgresql")
	if err != nil {
		t.Fatalf("FromRuntimeDSN: %v", err)
	}
	if cfg.Host != "/var/run/postgresql" {
		t.Errorf("Host = %q, want Unix socket dir preserved for peer auth", cfg.Host)
	}
	if cfg.User != DDLRoleUser {
		t.Errorf("User = %q, want DDL role %q", cfg.User, DDLRoleUser)
	}
	if cfg.Port != 5432 {
		t.Errorf("Port = %d, want default 5432", cfg.Port)
	}
}

func TestFromRuntimeDSN_Invalid(t *testing.T) {
	if _, err := FromRuntimeDSN("not a dsn at all"); err == nil {
		t.Fatal("FromRuntimeDSN on invalid DSN: expected error, got nil")
	}
}

// TestRunMigrations_UsesDedicatedDDLConnection proves the migration batch
// executes on the dedicated DDL connection injected through the openDDL
// seam: the runtime pool is never involved, all pending up migrations are
// applied, and the dedicated connection is closed afterwards.
func TestRunMigrations_UsesDedicatedDDLConnection(t *testing.T) {
	mock, err := pgxmock.NewConn()
	if err != nil {
		t.Fatal(err)
	}

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}))
	for _, m := range []struct {
		version, name, content string
	}{
		{"000001", "000001_foundation.up.sql", "(?i)CREATE TABLE"},
		{"000002", "000002_engine.up.sql", "(?i)CREATE TABLE"},
		{"000003", "000003_rename_api_key_column.up.sql", "(?i)ALTER TABLE"},
		{"000004", "000004_engine_matrix.up.sql", "(?i)CREATE TABLE IF NOT EXISTS gorouter_oauth_sessions"},
	} {
		mock.ExpectBegin()
		mock.ExpectExec(m.content).WillReturnResult(pgxmock.NewResult("CREATE", 0))
		mock.ExpectExec("INSERT INTO gorouter_migrations").
			WithArgs(m.version, m.name, pgxmock.AnyArg()).
			WillReturnResult(pgxmock.NewResult("INSERT", 1))
		mock.ExpectCommit()
		mock.ExpectRollback()
	}
	mock.ExpectClose()

	origOpenDDL := openDDL
	openDDL = func(_ context.Context, _ DDLConfig) (ddlConn, error) {
		return mock, nil
	}
	t.Cleanup(func() { openDDL = origOpenDDL })

	result, err := RunMigrations(context.Background(), DDLConfig{User: DDLRoleUser})
	if err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	if len(result.Applied) != 4 {
		t.Errorf("applied = %d, want 4", len(result.Applied))
	}
	if len(result.Skipped) != 0 {
		t.Errorf("skipped = %d, want 0", len(result.Skipped))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestRunMigrations_EmptyRoleFailsClosed proves a missing DDL role fails
// closed before any connection is attempted.
func TestRunMigrations_EmptyRoleFailsClosed(t *testing.T) {
	origOpenDDL := openDDL
	called := false
	openDDL = func(_ context.Context, _ DDLConfig) (ddlConn, error) {
		called = true
		return nil, errors.New("must not connect")
	}
	t.Cleanup(func() { openDDL = origOpenDDL })

	if _, err := RunMigrations(context.Background(), DDLConfig{}); err == nil {
		t.Fatal("RunMigrations with empty DDL role: expected error, got nil")
	}
	if called {
		t.Error("RunMigrations with empty role attempted to connect")
	}
}

// TestRunMigrations_OpenErrorFailsClosed proves a connection-open failure
// surfaces the error and never runs the batch.
func TestRunMigrations_OpenErrorFailsClosed(t *testing.T) {
	origOpenDDL := openDDL
	openDDL = func(_ context.Context, _ DDLConfig) (ddlConn, error) {
		return nil, errors.New("connection refused")
	}
	t.Cleanup(func() { openDDL = origOpenDDL })

	_, err := RunMigrations(context.Background(), DDLConfig{User: DDLRoleUser})
	if err == nil {
		t.Fatal("RunMigrations: expected open error, got nil")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("error = %q, want open failure surfaced", err.Error())
	}
}

// TestRunMigrations_BatchFailureClosesConnection proves a failed batch
// still closes the dedicated connection (no resource leak) and surfaces
// the failure so the caller can enter safe mode.
func TestRunMigrations_BatchFailureClosesConnection(t *testing.T) {
	mock, err := pgxmock.NewConn()
	if err != nil {
		t.Fatal(err)
	}

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS gorouter_migrations").
		WillReturnResult(pgxmock.NewResult("CREATE", 0))
	mock.ExpectQuery("SELECT version, name FROM gorouter_migrations ORDER BY version").
		WillReturnRows(mock.NewRows([]string{"version", "name"}))
	mock.ExpectBegin()
	mock.ExpectExec("(?i)CREATE TABLE").WillReturnError(errors.New("permission denied for schema public"))
	mock.ExpectRollback()
	mock.ExpectClose()

	origOpenDDL := openDDL
	openDDL = func(_ context.Context, _ DDLConfig) (ddlConn, error) {
		return mock, nil
	}
	t.Cleanup(func() { openDDL = origOpenDDL })

	_, err = RunMigrations(context.Background(), DDLConfig{User: DDLRoleUser})
	if err == nil {
		t.Fatal("RunMigrations: expected batch error, got nil")
	}
	if !strings.Contains(err.Error(), "DDL-role migration batch") {
		t.Errorf("error = %q, want batch failure wrapped", err.Error())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations (dedicated connection not closed): %v", err)
	}
}
