package migrations

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
)

// bootstrapRolesOnCluster provisions the externally-managed gorouter role
// topology on the cluster before any fixture grants or migration batches
// reference the roles. It is the test-side equivalent of the deploy.sh
// bootstrap step (Rev3 architecture C: externally provisioned LOGIN
// membership topology, no SUPERUSER): gorouter is the runtime role,
// gorouter_ddl is the migration executor whose membership in gorouter
// enables ownership transfer without superuser, and gorouter_ddl_nomember
// is the negative control that proves the fail-closed path.
//
// N1 password alignment: the tests connect AS the DDL roles while retaining
// the runtime credential (pgx.ParseConfig(runtimeBase) with only User
// swapped), so every bootstrapped role must hold the runtime password for
// password-authenticated (scram/md5) servers. The password is derived from
// runtimeDSN; when it carries none (peer/trust), roles get no password.
//
// Idempotency: CREATE ROLE runs inside DO-block IF NOT EXISTS guards so the
// helper is safe on shared clusters across tests and re-runs. When the roles
// already exist, the bootstrap reconciles attributes instead of skipping
// silently: it enforces LOGIN (the DDL connection fails closed with SQLSTATE
// 28000 on NOLOGIN), aligns passwords for scram, and grants the gorouter
// membership to gorouter_ddl. Attribute conditions the helper cannot repair
// (e.g. an existing SUPERUSER gorouter) fail closed with an actionable error.
func bootstrapRolesOnCluster(t interface {
	Helper()
	Fatalf(string, ...any)
}, ctx context.Context, runtimeDSN string) error {
	t.Helper()
	admin, err := pgx.Connect(ctx, runtimeDSN)
	if err != nil {
		return fmt.Errorf("bootstrap roles: connect as admin role: %w", err)
	}
	defer admin.Close(ctx)

	password := extractPasswordFromDSN(runtimeDSN)

	if err := failClosedOnUnsupportedAttributes(ctx, admin); err != nil {
		return err
	}

	for _, role := range []string{"gorouter", DDLRoleUser, nomemberDDLRole} {
		var exists bool
		if err := admin.QueryRow(ctx,
			`SELECT EXISTS (SELECT FROM pg_roles WHERE rolname = $1)`, role).Scan(&exists); err != nil {
			return fmt.Errorf("bootstrap roles: inspect %s: %w", role, err)
		}
		if !exists {
			stmt := "CREATE ROLE " + pgx.Identifier{role}.Sanitize() + " LOGIN"
			if password != "" {
				stmt += " PASSWORD " + quoteSQLLiteral(password)
			}
			if _, err := admin.Exec(ctx, stmt); err != nil {
				return fmt.Errorf("bootstrap roles: create %s: %w", role, err)
			}
			continue
		}
		var canLogin bool
		if err := admin.QueryRow(ctx,
			`SELECT rolcanlogin FROM pg_roles WHERE rolname = $1`, role).Scan(&canLogin); err != nil {
			return fmt.Errorf("bootstrap roles: inspect %s attributes: %w", role, err)
		}
		if !canLogin {
			if _, err := admin.Exec(ctx,
				"ALTER ROLE "+pgx.Identifier{role}.Sanitize()+" LOGIN"); err != nil {
				return fmt.Errorf("bootstrap roles: repair LOGIN on %s: %w", role, err)
			}
		}
		if password != "" {
			if _, err := admin.Exec(ctx,
				"ALTER ROLE "+pgx.Identifier{role}.Sanitize()+" PASSWORD "+quoteSQLLiteral(password)); err != nil {
				return fmt.Errorf("bootstrap roles: align password on %s: %w", role, err)
			}
		}
	}

	if _, err := admin.Exec(ctx,
		"GRANT "+pgx.Identifier{"gorouter"}.Sanitize()+" TO "+pgx.Identifier{DDLRoleUser}.Sanitize()); err != nil {
		return fmt.Errorf("bootstrap roles: grant gorouter membership to %s: %w", DDLRoleUser, err)
	}
	return nil
}

// failClosedOnUnsupportedAttributes aborts the bootstrap before any role
// reconciliation when the cluster carries a role attribute the membership
// topology cannot support. Fail-closed beats silently continuing with an
// unsafe privilege posture (Rev3 §3.4.2).
func failClosedOnUnsupportedAttributes(ctx context.Context, admin *pgx.Conn) error {
	for _, probe := range []struct {
		role      string
		condition string
		action    string
	}{
		{"gorouter", "rolsuper", "REVOKE superuser from gorouter or bootstrap on a cluster where gorouter is not superuser"},
		{DDLRoleUser, "rolsuper", "REVOKE superuser from " + DDLRoleUser + " or bootstrap on a cluster where " + DDLRoleUser + " is not superuser"},
	} {
		var unsupported bool
		err := admin.QueryRow(ctx,
			`SELECT `+probe.condition+` FROM pg_roles WHERE rolname = $1`, probe.role).Scan(&unsupported)
		if err != nil {
			continue // role absent; CREATE ROLE path handles it without superuser
		}
		if unsupported {
			return fmt.Errorf("bootstrap roles: fail closed: role %s has unsupported attribute %s; action: %s",
				probe.role, probe.condition, probe.action)
		}
	}
	return nil
}

// extractPasswordFromDSN extracts the password embedded in a PostgreSQL DSN;
// it returns "" when the DSN carries no credential (peer/trust transports).
func extractPasswordFromDSN(dsn string) string {
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

// quoteSQLLiteral renders a single-quoted SQL string literal with embedded
// single quotes doubled (standard_conforming_strings escaping).
func quoteSQLLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
