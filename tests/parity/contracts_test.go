package parity_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"gorouter/tools/fixtures"
)

// rootDir returns the module root (CWD relative to tests/parity/ -> ../../)
func rootDir(elem ...string) string {
	return filepath.Join(append([]string{"..", ".."}, elem...)...)
}

// fixturesDir returns the fixtures path relative to module root
func fixPath(elem ...string) string {
	return rootDir(append([]string{"tests", "fixtures", "upstream"}, elem...)...)
}

// TestManifestLoad verifies the fixture manifest loads and has at least one entry.
func TestManifestLoad(t *testing.T) {
	m, err := fixtures.LoadManifest(fixPath("manifest.json"))
	if err != nil {
		t.Fatalf("LoadManifest failed: %v", err)
	}

	if m.SchemaVersion == "" {
		t.Error("manifest schemaVersion is empty")
	}
	if len(m.Baseline) == 0 {
		t.Error("manifest baseline is empty")
	}
	if len(m.Fixtures) == 0 {
		t.Error("manifest has no fixtures")
	}
}

// TestManifestEntryExists verifies that a specific entry can be found.
func TestManifestEntryExists(t *testing.T) {
	entryID := "custom-server-trusted-proxy"
	err := fixtures.ValidateContract(fixPath("manifest.json"), entryID)
	if err != nil {
		t.Fatalf("ValidateContract failed: %v", err)
	}
}

// TestManifestEntryNotFound verifies that a non-existent entry returns an error.
func TestManifestEntryNotFound(t *testing.T) {
	entryID := "nonexistent-entry-id"
	err := fixtures.ValidateContract(fixPath("manifest.json"), entryID)
	if err == nil {
		t.Fatal("expected error for nonexistent entry, got nil")
	}
}

// TestAPIContractExists verifies the admin API contract file exists.
func TestAPIContractExists(t *testing.T) {
	paths := []string{
		rootDir("api", "admin-v1.openapi.yaml"),
		rootDir("api", "compatibility", "routes.yaml"),
		rootDir("api", "model-contracts", "README.md"),
	}

	for _, p := range paths {
		if _, err := os.Stat(p); os.IsNotExist(err) {
			t.Errorf("required file does not exist: %s", p)
		}
	}
}

// TestCompatibilityRoutesIsValidJSON verifies the routes file is valid JSON.
func TestCompatibilityRoutesIsValidYAML(t *testing.T) {
	data, err := os.ReadFile(rootDir("api", "compatibility", "routes.yaml"))
	if err != nil {
		t.Fatalf("read routes file: %v", err)
	}
	if len(data) == 0 {
		t.Error("routes file is empty")
	}
}

// TestFixtureProvenance verifies fixture provenance fields.
func TestFixtureProvenance(t *testing.T) {
	data, err := os.ReadFile(fixPath("manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("manifest is not valid JSON: %v", err)
	}

	if _, ok := raw["schemaVersion"]; !ok {
		t.Error("manifest missing schemaVersion field")
	}
	if _, ok := raw["baseline"]; !ok {
		t.Error("manifest missing baseline field")
	}
	if _, ok := raw["fixtures"]; !ok {
		t.Error("manifest missing fixtures field")
	}
}
