package migrations

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// auditOwnershipTestDBPrefix is the dedicated test database prefix for the
// audit-log ownership precondition tests.
const auditOwnershipTestDBPrefix = "audit_own_iso_"

// nomemberDDLRole is a test-only DDL role without membership in the runtime
// role. It is provisioned on the test containers so the fail-closed path can
// be proven under real memberships: the only difference from the regular DDL
// role is the missing membership that enables the ownership transfer.
const nomemberDDLRole = "gorouter_ddl_nomember"

// ownershipTestDB carries the fresh test database identity for a fixture.
type ownershipTestDB struct {
	runtimeDSN string
	dbName     string
}

// applyAs applies the given migration on a connection authenticated as the
// given role by swapping the runtime DSN role and database.
func applyAs(t *testing.T, ctx context.Context, runtimeBase, dbName, role string, m Migration) {
	t.Helper()
	cfg, err := pgx.ParseConfig(runtimeBase)
	if err != nil {
		t.Fatalf("parse runtime DSN for %s: %v", role, err)
	}
	cfg.User = role
	cfg.Database = dbName
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect as %s: %v", role, err)
	}
	defer conn.Close(ctx)
	runner := NewRunner(&ddlPool{conn: conn})
	if err := runner.ensureMigrationsTable(ctx); err != nil {
		t.Fatalf("ensure migrations table as %s: %v", role, err)
	}
	if err := runner.applyMigration(ctx, m); err != nil {
		t.Fatalf("apply %s as %s: %v", m.Name, role, err)
	}
}

// setupOwnershipTestDB creates a fresh database and applies the migrations
// up to (not including) the given version as the given role, then grants the
// DDL test roles schema-create and migration-tracking privileges. The
// tracking-table and schema grants mirror the production DDL role, which is
// provisioned superuser-by-construction and therefore bypasses such ACLs.
func setupOwnershipTestDB(t *testing.T, ctx context.Context, upToVersion, appliedAs string) ownershipTestDB {
	t.Helper()
	runtimeBase := os.Getenv("DATABASE_URL")
	if runtimeBase == "" {
		t.Skip("DATABASE_URL not set")
	}
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	if err := bootstrapRolesOnCluster(t, ctx, runtimeBase); err != nil {
		t.Fatalf("bootstrap roles: %v", err)
	}

	admin, err := pgx.Connect(ctx, runtimeBase)
	if err != nil {
		t.Fatalf("connect as runtime role: %v", err)
	}
	dbName := auditOwnershipTestDBPrefix + uuid.NewString()[:8]
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+dbName); err != nil {
		t.Fatalf("create test db %s: %v", dbName, err)
	}
	admin.Close(ctx)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cleanupConn, err := pgx.Connect(cleanupCtx, runtimeBase)
		if err != nil {
			return
		}
		defer cleanupConn.Close(cleanupCtx)
		_, _ = cleanupConn.Exec(cleanupCtx,
			"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()",
			dbName)
		_, _ = cleanupConn.Exec(cleanupCtx, "DROP DATABASE IF EXISTS "+dbName)
	})

	// Grant CREATE ON SCHEMA public to roles that may apply migrations:
	// - DDLRoleUser/nomemberDDLRole are the migration executors (per Rev3)
	// - gorouter is granted only for the legacy-schema simulation where the
	//   runtime role owns tables (fixtures at lines 307–346)
	for _, role := range []string{DDLRoleUser, nomemberDDLRole, "gorouter"} {
		freshAdmin, err := pgx.Connect(ctx, runtimeTestDSN(t, runtimeBase, dbName))
		if err != nil {
			t.Fatalf("connect to fresh db as runtime role: %v", err)
		}
		if _, err := freshAdmin.Exec(ctx, "GRANT CREATE ON SCHEMA public TO "+role); err != nil {
			freshAdmin.Close(ctx)
			t.Fatalf("grant %s schema create: %v", role, err)
		}
		freshAdmin.Close(ctx)
	}

	upMigrations, err := parseMigrations(DirectionUp)
	if err != nil {
		t.Fatalf("parse up migrations: %v", err)
	}
	for _, m := range upMigrations {
		if m.Version == upToVersion {
			break
		}
		applyAs(t, ctx, runtimeBase, dbName, appliedAs, m)
	}

	// The tracking table was created by appliedAs, so the DDL test roles
	// need read/write on it to run a batch, mirroring the superuser standing
	// of the production DDL role. The grants run as appliedAs, the table
	// owner. A virgin fixture (nothing applied) has no tracking table yet.
	if upToVersion != "000001" {
		for _, role := range []string{DDLRoleUser, nomemberDDLRole} {
			cfg, err := pgx.ParseConfig(runtimeBase)
			if err != nil {
				t.Fatalf("parse runtime DSN: %v", err)
			}
			cfg.User = appliedAs
			cfg.Database = dbName
			conn, err := pgx.ConnectConfig(ctx, cfg)
			if err != nil {
				t.Fatalf("connect as %s: %v", appliedAs, err)
			}
			if _, err := conn.Exec(ctx,
				"GRANT SELECT, INSERT, UPDATE ON gorouter_migrations TO "+role); err != nil {
				conn.Close(ctx)
				t.Fatalf("grant %s migrations-table access: %v", role, err)
			}
			conn.Close(ctx)
		}
	}

	return ownershipTestDB{runtimeDSN: runtimeTestDSN(t, runtimeBase, dbName), dbName: dbName}
}

// openDDLSeam replaces the openDDL seam with a real connection for the role
// requested in the DDL config on the given database, so the production
// RunMigrations path can be exercised end to end against a real server.
func openDDLSeam(t *testing.T, runtimeBase, dbName string) {
	t.Helper()
	origOpenDDL := openDDL
	openDDL = func(ctx context.Context, cfg DDLConfig) (ddlConn, error) {
		parsed, err := pgx.ParseConfig(runtimeBase)
		if err != nil {
			return nil, err
		}
		parsed.User = cfg.User
		parsed.Database = dbName
		return pgx.ConnectConfig(ctx, parsed)
	}
	t.Cleanup(func() { openDDL = origOpenDDL })
}

// auditLogOwner returns the role that owns gorouter_audit_log.
func auditLogOwner(ctx context.Context, conn *pgxpool.Pool) (string, error) {
	var owner string
	err := conn.QueryRow(ctx,
		`SELECT pg_catalog.pg_get_userbyid(c.relowner) FROM pg_class c
		 WHERE c.relname = 'gorouter_audit_log' AND c.relkind = 'r'`).Scan(&owner)
	return owner, err
}

// assertRuntimeAuditPrivileges asserts the runtime role holds INSERT and
// SELECT on the audit log and nothing else.
func assertRuntimeAuditPrivileges(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	for _, priv := range []string{"SELECT", "INSERT"} {
		var held bool
		if err := pool.QueryRow(ctx,
			`SELECT has_table_privilege('gorouter', 'gorouter_audit_log', $1)`, priv).Scan(&held); err != nil {
			t.Fatalf("privilege probe %s: %v", priv, err)
		}
		if !held {
			t.Errorf("runtime role %s on gorouter_audit_log = %v, want true", priv, held)
		}
	}
	for _, priv := range []string{"UPDATE", "DELETE", "TRUNCATE"} {
		var held bool
		if err := pool.QueryRow(ctx,
			`SELECT has_table_privilege('gorouter', 'gorouter_audit_log', $1)`, priv).Scan(&held); err != nil {
			t.Fatalf("privilege probe %s: %v", priv, err)
		}
		if held {
			t.Errorf("runtime role %s on gorouter_audit_log = %v, want false", priv, held)
		}
	}
}

// assertAuditMutationProbesFail asserts the runtime role cannot UPDATE,
// DELETE, TRUNCATE or alter the audit log.
func assertAuditMutationProbesFail(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx,
		`INSERT INTO gorouter_audit_log (id, action, resource_type) VALUES ($1, 'seed', 'settings')`,
		uuid.New()); err != nil {
		t.Fatalf("seed audit row: %v", err)
	}
	probes := []struct {
		name string
		sql  string
	}{
		{"UPDATE", `UPDATE gorouter_audit_log SET details = '{"tampered": true}'::jsonb WHERE action = 'seed'`},
		{"DELETE", `DELETE FROM gorouter_audit_log WHERE action = 'seed'`},
		{"TRUNCATE", `TRUNCATE gorouter_audit_log`},
		{"DDL", `ALTER TABLE gorouter_audit_log ADD COLUMN _ownership_probe TEXT`},
	}
	for _, p := range probes {
		t.Run(p.name, func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatalf("begin: %v", err)
			}
			defer tx.Rollback(ctx)

			_, err = tx.Exec(ctx, p.sql)
			if err == nil {
				t.Fatalf("%s on gorouter_audit_log as the runtime role must fail", p.name)
			}
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) {
				t.Fatalf("%s: expected a PostgreSQL permission error, got %v", p.name, err)
			}
			if pgErr.Code != "42501" {
				t.Errorf("%s: error code = %s, want 42501 insufficient_privilege (%v)", p.name, pgErr.Code, err)
			}
		})
	}
}

// TestAuditOwnershipPrecondition proves the audit-immutability ownership
// precondition on real PostgreSQL: a fresh install passes, a legacy schema
// whose audit log is owned by the runtime role is either transferred to the
// DDL role (when the DDL role has the required membership) or blocks the
// batch fail-closed with an actionable transfer instruction, and after the
// upgrade the runtime role is not the owner and cannot mutate the audit log.
func TestAuditOwnershipPrecondition(t *testing.T) {
	ctx := context.Background()
	runtimeBase := os.Getenv("DATABASE_URL")

	t.Run("fresh install applies the full batch under the precondition", func(t *testing.T) {
		db := setupOwnershipTestDB(t, ctx, "000001", DDLRoleUser)
		openDDLSeam(t, runtimeBase, db.dbName)

		result, err := RunMigrations(ctx, DDLConfig{User: DDLRoleUser})
		if err != nil {
			t.Fatalf("RunMigrations on fresh database: %v", err)
		}
		if len(result.Applied) != 9 {
			t.Errorf("applied = %d, want 9", len(result.Applied))
		}
		pool, err := pgxpool.New(ctx, db.runtimeDSN)
		if err != nil {
			t.Fatalf("runtime pool: %v", err)
		}
		defer pool.Close()
		owner, err := auditLogOwner(ctx, pool)
		if err != nil {
			t.Fatalf("audit log owner: %v", err)
		}
		if owner != DDLRoleUser {
			t.Fatalf("audit log owner = %q, want DDL role %q", owner, DDLRoleUser)
		}
		assertRuntimeAuditPrivileges(t, ctx, pool)
		assertAuditMutationProbesFail(t, ctx, pool)
	})

	t.Run("DDL-owned schema passes the precondition", func(t *testing.T) {
		db := setupOwnershipTestDB(t, ctx, "000009", DDLRoleUser)
		openDDLSeam(t, runtimeBase, db.dbName)

		result, err := RunMigrations(ctx, DDLConfig{User: DDLRoleUser})
		if err != nil {
			t.Fatalf("RunMigrations: %v", err)
		}
		if len(result.Applied) != 1 || result.Applied[0] != "000009_backups_audit.up.sql" {
			t.Errorf("applied = %v, want exactly 000009_backups_audit.up.sql", result.Applied)
		}
		pool, err := pgxpool.New(ctx, db.runtimeDSN)
		if err != nil {
			t.Fatalf("runtime pool: %v", err)
		}
		defer pool.Close()
		owner, err := auditLogOwner(ctx, pool)
		if err != nil {
			t.Fatalf("audit log owner: %v", err)
		}
		if owner != DDLRoleUser {
			t.Fatalf("audit log owner = %q, want DDL role %q", owner, DDLRoleUser)
		}
	})

	t.Run("legacy schema without transfer capability fails closed", func(t *testing.T) {
		db := setupOwnershipTestDB(t, ctx, "000005", "gorouter")
		openDDLSeam(t, runtimeBase, db.dbName)

		_, err := RunMigrations(ctx, DDLConfig{User: nomemberDDLRole})
		if err == nil {
			t.Fatal("RunMigrations on runtime-owned audit log: expected fail-closed error, got nil")
		}
		for _, want := range []string{
			"audit immutability precondition",
			"ALTER TABLE gorouter_audit_log OWNER TO " + nomemberDDLRole,
			"gorouter_audit_log",
		} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not contain %q", err.Error(), want)
			}
		}
		pool, err := pgxpool.New(ctx, db.runtimeDSN)
		if err != nil {
			t.Fatalf("runtime pool: %v", err)
		}
		defer pool.Close()
		owner, err := auditLogOwner(ctx, pool)
		if err != nil {
			t.Fatalf("audit log owner: %v", err)
		}
		if owner != "gorouter" {
			t.Errorf("audit log owner = %q, want gorouter (batch must not transfer without capability)", owner)
		}
		var applied009 int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM gorouter_migrations WHERE version = '000009'`).Scan(&applied009); err != nil {
			t.Fatalf("check 000009 record: %v", err)
		}
		if applied009 != 0 {
			t.Errorf("000009 recorded despite the fail-closed precondition (applied = %d)", applied009)
		}
	})

	t.Run("legacy schema transfers ownership and immutability holds", func(t *testing.T) {
		db := setupOwnershipTestDB(t, ctx, "000005", "gorouter")
		openDDLSeam(t, runtimeBase, db.dbName)

		result, err := RunMigrations(ctx, DDLConfig{User: DDLRoleUser})
		if err != nil {
			t.Fatalf("RunMigrations on legacy database: %v", err)
		}
		found := false
		for _, applied := range result.Applied {
			if strings.HasPrefix(applied, "000009_") {
				found = true
			}
		}
		if !found {
			t.Errorf("applied = %v, want 000009 included", result.Applied)
		}
		pool, err := pgxpool.New(ctx, db.runtimeDSN)
		if err != nil {
			t.Fatalf("runtime pool: %v", err)
		}
		defer pool.Close()
		owner, err := auditLogOwner(ctx, pool)
		if err != nil {
			t.Fatalf("audit log owner: %v", err)
		}
		if owner != DDLRoleUser {
			t.Fatalf("audit log owner = %q, want DDL role %q after transfer", owner, DDLRoleUser)
		}
		assertRuntimeAuditPrivileges(t, ctx, pool)
		assertAuditMutationProbesFail(t, ctx, pool)
	})
}
