package backup

import (
	"path/filepath"
	"testing"
)

// TestAllowedBackupImportersPinned proves the reviewed allow-list stays
// minimal (fail-closed bootstrap gate + the adminapi/v1 List/Download/Verify
// lane only) and that broader transport paths remain rejected. Any change
// requires review (DECISIONS #202, security P1-7).
func TestAllowedBackupImportersPinned(t *testing.T) {
	t.Parallel()
	want := []string{
		filepath.Join("internal", "bootstrap"),
		filepath.Join("internal", "transport", "httpserver", "adminapi", "v1"),
	}
	if len(allowedBackupImporters) != len(want) {
		t.Fatalf("allowedBackupImporters has %d entries, want %d; broadening requires review", len(allowedBackupImporters), len(want))
	}
	for i, w := range want {
		if allowedBackupImporters[i] != w {
			t.Fatalf("allowedBackupImporters[%d] = %q, want %q; changes require review", i, allowedBackupImporters[i], w)
		}
	}
	// Unrelated transport paths must remain rejected (deny-by-default).
	for _, rejected := range []string{
		filepath.Join("internal", "transport"),
		filepath.Join("internal", "transport", "httpserver"),
		filepath.Join("internal", "transport", "httpserver", "api"),
		filepath.Join("internal", "transport", "httpserver", "adminapi"),
		filepath.Join("cmd", "gorouter"),
	} {
		if underAllowed(rejected, allowedBackupImporters) {
			t.Errorf("path %q must remain outside the reviewed allow-list", rejected)
		}
	}
	// The destructive-restore caller list must remain empty.
	if len(allowedRestoreCallers) != 0 {
		t.Fatalf("allowedRestoreCallers must remain empty until the CH lane lands; got %v", allowedRestoreCallers)
	}
}
