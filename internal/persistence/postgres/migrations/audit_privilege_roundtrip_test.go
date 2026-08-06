package migrations

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// auditPrivilegeTestDBPrefix is the dedicated test database prefix for the
// audit-log privilege round-trip tests.
const auditPrivilegeTestDBPrefix = "audit_priv_iso_"

// auditPrivileges is a privilege snapshot of gorouter_audit_log for the
// runtime role and for PUBLIC, keyed by privilege name.
type auditPrivileges map[string]map[string]bool

// setupPrivilegeTestDB creates a fresh database whose schema 000001-000008
// is applied by the dedicated DDL role, so the runtime role never owns the
// audit log and the migration's GRANT/REVOKE statements bind. The runtime
// role is granted no audit privileges by default (the true pre-000009
// baseline, since no GRANT exists in 000001-000008); tests that model the
// pre-migration runtime access grant it explicitly.
func setupPrivilegeTestDB(t *testing.T, ctx context.Context) (runtimeDSN string, ddlConn *pgx.Conn) {
	t.Helper()
	runtimeBase := os.Getenv("DATABASE_URL")
	if runtimeBase == "" {
		t.Skip("DATABASE_URL not set")
	}
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	admin, err := pgx.Connect(ctx, runtimeBase)
	if err != nil {
		t.Fatalf("connect as runtime role: %v", err)
	}
	dbName := auditPrivilegeTestDBPrefix + uuid.NewString()[:8]
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

	freshAdmin, err := pgx.Connect(ctx, runtimeTestDSN(t, runtimeBase, dbName))
	if err != nil {
		t.Fatalf("connect to fresh db as runtime role: %v", err)
	}
	if _, err := freshAdmin.Exec(ctx, "GRANT CREATE ON SCHEMA public TO "+DDLRoleUser); err != nil {
		freshAdmin.Close(ctx)
		t.Fatalf("grant DDL role schema create: %v", err)
	}
	freshAdmin.Close(ctx)

	ddlCfg, err := pgx.ParseConfig(runtimeBase)
	if err != nil {
		t.Fatalf("parse runtime DSN for DDL role: %v", err)
	}
	ddlCfg.User = DDLRoleUser
	ddlCfg.Database = dbName
	conn, err := pgx.ConnectConfig(ctx, ddlCfg)
	if err != nil {
		t.Fatalf("connect as DDL role: %v", err)
	}
	t.Cleanup(func() { conn.Close(context.Background()) })

	ddlRunner := NewRunner(&ddlPool{conn: conn})
	if err := ddlRunner.ensureMigrationsTable(ctx); err != nil {
		t.Fatalf("ensure migrations table: %v", err)
	}
	upMigrations, err := parseMigrations(DirectionUp)
	if err != nil {
		t.Fatalf("parse up migrations: %v", err)
	}
	for _, m := range upMigrations {
		if m.Version == "000009" {
			break
		}
		if err := ddlRunner.applyMigration(ctx, m); err != nil {
			t.Fatalf("apply %s: %v", m.Name, err)
		}
	}
	return runtimeTestDSN(t, runtimeBase, dbName), conn
}

// snapshotAuditPrivileges reads the effective privileges of the runtime role
// and of PUBLIC on gorouter_audit_log via has_table_privilege.
func snapshotAuditPrivileges(ctx context.Context, pool *pgxpool.Pool) auditPrivileges {
	snap := auditPrivileges{}
	for _, role := range []string{"gorouter", "public"} {
		privs := make(map[string]bool)
		for _, priv := range []string{"SELECT", "INSERT", "UPDATE", "DELETE", "TRUNCATE"} {
			var held bool
			if err := pool.QueryRow(ctx,
				`SELECT has_table_privilege($1, 'gorouter_audit_log', $2)`, role, priv).Scan(&held); err != nil {
				return nil
			}
			privs[priv] = held
		}
		snap[role] = privs
	}
	return snap
}

// assertAuditPrivileges checks that the role holds exactly the wanted
// privileges on gorouter_audit_log.
func assertAuditPrivileges(t *testing.T, snap auditPrivileges, role string, want ...string) {
	t.Helper()
	wantSet := make(map[string]bool)
	for _, p := range want {
		wantSet[p] = true
	}
	for _, priv := range []string{"SELECT", "INSERT", "UPDATE", "DELETE", "TRUNCATE"} {
		got := snap[role][priv]
		if got != wantSet[priv] {
			t.Errorf("role %s %s on gorouter_audit_log = %v, want %v", role, priv, got, wantSet[priv])
		}
	}
}

// apply009Up applies migration 000009 up on the DDL connection; apply009Down
// applies the down and removes the tracking record.
func apply009Up(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	upMigrations, err := parseMigrations(DirectionUp)
	if err != nil {
		t.Fatalf("parse up migrations: %v", err)
	}
	for _, m := range upMigrations {
		if m.Version == "000009" {
			if err := NewRunner(&ddlPool{conn: conn}).applyMigration(ctx, m); err != nil {
				t.Fatalf("apply 000009: %v", err)
			}
			return
		}
	}
	t.Fatal("migration 000009 not found in embedded files")
}

func apply009Down(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	downMigrations, err := parseMigrations(DirectionDown)
	if err != nil {
		t.Fatalf("parse down migrations: %v", err)
	}
	runner := NewRunner(&ddlPool{conn: conn})
	for _, m := range downMigrations {
		if m.Version == "000009" {
			if err := runner.applyMigration(ctx, m); err != nil {
				t.Fatalf("apply 000009 down: %v", err)
			}
			if err := runner.removeMigrationRecord(ctx, "000009"); err != nil {
				t.Fatalf("remove 000009 record: %v", err)
			}
			return
		}
	}
	t.Fatal("migration 000009 down not found in embedded files")
}

// TestAuditPrivilegeRoundTrip proves the 000009 down migration reconstructs
// the actual pre-000009 privilege state on a DDL-role-owned schema: the
// runtime role returns to exactly the privileges it held before the up, and
// PUBLIC never holds UPDATE or DELETE at any stage.
func TestAuditPrivilegeRoundTrip(t *testing.T) {
	ctx := context.Background()
	runtimeDSN, ddlConn := setupPrivilegeTestDB(t, ctx)

	pool, err := pgxpool.New(ctx, runtimeDSN)
	if err != nil {
		t.Fatalf("runtime pool: %v", err)
	}
	defer pool.Close()

	pre := snapshotAuditPrivileges(ctx, pool)
	if pre == nil {
		t.Fatal("snapshot pre-000009 privileges")
	}
	assertAuditPrivileges(t, pre, "gorouter")
	assertAuditPrivileges(t, pre, "public")

	apply009Up(t, ctx, ddlConn)
	up := snapshotAuditPrivileges(ctx, pool)
	if up == nil {
		t.Fatal("snapshot post-up privileges")
	}
	assertAuditPrivileges(t, up, "gorouter", "SELECT", "INSERT")
	assertAuditPrivileges(t, up, "public")

	apply009Down(t, ctx, ddlConn)
	post := snapshotAuditPrivileges(ctx, pool)
	if post == nil {
		t.Fatal("snapshot post-down privileges")
	}
	for _, role := range []string{"gorouter", "public"} {
		for _, priv := range []string{"SELECT", "INSERT", "UPDATE", "DELETE", "TRUNCATE"} {
			if post[role][priv] != pre[role][priv] {
				t.Errorf("post-down %s %s = %v, want pre-state %v (down must reconstruct the actual pre-000009 privileges)",
					role, priv, post[role][priv], pre[role][priv])
			}
		}
	}
	assertAuditPrivileges(t, post, "public", "")
}

// TestAuditPrivilegeRoundTripPreGranted proves the down never broadens the
// privilege state beyond what the runtime role held before 000009: on a
// schema where the runtime role held full audit access before the up, the
// rollback leaves it no broader (and PUBLIC exactly as before).
func TestAuditPrivilegeRoundTripPreGranted(t *testing.T) {
	ctx := context.Background()
	runtimeDSN, ddlConn := setupPrivilegeTestDB(t, ctx)

	if _, err := ddlConn.Exec(ctx,
		`GRANT SELECT, INSERT, UPDATE, DELETE ON gorouter_audit_log TO gorouter`); err != nil {
		t.Fatalf("grant pre-000009 audit privileges: %v", err)
	}

	pool, err := pgxpool.New(ctx, runtimeDSN)
	if err != nil {
		t.Fatalf("runtime pool: %v", err)
	}
	defer pool.Close()

	pre := snapshotAuditPrivileges(ctx, pool)
	if pre == nil {
		t.Fatal("snapshot pre-000009 privileges")
	}
	assertAuditPrivileges(t, pre, "gorouter", "SELECT", "INSERT", "UPDATE", "DELETE")
	assertAuditPrivileges(t, pre, "public")

	apply009Up(t, ctx, ddlConn)
	up := snapshotAuditPrivileges(ctx, pool)
	if up == nil {
		t.Fatal("snapshot post-up privileges")
	}
	assertAuditPrivileges(t, up, "gorouter", "SELECT", "INSERT")
	assertAuditPrivileges(t, up, "public")

	apply009Down(t, ctx, ddlConn)
	post := snapshotAuditPrivileges(ctx, pool)
	if post == nil {
		t.Fatal("snapshot post-down privileges")
	}
	for _, role := range []string{"gorouter", "public"} {
		for _, priv := range []string{"SELECT", "INSERT", "UPDATE", "DELETE", "TRUNCATE"} {
			if post[role][priv] && !pre[role][priv] {
				t.Errorf("post-down %s %s = %v, never held pre-000009 (down must not broaden the pre-state)",
					role, priv, post[role][priv])
			}
		}
	}
	assertAuditPrivileges(t, post, "public", "")
}
