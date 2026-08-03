package backup

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"gorouter/internal/domain/backup"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// fakeShadowConn records shadow-database statements and scripts query
// results, satisfying shadowDBConn.
type fakeShadowConn struct {
	mu       sync.Mutex
	execs    []string
	queryRow func(sql string, args []any) pgx.Row
	err      error
	closed   bool
}

func (f *fakeShadowConn) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.execs = append(f.execs, sql)
	return pgconn.NewCommandTag("OK"), f.err
}

func (f *fakeShadowConn) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.execs = append(f.execs, sql)
	if f.queryRow != nil {
		return f.queryRow(sql, nil)
	}
	switch {
	case strings.Contains(sql, "gorouter_migrations"):
		return &mockShadowRow{vals: []any{int64(9)}}
	case strings.Contains(sql, "gorouter_audit_log"):
		return &mockShadowRow{vals: []any{int64(1)}}
	default:
		name := "public.gorouter_users"
		return &mockShadowRow{vals: []any{name}}
	}
}

func (f *fakeShadowConn) Close(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *fakeShadowConn) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.execs...)
}

type mockShadowRow struct {
	vals []any
	err  error
	pos  int
}

func (m *mockShadowRow) Scan(dest ...any) error {
	if m.err != nil {
		return m.err
	}
	for i, d := range dest {
		switch v := d.(type) {
		case *int:
			*v = int(m.vals[i].(int64))
		case **string:
			if s, ok := m.vals[i].(string); ok {
				*v = &s
			}
		default:
			return errors.New("mockShadowRow: unsupported dest")
		}
	}
	return nil
}

func validShadowBackup(t *testing.T) *backup.Backup {
	t.Helper()
	dir := t.TempDir()
	scope := newFakeBackupScope()
	b, _ := writeDummyBackup(t, dir, scope)
	return b
}

func TestPGShadowManager_VerifyShadow(t *testing.T) {
	t.Parallel()

	t.Run("success restores, validates, and always drops the shadow", func(t *testing.T) {
		f := &fakeShadowConn{}
		runner := &recordingRunner{}
		m := newPGShadowManager("postgres://g:p@127.0.0.1:5432/db", runner)
		m.connect = func(context.Context, string, string) (shadowDBConn, error) { return f, nil }
		b := validShadowBackup(t)
		if err := m.VerifyShadow(context.Background(), b); err != nil {
			t.Fatalf("VerifyShadow: %v", err)
		}
		execs := strings.Join(f.recorded(), "\n")
		if !strings.Contains(execs, "CREATE DATABASE") || !strings.Contains(execs, "DROP DATABASE IF EXISTS") {
			t.Errorf("shadow lifecycle missing: %s", execs)
		}
		if !strings.Contains(execs, "SELECT count(*) FROM gorouter_migrations") ||
			!strings.Contains(execs, "SELECT count(*) FROM gorouter_audit_log") ||
			!strings.Contains(execs, "to_regclass") {
			t.Errorf("shadow validation queries missing: %s", execs)
		}
		if runner.callCount() != 1 || !strings.Contains(runner.calls[0], "pg_restore") {
			t.Errorf("calls = %v", runner.calls)
		}
		if !f.closed {
			t.Error("shadow management connection not closed")
		}
	})

	t.Run("connect failure fails closed", func(t *testing.T) {
		m := newPGShadowManager("postgres://g:p@127.0.0.1:5432/db", &recordingRunner{})
		m.connect = func(context.Context, string, string) (shadowDBConn, error) {
			return nil, errors.New("connect refused")
		}
		b := validShadowBackup(t)
		if err := m.VerifyShadow(context.Background(), b); err == nil {
			t.Fatal("connect failure must fail closed")
		}
	})

	t.Run("create failure returns error", func(t *testing.T) {
		created := false
		m := newPGShadowManager("postgres://g:p@127.0.0.1:5432/db", &recordingRunner{})
		m.connect = func(context.Context, string, string) (shadowDBConn, error) {
			return &failAtCreateConn{created: &created}, nil
		}
		b := validShadowBackup(t)
		if err := m.VerifyShadow(context.Background(), b); err == nil {
			t.Fatal("create failure must fail closed")
		}
		if !created {
			t.Error("CREATE DATABASE was never attempted")
		}
	})

	t.Run("restore failure always drops the shadow", func(t *testing.T) {
		f := &fakeShadowConn{}
		runner := &recordingRunner{err: errors.New("restore failed"), stderr: "syntax error"}
		m := newPGShadowManager("postgres://g:p@127.0.0.1:5432/db", runner)
		m.connect = func(context.Context, string, string) (shadowDBConn, error) { return f, nil }
		b := validShadowBackup(t)
		err := m.VerifyShadow(context.Background(), b)
		if err == nil || !strings.Contains(err.Error(), "shadow restore failed") {
			t.Fatalf("err = %v, want shadow restore failure", err)
		}
		execs := strings.Join(f.recorded(), "\n")
		if !strings.Contains(execs, "DROP DATABASE IF EXISTS") {
			t.Errorf("shadow must be dropped after restore failure: %s", execs)
		}
		if !strings.Contains(execs, "pg_terminate_backend") {
			t.Errorf("shadow backends must be terminated after restore failure: %s", execs)
		}
	})

	t.Run("missing dsn fails closed", func(t *testing.T) {
		m := newPGShadowManager("", &recordingRunner{})
		b := validShadowBackup(t)
		if err := m.VerifyShadow(context.Background(), b); err == nil {
			t.Fatal("empty dsn must fail closed")
		}
	})
}

type failAtCreateConn struct {
	created *bool
}

func (c *failAtCreateConn) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	if strings.Contains(sql, "CREATE DATABASE") {
		*c.created = true
		return pgconn.CommandTag{}, errors.New("create failed")
	}
	return pgconn.NewCommandTag("OK"), nil
}

func (c *failAtCreateConn) QueryRow(context.Context, string, ...any) pgx.Row {
	return &mockShadowRow{err: errors.New("unexpected query")}
}

func (c *failAtCreateConn) Close(context.Context) error { return nil }

func TestValidateShadowDB(t *testing.T) {
	t.Parallel()

	connect := func(f *fakeShadowConn) func(context.Context, string, string) (shadowDBConn, error) {
		return func(context.Context, string, string) (shadowDBConn, error) { return f, nil }
	}

	t.Run("no migrations recorded fails closed", func(t *testing.T) {
		f := &fakeShadowConn{queryRow: func(string, []any) pgx.Row {
			return &mockShadowRow{vals: []any{int64(0)}}
		}}
		err := validateShadowDB(context.Background(), connect(f), "dsn", "shadow")
		if err == nil || !strings.Contains(err.Error(), "no migrations recorded") {
			t.Errorf("err = %v, want no-migrations error", err)
		}
	})

	t.Run("audit read failure fails closed", func(t *testing.T) {
		n := 0
		f := &fakeShadowConn{queryRow: func(string, []any) pgx.Row {
			n++
			if n == 2 {
				return &mockShadowRow{err: errors.New("audit read failed")}
			}
			return &mockShadowRow{vals: []any{int64(9)}}
		}}
		err := validateShadowDB(context.Background(), connect(f), "dsn", "shadow")
		if err == nil || !strings.Contains(err.Error(), "audit read failed") {
			t.Errorf("err = %v, want audit read failure", err)
		}
	})

	t.Run("missing core table fails closed", func(t *testing.T) {
		n := 0
		f := &fakeShadowConn{queryRow: func(string, []any) pgx.Row {
			n++
			if n <= 2 {
				return &mockShadowRow{vals: []any{int64(9)}}
			}
			return &mockShadowRow{vals: []any{(*string)(nil)}}
		}}
		err := validateShadowDB(context.Background(), connect(f), "dsn", "shadow")
		if err == nil || !strings.Contains(err.Error(), "missing") {
			t.Errorf("err = %v, want missing-table error", err)
		}
	})

	t.Run("connect failure fails closed", func(t *testing.T) {
		connectErr := func(context.Context, string, string) (shadowDBConn, error) {
			return nil, errors.New("connect shadow failed")
		}
		if err := validateShadowDB(context.Background(), connectErr, "dsn", "shadow"); err == nil {
			t.Error("connect failure must fail closed")
		}
	})
}

var _ pgx.Row = (*mockShadowRow)(nil)

func TestTailAndSanitizeBoundLongOutput(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 600)
	if got := tail([]byte(long)); len(got) != 500 {
		t.Errorf("tail len = %d, want 500", len(got))
	}
	if got := sanitizeReason(errors.New(long)); len(got) != 500 {
		t.Errorf("sanitizeReason len = %d, want 500", len(got))
	}
}

func TestCommandRunnerFunc(t *testing.T) {
	t.Parallel()
	got := CommandRunnerFunc(func(_ context.Context, name string, _ []string, _ []string) ([]byte, []byte, error) {
		return []byte(name), nil, nil
	})
	out, _, err := got.Run(context.Background(), "x", nil, nil)
	if err != nil || string(out) != "x" {
		t.Errorf("out = %q err = %v", out, err)
	}
}

func TestNewServiceDefaultsShadowManager(t *testing.T) {
	t.Parallel()
	svc := NewService(Config{Dir: t.TempDir(), DatabaseURL: "postgres://g:p@127.0.0.1:5432/db"},
		BackupScopeBeginnerFunc(func(context.Context) (BackupScope, error) { return newFakeBackupScope(), nil }),
		&recordingRunner{}, nil)
	if svc.shadows == nil {
		t.Error("NewService must default the shadow manager")
	}
}
