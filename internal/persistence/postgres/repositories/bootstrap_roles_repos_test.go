package repositories

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gorouter/internal/persistence/postgres/migrations"
)

// repoTestDSNFromBase derives an isolated test database DSN from the admin
// base DSN by substituting the database name.
func repoTestDSNFromBase(t *testing.T, adminDSN, dbName string) string {
	t.Helper()
	slash := strings.LastIndex(adminDSN, "/")
	if slash == -1 {
		t.Fatalf("admin DSN missing /dbname: %q", redactDSN(adminDSN))
	}
	return adminDSN[:slash+1] + dbName + "?sslmode=disable"
}

// bootstrapRolesForRepoTest provisions the gorouter role topology on the test
// cluster. This is a test-only helper that mirrors the U1 bootstrap logic but
// is callable from the repositories package without exposing production API.
// The duplication is necessary because the U1 helper (bootstrapRolesOnCluster)
// is unexported and package-private to migrations; exact semantic reuse would
// require exporting it, which M1/M2 prohibits. This test-only implementation
// ensures no production bootstrap logic exists in the codebase.
func bootstrapRolesForRepoTest(t *testing.T, ctx context.Context, adminDSN string) {
	t.Helper()
	admin, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		t.Fatalf("bootstrap roles: connect as admin: %v", err)
	}
	defer admin.Close(ctx)

	password := extractPasswordFromDSNRepoTest(adminDSN)

	for _, probe := range []struct{ role, condition string }{
		{"gorouter", "rolsuper"},
		{"gorouter_ddl", "rolsuper"},
	} {
		var unsupported bool
		if err := admin.QueryRow(ctx,
			`SELECT `+probe.condition+` FROM pg_roles WHERE rolname = $1`, probe.role).Scan(&unsupported); err != nil {
			continue
		}
		if unsupported {
			t.Fatalf("bootstrap roles: fail closed: role %s has %s", probe.role, probe.condition)
		}
	}

	for _, role := range []string{"gorouter", "gorouter_ddl", "gorouter_ddl_nomember"} {
		var exists bool
		if err := admin.QueryRow(ctx,
			`SELECT EXISTS (SELECT FROM pg_roles WHERE rolname = $1)`, role).Scan(&exists); err != nil {
			t.Fatalf("bootstrap roles: inspect %s: %v", role, err)
		}
		if !exists {
			stmt := "CREATE ROLE " + pgx.Identifier{role}.Sanitize() + " LOGIN"
			if password != "" {
				stmt += " PASSWORD " + quoteSQLLiteralRepoTest(password)
			}
			if _, err := admin.Exec(ctx, stmt); err != nil {
				t.Fatalf("bootstrap roles: create %s: %v", role, err)
			}
			continue
		}
		var canLogin bool
		if err := admin.QueryRow(ctx,
			`SELECT rolcanlogin FROM pg_roles WHERE rolname = $1`, role).Scan(&canLogin); err != nil {
			t.Fatalf("bootstrap roles: inspect %s attributes: %v", role, err)
		}
		if !canLogin {
			if _, err := admin.Exec(ctx, "ALTER ROLE "+pgx.Identifier{role}.Sanitize()+" LOGIN"); err != nil {
				t.Fatalf("bootstrap roles: repair LOGIN on %s: %v", role, err)
			}
		}
		if password != "" {
			if _, err := admin.Exec(ctx,
				"ALTER ROLE "+pgx.Identifier{role}.Sanitize()+" PASSWORD "+quoteSQLLiteralRepoTest(password)); err != nil {
				t.Fatalf("bootstrap roles: align password on %s: %v", role, err)
			}
		}
	}

	if _, err := admin.Exec(ctx,
		"GRANT gorouter TO gorouter_ddl"); err != nil {
		t.Fatalf("bootstrap roles: grant gorouter membership to gorouter_ddl: %v", err)
	}
}

// extractPasswordFromDSNRepoTest extracts the password embedded in a DSN.
func extractPasswordFromDSNRepoTest(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return ""
	}
	if u.User == nil {
		return ""
	}
	password, _ := u.User.Password()
	return password
}

// quoteSQLLiteralRepoTest renders a single-quoted SQL string literal.
func quoteSQLLiteralRepoTest(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// grantSchemaCreateToDDLRepoTest grants CREATE ON SCHEMA public to gorouter_ddl
// in the given database. This is the per-database grant that the migration
// executor needs to create the tracking table and apply migrations on PG 15+.
func grantSchemaCreateToDDLRepoTest(t *testing.T, ctx context.Context, dbDSN string) {
	t.Helper()
	conn, err := pgx.Connect(ctx, dbDSN)
	if err != nil {
		t.Fatalf("grant schema create: connect: %v", err)
	}
	defer conn.Close(ctx)

	if _, err := conn.Exec(ctx, "GRANT CREATE ON SCHEMA public TO gorouter_ddl"); err != nil {
		t.Fatalf("grant CREATE ON SCHEMA public to gorouter_ddl: %v", err)
	}
}

// migrateAsDDLRepoTest runs migrations using the DDL role (gorouter_ddl),
// ensuring separation of duty: DDL operations never run as the runtime role.
// After migrations complete, the runtime pool is opened as gorouter.
func migrateAsDDLRepoTest(t *testing.T, ctx context.Context, dsn string) *pgxpool.Pool {
	t.Helper()

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse DSN for DDL migration: %v", err)
	}
	cfg.ConnConfig.User = "gorouter_ddl"
	ddlPool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("create DDL pool: %v", err)
	}
	defer ddlPool.Close()

	if _, err := migrations.Migrate(ctx, ddlPool, migrations.DirectionUp); err != nil {
		t.Fatalf("migrate as DDL role: %v", err)
	}

	grantRuntimeCRUDRepoTest(t, ctx, ddlPool)

	cfg, err = pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse DSN for runtime pool: %v", err)
	}
	cfg.ConnConfig.User = "gorouter"
	runtimePool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("create runtime pool: %v", err)
	}
	return runtimePool
}

// grantRuntimeCRUDRepoTest grants the runtime role (gorouter) the privileges it
// needs to exercise the repositories against tables owned by the DDL role. Rev3
// applies every migration as gorouter_ddl, so all application tables are
// DDL-owned and the runtime role receives explicit grants. The audit log keeps
// its migration-000009 posture (INSERT+SELECT only) and is excluded from the
// CRUD grant so the immutability invariant stays intact. The migration tracking
// table is granted read-only because the backup bootstrap gate reads it as the
// runtime role. This is a test fixture grant mirroring the production runtime
// privilege bootstrap.
func grantRuntimeCRUDRepoTest(t *testing.T, ctx context.Context, ddlPool *pgxpool.Pool) {
	t.Helper()
	doStmt := `DO $$
DECLARE r RECORD;
BEGIN
  FOR r IN SELECT tablename FROM pg_tables
           WHERE schemaname = 'public'
             AND tablename NOT IN ('gorouter_audit_log', 'gorouter_migrations')
  LOOP
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON public.%I TO gorouter', r.tablename);
  END LOOP;
END $$`
	if _, err := ddlPool.Exec(ctx, doStmt); err != nil {
		t.Fatalf("grant runtime CRUD on business tables: %v", err)
	}
	if _, err := ddlPool.Exec(ctx, "GRANT SELECT ON gorouter_migrations TO gorouter"); err != nil {
		t.Fatalf("grant runtime read on migrations table: %v", err)
	}
	if _, err := ddlPool.Exec(ctx, "GRANT CREATE ON SCHEMA public TO gorouter"); err != nil {
		t.Fatalf("grant schema create for backup bootstrap gate: %v", err)
	}
	if _, err := ddlPool.Exec(ctx, "GRANT USAGE ON ALL SEQUENCES IN SCHEMA public TO gorouter"); err != nil {
		t.Fatalf("grant runtime sequence usage: %v", err)
	}
}

// redactDSN removes the password from a DSN for safe logging.
func redactDSN(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return "<unparseable-dsn>"
	}
	if u.User != nil {
		u.User = url.UserPassword(u.User.Username(), "REDACTED")
	}
	return u.String()
}
