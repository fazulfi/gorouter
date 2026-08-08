// Package backup provides the validated disaster-recovery backup service:
// daily schema+data dumps with validated retention, shadow-database restore
// verification, audited download, and a local-CLI-only destructive restore
// with typed confirmation. Manual configuration transfer is a separate
// contract (DECISIONS #378) and lives in internal/app/settings.
package backup

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gorouter/internal/app/tx"
	"gorouter/internal/domain/auth"
	"gorouter/internal/domain/backup"

	"github.com/google/uuid"
)

// Backup represents a backup artifact.
type Backup = backup.Backup

// DefaultKeep is the retention target: exactly this many newest validated
// backups are retained after every generation (DECISIONS #203: 30 daily
// backups). Unvalidated artifacts beyond the validated retention window are
// pruned, and the just-generated artifact is never pruned before it can be
// verified.
const DefaultKeep = 30

// storageRootDirMode is the directory tier of the backup permission model
// (directories 0700, files 0600; see Config). Gosec G302's file mode threshold
// (0600) does not apply to directories; 0700 is the most restrictive mode that
// still permits owner traversal (gosec's directory rule G301 allows up to 0750).
const storageRootDirMode os.FileMode = 0o700

// Bounded command and shadow-database contexts: every subprocess and every
// shadow-database management step runs under a timeout so a stuck tool or
// endpoint cannot hang the scheduler job.
const (
	pgDumpTimeout    = 15 * time.Minute
	pgRestoreTimeout = 15 * time.Minute
	shadowDBTimeout  = 2 * time.Minute
)

var (
	// ErrBackupActorRequired reports a backup operation without an actor.
	ErrBackupActorRequired = errors.New("backup: actor is required")
	// ErrRestoreDeclined reports a restore whose typed confirmation did not
	// match. The decline is audited; nothing is restored.
	ErrRestoreDeclined = errors.New("backup: restore declined")
	// ErrBackupDailyLimit reports a second generation attempt within the
	// same UTC day: the scheduler generates exactly one backup per day.
	ErrBackupDailyLimit = errors.New("backup: a backup was already generated today")
	// ErrRestoreLocalCLIOnly reports a restore attempt by an actor that the
	// local-CLI-only contract cannot accept.
	ErrRestoreLocalCLIOnly = errors.New("backup: restore is a local CLI-only operation")
	// ErrBackupPathEscape reports a registry path that escapes the storage
	// root or traverses a symlink.
	ErrBackupPathEscape = errors.New("backup: registry path escapes the storage root")
	// ErrBackupConfigInvalid reports a database DSN that could not be parsed.
	// The underlying parse error is never surfaced: it can embed the DSN
	// including its password (security K5).
	ErrBackupConfigInvalid = errors.New("backup: invalid database configuration")
)

// Config configures the backup service. Dir is the storage root (directories
// 0700, files 0600), DatabaseURL the runtime database endpoint (loopback
// only), and Keep the retention target (0 means DefaultKeep).
type Config struct {
	Dir         string
	DatabaseURL string
	Keep        int
}

func (c Config) withDefaults() Config {
	if c.Keep <= 0 {
		c.Keep = DefaultKeep
	}
	return c
}

// CommandRunner executes a subprocess with an argv array (never a shell) and
// an explicit environment. It is an interface so tests can substitute a
// recording fake; the production runner is realExecRunner.
type CommandRunner interface {
	Run(ctx context.Context, name string, argv []string, env []string) (stdout, stderr []byte, err error)
}

// CommandRunnerFunc adapts a function to CommandRunner.
type CommandRunnerFunc func(ctx context.Context, name string, argv []string, env []string) ([]byte, []byte, error)

// Run implements CommandRunner.
func (f CommandRunnerFunc) Run(ctx context.Context, name string, argv []string, env []string) ([]byte, []byte, error) {
	return f(ctx, name, argv, env)
}

// RestoreConfirmation is the typed confirmation required before a destructive
// restore: the backup's sha256[:8] retyped and the explicit
// acknowledge-destructive flag (security P1-7, DECISIONS #202).
type RestoreConfirmation struct {
	SHA256Prefix           string
	AcknowledgeDestructive bool
}

// DownloadResult describes a validated backup file ready for download.
type DownloadResult struct {
	Path   string
	SHA256 string
	Bytes  int64
}

// BackupScope is the transaction-scoped persistence surface used by the
// backup service. *tx.TxScope satisfies it.
type BackupScope interface {
	Backups() backup.BackupRepository
	AuditLog() tx.AuditLogRepository
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

var _ BackupScope = (*tx.TxScope)(nil)

// BackupScopeBeginner begins a backup transaction scope.
type BackupScopeBeginner interface {
	Begin(ctx context.Context) (BackupScope, error)
}

// BackupScopeBeginnerFunc adapts a function to BackupScopeBeginner.
type BackupScopeBeginnerFunc func(ctx context.Context) (BackupScope, error)

// Begin implements BackupScopeBeginner.
func (f BackupScopeBeginnerFunc) Begin(ctx context.Context) (BackupScope, error) { return f(ctx) }

// ShadowDBManager verifies a backup by restoring it into a disposable shadow
// database on the loopback endpoint. Every implementation must terminate and
// drop the shadow database in every outcome (security P1-7). The production
// implementation is pgShadowManager; tests substitute a fake.
type ShadowDBManager interface {
	// VerifyShadow restores the backup into a fresh disposable shadow
	// database, runs the verification checks, and always drops the shadow.
	VerifyShadow(ctx context.Context, b *backup.Backup) error
}

// Service implements the validated disaster-recovery backup lifecycle.
type Service struct {
	cfg      Config
	beginner BackupScopeBeginner
	runner   CommandRunner
	shadows  ShadowDBManager
	// genMu serializes Generate within the process (single-instance model,
	// DECISIONS #79): a concurrent Generate must never publish an artifact
	// that the other's prune removes before it can be verified (C4). No
	// cross-process lock is invented; the scheduler is single-flight.
	genMu sync.Mutex
}

// NewService creates a backup Service. The shadow-database manager defaults
// to the production pgShadowManager over cfg.DatabaseURL.
func NewService(cfg Config, beginner BackupScopeBeginner, runner CommandRunner, shadows ShadowDBManager) *Service {
	if shadows == nil {
		shadows = newPGShadowManager(cfg.DatabaseURL, runner)
	}
	return &Service{cfg: cfg.withDefaults(), beginner: beginner, runner: runner, shadows: shadows}
}

// Generate dumps the schema and data into a custom-format file under the
// storage root, records the registry entry and the audit event in one
// transaction, and prunes files beyond the validated retention target. Daily
// semantics: exactly one generation per UTC day. On any failure the partial
// file is removed (fail closed). The dump is written to a temporary file,
// fsynced, atomically renamed, and the storage directory fsynced; the file
// is chmod 0600 and the storage root 0700. The actor may be a human user or
// a scheduler job actor (job provenance lands with the scheduler lane; the
// service audits the actor it is handed).
func (s *Service) Generate(ctx context.Context, actor *auth.Actor) (*backup.Backup, error) {
	s.genMu.Lock()
	defer s.genMu.Unlock()
	if actor == nil {
		return nil, ErrBackupActorRequired
	}
	if s.cfg.Dir == "" {
		return nil, errors.New("backup: storage directory not configured")
	}
	if s.cfg.DatabaseURL == "" {
		return nil, errors.New("backup: database not configured")
	}
	if err := os.MkdirAll(s.cfg.Dir, 0o700); err != nil {
		return nil, fmt.Errorf("backup: create storage directory: %w", err)
	}
	_ = os.Chmod(s.cfg.Dir, storageRootDirMode)
	if err := s.enforceDaily(ctx); err != nil {
		return nil, err
	}

	base := "gorouter-" + time.Now().UTC().Format("20060102T150405Z") + "-" + shortID()
	tmpPath := filepath.Join(s.cfg.Dir, ".tmp-"+base+".dump")
	finalPath := filepath.Join(s.cfg.Dir, base+".dump")
	defer func() { _ = os.Remove(tmpPath) }()

	env, err := pgEnv(s.cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	dumpCtx, cancel := context.WithTimeout(ctx, pgDumpTimeout)
	defer cancel()
	argv := []string{"--format=custom", "--no-owner", "--no-privileges", "--file=" + tmpPath}
	if _, stderr, err := s.runner.Run(dumpCtx, "pg_dump", argv, env); err != nil {
		return nil, fmt.Errorf("backup: dump failed: %w: %s", err, tail(stderr))
	}
	if err := finalizeDumpFile(tmpPath); err != nil {
		return nil, err
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return nil, fmt.Errorf("backup: publish dump: %w", err)
	}
	if err := fsyncDir(s.cfg.Dir); err != nil {
		// The published dump is intact and will be picked up by the next
		// generation cycle if the directory sync fails; surface the error
		// rather than pretending durability succeeded.
		return nil, fmt.Errorf("backup: sync storage directory: %w", err)
	}

	b, err := s.record(ctx, actor, finalPath)
	if err != nil {
		_ = os.Remove(finalPath)
		return nil, err
	}
	if err := s.prune(ctx, actor, b.ID); err != nil {
		return nil, fmt.Errorf("backup: retention failed: %w", err)
	}
	return b, nil
}

// enforceDaily refuses a second generation within the same UTC day (design
// §11 job set: "backup generation (daily, 30 validated retained)").
func (s *Service) enforceDaily(ctx context.Context) error {
	scope, err := s.beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = scope.Rollback(ctx) }()
	list, err := scope.Backups().List(ctx)
	if err != nil {
		return fmt.Errorf("backup: list registry: %w", err)
	}
	if len(list) == 0 {
		return nil
	}
	newest := list[0] // List is newest-first, deterministic.
	if newest.CreatedAt != nil && sameUTCDay(*newest.CreatedAt, time.Now().UTC()) {
		return ErrBackupDailyLimit
	}
	return nil
}

func sameUTCDay(a, b time.Time) bool {
	ay, am, ad := a.UTC().Date()
	by, bm, bd := b.UTC().Date()
	return ay == by && am == bm && ad == bd
}

// record verifies the dump file, registers it, and audits the generation in
// one transaction.
func (s *Service) record(ctx context.Context, actor *auth.Actor, path string) (*backup.Backup, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("backup: stat dump: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("backup: dump is not a regular file")
	}
	if info.Mode().Perm() != 0o600 {
		return nil, fmt.Errorf("backup: dump file mode = %o, want 600", info.Mode().Perm())
	}
	sha, err := HashFileSHA256(path)
	if err != nil {
		return nil, fmt.Errorf("backup: hash dump: %w", err)
	}

	generatedBy := actor.UserID.String()
	createdAt := time.Now().UTC()
	b := &backup.Backup{
		ID: uuid.New(), Path: path, SHA256: sha, Bytes: info.Size(),
		GeneratedBy: &generatedBy, CreatedAt: &createdAt,
	}
	scope, err := s.beginner.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = scope.Rollback(ctx) }()
	if err := scope.Backups().Create(ctx, b); err != nil {
		return nil, fmt.Errorf("register backup: %w", err)
	}
	details := map[string]any{"path": path, "sha256": sha, "bytes": info.Size()}
	if err := writeBackupAudit(ctx, scope.AuditLog(), actor, nil, "backup.generate", details); err != nil {
		return nil, fmt.Errorf("audit backup generate: %w", err)
	}
	if err := scope.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return b, nil
}

// Verify checks the backup file (size and sha256 against the registry) and
// restores it into a disposable shadow database. On success the verified_at
// and restore_verified_at markers are recorded in one transaction with the
// audit entry. On any failure the failure is audited and no verification
// marker is set (fail closed).
func (s *Service) Verify(ctx context.Context, actor *auth.Actor, id uuid.UUID) error {
	if actor == nil {
		return ErrBackupActorRequired
	}
	b, err := s.find(ctx, id)
	if err != nil {
		return err
	}
	if err := s.verifyRegistryFile(b); err != nil {
		s.auditFailure(ctx, actor, id, err)
		return err
	}
	if err := s.shadows.VerifyShadow(ctx, b); err != nil {
		s.auditFailure(ctx, actor, id, err)
		return err
	}
	now := time.Now().UTC()
	scope, err := s.beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = scope.Rollback(ctx) }()
	if err := scope.Backups().UpdateVerification(ctx, id, now); err != nil {
		return fmt.Errorf("record verification: %w", err)
	}
	if err := scope.Backups().UpdateRestoreVerification(ctx, id, now); err != nil {
		return fmt.Errorf("record restore verification: %w", err)
	}
	if err := writeBackupAudit(ctx, scope.AuditLog(), actor, &id, "backup.verify.restore", map[string]any{
		"result": "verified",
		"marker": "verified_at, restore_verified_at",
	}); err != nil {
		return fmt.Errorf("audit backup verify: %w", err)
	}
	if err := scope.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

// auditFailure records a sanitized failed-verification audit entry in its
// own transaction. A failed verify never sets a verification marker.
func (s *Service) auditFailure(ctx context.Context, actor *auth.Actor, id uuid.UUID, cause error) {
	_ = s.audit(ctx, actor, id, "backup.verify.restore", map[string]any{
		"result": "failed", "reason": sanitizeReason(cause),
	})
}

// find reads one registry entry.
func (s *Service) find(ctx context.Context, id uuid.UUID) (*backup.Backup, error) {
	scope, err := s.beginner.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = scope.Rollback(ctx) }()
	return scope.Backups().FindByID(ctx, id)
}

// audit appends one immutable audit entry and commits.
func (s *Service) audit(ctx context.Context, actor *auth.Actor, id uuid.UUID, action string, details map[string]any) error {
	scope, err := s.beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = scope.Rollback(ctx) }()
	if err := writeBackupAudit(ctx, scope.AuditLog(), actor, &id, action, details); err != nil {
		return fmt.Errorf("audit %s: %w", action, err)
	}
	if err := scope.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

// List returns the usable backups: registry entries whose file still exists.
// Pruned backups (beyond the retention target) never surface here.
func (s *Service) List(ctx context.Context, actor *auth.Actor) ([]backup.Backup, error) {
	if actor == nil {
		return nil, ErrBackupActorRequired
	}
	scope, err := s.beginner.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = scope.Rollback(ctx) }()
	list, err := scope.Backups().List(ctx)
	if err != nil {
		return nil, err
	}
	usable := make([]backup.Backup, 0, len(list))
	for _, b := range list {
		if info, err := os.Stat(b.Path); err == nil && info.Mode().IsRegular() {
			usable = append(usable, b)
		}
	}
	return usable, nil
}

// Download validates the backup file (path containment, mode, size, sha256)
// and audits the download with the actor, IP, backup id, and sha256 (P1-7).
// The file itself is served by the caller.
func (s *Service) Download(ctx context.Context, actor *auth.Actor, ip net.IP, id uuid.UUID) (*DownloadResult, error) {
	if actor == nil {
		return nil, ErrBackupActorRequired
	}
	if s.cfg.Dir == "" {
		return nil, errors.New("backup: storage directory not configured")
	}
	scope, err := s.beginner.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = scope.Rollback(ctx) }()
	b, err := scope.Backups().FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := securePathInDir(s.cfg.Dir, b.Path); err != nil {
		return nil, err
	}
	if err := s.verifyRegistryFile(b); err != nil {
		return nil, err
	}
	details := map[string]any{
		"backup_id": b.ID, "sha256": b.SHA256, "bytes": b.Bytes,
	}
	actorID := actor.UserID
	raw, err := json.Marshal(details)
	if err != nil {
		return nil, err
	}
	entry := &tx.AuditLogEntry{
		ID: uuid.New(), ActorID: &actorID, Action: "backup.download",
		ResourceType: "backup", ResourceID: &id, Details: raw,
		IPAddress: ip, OccurredAt: time.Now().UTC(),
	}
	if err := scope.AuditLog().Create(ctx, entry); err != nil {
		return nil, fmt.Errorf("audit backup download: %w", err)
	}
	if err := scope.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return &DownloadResult{Path: b.Path, SHA256: b.SHA256, Bytes: b.Bytes}, nil
}

// Restore is the destructive local-CLI-only restore. The typed confirmation
// must retype sha256[:8] of the backup and acknowledge destructiveness
// (security P1-7, DECISIONS #202); accept and decline are both audited, and a
// failed pg_restore is audited as a failure, never reported as success. The
// actor identity must be the host-local CLI kind (design §6 P2-15):
// session/PAT/user/job actors and actors with unset (zero) Kind/Origin are
// refused before any lookup, audit, or destructive work.
func (s *Service) Restore(ctx context.Context, actor *auth.Actor, id uuid.UUID, confirmation RestoreConfirmation) error {
	if actor == nil {
		return ErrBackupActorRequired
	}
	if actor.Kind != auth.ActorKindCLI || actor.Origin != auth.ActorOriginLocal || !actor.IsAdmin {
		return ErrRestoreLocalCLIOnly
	}
	b, err := s.find(ctx, id)
	if err != nil {
		return err
	}
	if !confirmation.AcknowledgeDestructive || len(b.SHA256) < 8 ||
		!strings.EqualFold(strings.TrimSpace(confirmation.SHA256Prefix), b.SHA256[:8]) {
		if err := s.audit(ctx, actor, id, "backup.restore.decline", map[string]any{
			"result": "declined", "reason": "typed confirmation did not match",
		}); err != nil {
			return err
		}
		return ErrRestoreDeclined
	}
	// Fail closed before any destructive action: the backup must have passed
	// shadow verification (both markers) and the file must still match the
	// registry (path containment, mode 0600, size, sha256).
	if err := s.verifyRestoreReadiness(b); err != nil {
		_ = s.audit(ctx, actor, id, "backup.restore.decline", map[string]any{
			"result": "declined", "reason": "pre-restore verification failed",
		})
		return err
	}
	if err := s.audit(ctx, actor, id, "backup.restore.accept", map[string]any{
		"result": "accepted", "sha256_prefix": b.SHA256[:8],
	}); err != nil {
		return err
	}
	if s.cfg.DatabaseURL == "" {
		return errors.New("backup: database not configured")
	}
	env, err := pgEnv(s.cfg.DatabaseURL)
	if err != nil {
		return err
	}
	dbName, err := pgDatabaseName(s.cfg.DatabaseURL)
	if err != nil {
		return err
	}
	restoreCtx, cancel := context.WithTimeout(ctx, pgRestoreTimeout)
	defer cancel()
	argv := []string{"--clean", "--if-exists", "--no-owner", "--no-privileges", "--exit-on-error", "-d", dbName, b.Path}
	if _, stderr, err := s.runner.Run(restoreCtx, "pg_restore", argv, env); err != nil {
		// The destructive run failed after the accept audit: record the
		// failure (sanitized and bounded) so the immutable audit trail
		// never misreports the outcome as success (C5).
		_ = s.audit(ctx, actor, id, "backup.restore.failed", map[string]any{
			"result": "failed", "reason": sanitizeReason(fmt.Errorf("%w: %s", err, tail(stderr))),
		})
		return fmt.Errorf("backup: restore failed: %w: %s", err, tail(stderr))
	}
	return nil
}

// verifyRestoreReadiness fails closed before a destructive restore: the
// backup must already carry both verification markers from a previous
// shadow-database verification, and the file must match the registry.
func (s *Service) verifyRestoreReadiness(b *backup.Backup) error {
	if b.VerifiedAt == nil || b.RestoreVerifiedAt == nil {
		return errors.New("backup: restore refused: backup has not passed verification")
	}
	if err := securePathInDir(s.cfg.Dir, b.Path); err != nil {
		return err
	}
	return s.verifyRegistryFile(b)
}

// prune removes the files of backups beyond the newest Keep validated
// entries. The just-generated artifact is never pruned, and the registry
// stays append-only: pruned rows remain as history and never surface
// through List. Retention qualification requires BOTH verification markers
// (C3): a one-marker backup has not passed shadow verification and never
// counts toward the validated keep window. Every deletion candidate is
// re-canonicalized inside the storage root (C1/K2): out-of-root, symlink,
// non-regular or wrong-mode candidates are never deleted; they are skipped
// and recorded as a bounded, sanitized audit entry.
func (s *Service) prune(ctx context.Context, actor *auth.Actor, current uuid.UUID) error {
	scope, err := s.beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = scope.Rollback(ctx) }()
	list, err := scope.Backups().List(ctx)
	if err != nil {
		return err
	}
	_ = scope.Rollback(ctx)

	keep := make(map[uuid.UUID]bool, s.cfg.Keep+1)
	keep[current] = true // the current artifact must never be pruned
	validated := 0
	for _, b := range list {
		if b.VerifiedAt == nil || b.RestoreVerifiedAt == nil {
			continue
		}
		if validated >= s.cfg.Keep {
			continue
		}
		validated++
		keep[b.ID] = true
	}
	var skipped []string
	for _, b := range list {
		if keep[b.ID] {
			continue
		}
		if err := s.removeContained(&b); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue // already gone; nothing to delete
			}
			skipped = append(skipped, sanitizeReason(err))
			continue
		}
	}
	if len(skipped) > 0 {
		reason := strings.Join(skipped, "; ")
		if len(reason) > 500 {
			reason = reason[len(reason)-500:]
		}
		_ = s.audit(ctx, actor, current, "backup.prune.skip", map[string]any{
			"result": "skipped", "reason": reason,
		})
	}
	return nil
}

// removeContained deletes one prune candidate only after re-validating it,
// so a registry path can never delete anything outside the storage root
// (C1/K2): the path must stay inside the root (no traversal, no absolute
// escape, no symlink) and the file must still be a regular 0600 file whose
// size and sha256 match the registry row.
func (s *Service) removeContained(b *backup.Backup) error {
	if err := securePathInDir(s.cfg.Dir, b.Path); err != nil {
		return err
	}
	if err := s.verifyRegistryFile(b); err != nil {
		return err
	}
	return removeFile(b.Path)
}

func removeFile(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("backup: prune %s: %w", path, err)
	}
	return nil
}

// verifyRegistryFile fails closed unless the registry entry's file exists,
// is a regular file with mode 0600, and its size and sha256 match the
// registry exactly.
func (s *Service) verifyRegistryFile(b *backup.Backup) error {
	info, err := os.Stat(b.Path)
	if err != nil {
		return fmt.Errorf("backup file unavailable: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("backup file is not a regular file")
	}
	if info.Mode().Perm() != 0o600 {
		return fmt.Errorf("backup file mode = %o, want 600", info.Mode().Perm())
	}
	if info.Size() != b.Bytes {
		return fmt.Errorf("backup file size mismatch: registry %d, disk %d", b.Bytes, info.Size())
	}
	got, err := HashFileSHA256(b.Path)
	if err != nil {
		return fmt.Errorf("backup file hash unavailable: %w", err)
	}
	if !strings.EqualFold(got, b.SHA256) {
		return errors.New("backup file hash mismatch")
	}
	return nil
}

// writeBackupAudit appends an immutable audit entry for a backup event.
func writeBackupAudit(ctx context.Context, log tx.AuditLogRepository, actor *auth.Actor, resourceID *uuid.UUID, action string, details map[string]any) error {
	raw, err := json.Marshal(details)
	if err != nil {
		return err
	}
	actorID := actor.UserID
	entry := &tx.AuditLogEntry{
		ID: uuid.New(), ActorID: &actorID, Action: action,
		ResourceType: "backup", ResourceID: resourceID, Details: raw,
		OccurredAt: time.Now().UTC(),
	}
	return log.Create(ctx, entry)
}

// shortID returns an 8-hex-character random suffix for unique names.
func shortID() string {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return hex.EncodeToString([]byte(time.Now().UTC().Format("150405.000000")))
	}
	return hex.EncodeToString(buf[:])
}

// tail bounds error output to a readable size.
func tail(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 500 {
		return s[len(s)-500:]
	}
	return s
}

// sanitizeReason bounds an error message for audit storage; no secrets are
// ever expected here (passwords travel only in the process environment) but
// the size is still capped.
func sanitizeReason(err error) string {
	s := err.Error()
	if len(s) > 500 {
		return s[len(s)-500:]
	}
	return s
}
