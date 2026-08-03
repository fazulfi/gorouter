package backup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// moduleRoot locates the module root from any package working directory.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("module root not found")
		}
		dir = parent
	}
}

// TestNoHTTPRestoreSurface guards the local-CLI-only restore contract
// (DECISIONS #202, security P1-7): no HTTP transport or entrypoint may call
// the destructive restore. Backup list/download/verify are HTTP-legal; only
// restore must never surface through the server. The CLI (internal/transport/
// cli) is the only allowed caller.
func TestNoHTTPRestoreSurface(t *testing.T) {
	t.Parallel()
	root := moduleRoot(t)
	scanned := 0
	for _, dir := range []string{
		filepath.Join(root, "internal", "transport", "httpserver"),
		filepath.Join(root, "internal", "transport", "middleware"),
		filepath.Join(root, "cmd"),
	} {
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			scanned++
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if strings.Contains(string(content), ".Restore(") {
				t.Errorf("HTTP surface calls destructive restore at %s", path)
			}
			if strings.Contains(string(content), `"gorouter/internal/app/backup"`) {
				t.Errorf("HTTP surface imports the backup service at %s", path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
	if scanned == 0 {
		t.Fatal("no transport sources scanned")
	}
}
