package fixtures

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func manifestPath() string {
	return filepath.Join("..", "..", "tests", "fixtures", "upstream", "manifest.json")
}

func tempManifest(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write temp manifest: %v", err)
	}
	return path
}

func TestLoadManifest_Valid(t *testing.T) {
	m, err := LoadManifest(manifestPath())
	if err != nil {
		t.Fatalf("LoadManifest() unexpected error: %v", err)
	}
	if m == nil {
		t.Fatal("LoadManifest() returned nil")
	}
	if m.SchemaVersion != "1.0" {
		t.Errorf("SchemaVersion = %q, want %q", m.SchemaVersion, "1.0")
	}
	if m.Baseline == nil {
		t.Error("Baseline map is nil")
	} else {
		if got := m.Baseline["repository"]; got != "decolua/9router" {
			t.Errorf("Baseline[repository] = %q, want %q", got, "decolua/9router")
		}
		if got := m.Baseline["commit"]; got == "" {
			t.Error("Baseline[commit] is empty")
		}
	}
	if len(m.Fixtures) == 0 {
		t.Fatal("expected at least one fixture entry")
	}
	found := false
	for _, f := range m.Fixtures {
		if f.ID == "custom-server-trusted-proxy" {
			found = true
			if f.Description == "" {
				t.Error("fixture Description is empty")
			}
			break
		}
	}
	if !found {
		t.Errorf("expected fixture entry %q not found", "custom-server-trusted-proxy")
	}
}

func TestLoadManifest_MissingFile(t *testing.T) {
	_, err := LoadManifest(filepath.Join(os.TempDir(), " none-existent-manifest-999.json"))
	if err == nil {
		t.Fatal("LoadManifest() expected error for missing file, got nil")
	}
}

func TestLoadManifest_InvalidJSON(t *testing.T) {
	path := tempManifest(t, "{invalid json content}")
	_, err := LoadManifest(path)
	if err == nil {
		t.Fatal("LoadManifest() expected error for invalid JSON, got nil")
	}
}

func TestLoadManifest_EmptyFile(t *testing.T) {
	path := tempManifest(t, "")
	_, err := LoadManifest(path)
	if err == nil {
		t.Fatal("LoadManifest() expected error for empty file, got nil")
	}
}

func TestLoadManifest_ValidButEmptyFixtures(t *testing.T) {
	path := tempManifest(t, `{"schemaVersion":"1.0","baseline":{},"fixtures":[]}`)
	m, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest() unexpected error: %v", err)
	}
	if m == nil {
		t.Fatal("LoadManifest() returned nil")
	}
	if len(m.Fixtures) != 0 {
		t.Errorf("expected 0 fixtures, got %d", len(m.Fixtures))
	}
}

func TestValidateContract_ExistingEntry(t *testing.T) {
	err := ValidateContract(manifestPath(), "custom-server-trusted-proxy")
	if err != nil {
		t.Errorf("ValidateContract() unexpected error: %v", err)
	}
}

func TestValidateContract_AllExistingEntries(t *testing.T) {
	entries := []string{
		"dashboard-guard-local-only",
		"dashboard-session-jwt",
		"login-limiter",
		"api-key-generation",
	}
	for _, id := range entries {
		if err := ValidateContract(manifestPath(), id); err != nil {
			t.Errorf("ValidateContract(%q) unexpected error: %v", id, err)
		}
	}
}

func TestValidateContract_MissingEntry(t *testing.T) {
	err := ValidateContract(manifestPath(), "non-existent-id")
	if err == nil {
		t.Fatal("ValidateContract() expected error for non-existent entry, got nil")
	}
}

func TestValidateContract_MissingManifest(t *testing.T) {
	err := ValidateContract(filepath.Join(os.TempDir(), " no-such-manifest.json"), "any-id")
	if err == nil {
		t.Fatal("ValidateContract() expected error for missing manifest, got nil")
	}
}

func TestValidateContract_EmptyEntryID(t *testing.T) {
	err := ValidateContract(manifestPath(), "")
	if err == nil {
		t.Fatal("ValidateContract() expected error for empty entry ID, got nil")
	}
}

func TestValidateContract_PropagatesLoadError(t *testing.T) {
	err := ValidateContract("/proc/this-does-not-exist/manifest.json", "x")
	if err == nil {
		t.Fatal("ValidateContract() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "load manifest") {
		t.Errorf("error should wrap 'load manifest', got: %v", err)
	}
}

func TestManifestEntry_AllFixturesHaveIDs(t *testing.T) {
	m, err := LoadManifest(manifestPath())
	if err != nil {
		t.Fatalf("LoadManifest() unexpected error: %v", err)
	}
	for i, f := range m.Fixtures {
		if f.ID == "" {
			t.Errorf("fixtures[%d] has empty ID", i)
		}
	}
}

func TestManifestEntry_UnmarshalsOnlyKnownFields(t *testing.T) {
	m, err := LoadManifest(manifestPath())
	if err != nil {
		t.Fatalf("LoadManifest() unexpected error: %v", err)
	}
	for _, f := range m.Fixtures {
		if f.ID == "" {
			t.Error("expected ID to be populated")
		}
		if f.Source != "" {
			t.Errorf("Source should be zero-valued, got %q", f.Source)
		}
		if f.UpstreamCommit != "" {
			t.Errorf("UpstreamCommit should be zero-valued, got %q", f.UpstreamCommit)
		}
		if f.CapturedAt != "" {
			t.Errorf("CapturedAt should be zero-valued, got %q", f.CapturedAt)
		}
	}
}
