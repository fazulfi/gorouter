package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureRoot locates tests/fixtures/upstream from any package working
// directory by walking up to the module root.
func fixtureRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "tests", "fixtures", "upstream")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("module root not found")
		}
		dir = parent
	}
}

// TestUpstreamConfigTransferExportFixtureFrozen pins the exact upstream
// export payload: the ten domains, the always-excluded domains absent, and
// sanitized credential fields with provenance metadata.
func TestUpstreamConfigTransferExportFixtureFrozen(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join(fixtureRoot(t), "config-transfer.export.json"))
	if err != nil {
		t.Fatalf("read export fixture: %v", err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("export fixture must be valid JSON: %v", err)
	}
	wantDomains := []string{"settings", "providerConnections", "providerNodes", "proxyPools",
		"apiKeys", "combos", "modelAliases", "customModels", "mitmAlias", "pricing"}
	if len(payload) != len(wantDomains) {
		t.Fatalf("export fixture domains = %d, want %d: %v", len(payload), len(wantDomains), keysOf(payload))
	}
	for _, d := range wantDomains {
		if _, ok := payload[d]; !ok {
			t.Errorf("export fixture missing domain %q", d)
		}
	}
	for _, banned := range []string{"usageHistory", "usageDaily", "requestDetails", "sessions", "audit"} {
		if _, ok := payload[banned]; ok {
			t.Errorf("export fixture must never carry excluded domain %q", banned)
		}
		if strings.Contains(string(raw), "\""+banned+"\"") {
			t.Errorf("export fixture mentions excluded domain %q", banned)
		}
	}
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(payload["settings"], &settings); err != nil {
		t.Fatal("settings domain must be an object")
	}
	if _, ok := settings["rtkEnabled"]; !ok {
		t.Error("settings domain must carry the rtkEnabled key (pinned upstream settings shape)")
	}
	var conns []map[string]json.RawMessage
	if err := json.Unmarshal(payload["providerConnections"], &conns); err != nil {
		t.Fatal("providerConnections must be an array")
	}
	if len(conns) == 0 {
		t.Fatal("providerConnections must not be empty")
	}
	for _, field := range []string{"id", "provider", "authType", "name", "isActive"} {
		if _, ok := conns[0][field]; !ok {
			t.Errorf("providerConnections row missing upstream field %q", field)
		}
	}
	// Credential fields must be placeholder-sanitized: never a live-shaped
	// provider key or OAuth token.
	for _, banned := range []string{"sk-proj-", "sk-ant-", "ya29.", "ghp_", "glpat-"} {
		if strings.Contains(string(raw), banned) {
			t.Errorf("export fixture contains live credential shape %q", banned)
		}
	}
	for _, marker := range []string{"placeholder", "redacted"} {
		if !strings.Contains(string(raw), marker) {
			t.Errorf("export fixture lacks sanitization marker %q", marker)
		}
	}
}

// TestUpstreamConfigTransferImportFixtureFrozen pins the partial import
// payload: absent domains still exercise missing-key wipe on import.
func TestUpstreamConfigTransferImportFixtureFrozen(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join(fixtureRoot(t), "config-transfer.import.json"))
	if err != nil {
		t.Fatalf("read import fixture: %v", err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("import fixture must be valid JSON: %v", err)
	}
	if _, ok := payload["settings"]; !ok {
		t.Error("import fixture must carry the settings domain")
	}
	for _, banned := range []string{"usageHistory", "usageDaily", "requestDetails", "sessions", "audit"} {
		if _, ok := payload[banned]; ok {
			t.Errorf("import fixture must never carry excluded domain %q", banned)
		}
	}
	for _, marker := range []string{"placeholder", "redacted"} {
		if !strings.Contains(string(raw), marker) {
			t.Errorf("import fixture lacks sanitization marker %q", marker)
		}
	}
}

// TestConfigTransferProvenance pins the provenance metadata of the frozen
// fixtures against the pinned upstream commit and source anchors.
func TestConfigTransferProvenance(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join(fixtureRoot(t), "config-transfer.provenance.json"))
	if err != nil {
		t.Fatalf("read provenance: %v", err)
	}
	var meta struct {
		PinnedUpstream struct {
			Repository string `json:"repository"`
			Commit     string `json:"commit"`
			Version    string `json:"version"`
		} `json:"pinnedUpstream"`
		SourceAnchors         []string `json:"sourceAnchors"`
		ExportDomains         []string `json:"exportDomains"`
		AlwaysExcludedDomains []string `json:"alwaysExcludedDomains"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatalf("provenance must be valid JSON: %v", err)
	}
	if meta.PinnedUpstream.Repository != "decolua/9router" ||
		meta.PinnedUpstream.Commit != "79918c7830695bbca4a45c9fea4a42c3e9fd73d1" ||
		meta.PinnedUpstream.Version != "0.5.40" {
		t.Errorf("provenance pin = %+v", meta.PinnedUpstream)
	}
	if len(meta.SourceAnchors) == 0 {
		t.Error("provenance must cite upstream source anchors")
	}
	if len(meta.ExportDomains) != 10 {
		t.Errorf("provenance export domains = %v", meta.ExportDomains)
	}
	for _, d := range []string{"usageHistory", "usageDaily", "requestDetails", "sessions", "audit"} {
		found := false
		for _, e := range meta.AlwaysExcludedDomains {
			if e == d {
				found = true
			}
		}
		if !found {
			t.Errorf("provenance must record excluded domain %q", d)
		}
	}
}

// TestConfigTransferManifestEntries verifies the frozen fixtures are
// registered in the fixture manifest with P4-T15 provenance.
func TestConfigTransferManifestEntries(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join(fixtureRoot(t), "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest struct {
		Baseline struct {
			Commit string `json:"commit"`
		} `json:"baseline"`
		Fixtures []struct {
			ID        string `json:"id"`
			Task      string `json:"task"`
			Phase     int    `json:"phase"`
			Sanitized bool   `json:"sanitized"`
		} `json:"fixtures"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("manifest must be valid JSON: %v", err)
	}
	if manifest.Baseline.Commit != "79918c7830695bbca4a45c9fea4a42c3e9fd73d1" {
		t.Errorf("manifest baseline = %s", manifest.Baseline.Commit)
	}
	found := map[string]bool{}
	for _, f := range manifest.Fixtures {
		if f.ID == "config-transfer-export" || f.ID == "config-transfer-import" {
			found[f.ID] = true
			if f.Task != "P4-T15" || f.Phase != 4 || !f.Sanitized {
				t.Errorf("manifest entry %s provenance = %+v", f.ID, f)
			}
		}
	}
	if !found["config-transfer-export"] || !found["config-transfer-import"] {
		t.Errorf("manifest missing config-transfer entries: %v", found)
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
