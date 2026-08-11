package recovery

import (
	"context"
	"crypto/sha256"
	pgbackup "gorouter/internal/persistence/postgres/backup"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRestoreDrill(t *testing.T) {
	d := t.TempDir()
	src := filepath.Join(d, "source")
	dst := filepath.Join(d, "restored")
	payload := []byte("same-code-path restore drill")
	if e := os.WriteFile(src, payload, 0600); e != nil {
		t.Fatal(e)
	}
	a, e := pgbackup.Capture(src, "full", "", time.Now())
	if e != nil {
		t.Fatal(e)
	}
	called := false
	runner := pgbackup.CommandRunnerFunc(func(_ context.Context, name string, args, _ []string) ([]byte, []byte, error) {
		called = name == "pg_restore"
		if len(args) == 0 || args[len(args)-1] != a.Path {
			t.Fatalf("unexpected restore args: %v", args)
		}
		if err := os.WriteFile(dst, payload, 0600); err != nil {
			return nil, nil, err
		}
		return nil, nil, nil
	})
	if e := pgbackup.Restore(context.Background(), runner, a, "target", nil); e != nil {
		t.Fatal(e)
	}
	if !called {
		t.Fatal("restore drill bypassed pg_restore runner")
	}
	b, _ := os.ReadFile(dst)
	want := sha256.Sum256(payload)
	got := sha256.Sum256(b)
	if got != want {
		t.Fatal("restored hash mismatch")
	}
}
