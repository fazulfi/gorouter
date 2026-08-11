package migrations

import (
	"context"
	"crypto/tls"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// DDLRoleUser is the dedicated PostgreSQL role that owns schema changes.
// Migration batches connect as this role; the runtime role is never granted
// DDL responsibility.
const DDLRoleUser = "gorouter_ddl"

// DDLConfig selects the dedicated DDL-role connection used for migration
// batches.  It supports an optional password field for TCP+SCRAM
// authentication on managed PostgreSQL; the connection authenticates via
// Unix socket peer auth (Password="") or TCP+password (Password sourced
// from secret store) for the DDL role, so the DDL connection never
// transports the runtime credential.
type DDLConfig struct {
	Host          string
	Port          uint16
	Database      string
	User          string
	Password      string
	RuntimeParams map[string]string
	TLSConfig     *tls.Config
	Fallbacks     []*pgconn.FallbackConfig
}

// FromRuntimeDSN derives the DDL connection config from the runtime DSN,
// substituting the DDL role for the runtime role and dropping the runtime
// credential.  Host, port, database, TLS and runtime parameters are
// preserved so a socket peer-auth or TCP deployment keeps its transport
// settings; only the identity changes.
func FromRuntimeDSN(runtimeDSN string) (DDLConfig, error) {
	parsed, err := pgconn.ParseConfig(runtimeDSN)
	if err != nil {
		return DDLConfig{}, fmt.Errorf("parse runtime DSN: %w", err)
	}
	params := make(map[string]string, len(parsed.RuntimeParams))
	for k, v := range parsed.RuntimeParams {
		params[k] = v
	}
	return DDLConfig{
		Host:          parsed.Host,
		Port:          parsed.Port,
		Database:      parsed.Database,
		User:          DDLRoleUser,
		RuntimeParams: params,
		TLSConfig:     parsed.TLSConfig,
		Fallbacks:     parsed.Fallbacks,
	}, nil
}

// ddlConn is the minimal dedicated-connection surface used to run migration
// batches.  *pgx.Conn satisfies it; tests substitute a mock connection.
type ddlConn interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Begin(ctx context.Context) (pgx.Tx, error)
	Close(ctx context.Context) error
}

// ddlConnConfig derives the pgx connection config for the dedicated
// DDL-role connection from the DDLConfig transport settings. pgx v5
// requires every ConnConfig to be created by pgx.ParseConfig or it panics
// with "config must be created by ParseConfig". Parse an empty DSN to
// obtain a parse-created config with defaults, then apply the DDL transport
// settings (Host/Port/Database/User/Password/TLS/Fallbacks/RuntimeParams)
// field-for-field so DSN semantics are preserved.
func ddlConnConfig(cfg DDLConfig) *pgx.ConnConfig {
	parsed, err := pgx.ParseConfig("")
	if err != nil {
		// An empty DSN always parses to defaults and cannot fail; this is
		// purely defensive. Fail closed rather than hand-build a config
		// that pgx would reject at runtime.
		panic(fmt.Sprintf("ddlConnConfig: parse empty DSN: %v (bug)", err))
	}
	parsed.Host = cfg.Host
	parsed.Port = cfg.Port
	parsed.Database = cfg.Database
	parsed.User = cfg.User
	parsed.Password = cfg.Password
	parsed.TLSConfig = cfg.TLSConfig
	parsed.Fallbacks = cfg.Fallbacks
	parsed.RuntimeParams = cfg.RuntimeParams
	return parsed
}

// openDDL opens the dedicated DDL-role connection for migration batches.
// It is a package-level variable so tests can inject a mock and assert the
// batch executes on the dedicated connection, never the runtime pool.
var openDDL = func(ctx context.Context, cfg DDLConfig) (ddlConn, error) {
	conn, err := pgx.ConnectConfig(ctx, ddlConnConfig(cfg))
	if err != nil {
		return nil, fmt.Errorf("open DDL-role connection: %w", err)
	}
	return conn, nil
}

// ddlPool adapts the dedicated DDL connection to the Runner Pool interface
// so migration batches execute over the DDL connection.
type ddlPool struct {
	conn ddlConn
}

func (p *ddlPool) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return p.conn.Exec(ctx, sql, args...)
}
func (p *ddlPool) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return p.conn.Query(ctx, sql, args...)
}
func (p *ddlPool) Begin(ctx context.Context) (pgx.Tx, error) { return p.conn.Begin(ctx) }
func (p *ddlPool) Close()                                    { _ = p.conn.Close(context.Background()) }

// ensureAuditLogOwnership enforces the audit-immutability ownership
// precondition before a migration batch runs. The audit log must be owned by
// the DDL role: a runtime-role-owned audit log makes migration 000009's
// REVOKE UPDATE, DELETE a silent no-op (the owner's implicit privileges
// cannot be stripped), so immutability would not hold after the upgrade.
// When the audit log exists and is owned by another role, the batch attempts
// the automatic ownership transfer first; that succeeds when the DDL role is
// a superuser or a member of the current owner role (the production DDL role
// is provisioned superuser-by-construction). If the transfer is refused, the
// batch fails closed with an actionable, secret-free transfer instruction
// instead of continuing with false immutability. A missing audit log (fresh
// install) needs no action.
func ensureAuditLogOwnership(ctx context.Context, conn ddlConn, ddlRole string) error {
	rows, err := conn.Query(ctx,
		`SELECT r.rolname FROM pg_class c
		 JOIN pg_roles r ON r.oid = c.relowner
		 WHERE c.relname = 'gorouter_audit_log' AND c.relkind = 'r'`)
	if err != nil {
		return fmt.Errorf("audit immutability precondition: inspect gorouter_audit_log ownership: %w", err)
	}
	hasOwner := rows.Next()
	var owner string
	if hasOwner {
		if err := rows.Scan(&owner); err != nil {
			rows.Close()
			return fmt.Errorf("audit immutability precondition: scan gorouter_audit_log owner: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("audit immutability precondition: inspect gorouter_audit_log ownership: %w", err)
	}
	rows.Close()

	if !hasOwner || owner == ddlRole {
		return nil
	}

	if _, err := conn.Exec(ctx,
		"ALTER TABLE gorouter_audit_log OWNER TO "+pgx.Identifier{ddlRole}.Sanitize()); err != nil {
		return fmt.Errorf(
			"audit immutability precondition: table gorouter_audit_log is owned by role %q, not the DDL role %q, "+
				"and the automatic ownership transfer was refused (insufficient privilege). Transfer ownership before "+
				"migrating, then re-run: ALTER TABLE gorouter_audit_log OWNER TO %s; (run as a superuser or as a member "+
				"of the current owner role; the DDL role must have CREATE on the table schema): %w",
			owner, ddlRole, ddlRole, err)
	}
	return nil
}

// RunMigrations opens a dedicated connection with the DDL role, enforces the
// audit-immutability ownership precondition, applies all pending up
// migrations on it, and closes it.  The runtime pool is never used for DDL;
// role selection is explicit in cfg.User.
func RunMigrations(ctx context.Context, cfg DDLConfig) (*Result, error) {
	if cfg.User == "" {
		return nil, fmt.Errorf("DDL-role migration batch: no DDL role configured")
	}
	conn, err := openDDL(ctx, cfg)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = conn.Close(context.Background())
	}()

	if err := ensureAuditLogOwnership(ctx, conn, cfg.User); err != nil {
		return nil, err
	}

	result, err := NewRunner(&ddlPool{conn: conn}).Migrate(ctx, DirectionUp)
	if err != nil {
		return nil, fmt.Errorf("DDL-role migration batch: %w", err)
	}
	return result, nil
}
