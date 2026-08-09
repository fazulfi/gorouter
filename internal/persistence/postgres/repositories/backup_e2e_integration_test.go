package repositories

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	backupapp "gorouter/internal/app/backup"
	"gorouter/internal/domain/auth"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// containerRunner wraps the version-matching pg_dump/pg_restore inside the
// per-version test container, mirroring the production command surface (argv
// array, no shell, explicit environment) through docker exec. pg_dump output
// is captured to the host --file path; pg_restore receives the host dump via
// docker cp so the container binary restores it by path; PGPORT is rewritten
// to the container-internal 5432 so the tools reach the same loopback-only
// postgres instance as the host-side service.
type containerRunner struct{ container string }

func (r containerRunner) Run(ctx context.Context, name string, argv []string, env []string) ([]byte, []byte, error) {
	var dumpOut string
	var restoreIn string
	args := make([]string, 0, len(argv))
	prev := ""
	for _, a := range argv {
		if strings.HasPrefix(a, "--file=") {
			dumpOut = strings.TrimPrefix(a, "--file=")
			prev = a
			continue
		}
		if prev == "-d" || prev == "--dbname" || strings.HasPrefix(a, "-d=") {
			args = append(args, a)
			prev = a
			continue
		}
		if !strings.HasPrefix(a, "-") && restoreIn == "" {
			restoreIn = a
			prev = a
			continue
		}
		args = append(args, a)
		prev = a
	}
	if restoreIn != "" {
		inPath := "/tmp/" + filepath.Base(restoreIn)
		if out, err := exec.CommandContext(ctx, "docker", "cp", restoreIn, r.container+":"+inPath).CombinedOutput(); err != nil {
			return nil, nil, fmt.Errorf("copy dump into container: %w: %s", err, strings.TrimSpace(string(out)))
		}
		defer func() {
			_ = exec.CommandContext(context.Background(), "docker", "exec", r.container, "rm", "-f", inPath).Run()
		}()
		args = append(args, inPath)
	}
	dockerArgs := []string{"exec"}
	for _, e := range env {
		if !strings.HasPrefix(e, "PG") {
			continue
		}
		e = strings.Replace(e, "PGPORT=5442", "PGPORT=5432", 1)
		e = strings.Replace(e, "PGPORT=5443", "PGPORT=5432", 1)
		e = strings.Replace(e, "PGPORT=5444", "PGPORT=5432", 1)
		dockerArgs = append(dockerArgs, "-e", e)
	}
	dockerArgs = append(dockerArgs, r.container, name)
	dockerArgs = append(dockerArgs, args...)
	cmd := exec.CommandContext(ctx, "docker", dockerArgs...)
	var stdout, stderr strings.Builder
	cmd.Stderr = &stderr
	if dumpOut != "" {
		f, err := os.OpenFile(dumpOut, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return nil, nil, fmt.Errorf("create dump file: %w", err)
		}
		defer f.Close()
		cmd.Stdout = f
	} else {
		cmd.Stdout = &stdout
	}
	err := cmd.Run()
	return []byte(stdout.String()), []byte(stderr.String()), err
}

func backupRunner(t *testing.T) backupapp.CommandRunner {
	t.Helper()
	if container := os.Getenv("GOROUTER_PG_CONTAINER"); container != "" {
		return containerRunner{container: container}
	}
	return backupapp.CommandRunnerFunc(func(ctx context.Context, name string, argv []string, env []string) ([]byte, []byte, error) {
		cmd := exec.CommandContext(ctx, name, argv...)
		cmd.Env = env
		var stdout, stderr strings.Builder
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		return []byte(stdout.String()), []byte(stderr.String()), err
	})
}

func backupDSN(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Fatal("DATABASE_URL not set")
	}
	slash := strings.LastIndex(dsn, "/")
	db := pool.Config().ConnConfig.Database
	return dsn[:slash+1] + db + "?sslmode=disable"
}

func backupBeginner(pool *pgxpool.Pool) backupapp.BackupScopeBeginner {
	return backupapp.BackupScopeBeginnerFunc(func(ctx context.Context) (backupapp.BackupScope, error) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return nil, err
		}
		return NewTxScope(tx), nil
	})
}

func seedBackupUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool) *auth.Actor {
	t.Helper()
	userID := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO gorouter_users (id, email, password_hash, is_admin, is_active)
		 VALUES ($1, $2, $3, true, true)`,
		userID, "backup-e2e-"+uuid.NewString()[:8]+"@gorouter.local", "test-hash"); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return &auth.Actor{UserID: userID, IsAdmin: true, Kind: auth.ActorKindSession, Origin: auth.ActorOriginRemote}
}

// TestBackupServiceEndToEnd_Integration drives the full backup lifecycle
// through the real wired chain with real pg_dump/pg_restore: generate
// (registry + audit + file 0600 + daily semantics + retention), shadow-
// database restore verification with fail-closed drop, download auditing,
// and the typed restore confirmation.
func TestBackupServiceEndToEnd_Integration(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()
	actor := seedBackupUser(t, ctx, pool)

	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	svc := backupapp.NewService(backupapp.Config{
		Dir:         dir,
		DatabaseURL: backupDSN(t, pool),
		Keep:        2,
	}, backupBeginner(pool), backupRunner(t), nil)

	// Generate: real dump of the isolated database.
	b, err := svc.Generate(ctx, actor)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if b.SHA256 == "" || b.Bytes == 0 {
		t.Fatalf("registry entry incomplete: %+v", b)
	}
	info, err := os.Stat(b.Path)
	if err != nil {
		t.Fatalf("dump file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("dump mode = %o, want 600", info.Mode().Perm())
	}
	// A second generation in the same UTC day is refused (daily semantics).
	if _, err := svc.Generate(ctx, actor); !errors.Is(err, backupapp.ErrBackupDailyLimit) {
		t.Fatalf("second same-day Generate err = %v, want ErrBackupDailyLimit", err)
	}
	// Verify: shadow-database restore, both markers recorded, shadow dropped.
	if err := svc.Verify(ctx, actor, b.ID); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	got, err := svc.List(ctx, actor)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].ID != b.ID || got[0].RestoreVerifiedAt == nil || got[0].VerifiedAt == nil {
		t.Fatalf("verified backup = %+v", got)
	}
	// Download audits the actor, ip, backup id, sha256.
	if _, err := svc.Download(ctx, actor, nil, b.ID); err != nil {
		t.Fatalf("Download: %v", err)
	}
	shadowRows, err := pool.Query(ctx, "SELECT datname FROM pg_database WHERE datname LIKE 'gorouter_shadow_%'")
	if err != nil {
		t.Fatal(err)
	}
	var leftovers []string
	for shadowRows.Next() {
		var name string
		if err := shadowRows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		leftovers = append(leftovers, name)
	}
	shadowRows.Close()
	if len(leftovers) != 0 {
		t.Errorf("shadow databases left behind: %v", leftovers)
	}

	// Verify failure path: a tampered file with a matching size fails
	// closed, drops its shadow, audits the failure, and leaves the earlier
	// verification markers untouched.
	if err := os.WriteFile(b.Path, []byte(strings.Repeat("X", int(b.Bytes))), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := svc.Verify(ctx, actor, b.ID); err == nil {
		t.Fatal("tampered dump must fail verification")
	}
	got, err = svc.List(ctx, actor)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].RestoreVerifiedAt == nil {
		t.Fatalf("list after failed verify = %+v", got)
	}
	rows, err := pool.Query(ctx,
		`SELECT details::text FROM gorouter_audit_log WHERE action = 'backup.verify.restore' ORDER BY occurred_at`)
	if err != nil {
		t.Fatal(err)
	}
	var audits []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			t.Fatal(err)
		}
		audits = append(audits, d)
	}
	rows.Close()
	if len(audits) != 2 {
		t.Fatalf("verify audits = %d, want 2", len(audits))
	}
	if !strings.Contains(audits[0], "verified") || !strings.Contains(audits[1], "failed") {
		t.Errorf("verify audit sequence = %v", audits)
	}

	// Restore confirmation decline: audited, nothing changes. Only the
	// host-local CLI actor may attempt a restore (DECISIONS #202).
	cliActor := &auth.Actor{UserID: actor.UserID, IsAdmin: true, Kind: auth.ActorKindCLI, Origin: auth.ActorOriginLocal}
	if err := svc.Restore(ctx, cliActor, b.ID, backupapp.RestoreConfirmation{
		SHA256Prefix: "wrongprefix", AcknowledgeDestructive: true,
	}); !errors.Is(err, backupapp.ErrRestoreDeclined) {
		t.Fatalf("err = %v, want ErrRestoreDeclined", err)
	}
	// A dashboard-session admin actor must never pass the local-CLI-only
	// restore gate.
	if err := svc.Restore(ctx, actor, b.ID, backupapp.RestoreConfirmation{
		SHA256Prefix: "wrongprefix", AcknowledgeDestructive: true,
	}); !errors.Is(err, backupapp.ErrRestoreLocalCLIOnly) {
		t.Fatalf("session actor err = %v, want ErrRestoreLocalCLIOnly", err)
	}
	declines, err := pool.Query(ctx, `SELECT count(*) FROM gorouter_audit_log WHERE action = 'backup.restore.decline'`)
	if err != nil {
		t.Fatal(err)
	}
	var declineCount int
	if declines.Next() {
		_ = declines.Scan(&declineCount)
	}
	declines.Close()
	if declineCount != 1 {
		t.Errorf("decline audits = %d, want 1", declineCount)
	}

	// Retention: with Keep=2, a generation on the next UTC day prunes the
	// oldest file while keeping the newest validated backup and the current
	// artifact. Backdate the existing entry past the daily window and seed
	// an older unvalidated entry with a real file.
	oldPath := filepath.Join(dir, "oldest.dump")
	if err := os.WriteFile(oldPath, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldSHA, err := backupapp.HashFileSHA256(oldPath)
	if err != nil {
		t.Fatal(err)
	}
	oldCreated := time.Now().UTC().Add(-72 * time.Hour)
	oldGenerated := "test"
	if _, err := pool.Exec(ctx,
		`INSERT INTO gorouter_backups (id, path, sha256, bytes, generated_by, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		uuid.New(), oldPath, oldSHA, int64(len("old")), oldGenerated, oldCreated); err != nil {
		t.Fatal(err)
	}
	// A malicious registry row pointing outside the storage root must never
	// make prune delete the external file (C1/K2 containment): prune skips
	// the candidate and records a bounded skip audit.
	outsideDir := t.TempDir()
	sentinel := filepath.Join(outsideDir, "sentinel.env")
	if err := os.WriteFile(sentinel, []byte("external secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	sentinelSHA, err := backupapp.HashFileSHA256(sentinel)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO gorouter_backups (id, path, sha256, bytes, generated_by, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		uuid.New(), sentinel, sentinelSHA, int64(len("external secret")), oldGenerated, oldCreated.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE gorouter_backups SET created_at = now() - interval '24 hours' WHERE id = $1`, b.ID); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b.Path, []byte("restored original content"), 0o600); err != nil {
		t.Fatal(err)
	}
	b2, err := svc.Generate(ctx, actor)
	if err != nil {
		t.Fatalf("Generate 2: %v", err)
	}
	files := 0
	_ = filepath.Walk(dir, func(_ string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			files++
		}
		return nil
	})
	if files != 2 {
		t.Errorf("retained files = %d, want 2", files)
	}
	if _, err := os.Stat(oldPath); err == nil {
		t.Error("oldest backup file must be pruned")
	}
	if _, err := os.Stat(b2.Path); err != nil {
		t.Errorf("newest backup file missing: %v", err)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Errorf("external sentinel was deleted by prune: %v", err)
	}
	skipRows, err := pool.Query(ctx, `SELECT count(*) FROM gorouter_audit_log WHERE action = 'backup.prune.skip'`)
	if err != nil {
		t.Fatal(err)
	}
	var skipCount int
	if skipRows.Next() {
		_ = skipRows.Scan(&skipCount)
	}
	skipRows.Close()
	if skipCount != 1 {
		t.Errorf("prune skip audits = %d, want 1", skipCount)
	}
}

// TestValidateBootstrapBackup_Integration proves the fail-closed gate against
// real PostgreSQL: a fully-migrated database passes, a pending migration
// without a validated backup fails closed, and a validated backup with a
// matching file passes; only-one-marker rows and size/hash mismatches fail.
func TestValidateBootstrapBackup_Integration(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	if err := backupapp.ValidateBootstrapBackup(ctx, pool); err != nil {
		t.Fatalf("fully-migrated database must pass: %v", err)
	}

	// Make one migration pending by removing its tracking row.
	if _, err := pool.Exec(ctx, `DELETE FROM gorouter_migrations WHERE version = '000009'`); err != nil {
		t.Fatal(err)
	}
	if err := backupapp.ValidateBootstrapBackup(ctx, pool); err == nil {
		t.Fatal("pending migration without validated backup must fail closed")
	}

	// A registry row with only verified_at is NOT a validated backup.
	dir := t.TempDir()
	path := filepath.Join(dir, "partial.dump")
	if err := os.WriteFile(path, []byte("partial content"), 0o600); err != nil {
		t.Fatal(err)
	}
	generatedBy := "test"
	baseTime := time.Now().UTC()
	verifiedAt := time.Now().UTC()
	if _, err := pool.Exec(ctx,
		`INSERT INTO gorouter_backups (id, path, sha256, bytes, generated_by, verified_at, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		uuid.New(), path, "aa", int64(15), generatedBy, verifiedAt, baseTime); err != nil {
		t.Fatal(err)
	}
	if err := backupapp.ValidateBootstrapBackup(ctx, pool); err == nil {
		t.Fatal("backup with only one marker must fail closed")
	}

	// A fully validated backup whose file hash matches passes.
	validatedPath := filepath.Join(dir, "validated.dump")
	body := []byte("validated content")
	if err := os.WriteFile(validatedPath, body, 0o600); err != nil {
		t.Fatal(err)
	}
	sha, err := backupapp.HashFileSHA256(validatedPath)
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO gorouter_backups (id, path, sha256, bytes, generated_by, verified_at, restore_verified_at, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		id, validatedPath, sha, int64(len(body)), generatedBy, verifiedAt, verifiedAt, baseTime.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := backupapp.ValidateBootstrapBackup(ctx, pool); err != nil {
		t.Fatalf("validated backup must pass: %v", err)
	}

	// A validated registry row whose file is missing fails closed.
	ghostPath := filepath.Join(dir, "ghost.dump")
	ghostSHA := strings.Repeat("cd", 32)
	if _, err := pool.Exec(ctx,
		`INSERT INTO gorouter_backups (id, path, sha256, bytes, generated_by, verified_at, restore_verified_at, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		uuid.New(), ghostPath, ghostSHA, int64(5), generatedBy, verifiedAt, verifiedAt, baseTime.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	// The newest validated row is the ghost (created later), so the gate
	// must fail closed on the missing file.
	if err := backupapp.ValidateBootstrapBackup(ctx, pool); err == nil {
		t.Fatal("missing validated backup file must fail closed")
	}

	// A validated row whose registry size does not match the file fails.
	sizeMismatchPath := filepath.Join(dir, "sizemismatch.dump")
	if err := os.WriteFile(sizeMismatchPath, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO gorouter_backups (id, path, sha256, bytes, generated_by, verified_at, restore_verified_at, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		uuid.New(), sizeMismatchPath, sha, int64(999), generatedBy, verifiedAt, verifiedAt, baseTime.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := backupapp.ValidateBootstrapBackup(ctx, pool); err == nil {
		t.Fatal("validated backup with size mismatch must fail closed")
	}
}

// TestValidateBootstrapBackup_FreshDB_Integration proves a fresh database
// (no applied migrations) passes without any backup, so development and
// first-time deploys never require production resources.
func TestValidateBootstrapBackup_FreshDB_Integration(t *testing.T) {
	adminDSN := os.Getenv("DATABASE_URL")
	if adminDSN == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		t.Fatalf("admin connect: %v", err)
	}
	defer conn.Close(ctx)
	dbName := "gorouter_be15_fresh_" + uuid.NewString()[:8]
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+dbName); err != nil {
		t.Fatalf("create fresh db: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		c, err := pgx.Connect(cleanupCtx, adminDSN)
		if err != nil {
			return
		}
		_, _ = c.Exec(cleanupCtx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()`, dbName)
		_, _ = c.Exec(cleanupCtx, "DROP DATABASE IF EXISTS "+dbName)
		c.Close(cleanupCtx)
	})

	slash := strings.LastIndex(adminDSN, "/")
	dsn := adminDSN[:slash+1] + dbName + "?sslmode=disable"

	bootstrapRolesForRepoTest(t, ctx, adminDSN)

	runtimePool := freshRuntimePoolForBackupTest(t, ctx, dsn)
	defer runtimePool.Close()

	// The tracking table does not exist yet: the gate treats it as fresh.
	if err := backupapp.ValidateBootstrapBackup(ctx, runtimePool); err != nil {
		t.Fatalf("fresh database must pass: %v", err)
	}

	// After a full migration run the database is non-fresh with no pending
	// migrations: still passes without any backup. Migrations run as the DDL
	// role; the runtime pool is then reopened as the runtime role.
	migratedPool := migrateAsDDLRepoTest(t, ctx, dsn)
	defer migratedPool.Close()
	grantRuntimeFixturePrivilegesRepoTest(t, ctx, dsn)
	if err := backupapp.ValidateBootstrapBackup(ctx, migratedPool); err != nil {
		t.Fatalf("migrated database must pass: %v", err)
	}
}

// freshRuntimePoolForBackupTest opens a runtime pool (gorouter) on a fresh
// database before any migration has run, so the fresh-database precondition
// of the backup gate can be observed.
func freshRuntimePoolForBackupTest(t *testing.T, ctx context.Context, dsn string) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse DSN: %v", err)
	}
	cfg.ConnConfig.User = "gorouter"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return pool
}
