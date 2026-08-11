package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gorouter/internal/domain/backup"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// realExecRunner is the production CommandRunner: exec.Command with an argv
// array, no shell, and an explicit minimal environment.
type realExecRunner struct{}

// Run implements CommandRunner with exec.CommandContext and cmd.Env set to
// the provided minimal environment.
func (realExecRunner) Run(ctx context.Context, name string, argv []string, env []string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, name)
	cmd.Args = append(cmd.Args, argv...)
	cmd.Env = env
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return []byte(stdout.String()), []byte(stderr.String()), err
}

// parsePGConfig parses a database DSN without ever surfacing the raw parse
// error: pgconn parse failures can embed the full connection string,
// including the password, which must never reach logs or the immutable
// audit log (security K5).
func parsePGConfig(dsn string) (*pgconn.Config, error) {
	cfg, err := pgconn.ParseConfig(dsn)
	if err != nil {
		return nil, ErrBackupConfigInvalid
	}
	return cfg, nil
}

// pgEnv derives the minimal libpq environment from the runtime DSN. The
// password travels in the environment (never in argv, which is visible in
// process listings) and never in logs.
func pgEnv(dsn string) ([]string, error) {
	cfg, err := parsePGConfig(dsn)
	if err != nil {
		return nil, err
	}
	if err := loopbackOnly(cfg); err != nil {
		return nil, err
	}
	env := []string{
		"PGHOST=" + cfg.Host,
		"PGPORT=" + strconv.Itoa(int(cfg.Port)),
		"PGUSER=" + cfg.User,
		"PGDATABASE=" + cfg.Database,
		"PGSSLMODE=" + sslMode(cfg),
	}
	if cfg.Password != "" {
		env = append(env, "PGPASSWORD="+cfg.Password)
	}
	return env, nil
}

// loopbackOnly refuses non-loopback database hosts: the one-listener model
// binds 127.0.0.1 (or a Unix socket), and shadow-database work must never
// reach a remote endpoint.
func loopbackOnly(cfg *pgconn.Config) error {
	host := cfg.Host
	if host == "" || host == "localhost" || host == "127.0.0.1" || host == "::1" || strings.HasPrefix(host, "/") {
		return nil
	}
	return fmt.Errorf("backup: refusing non-loopback database host %q", host)
}

func sslMode(cfg *pgconn.Config) string {
	if v, ok := cfg.RuntimeParams["sslmode"]; ok {
		return v
	}
	if cfg.TLSConfig != nil {
		return "require"
	}
	return "prefer"
}

// setPGDatabase overrides the PGDATABASE entry in a pgEnv slice.
func setPGDatabase(env []string, db string) []string {
	out := make([]string, 0, len(env)+1)
	for _, e := range env {
		if strings.HasPrefix(e, "PGDATABASE=") {
			continue
		}
		out = append(out, e)
	}
	return append(out, "PGDATABASE="+db)
}

// connectLoopbackDB opens a guarded single connection, optionally to the
// given database name instead of the DSN default.
func connectLoopbackDB(ctx context.Context, dsn, dbName string) (*pgx.Conn, error) {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, ErrBackupConfigInvalid
	}
	if err := loopbackOnly(&cfg.Config); err != nil {
		return nil, err
	}
	if dbName != "" {
		cfg.Database = dbName
	}
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("backup: connect: %w", err)
	}
	return conn, nil
}

// pgDatabaseName returns the database name of a runtime DSN for the
// pg_restore -d target (PostgreSQL 15+ requires an explicit -d or -f).
func pgDatabaseName(dsn string) (string, error) {
	cfg, err := parsePGConfig(dsn)
	if err != nil {
		return "", err
	}
	return cfg.Database, nil
}

// shadowDBConn is the minimal connection surface the shadow-database
// manager needs; *pgx.Conn satisfies it and tests substitute a fake.
type shadowDBConn interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Close(ctx context.Context) error
}

// pgShadowManager is the production ShadowDBManager: it restores the backup
// into a disposable shadow database on the loopback endpoint, validates the
// restored schema, and always terminates and drops the shadow database
// (security P1-7).
type pgShadowManager struct {
	dsn     string
	runner  CommandRunner
	connect func(ctx context.Context, dsn, dbName string) (shadowDBConn, error)
}

func newPGShadowManager(dsn string, runner CommandRunner) *pgShadowManager {
	return &pgShadowManager{
		dsn:    dsn,
		runner: runner,
		connect: func(ctx context.Context, dsn, dbName string) (shadowDBConn, error) {
			return connectLoopbackDB(ctx, dsn, dbName)
		},
	}
}

// VerifyShadow implements ShadowDBManager. The shadow database is created on
// the same loopback lane as a separate database; pg_restore restores the
// dump with an argv array and a minimal environment, the restored schema is
// validated, and the shadow is dropped in every outcome.
func (m *pgShadowManager) VerifyShadow(ctx context.Context, b *backup.Backup) error {
	if m.dsn == "" {
		return errors.New("backup: database not configured")
	}
	mgmtCtx, cancel := context.WithTimeout(ctx, shadowDBTimeout)
	defer cancel()
	conn, err := m.connect(mgmtCtx, m.dsn, "")
	if err != nil {
		return err
	}
	defer conn.Close(mgmtCtx)

	shadow := "gorouter_shadow_" + time.Now().UTC().Format("20060102T150405Z") + "_" + shortID()
	if _, err := conn.Exec(mgmtCtx, `DROP DATABASE IF EXISTS "`+shadow+`" WITH (FORCE)`); err != nil {
		return fmt.Errorf("prepare shadow database: %w", err)
	}
	if _, err := conn.Exec(mgmtCtx, `CREATE DATABASE "`+shadow+`"`); err != nil {
		return fmt.Errorf("create shadow database: %w", err)
	}
	// The shadow database is disposable and must never leak: terminate its
	// backends and drop it even when the restore or validation fails, using
	// a fresh bounded context so a cancelled caller context cannot skip it.
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), shadowDBTimeout)
		defer cancel()
		if c, err := m.connect(cleanupCtx, m.dsn, ""); err == nil {
			defer c.Close(cleanupCtx)
			_, _ = c.Exec(cleanupCtx,
				`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()`,
				shadow)
			_, _ = c.Exec(cleanupCtx, `DROP DATABASE IF EXISTS "`+shadow+`" WITH (FORCE)`)
		}
	}()

	env, err := pgEnv(m.dsn)
	if err != nil {
		return err
	}
	env = setPGDatabase(env, shadow)
	restoreCtx, cancel := context.WithTimeout(ctx, pgRestoreTimeout)
	defer cancel()
	argv := []string{"--no-owner", "--no-privileges", "--exit-on-error", "-d", shadow, b.Path}
	if _, stderr, err := m.runner.Run(restoreCtx, "pg_restore", argv, env); err != nil {
		return fmt.Errorf("shadow restore failed: %w: %s", err, tail(stderr))
	}
	return validateShadowDB(ctx, m.connect, m.dsn, shadow)
}

func validateShadowDB(ctx context.Context, connect func(context.Context, string, string) (shadowDBConn, error), dsn, shadow string) error {
	validateCtx, cancel := context.WithTimeout(ctx, shadowDBTimeout)
	defer cancel()
	shadowConn, err := connect(validateCtx, dsn, shadow)
	if err != nil {
		return fmt.Errorf("connect shadow: %w", err)
	}
	defer shadowConn.Close(validateCtx)
	var applied int
	if err := shadowConn.QueryRow(validateCtx, "SELECT count(*) FROM gorouter_migrations").Scan(&applied); err != nil {
		return fmt.Errorf("shadow validation: %w", err)
	}
	if applied <= 0 {
		return errors.New("shadow validation: no migrations recorded")
	}
	var auditRows int
	if err := shadowConn.QueryRow(validateCtx, "SELECT count(*) FROM gorouter_audit_log").Scan(&auditRows); err != nil {
		return fmt.Errorf("shadow validation: audit read failed: %w", err)
	}
	for _, table := range []string{"gorouter_users", "gorouter_settings"} {
		var reg *string
		if err := shadowConn.QueryRow(validateCtx, `SELECT to_regclass($1)::text`, table).Scan(&reg); err != nil {
			return fmt.Errorf("shadow validation: %w", err)
		}
		if reg == nil || *reg == "" {
			return fmt.Errorf("shadow validation: table %s missing", table)
		}
	}
	return nil
}

// HashFileSHA256 returns the lowercase hex sha256 of the file at path.
func HashFileSHA256(path string) (string, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// fsyncFile flushes the file at path to stable storage.
func fsyncFile(path string) error {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

// fsyncDir flushes the directory entry metadata for path to stable storage.
func fsyncDir(path string) error {
	d, err := os.Open(filepath.Clean(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// finalizeDumpFile makes a finished dump durable and private: it must be a
// regular file, chmod 0600, and fsynced before the atomic rename publishes
// it under the storage root.
func finalizeDumpFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("backup: stat dump: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("backup: dump is not a regular file")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("backup: secure dump file: %w", err)
	}
	if err := fsyncFile(path); err != nil {
		return fmt.Errorf("backup: sync dump file: %w", err)
	}
	return nil
}

// securePathInDir rejects registry paths that escape dir or traverse a
// symlink: the path must resolve to a contained file inside dir (security
// P1-7: backup files 0600 under a 0700 storage root).
func securePathInDir(dir, path string) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("backup: resolve storage root: %w", err)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("backup: resolve backup path: %w", err)
	}
	if escaped(absDir, absPath) {
		return ErrBackupPathEscape
	}
	info, err := os.Lstat(absPath)
	if err != nil {
		return fmt.Errorf("backup file unavailable: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: symlink", ErrBackupPathEscape)
	}
	resolved, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return fmt.Errorf("backup file unavailable: %w", err)
	}
	if escaped(absDir, resolved) {
		return ErrBackupPathEscape
	}
	return nil
}

func escaped(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return true
	}
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
