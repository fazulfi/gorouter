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

// allowedBackupImporters lists the only production packages allowed to
// import the backup service package (for non-destructive operations). The
// fail-closed bootstrap gate and the reviewed adminapi/v1 List/Download/Verify
// lanes are the only importers; further extension requires explicit review
// (DECISIONS #202, security P1-7).
var allowedBackupImporters = []string{
	filepath.Join("internal", "bootstrap"),
	filepath.Join("internal", "transport", "httpserver", "adminapi", "v1"),
}

// allowedRestoreCallers lists the only production packages allowed to invoke
// the destructive restore. It is intentionally EMPTY: no production caller
// may exist until the CH lane lands the local CLI owner; Service.Restore is
// runtime-enforced to require the local-CLI actor (Kind==cli, Origin==local,
// DECISIONS #202, design §6 P2-15).
var allowedRestoreCallers = []string{}

// tripwireSkippedDirs are repository subtrees without production Go sources
// relevant to the restore surface (VCS, vendored, frontend, working-memory
// trees).
var tripwireSkippedDirs = map[string]bool{
	".git": true, "node_modules": true, ".sisyphus": true, ".superpowers": true,
	"playwright-report": true, "dist": true, "build": true, "coverage": true,
	"frontend": true,
}

// tripwireSkippedFiles are non-package Go test files that legitimately
// exercise the backup service end-to-end against real PostgreSQL.
var tripwireSkippedFiles = map[string]bool{
	filepath.Join("internal", "persistence", "postgres", "repositories", "backup_e2e_integration_test.go"): true,
}

func underAllowed(dir string, allowed []string) bool {
	for _, a := range allowed {
		if dir == a || strings.HasPrefix(dir, a+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// TestNoHTTPRestoreSurface guards the local-CLI-only restore contract
// (DECISIONS #202, security P1-7): every production Go source in the
// repository is scanned — including repo-root files (the design §5 embed
// lives at the root), every transport package, cmd, and future job lanes —
// and no file outside the reviewed allow-lists may import the backup service
// or invoke the destructive restore. Runtime Kind/Origin enforcement in
// Service.Restore is the primary control; this tripwire is defense-in-depth
// and fails loudly if the scan surface is narrowed.
func TestNoHTTPRestoreSurface(t *testing.T) {
	t.Parallel()
	root := moduleRoot(t)
	scanned := 0
	scannedDirs := map[string]bool{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if path != root && tripwireSkippedDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if tripwireSkippedFiles[rel] {
			return nil
		}
		backupPkg := filepath.Join("internal", "app", "backup")
		if rel == backupPkg || strings.HasPrefix(rel, backupPkg+string(filepath.Separator)) {
			return nil // the implementation and its own tests
		}
		scanned++
		dir := filepath.Dir(rel)
		scannedDirs[dir] = true
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		importsBackup := strings.Contains(string(content), `"gorouter/internal/app/backup"`)
		if importsBackup && !underAllowed(dir, allowedBackupImporters) {
			t.Errorf("backup service imported outside the reviewed allow-list at %s", rel)
		}
		// A restore call only matters when the file can reach the backup
		// service: require the import alongside the call so unrelated
		// Restore methods (providers/state, engine/routing) are not flagged.
		if importsBackup && strings.Contains(string(content), ".Restore(") && !underAllowed(dir, allowedRestoreCallers) {
			t.Errorf("destructive restore invoked outside the allowed callers at %s", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	if scanned < 30 {
		t.Fatalf("scanned %d production Go files, want >= 30; the scan surface must never be narrowed", scanned)
	}
	for _, must := range []string{
		filepath.Join("internal", "bootstrap"),
		filepath.Join("cmd", "gorouter"),
		filepath.Join("internal", "transport", "httpserver"),
		filepath.Join("internal", "transport", "httpserver", "api"),
	} {
		if !scannedDirs[must] {
			t.Errorf("scan did not reach %s; the scan surface must cover the whole repository", must)
		}
	}
}
