package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestPGEnv(t *testing.T) {
	t.Parallel()

	t.Run("derives minimal libpq env and keeps the password out of argv", func(t *testing.T) {
		env, err := pgEnv("postgres://gorouter:secret@127.0.0.1:5442/gorouter?sslmode=disable")
		if err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(env, "|")
		for _, want := range []string{"PGHOST=127.0.0.1", "PGPORT=5442", "PGUSER=gorouter", "PGDATABASE=gorouter", "PGSSLMODE=prefer", "PGPASSWORD=secret"} {
			if !strings.Contains(joined, want) {
				t.Errorf("env missing %s: %s", want, joined)
			}
		}
		if len(env) != 6 {
			t.Errorf("env entries = %d, want exactly 6 (minimal): %v", len(env), env)
		}
	})

	t.Run("no password entry when the DSN has none", func(t *testing.T) {
		env, err := pgEnv("postgres://gorouter@127.0.0.1:5442/gorouter")
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range env {
			if strings.HasPrefix(e, "PGPASSWORD=") {
				t.Errorf("unexpected password entry: %s", e)
			}
		}
	})

	t.Run("unix socket host passes", func(t *testing.T) {
		if _, err := pgEnv("postgres://g:p@/var/run/postgresql/db?host=/var/run/postgresql"); err != nil {
			t.Errorf("unix socket DSN refused: %v", err)
		}
	})

	t.Run("invalid DSN fails closed", func(t *testing.T) {
		if _, err := pgEnv("not-a-dsn"); err == nil {
			t.Error("invalid DSN must fail closed")
		}
	})
}

func TestPGConfigParseErrorsAreRedacted(t *testing.T) {
	t.Parallel()
	bad := "postgres://user:secret-password@127.0.0.1:notaport/db?sslmode=disable"
	if _, err := pgEnv(bad); !errors.Is(err, ErrBackupConfigInvalid) {
		t.Errorf("pgEnv err = %v, want ErrBackupConfigInvalid", err)
	}
	if _, err := pgDatabaseName(bad); !errors.Is(err, ErrBackupConfigInvalid) {
		t.Errorf("pgDatabaseName err = %v, want ErrBackupConfigInvalid", err)
	}
	if _, err := connectLoopbackDB(context.Background(), bad, ""); !errors.Is(err, ErrBackupConfigInvalid) {
		t.Errorf("connectLoopbackDB err = %v, want ErrBackupConfigInvalid", err)
	}
	_, err := pgEnv(bad)
	if err == nil {
		t.Fatal("expected a parse failure")
	}
	msg := err.Error()
	if strings.Contains(msg, "secret-password") || strings.Contains(msg, "notaport") || strings.Contains(msg, "postgres://") {
		t.Errorf("parse errors must never embed the DSN or its password: %q", msg)
	}
}

func TestLoopbackOnly(t *testing.T) {
	t.Parallel()

	for host, wantErr := range map[string]bool{
		"127.0.0.1":           false,
		"localhost":           false,
		"::1":                 false,
		"/var/run/postgresql": false,
		"10.0.0.5":            true,
		"db.example.com":      true,
		"192.168.1.1":         true,
	} {
		got := loopbackOnly(&pgconn.Config{Host: host})
		if wantErr && got == nil {
			t.Errorf("host %q must be refused", host)
		}
		if !wantErr && got != nil {
			t.Errorf("host %q must pass, got %v", host, got)
		}
	}
}

func TestSetPGDatabase(t *testing.T) {
	t.Parallel()
	env := setPGDatabase([]string{"PGHOST=127.0.0.1", "PGDATABASE=gorouter", "PGUSER=gorouter"}, "shadow_db")
	joined := strings.Join(env, "|")
	if !strings.Contains(joined, "PGDATABASE=shadow_db") {
		t.Errorf("PGDATABASE not replaced: %s", joined)
	}
	if strings.Contains(joined, "PGDATABASE=gorouter|") {
		t.Errorf("old PGDATABASE retained: %s", joined)
	}
	if len(env) != 3 {
		t.Errorf("env entries = %d, want 3: %v", len(env), env)
	}
}

func TestHashFileSHA256Missing(t *testing.T) {
	t.Parallel()
	if _, err := HashFileSHA256(filepath.Join(t.TempDir(), "missing.dump")); err == nil {
		t.Error("missing file must error")
	}
}

func TestFsyncHelpers(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f.dump")
	if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := fsyncFile(path); err != nil {
		t.Errorf("fsyncFile: %v", err)
	}
	if err := fsyncDir(dir); err != nil {
		t.Errorf("fsyncDir: %v", err)
	}
	if err := fsyncFile(filepath.Join(dir, "missing.dump")); err == nil {
		t.Error("fsyncFile on missing file must error")
	}
}

func TestSecurePathInDir(t *testing.T) {
	t.Parallel()

	t.Run("contained path passes", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "ok.dump")
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := securePathInDir(dir, path); err != nil {
			t.Errorf("contained path rejected: %v", err)
		}
	})

	t.Run("path outside dir is refused", func(t *testing.T) {
		dir := t.TempDir()
		outside := filepath.Join(t.TempDir(), "x.dump")
		if err := os.WriteFile(outside, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := securePathInDir(dir, outside); !errors.Is(err, ErrBackupPathEscape) {
			t.Errorf("err = %v, want ErrBackupPathEscape", err)
		}
	})

	t.Run("traversal segments are refused", func(t *testing.T) {
		dir := t.TempDir()
		if err := securePathInDir(dir, filepath.Join(dir, "..", "esc.dump")); !errors.Is(err, ErrBackupPathEscape) {
			t.Errorf("err = %v, want ErrBackupPathEscape", err)
		}
	})

	t.Run("symlink is refused", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(t.TempDir(), "target.dump")
		if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(dir, "link.dump")
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if err := securePathInDir(dir, link); !errors.Is(err, ErrBackupPathEscape) {
			t.Errorf("err = %v, want ErrBackupPathEscape", err)
		}
	})

	t.Run("missing file fails closed", func(t *testing.T) {
		dir := t.TempDir()
		err := securePathInDir(dir, filepath.Join(dir, "missing.dump"))
		if err == nil || !strings.Contains(err.Error(), "unavailable") {
			t.Errorf("err = %v, want unavailable", err)
		}
	})
}

func TestRealExecRunner(t *testing.T) {
	t.Parallel()

	t.Run("runs argv without a shell", func(t *testing.T) {
		r := realExecRunner{}
		out, _, err := r.Run(context.Background(), "true", []string{}, nil)
		if err != nil {
			t.Fatalf("true failed: %v", err)
		}
		if len(out) != 0 {
			t.Errorf("unexpected stdout: %q", out)
		}
	})

	t.Run("missing binary fails closed", func(t *testing.T) {
		r := realExecRunner{}
		if _, _, err := r.Run(context.Background(), "gorouter-no-such-binary-9f3a", []string{}, nil); err == nil {
			t.Error("missing binary must fail closed")
		}
	})
}
