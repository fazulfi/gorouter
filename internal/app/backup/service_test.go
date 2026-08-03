package backup

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gorouter/internal/app/tx"
	"gorouter/internal/domain/auth"
	"gorouter/internal/domain/backup"

	"github.com/google/uuid"
)

// fakeBackupRepo is the in-memory registry used by the fake scope.
type fakeBackupRepo struct {
	mu        sync.Mutex
	entries   map[uuid.UUID]backup.Backup
	order     []uuid.UUID
	listErr   error
	createErr error
	updateErr error
}

func newFakeBackupRepo() *fakeBackupRepo {
	return &fakeBackupRepo{entries: map[uuid.UUID]backup.Backup{}}
}

func (f *fakeBackupRepo) Create(_ context.Context, b *backup.Backup) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return f.createErr
	}
	f.entries[b.ID] = *b
	f.order = append([]uuid.UUID{b.ID}, f.order...)
	return nil
}

func (f *fakeBackupRepo) List(_ context.Context) ([]backup.Backup, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]backup.Backup, 0, len(f.order))
	for _, id := range f.order {
		out = append(out, f.entries[id])
	}
	return out, nil
}

func (f *fakeBackupRepo) FindByID(_ context.Context, id uuid.UUID) (*backup.Backup, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.entries[id]
	if !ok {
		return nil, backup.ErrBackupNotFound
	}
	cp := b
	return &cp, nil
}

func (f *fakeBackupRepo) UpdateVerification(_ context.Context, id uuid.UUID, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.updateErr != nil {
		return f.updateErr
	}
	b, ok := f.entries[id]
	if !ok {
		return backup.ErrBackupNotFound
	}
	b.VerifiedAt = &at
	f.entries[id] = b
	return nil
}

func (f *fakeBackupRepo) UpdateRestoreVerification(_ context.Context, id uuid.UUID, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.updateErr != nil {
		return f.updateErr
	}
	b, ok := f.entries[id]
	if !ok {
		return backup.ErrBackupNotFound
	}
	b.RestoreVerifiedAt = &at
	f.entries[id] = b
	return nil
}

// fakeBackupScope mirrors the transaction contract: writes stage and become
// visible on Commit.
type fakeBackupScope struct {
	backing   *fakeBackupRepo
	audit     *fakeAuditLog
	mu        sync.Mutex
	commits   int
	rollbacks int
	commitErr error
}

func newFakeBackupScope() *fakeBackupScope {
	return &fakeBackupScope{backing: newFakeBackupRepo(), audit: &fakeAuditLog{}}
}

func (s *fakeBackupScope) Backups() backup.BackupRepository { return s.backing }
func (s *fakeBackupScope) AuditLog() tx.AuditLogRepository  { return s.audit }
func (s *fakeBackupScope) Commit(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commits++
	return s.commitErr
}
func (s *fakeBackupScope) Rollback(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rollbacks++
	return nil
}

var _ BackupScope = (*fakeBackupScope)(nil)

type fakeAuditLog struct {
	mu      sync.Mutex
	entries []tx.AuditLogEntry
	err     error
}

func (f *fakeAuditLog) Create(_ context.Context, entry *tx.AuditLogEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	cp := *entry
	f.entries = append(f.entries, cp)
	return nil
}

// recordingRunner captures argv and env and returns a scripted result. When
// the argv carries a --file= path (pg_dump), it materializes the dump body
// there so the service can stat and hash it.
type recordingRunner struct {
	mu      sync.Mutex
	calls   []string
	envs    []string
	err     error
	stderr  string
	body    []byte
	created []string
}

func (r *recordingRunner) Run(_ context.Context, name string, argv []string, env []string) ([]byte, []byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, name+" "+strings.Join(argv, " "))
	r.envs = append(r.envs, strings.Join(env, "|"))
	for _, a := range argv {
		if strings.HasPrefix(a, "--file=") {
			path := strings.TrimPrefix(a, "--file=")
			_ = os.WriteFile(path, r.body, 0o600)
			r.created = append(r.created, path)
		}
	}
	return nil, []byte(r.stderr), r.err
}

func (r *recordingRunner) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

// fakeShadowManager substitutes the disposable shadow database: success by
// default, scripted error when set.
type fakeShadowManager struct {
	err error
}

func (f *fakeShadowManager) VerifyShadow(context.Context, *backup.Backup) error { return f.err }

func newTestService(t *testing.T, cfg Config, scope *fakeBackupScope, runner CommandRunner, shadows ShadowDBManager) (*Service, *fakeBackupScope) {
	t.Helper()
	if scope == nil {
		scope = newFakeBackupScope()
	}
	if runner == nil {
		runner = &recordingRunner{}
	}
	if shadows == nil {
		shadows = &fakeShadowManager{}
	}
	svc := NewService(cfg, BackupScopeBeginnerFunc(func(context.Context) (BackupScope, error) {
		return scope, nil
	}), runner, shadows)
	return svc, scope
}

func actor() *auth.Actor { return &auth.Actor{UserID: uuid.New(), IsAdmin: true} }

func nonAdminActor() *auth.Actor { return &auth.Actor{UserID: uuid.New(), IsAdmin: false} }

// writeDummyBackup creates a real file and a registry entry for it.
func writeDummyBackup(t *testing.T, dir string, scope *fakeBackupScope) (*backup.Backup, string) {
	t.Helper()
	return writeDummyBackupAt(t, dir, scope, time.Now().UTC(), false)
}

func writeDummyBackupAt(t *testing.T, dir string, scope *fakeBackupScope, createdAt time.Time, validated bool) (*backup.Backup, string) {
	t.Helper()
	path := filepath.Join(dir, "seed-"+uuid.NewString()[:8]+".dump")
	body := []byte("seed dump content " + uuid.NewString())
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	sha, err := HashFileSHA256(path)
	if err != nil {
		t.Fatal(err)
	}
	generatedBy := "test"
	b := &backup.Backup{ID: uuid.New(), Path: path, SHA256: sha, Bytes: int64(len(body)),
		GeneratedBy: &generatedBy, CreatedAt: &createdAt}
	if validated {
		at := createdAt.Add(time.Minute)
		b.VerifiedAt = &at
		b.RestoreVerifiedAt = &at
	}
	if err := scope.backing.Create(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	return b, sha
}

func TestBackupService_Generate(t *testing.T) {
	t.Parallel()

	t.Run("actor required", func(t *testing.T) {
		svc, _ := newTestService(t, Config{Dir: t.TempDir(), DatabaseURL: "postgres://g:p@127.0.0.1:5432/db"}, nil, nil, nil)
		if _, err := svc.Generate(context.Background(), nil); !errors.Is(err, ErrBackupActorRequired) {
			t.Errorf("err = %v, want ErrBackupActorRequired", err)
		}
	})

	t.Run("dump runs without shell, minimal env, registry and audit recorded, file 0600", func(t *testing.T) {
		dir := t.TempDir()
		_ = os.Chmod(dir, 0o700)
		runner := &recordingRunner{body: []byte("dump-bytes")}
		svc, scope := newTestService(t, Config{Dir: dir, DatabaseURL: "postgres://gorouter:secret@127.0.0.1:5442/gorouter"}, nil, runner, nil)
		b, err := svc.Generate(context.Background(), actor())
		if err != nil {
			t.Fatal(err)
		}
		if runner.callCount() != 1 || !strings.Contains(runner.calls[0], "pg_dump") {
			t.Fatalf("calls = %v", runner.calls)
		}
		call := runner.calls[0]
		if strings.Contains(call, "|") || strings.Contains(call, "&&") || strings.Contains(call, ";") || strings.Contains(call, "$(") {
			t.Errorf("dump must be argv-only, no shell metacharacters: %s", call)
		}
		if !strings.Contains(call, "--format=custom") || !strings.Contains(call, "--no-owner") {
			t.Errorf("dump args = %s", call)
		}
		env := runner.envs[0]
		for _, want := range []string{"PGHOST=127.0.0.1", "PGPORT=5442", "PGUSER=gorouter", "PGDATABASE=gorouter", "PGPASSWORD=secret"} {
			if !strings.Contains(env, want) {
				t.Errorf("env missing %s: %s", want, env)
			}
		}
		if strings.Contains(call, "secret") {
			t.Error("password must never appear in argv")
		}
		if b.Path == "" || b.SHA256 == "" || b.Bytes == 0 {
			t.Errorf("registry entry = %+v", b)
		}
		info, err := os.Stat(b.Path)
		if err != nil {
			t.Fatalf("dump file missing: %v", err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("dump file mode = %o, want 600", info.Mode().Perm())
		}
		dirInfo, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if dirInfo.Mode().Perm() != 0o700 {
			t.Errorf("storage dir mode = %o, want 700", dirInfo.Mode().Perm())
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 {
			t.Errorf("temporary dump not cleaned up; files = %v", entries)
		}
		if len(scope.audit.entries) != 1 || scope.audit.entries[0].Action != "backup.generate" {
			t.Errorf("audit = %+v", scope.audit.entries)
		}
		if scope.commits == 0 {
			t.Error("no commit recorded")
		}
	})

	t.Run("dump failure removes nothing and returns error", func(t *testing.T) {
		dir := t.TempDir()
		runner := &recordingRunner{err: errors.New("pg_dump failed"), stderr: "connection refused"}
		svc, scope := newTestService(t, Config{Dir: dir, DatabaseURL: "postgres://g:p@127.0.0.1:5432/db"}, nil, runner, nil)
		if _, err := svc.Generate(context.Background(), actor()); err == nil {
			t.Fatal("expected dump failure")
		}
		if len(scope.audit.entries) != 0 {
			t.Error("failed dump must not be audited")
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Errorf("partial dump not cleaned up: %v", entries)
		}
	})

	t.Run("registry failure removes the partial file", func(t *testing.T) {
		dir := t.TempDir()
		scope := newFakeBackupScope()
		scope.backing.createErr = errors.New("registry down")
		runner := &recordingRunner{body: []byte("x")}
		svc, _ := newTestService(t, Config{Dir: dir, DatabaseURL: "postgres://g:p@127.0.0.1:5432/db"}, scope, runner, nil)
		if _, err := svc.Generate(context.Background(), actor()); err == nil {
			t.Fatal("expected registry failure")
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Errorf("partial dump not cleaned up: %v", entries)
		}
	})

	t.Run("commit failure surfaces the transaction error", func(t *testing.T) {
		dir := t.TempDir()
		scope := newFakeBackupScope()
		scope.commitErr = errors.New("commit failed")
		runner := &recordingRunner{body: []byte("x")}
		svc, _ := newTestService(t, Config{Dir: dir, DatabaseURL: "postgres://g:p@127.0.0.1:5432/db"}, scope, runner, nil)
		if _, err := svc.Generate(context.Background(), actor()); err == nil {
			t.Fatal("expected commit failure")
		}
	})

	t.Run("non-loopback database refused", func(t *testing.T) {
		svc, _ := newTestService(t, Config{Dir: t.TempDir(), DatabaseURL: "postgres://g:p@10.0.0.5:5432/db"}, nil, nil, nil)
		if _, err := svc.Generate(context.Background(), actor()); err == nil || !strings.Contains(err.Error(), "loopback") {
			t.Errorf("err = %v, want loopback refusal", err)
		}
	})

	t.Run("daily semantics refuse a second generation in the same UTC day", func(t *testing.T) {
		dir := t.TempDir()
		scope := newFakeBackupScope()
		writeDummyBackupAt(t, dir, scope, time.Now().UTC().Add(-time.Hour), false)
		svc, _ := newTestService(t, Config{Dir: dir, DatabaseURL: "postgres://g:p@127.0.0.1:5432/db"}, scope, &recordingRunner{body: []byte("x")}, nil)
		if _, err := svc.Generate(context.Background(), actor()); !errors.Is(err, ErrBackupDailyLimit) {
			t.Errorf("err = %v, want ErrBackupDailyLimit", err)
		}
	})

	t.Run("daily semantics allow a generation on the next UTC day", func(t *testing.T) {
		dir := t.TempDir()
		scope := newFakeBackupScope()
		writeDummyBackupAt(t, dir, scope, time.Now().UTC().Add(-24*time.Hour), false)
		svc, _ := newTestService(t, Config{Dir: dir, DatabaseURL: "postgres://g:p@127.0.0.1:5432/db"}, scope, &recordingRunner{body: []byte("x")}, nil)
		if _, err := svc.Generate(context.Background(), actor()); err != nil {
			t.Errorf("err = %v, want success on next day", err)
		}
	})

	t.Run("retention keeps newest validated backups and the current artifact", func(t *testing.T) {
		dir := t.TempDir()
		scope := newFakeBackupScope()
		now := time.Now().UTC()
		writeDummyBackupAt(t, dir, scope, now.Add(-96*time.Hour), true)
		writeDummyBackupAt(t, dir, scope, now.Add(-72*time.Hour), false)
		writeDummyBackupAt(t, dir, scope, now.Add(-48*time.Hour), true)
		writeDummyBackupAt(t, dir, scope, now.Add(-24*time.Hour), true)
		runner := &recordingRunner{body: []byte("new")}
		svc, _ := newTestService(t, Config{Dir: dir, Keep: 2, DatabaseURL: "postgres://g:p@127.0.0.1:5432/db"}, scope, runner, nil)
		b, err := svc.Generate(context.Background(), actor())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(b.Path); err != nil {
			t.Errorf("current artifact must be retained: %v", err)
		}
		files := 0
		_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				files++
			}
			return nil
		})
		if files != 3 {
			t.Errorf("retained files = %d, want 3 (2 validated + current)", files)
		}
		entries, _ := scope.backing.List(context.Background())
		if len(entries) != 5 {
			t.Errorf("registry entries = %d, want 5 (append-only registry)", len(entries))
		}
	})
}

func TestBackupService_ListAndDownload(t *testing.T) {
	t.Parallel()

	t.Run("list requires an actor", func(t *testing.T) {
		svc, _ := newTestService(t, Config{Dir: t.TempDir()}, nil, nil, nil)
		if _, err := svc.List(context.Background(), nil); !errors.Is(err, ErrBackupActorRequired) {
			t.Errorf("err = %v, want ErrBackupActorRequired", err)
		}
	})

	t.Run("list returns only backups whose file exists", func(t *testing.T) {
		dir := t.TempDir()
		scope := newFakeBackupScope()
		b, _ := writeDummyBackup(t, dir, scope)
		gHost, gSha := "ghost", "ghost-sha"
		gPath := filepath.Join(dir, "ghost.dump")
		createdAt := time.Now().UTC()
		_ = scope.backing.Create(context.Background(), &backup.Backup{
			ID: uuid.New(), Path: gPath, SHA256: gSha, Bytes: 1, GeneratedBy: &gHost, CreatedAt: &createdAt,
		})
		svc, _ := newTestService(t, Config{Dir: dir, DatabaseURL: "postgres://g:p@127.0.0.1:5432/db"}, scope, nil, nil)
		list, err := svc.List(context.Background(), actor())
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 1 || list[0].ID != b.ID {
			t.Errorf("list = %+v, want only the existing file", list)
		}
	})

	t.Run("download audits actor ip backup id sha256", func(t *testing.T) {
		dir := t.TempDir()
		scope := newFakeBackupScope()
		b, sha := writeDummyBackup(t, dir, scope)
		svc, _ := newTestService(t, Config{Dir: dir, DatabaseURL: "postgres://g:p@127.0.0.1:5432/db"}, scope, nil, nil)
		act := actor()
		res, err := svc.Download(context.Background(), act, net.ParseIP("192.168.1.10"), b.ID)
		if err != nil {
			t.Fatal(err)
		}
		if res.Path != b.Path || res.SHA256 != sha || res.Bytes != b.Bytes {
			t.Errorf("result = %+v", res)
		}
		if len(scope.audit.entries) != 1 {
			t.Fatalf("audit = %d entries, want 1", len(scope.audit.entries))
		}
		e := scope.audit.entries[0]
		if e.Action != "backup.download" || e.IPAddress == nil || !e.IPAddress.Equal(net.ParseIP("192.168.1.10")) {
			t.Errorf("download audit = %+v", e)
		}
		details := string(e.Details)
		if !strings.Contains(details, b.ID.String()) || !strings.Contains(details, sha) {
			t.Errorf("download audit details missing backup_id/sha256: %s", details)
		}
	})

	t.Run("download requires an actor", func(t *testing.T) {
		svc, _ := newTestService(t, Config{Dir: t.TempDir()}, nil, nil, nil)
		if _, err := svc.Download(context.Background(), nil, nil, uuid.New()); !errors.Is(err, ErrBackupActorRequired) {
			t.Errorf("err = %v, want ErrBackupActorRequired", err)
		}
	})

	t.Run("download of a missing file fails closed", func(t *testing.T) {
		dir := t.TempDir()
		scope := newFakeBackupScope()
		createdAt := time.Now().UTC()
		generatedBy := "x"
		b := &backup.Backup{
			ID: uuid.New(), Path: filepath.Join(dir, "nope.dump"), SHA256: "aabb", Bytes: 5,
			GeneratedBy: &generatedBy, CreatedAt: &createdAt,
		}
		_ = scope.backing.Create(context.Background(), b)
		svc, _ := newTestService(t, Config{Dir: dir, DatabaseURL: "postgres://g:p@127.0.0.1:5432/db"}, scope, nil, nil)
		if _, err := svc.Download(context.Background(), actor(), nil, b.ID); err == nil ||
			!strings.Contains(err.Error(), "file unavailable") {
			t.Errorf("err = %v, want fail-closed missing file", err)
		}
		if len(scope.audit.entries) != 0 {
			t.Error("failed download must not be audited")
		}
	})

	t.Run("download rejects a tampered file via hash mismatch", func(t *testing.T) {
		dir := t.TempDir()
		scope := newFakeBackupScope()
		b, _ := writeDummyBackup(t, dir, scope)
		if err := os.WriteFile(b.Path, []byte(strings.Repeat("X", int(b.Bytes))), 0o600); err != nil {
			t.Fatal(err)
		}
		svc, _ := newTestService(t, Config{Dir: dir, DatabaseURL: "postgres://g:p@127.0.0.1:5432/db"}, scope, nil, nil)
		if _, err := svc.Download(context.Background(), actor(), nil, b.ID); err == nil ||
			!strings.Contains(err.Error(), "hash mismatch") {
			t.Errorf("err = %v, want hash mismatch", err)
		}
		if len(scope.audit.entries) != 0 {
			t.Error("failed download must not be audited")
		}
	})

	t.Run("download rejects a world-readable backup file", func(t *testing.T) {
		dir := t.TempDir()
		scope := newFakeBackupScope()
		b, _ := writeDummyBackup(t, dir, scope)
		if err := os.Chmod(b.Path, 0o644); err != nil {
			t.Fatal(err)
		}
		svc, _ := newTestService(t, Config{Dir: dir, DatabaseURL: "postgres://g:p@127.0.0.1:5432/db"}, scope, nil, nil)
		if _, err := svc.Download(context.Background(), actor(), nil, b.ID); err == nil ||
			!strings.Contains(err.Error(), "want 600") {
			t.Errorf("err = %v, want mode-0600 refusal", err)
		}
	})

	t.Run("download rejects a path that escapes the storage root", func(t *testing.T) {
		dir := t.TempDir()
		outside := t.TempDir()
		escapePath := filepath.Join(outside, "escape.dump")
		if err := os.WriteFile(escapePath, []byte("escaped"), 0o600); err != nil {
			t.Fatal(err)
		}
		scope := newFakeBackupScope()
		createdAt := time.Now().UTC()
		generatedBy := "x"
		b := &backup.Backup{
			ID: uuid.New(), Path: escapePath, SHA256: "aa", Bytes: 7,
			GeneratedBy: &generatedBy, CreatedAt: &createdAt,
		}
		_ = scope.backing.Create(context.Background(), b)
		svc, _ := newTestService(t, Config{Dir: dir, DatabaseURL: "postgres://g:p@127.0.0.1:5432/db"}, scope, nil, nil)
		if _, err := svc.Download(context.Background(), actor(), nil, b.ID); !errors.Is(err, ErrBackupPathEscape) {
			t.Errorf("err = %v, want ErrBackupPathEscape", err)
		}
		if len(scope.audit.entries) != 0 {
			t.Error("failed download must not be audited")
		}
	})

	t.Run("download rejects a symlink inside the storage root", func(t *testing.T) {
		dir := t.TempDir()
		target := t.TempDir()
		realFile := filepath.Join(target, "outside.dump")
		if err := os.WriteFile(realFile, []byte("symlinked"), 0o600); err != nil {
			t.Fatal(err)
		}
		linkPath := filepath.Join(dir, "link.dump")
		if err := os.Symlink(realFile, linkPath); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		scope := newFakeBackupScope()
		createdAt := time.Now().UTC()
		generatedBy := "x"
		b := &backup.Backup{
			ID: uuid.New(), Path: linkPath, SHA256: "bb", Bytes: 9,
			GeneratedBy: &generatedBy, CreatedAt: &createdAt,
		}
		_ = scope.backing.Create(context.Background(), b)
		svc, _ := newTestService(t, Config{Dir: dir, DatabaseURL: "postgres://g:p@127.0.0.1:5432/db"}, scope, nil, nil)
		if _, err := svc.Download(context.Background(), actor(), nil, b.ID); !errors.Is(err, ErrBackupPathEscape) {
			t.Errorf("err = %v, want ErrBackupPathEscape for symlink", err)
		}
		if len(scope.audit.entries) != 0 {
			t.Error("failed download must not be audited")
		}
	})

	t.Run("download rejects a registry path with traversal segments", func(t *testing.T) {
		dir := t.TempDir()
		scope := newFakeBackupScope()
		createdAt := time.Now().UTC()
		generatedBy := "x"
		b := &backup.Backup{
			ID: uuid.New(), Path: filepath.Join(dir, "..", "esc.dump"), SHA256: "cc", Bytes: 1,
			GeneratedBy: &generatedBy, CreatedAt: &createdAt,
		}
		_ = scope.backing.Create(context.Background(), b)
		svc, _ := newTestService(t, Config{Dir: dir, DatabaseURL: "postgres://g:p@127.0.0.1:5432/db"}, scope, nil, nil)
		if _, err := svc.Download(context.Background(), actor(), nil, b.ID); !errors.Is(err, ErrBackupPathEscape) {
			t.Errorf("err = %v, want ErrBackupPathEscape for traversal", err)
		}
	})
}

func TestBackupService_Restore(t *testing.T) {
	t.Parallel()

	t.Run("actor required", func(t *testing.T) {
		svc, _ := newTestService(t, Config{Dir: t.TempDir(), DatabaseURL: "postgres://g:p@127.0.0.1:5432/db"}, nil, &recordingRunner{}, nil)
		if err := svc.Restore(context.Background(), nil, uuid.New(), RestoreConfirmation{}); !errors.Is(err, ErrBackupActorRequired) {
			t.Errorf("err = %v, want ErrBackupActorRequired", err)
		}
	})

	t.Run("non-admin actor is refused as local CLI only", func(t *testing.T) {
		dir := t.TempDir()
		scope := newFakeBackupScope()
		b, sha := writeDummyBackup(t, dir, scope)
		runner := &recordingRunner{}
		svc, _ := newTestService(t, Config{Dir: dir, DatabaseURL: "postgres://g:p@127.0.0.1:5432/db"}, scope, runner, nil)
		err := svc.Restore(context.Background(), nonAdminActor(), b.ID, RestoreConfirmation{
			SHA256Prefix: sha[:8], AcknowledgeDestructive: true,
		})
		if !errors.Is(err, ErrRestoreLocalCLIOnly) {
			t.Errorf("err = %v, want ErrRestoreLocalCLIOnly", err)
		}
		if runner.callCount() != 0 {
			t.Errorf("restore ran %d times for a refused actor", runner.callCount())
		}
		if len(scope.audit.entries) != 0 {
			t.Error("refused restore must not be audited as accept or decline")
		}
	})

	t.Run("declined confirmation audits decline and never runs restore", func(t *testing.T) {
		dir := t.TempDir()
		scope := newFakeBackupScope()
		b, sha := writeDummyBackup(t, dir, scope)
		runner := &recordingRunner{}
		svc, _ := newTestService(t, Config{Dir: dir, DatabaseURL: "postgres://g:p@127.0.0.1:5432/db"}, scope, runner, nil)
		act := actor()
		for name, confirmation := range map[string]RestoreConfirmation{
			"wrong prefix": {SHA256Prefix: "deadbeef", AcknowledgeDestructive: true},
			"no flag":      {SHA256Prefix: sha[:8], AcknowledgeDestructive: false},
			"empty":        {},
		} {
			if err := svc.Restore(context.Background(), act, b.ID, confirmation); !errors.Is(err, ErrRestoreDeclined) {
				t.Errorf("%s: err = %v, want ErrRestoreDeclined", name, err)
			}
		}
		if runner.callCount() != 0 {
			t.Errorf("restore ran %d times despite declines", runner.callCount())
		}
		declines := 0
		for _, e := range scope.audit.entries {
			if e.Action == "backup.restore.decline" {
				declines++
			}
		}
		if declines != 3 {
			t.Errorf("decline audits = %d, want 3", declines)
		}
	})

	t.Run("unverified backup with valid confirmation is refused before restore", func(t *testing.T) {
		dir := t.TempDir()
		scope := newFakeBackupScope()
		b, sha := writeDummyBackup(t, dir, scope)
		runner := &recordingRunner{}
		svc, _ := newTestService(t, Config{Dir: dir, DatabaseURL: "postgres://g:p@127.0.0.1:5432/db"}, scope, runner, nil)
		act := actor()
		err := svc.Restore(context.Background(), act, b.ID, RestoreConfirmation{
			SHA256Prefix: sha[:8], AcknowledgeDestructive: true,
		})
		if err == nil || !strings.Contains(err.Error(), "not passed verification") {
			t.Errorf("err = %v, want pre-restore verification refusal", err)
		}
		if runner.callCount() != 0 {
			t.Errorf("restore ran %d times despite failed readiness", runner.callCount())
		}
		declines := 0
		for _, e := range scope.audit.entries {
			if e.Action == "backup.restore.decline" {
				declines++
			}
		}
		if declines != 1 {
			t.Errorf("readiness refusal audits = %d, want 1 decline", declines)
		}
	})

	t.Run("accepted confirmation audits accept then restores destructively", func(t *testing.T) {
		dir := t.TempDir()
		scope := newFakeBackupScope()
		b, sha := writeDummyBackup(t, dir, scope)
		now := time.Now().UTC()
		if err := scope.backing.UpdateVerification(context.Background(), b.ID, now); err != nil {
			t.Fatal(err)
		}
		if err := scope.backing.UpdateRestoreVerification(context.Background(), b.ID, now); err != nil {
			t.Fatal(err)
		}
		runner := &recordingRunner{}
		svc, _ := newTestService(t, Config{Dir: dir, DatabaseURL: "postgres://g:p@127.0.0.1:5432/db"}, scope, runner, nil)
		act := actor()
		if err := svc.Restore(context.Background(), act, b.ID, RestoreConfirmation{
			SHA256Prefix: strings.ToUpper(sha[:8]), AcknowledgeDestructive: true,
		}); err != nil {
			t.Fatal(err)
		}
		if runner.callCount() != 1 || !strings.Contains(runner.calls[0], "pg_restore") {
			t.Fatalf("calls = %v", runner.calls)
		}
		call := runner.calls[0]
		for _, want := range []string{"--clean", "--if-exists", "--no-owner"} {
			if !strings.Contains(call, want) {
				t.Errorf("restore args missing %s: %s", want, call)
			}
		}
		accepts := 0
		for _, e := range scope.audit.entries {
			if e.Action == "backup.restore.accept" {
				accepts++
				if !strings.Contains(string(e.Details), sha[:8]) {
					t.Errorf("accept audit missing sha prefix: %s", e.Details)
				}
			}
		}
		if accepts != 1 {
			t.Errorf("accept audits = %d, want 1", accepts)
		}
	})

	t.Run("restore command failure returns an error", func(t *testing.T) {
		dir := t.TempDir()
		scope := newFakeBackupScope()
		b, sha := writeDummyBackup(t, dir, scope)
		now := time.Now().UTC()
		_ = scope.backing.UpdateVerification(context.Background(), b.ID, now)
		_ = scope.backing.UpdateRestoreVerification(context.Background(), b.ID, now)
		runner := &recordingRunner{err: errors.New("pg_restore failed"), stderr: "syntax error"}
		svc, _ := newTestService(t, Config{Dir: dir, DatabaseURL: "postgres://g:p@127.0.0.1:5432/db"}, scope, runner, nil)
		err := svc.Restore(context.Background(), actor(), b.ID, RestoreConfirmation{
			SHA256Prefix: sha[:8], AcknowledgeDestructive: true,
		})
		if err == nil || !strings.Contains(err.Error(), "restore failed") {
			t.Errorf("err = %v, want restore failure", err)
		}
	})
}

func TestBackupService_Verify(t *testing.T) {
	t.Parallel()

	t.Run("actor required", func(t *testing.T) {
		svc, _ := newTestService(t, Config{Dir: t.TempDir(), DatabaseURL: "postgres://g:p@127.0.0.1:5432/db"}, nil, nil, nil)
		if err := svc.Verify(context.Background(), nil, uuid.New()); !errors.Is(err, ErrBackupActorRequired) {
			t.Errorf("err = %v, want ErrBackupActorRequired", err)
		}
	})

	t.Run("missing backup fails", func(t *testing.T) {
		svc, _ := newTestService(t, Config{Dir: t.TempDir(), DatabaseURL: "postgres://g:p@127.0.0.1:5432/db"}, nil, nil, nil)
		if err := svc.Verify(context.Background(), actor(), uuid.New()); !errors.Is(err, backup.ErrBackupNotFound) {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("hash mismatch fails closed with failure audit and no marker", func(t *testing.T) {
		dir := t.TempDir()
		scope := newFakeBackupScope()
		b, _ := writeDummyBackup(t, dir, scope)
		if err := os.WriteFile(b.Path, []byte(strings.Repeat("X", int(b.Bytes))), 0o600); err != nil {
			t.Fatal(err)
		}
		svc, _ := newTestService(t, Config{Dir: dir, DatabaseURL: "postgres://g:p@127.0.0.1:5432/db"}, scope, nil, nil)
		if err := svc.Verify(context.Background(), actor(), b.ID); err == nil ||
			!strings.Contains(err.Error(), "hash mismatch") {
			t.Errorf("err = %v, want hash mismatch", err)
		}
		last := scope.audit.entries[len(scope.audit.entries)-1]
		if last.Action != "backup.verify.restore" || !strings.Contains(string(last.Details), "failed") {
			t.Errorf("failure audit = %+v", last)
		}
		got, _ := scope.backing.FindByID(context.Background(), b.ID)
		if got.VerifiedAt != nil || got.RestoreVerifiedAt != nil {
			t.Error("failed verify must not set any verification marker")
		}
	})

	t.Run("shadow restore failure fails closed with failure audit and no marker", func(t *testing.T) {
		dir := t.TempDir()
		scope := newFakeBackupScope()
		b, _ := writeDummyBackup(t, dir, scope)
		shadows := &fakeShadowManager{err: errors.New("shadow restore failed")}
		svc, _ := newTestService(t, Config{Dir: dir, DatabaseURL: "postgres://g:p@127.0.0.1:5432/db"}, scope, nil, shadows)
		if err := svc.Verify(context.Background(), actor(), b.ID); err == nil {
			t.Fatal("verify must fail when the shadow restore fails")
		}
		last := scope.audit.entries[len(scope.audit.entries)-1]
		if last.Action != "backup.verify.restore" || !strings.Contains(string(last.Details), "failed") {
			t.Errorf("failure audit = %+v", last)
		}
		got, _ := scope.backing.FindByID(context.Background(), b.ID)
		if got.VerifiedAt != nil || got.RestoreVerifiedAt != nil {
			t.Error("failed verify must not set any verification marker")
		}
	})

	t.Run("successful shadow restore records both markers and audits", func(t *testing.T) {
		dir := t.TempDir()
		scope := newFakeBackupScope()
		b, _ := writeDummyBackup(t, dir, scope)
		svc, _ := newTestService(t, Config{Dir: dir, DatabaseURL: "postgres://g:p@127.0.0.1:5432/db"}, scope, nil, nil)
		if err := svc.Verify(context.Background(), actor(), b.ID); err != nil {
			t.Fatal(err)
		}
		got, _ := scope.backing.FindByID(context.Background(), b.ID)
		if got.VerifiedAt == nil || got.RestoreVerifiedAt == nil {
			t.Errorf("verified markers = verified %v, restore %v; want both set", got.VerifiedAt, got.RestoreVerifiedAt)
		}
		last := scope.audit.entries[len(scope.audit.entries)-1]
		if last.Action != "backup.verify.restore" || !strings.Contains(string(last.Details), "verified") {
			t.Errorf("success audit = %+v", last)
		}
	})

	t.Run("marker update failure surfaces without audit", func(t *testing.T) {
		dir := t.TempDir()
		scope := newFakeBackupScope()
		b, _ := writeDummyBackup(t, dir, scope)
		scope.backing.updateErr = errors.New("update failed")
		svc, _ := newTestService(t, Config{Dir: dir, DatabaseURL: "postgres://g:p@127.0.0.1:5432/db"}, scope, nil, nil)
		if err := svc.Verify(context.Background(), actor(), b.ID); err == nil ||
			!strings.Contains(err.Error(), "record verification") {
			t.Errorf("err = %v, want marker update failure", err)
		}
	})
}
