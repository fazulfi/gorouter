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
// batches.  It deliberately has no password field: the connection
// authenticates via Unix socket peer auth for the DDL role by default, so
// the DDL connection never transports the runtime credential.
type DDLConfig struct {
	Host          string
	Port          uint16
	Database      string
	User          string
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

// openDDL opens the dedicated DDL-role connection for migration batches.
// It is a package-level variable so tests can inject a mock and assert the
// batch executes on the dedicated connection, never the runtime pool.
var openDDL = func(ctx context.Context, cfg DDLConfig) (ddlConn, error) {
	connCfg := &pgx.ConnConfig{
		Config: pgconn.Config{
			Host:          cfg.Host,
			Port:          cfg.Port,
			Database:      cfg.Database,
			User:          cfg.User,
			TLSConfig:     cfg.TLSConfig,
			Fallbacks:     cfg.Fallbacks,
			RuntimeParams: cfg.RuntimeParams,
		},
	}
	conn, err := pgx.ConnectConfig(ctx, connCfg)
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

// RunMigrations opens a dedicated connection with the DDL role, applies all
// pending up migrations on it, and closes it.  The runtime pool is never
// used for DDL; role selection is explicit in cfg.User.
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

	result, err := NewRunner(&ddlPool{conn: conn}).Migrate(ctx, DirectionUp)
	if err != nil {
		return nil, fmt.Errorf("DDL-role migration batch: %w", err)
	}
	return result, nil
}
