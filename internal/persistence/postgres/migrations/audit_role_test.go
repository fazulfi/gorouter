package migrations

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// auditRoleTestDBPrefix is the dedicated test database prefix for the audit
// immutability and job-provenance DB tests.
const auditRoleTestDBPrefix = "gorouter_audit_iso_"

// setupAuditRoleTestDB creates a fresh database and returns the runtime DSN
// for it. The schema is applied by the dedicated DDL role so the runtime
// role is never the owner of the audit log; before migration 000009 the
// fixture grants the pre-migration runtime privileges (making the negative
// probes succeed on the RED run), and when 000009 is present it revokes
// UPDATE and DELETE (making them fail on the GREEN run).
func setupAuditRoleTestDB(t *testing.T, ctx context.Context) string {
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

	// The runtime role creates the database (CREATEDB) and owns it, which
	// makes it the effective owner of the public schema; it then grants the
	// DDL role schema-create rights for the migration batch.
	admin, err := pgx.Connect(ctx, runtimeBase)
	if err != nil {
		t.Fatalf("connect as runtime role: %v", err)
	}
	dbName := auditRoleTestDBPrefix + uuid.NewString()[:8]
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
	// The schema grant must target the fresh database, so it runs on a
	// connection to it, not on the shared database the DSN points at.
	freshAdmin, err := pgx.Connect(ctx, runtimeTestDSN(t, runtimeBase, dbName))
	if err != nil {
		t.Fatalf("connect to fresh db as runtime role: %v", err)
	}
	if _, err := freshAdmin.Exec(ctx, "GRANT CREATE ON SCHEMA public TO "+DDLRoleUser); err != nil {
		freshAdmin.Close(ctx)
		t.Fatalf("grant DDL role schema create: %v", err)
	}
	freshAdmin.Close(ctx)

	// DDL-role connection: same transport as the runtime DSN with the DDL
	// role substituted. The runtime credential is retained because the
	// password-authenticated test instances require it; the production DDL
	// path (FromRuntimeDSN) intentionally drops it for peer auth.
	ddlCfg, err := pgx.ParseConfig(runtimeBase)
	if err != nil {
		t.Fatalf("parse runtime DSN for DDL role: %v", err)
	}
	ddlCfg.User = DDLRoleUser
	ddlCfg.Database = dbName
	ddlConn, err := pgx.ConnectConfig(ctx, ddlCfg)
	if err != nil {
		t.Fatalf("connect as DDL role: %v", err)
	}
	defer ddlConn.Close(ctx)

	ddlRunner := NewRunner(&ddlPool{conn: ddlConn})
	if err := ddlRunner.ensureMigrationsTable(ctx); err != nil {
		t.Fatalf("ensure migrations table: %v", err)
	}

	upMigrations, err := parseMigrations(DirectionUp)
	if err != nil {
		t.Fatalf("parse up migrations: %v", err)
	}

	// Apply every migration before 000009 as the DDL role; all tables are
	// therefore owned by the DDL role, never by the runtime role.
	for _, m := range upMigrations {
		if m.Version == "000009" {
			break
		}
		if err := ddlRunner.applyMigration(ctx, m); err != nil {
			t.Fatalf("apply %s: %v", m.Name, err)
		}
	}

	// Pre-000009 runtime state: full access to the audit log, exactly as
	// before migration 000009 revoked UPDATE and DELETE. On the RED run
	// (000009 absent) the probes below therefore succeed and the negative
	// test fails; on the GREEN run 000009 revokes the privileges and the
	// probes fail as required.
	if _, err := ddlConn.Exec(ctx,
		`GRANT SELECT, INSERT, UPDATE, DELETE ON gorouter_audit_log TO gorouter`); err != nil {
		t.Fatalf("grant pre-000009 audit privileges: %v", err)
	}

	// Apply 000009 when present.
	for _, m := range upMigrations {
		if m.Version == "000009" {
			if err := ddlRunner.applyMigration(ctx, m); err != nil {
				t.Fatalf("apply %s: %v", m.Name, err)
			}
		}
	}

	// Fixture-only grants for the human-actor and FK probes; production
	// grants are untouched.
	if _, err := ddlConn.Exec(ctx,
		`GRANT INSERT, SELECT ON gorouter_users TO gorouter`); err != nil {
		t.Fatalf("grant human-actor fixture privileges: %v", err)
	}

	return runtimeTestDSN(t, runtimeBase, dbName)
}

// runtimeTestDSN rewrites the database name in the runtime DSN to the fresh
// test database, preserving transport and credentials.
func runtimeTestDSN(t *testing.T, base, dbName string) string {
	t.Helper()
	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse runtime DSN: %v", err)
	}
	u.Path = "/" + dbName
	return u.String()
}

// TestJobAuditInsert proves, at the DB level, that an audit INSERT with
// actor_kind='job', job_id=<job-type> and actor_id IS NULL succeeds with the
// FK/CHECK/GRANT satisfied, that the inserted job row is filterable by
// actor_kind/job_id via idx_audit_log_job_provenance, and that human audit
// writes remain valid with the preserved actor_id FK.
func TestJobAuditInsert(t *testing.T) {
	ctx := context.Background()
	runtimeDSN := setupAuditRoleTestDB(t, ctx)

	pool, err := pgxpool.New(ctx, runtimeDSN)
	if err != nil {
		t.Fatalf("runtime pool: %v", err)
	}
	defer pool.Close()

	var currentUser string
	if err := pool.QueryRow(ctx, `SELECT current_user`).Scan(&currentUser); err != nil {
		t.Fatalf("current_user: %v", err)
	}
	if currentUser != "gorouter" {
		t.Fatalf("runtime role = %q, want gorouter", currentUser)
	}

	// job_id is UUID-typed per the migration schema; job types are mapped
	// to stable UUIDs at the application layer, and PostgreSQL rejects
	// non-UUID values. The value below is the deterministic identifier for
	// the token-refresh job type.
	jobID := "8f8f8f8f-8f8f-8f8f-8f8f-8f8f8f8f8f8f"

	t.Run("job audit insert with NULL actor_id succeeds", func(t *testing.T) {
		rowID := uuid.New()
		_, err := pool.Exec(ctx,
			`INSERT INTO gorouter_audit_log (id, actor_id, action, resource_type, resource_id, details, ip_address, occurred_at, job_id, actor_kind)
			 VALUES ($1, NULL, 'job.run', 'scheduler', NULL, NULL, NULL, NOW(), $2, 'job')`,
			rowID, jobID)
		if err != nil {
			t.Fatalf("job audit insert must succeed: %v", err)
		}

		var gotID uuid.UUID
		var gotActorID *uuid.UUID
		var gotKind, gotJobID string
		err = pool.QueryRow(ctx,
			`SELECT id, actor_id, actor_kind, job_id FROM gorouter_audit_log
			 WHERE actor_kind = 'job' AND job_id = $1`, jobID).
			Scan(&gotID, &gotActorID, &gotKind, &gotJobID)
		if err != nil {
			t.Fatalf("filter job row by provenance predicate: %v", err)
		}
		if gotID != rowID {
			t.Errorf("filtered id = %s, want %s", gotID, rowID)
		}
		if gotKind != "job" || gotJobID != jobID {
			t.Errorf("filtered row = kind %q job_id %q, want kind %q job_id %q", gotKind, gotJobID, "job", jobID)
		}
		if gotActorID != nil {
			t.Errorf("job row actor_id = %v, want NULL", gotActorID)
		}
	})

	t.Run("job rows are filterable via idx_audit_log_job_provenance", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer tx.Rollback(ctx)

		if _, err := tx.Exec(ctx, `SET enable_seqscan = off`); err != nil {
			t.Fatalf("disable seqscan: %v", err)
		}
		var plan []byte
		if err := tx.QueryRow(ctx,
			`EXPLAIN (FORMAT JSON) SELECT id FROM gorouter_audit_log
			 WHERE actor_kind = 'job' AND job_id = $1`, jobID).Scan(&plan); err != nil {
			t.Fatalf("explain provenance predicate: %v", err)
		}
		if !strings.Contains(string(plan), "idx_audit_log_job_provenance") {
			t.Errorf("plan does not use idx_audit_log_job_provenance:\n%s", plan)
		}
	})

	t.Run("human audit write with default actor_kind remains valid", func(t *testing.T) {
		rowID := uuid.New()
		if _, err := pool.Exec(ctx,
			`INSERT INTO gorouter_audit_log (id, action, resource_type) VALUES ($1, 'settings.set', 'settings')`,
			rowID); err != nil {
			t.Fatalf("human audit insert must succeed: %v", err)
		}
		var kind string
		var jobIDCol, actorID *string
		err := pool.QueryRow(ctx,
			`SELECT actor_kind, job_id::text, actor_id::text FROM gorouter_audit_log WHERE id = $1`, rowID).
			Scan(&kind, &jobIDCol, &actorID)
		if err != nil {
			t.Fatalf("read human row: %v", err)
		}
		if kind != "user" {
			t.Errorf("default actor_kind = %q, want user", kind)
		}
		if jobIDCol != nil || actorID != nil {
			t.Errorf("human row carries provenance columns: job_id %v actor_id %v, want NULL", jobIDCol, actorID)
		}
	})

	t.Run("human audit write with real actor keeps the actor_id FK", func(t *testing.T) {
		var actorID uuid.UUID
		if err := pool.QueryRow(ctx,
			`INSERT INTO gorouter_users (email, password_hash) VALUES ('audit-actor@example.local', 'fixture-hash') RETURNING id`).
			Scan(&actorID); err != nil {
			t.Fatalf("insert fixture user: %v", err)
		}
		rowID := uuid.New()
		if _, err := pool.Exec(ctx,
			`INSERT INTO gorouter_audit_log (id, actor_id, action, resource_type) VALUES ($1, $2, 'settings.set', 'settings')`,
			rowID, actorID); err != nil {
			t.Fatalf("human audit insert with actor_id must succeed: %v", err)
		}
		var gotKind string
		var gotActorID uuid.UUID
		err := pool.QueryRow(ctx,
			`SELECT actor_kind, actor_id FROM gorouter_audit_log WHERE id = $1`, rowID).
			Scan(&gotKind, &gotActorID)
		if err != nil {
			t.Fatalf("read human row: %v", err)
		}
		if gotKind != "user" || gotActorID != actorID {
			t.Errorf("human row = kind %q actor_id %s, want kind user actor_id %s", gotKind, gotActorID, actorID)
		}
	})

	t.Run("CHECK rejects a job row carrying a non-null actor_id", func(t *testing.T) {
		var actorID uuid.UUID
		if err := pool.QueryRow(ctx,
			`SELECT id FROM gorouter_users WHERE email = 'audit-actor@example.local'`).Scan(&actorID); err != nil {
			t.Fatalf("lookup fixture user: %v", err)
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer tx.Rollback(ctx)

		_, err = tx.Exec(ctx,
			`INSERT INTO gorouter_audit_log (actor_id, action, resource_type, job_id, actor_kind)
			 VALUES ($1, 'job.run', 'scheduler', $2, 'job')`,
			actorID, jobID)
		if err == nil {
			t.Fatal("job row with actor_id must be rejected by chk_audit_actor_kind")
		}
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) {
			t.Fatalf("expected a PostgreSQL error, got %v", err)
		}
		if pgErr.Code != "23514" || !strings.Contains(pgErr.ConstraintName, "chk_audit_actor_kind") {
			t.Errorf("error = code %s constraint %q (%v), want check violation on chk_audit_actor_kind",
				pgErr.Code, pgErr.ConstraintName, err)
		}
	})
}

// TestAuditRoleImmutability proves that the runtime role cannot UPDATE,
// DELETE, TRUNCATE or alter the audit log once migration 000009 revoked
// UPDATE and DELETE and granted INSERT and SELECT only.
func TestAuditRoleImmutability(t *testing.T) {
	ctx := context.Background()
	runtimeDSN := setupAuditRoleTestDB(t, ctx)

	pool, err := pgxpool.New(ctx, runtimeDSN)
	if err != nil {
		t.Fatalf("runtime pool: %v", err)
	}
	defer pool.Close()

	// The runtime role must never be the audit log owner: ownership is the
	// DDL role's, so the 000009 REVOKE binds and no implicit owner
	// privileges (UPDATE/DELETE/TRUNCATE/DDL) exist.
	var ownedByDDL bool
	if err := pool.QueryRow(ctx,
		`SELECT (c.relowner = r.oid) FROM pg_class c
		 JOIN pg_roles r ON r.rolname = $1
		 WHERE c.relname = 'gorouter_audit_log' AND c.relkind = 'r'`, DDLRoleUser).Scan(&ownedByDDL); err != nil {
		t.Fatalf("audit log ownership: %v", err)
	}
	if !ownedByDDL {
		t.Fatalf("audit log must be owned by DDL role %q, not the runtime role", DDLRoleUser)
	}

	// Seed one row through the granted INSERT path so the mutation probes
	// target existing data. The seed uses only baseline columns so it is
	// valid on both the pre-000009 (RED) and post-000009 (GREEN) schemas.
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
		{"DDL", `ALTER TABLE gorouter_audit_log ADD COLUMN _be09_probe TEXT`},
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
