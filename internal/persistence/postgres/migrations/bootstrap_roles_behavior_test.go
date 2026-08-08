package migrations

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestExtractPasswordFromDSN(t *testing.T) {
	cases := []struct {
		name string
		dsn  string
		want string
	}{
		{name: "standard URL with password", dsn: "postgres://postgres:postgres@localhost:5432/gorouter_test?sslmode=disable", want: "postgres"},
		{name: "URL with percent-encoded password", dsn: "postgres://gorouter:p%40ss%23word@localhost:5432/gorouter_test", want: "p@ss#word"},
		{name: "URL with empty password", dsn: "postgres://gorouter:@localhost:5432/gorouter_test", want: ""},
		{name: "URL with user but no password field", dsn: "postgres://gorouter@localhost:5432/gorouter_test", want: ""},
		{name: "URL with no userinfo (peer/trust transport)", dsn: "postgres://localhost:5432/gorouter_test", want: ""},
		{name: "empty string", dsn: "", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractPasswordFromDSN(tc.dsn)
			if got != tc.want {
				t.Errorf("extractPasswordFromDSN(%q) = %q, want %q", tc.dsn, got, tc.want)
			}
		})
	}
}

func TestQuoteSQLLiteral(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty string", input: "", want: "''"},
		{name: "plain password", input: "gorouter", want: "'gorouter'"},
		{name: "single embedded quote", input: "it's", want: "'it''s'"},
		{name: "multiple quotes", input: "a'b'c", want: "'a''b''c'"},
		{name: "backslash preserved", input: `a\b`, want: `'a\b'`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := quoteSQLLiteral(tc.input)
			if got != tc.want {
				t.Errorf("quoteSQLLiteral(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func skipIfNoPG(t *testing.T) {
	t.Helper()
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL not set — PostgreSQL-dependent test deferred to CI")
	}
	if testing.Short() {
		t.Skip("skipping PostgreSQL-dependent test in short mode")
	}
}

type fakeBootstrapT struct {
	failed bool
}

func (f *fakeBootstrapT) Helper()                             {}
func (f *fakeBootstrapT) Fatalf(format string, args ...any)   { f.failed = true }

func TestFailClosedOnUnsupportedAttributes_FreshCluster(t *testing.T) {
	skipIfNoPG(t)
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatalf("connect as admin: %v", err)
	}
	defer admin.Close(ctx)

	if err := failClosedOnUnsupportedAttributes(ctx, admin); err != nil {
		t.Errorf("failClosedOnUnsupportedAttributes on fresh cluster: unexpected error: %v", err)
	}
}

func TestBootstrapRolesOnCluster_TopologyAndIdempotency(t *testing.T) {
	skipIfNoPG(t)
	ctx := context.Background()
	runtimeDSN := os.Getenv("DATABASE_URL")

	ft := &fakeBootstrapT{}
	if err := bootstrapRolesOnCluster(ft, ctx, runtimeDSN); err != nil {
		t.Fatalf("bootstrapRolesOnCluster: %v", err)
	}
	if ft.failed {
		t.Fatal("bootstrapRolesOnCluster called Fatalf unexpectedly")
	}

	ft2 := &fakeBootstrapT{}
	if err := bootstrapRolesOnCluster(ft2, ctx, runtimeDSN); err != nil {
		t.Fatalf("bootstrapRolesOnCluster (idempotent re-run): %v", err)
	}
	if ft2.failed {
		t.Fatal("bootstrapRolesOnCluster (idempotent re-run) called Fatalf unexpectedly")
	}

	admin, err := pgx.Connect(ctx, runtimeDSN)
	if err != nil {
		t.Fatalf("connect for verification: %v", err)
	}
	defer admin.Close(ctx)

	for _, role := range []string{"gorouter", DDLRoleUser, nomemberDDLRole} {
		var canLogin, isSuper bool
		if err := admin.QueryRow(ctx,
			`SELECT rolcanlogin, rolsuper FROM pg_roles WHERE rolname = $1`, role).
			Scan(&canLogin, &isSuper); err != nil {
			t.Fatalf("query role %s: %v", role, err)
		}
		if !canLogin {
			t.Errorf("role %s: expected LOGIN=true, got false", role)
		}
		if isSuper {
			t.Errorf("role %s: expected SUPERUSER=false (Rev3 no-SUPERUSER), got true", role)
		}
	}

	var ddlIsMember bool
	if err := admin.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM pg_auth_members m
			JOIN pg_roles r ON r.oid = m.roleid
			JOIN pg_roles mem ON mem.oid = m.member
			WHERE r.rolname = 'gorouter' AND mem.rolname = $1
		)`, DDLRoleUser).Scan(&ddlIsMember); err != nil {
		t.Fatalf("query membership: %v", err)
	}
	if !ddlIsMember {
		t.Errorf("expected %s to be a member of gorouter (Rev3 membership), got false", DDLRoleUser)
	}

	var nomemberIsMember bool
	if err := admin.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT FROM pg_auth_members m
			JOIN pg_roles r ON r.oid = m.roleid
			JOIN pg_roles mem ON mem.oid = m.member
			WHERE r.rolname = 'gorouter' AND mem.rolname = $1
		)`, nomemberDDLRole).Scan(&nomemberIsMember); err != nil {
		t.Fatalf("query nomember membership: %v", err)
	}
	if nomemberIsMember {
		t.Errorf("expected %s to NOT be a member of gorouter (negative control), got true", nomemberDDLRole)
	}
}

func TestBootstrapRolesOnCluster_FailClosedOnSuperuser(t *testing.T) {
	skipIfNoPG(t)
	ctx := context.Background()
	runtimeDSN := os.Getenv("DATABASE_URL")

	admin, err := pgx.Connect(ctx, runtimeDSN)
	if err != nil {
		t.Fatalf("connect as admin: %v", err)
	}
	defer admin.Close(ctx)

	if _, err := admin.Exec(ctx,
		`DO $$ BEGIN
			IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'gorouter') THEN
				CREATE ROLE gorouter LOGIN SUPERUSER;
			ELSE
				ALTER ROLE gorouter SUPERUSER;
			END IF;
		END $$`); err != nil {
		t.Fatalf("setup superuser gorouter: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = admin.Exec(cleanupCtx, "ALTER ROLE gorouter NOSUPERUSER")
	})

	ft := &fakeBootstrapT{}
	err = bootstrapRolesOnCluster(ft, ctx, runtimeDSN)
	if err == nil {
		t.Fatal("bootstrapRolesOnCluster: expected fail-closed error when gorouter is SUPERUSER, got nil")
	}
	if !strings.Contains(err.Error(), "fail closed") {
		t.Errorf("bootstrapRolesOnCluster: expected fail-closed error message, got: %v", err)
	}
}
