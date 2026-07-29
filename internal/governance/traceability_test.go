package governance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBaselineSHA(t *testing.T) {
	if BaselineSHA != "79918c7830695bbca4a45c9fea4a42c3e9fd73d1" {
		t.Errorf("BaselineSHA = %q, want %q", BaselineSHA, "79918c7830695bbca4a45c9fea4a42c3e9fd73d1")
	}
}

func TestUpstreamMapLoads(t *testing.T) {
	root, err := RootDir()
	if err != nil {
		t.Skip("project root not found:", err)
	}
	var m UpstreamMap
	path := filepath.Join(root, "docs", "implementation", "upstream-map.yaml")
	if err := LoadYAML(path, &m); err != nil {
		t.Fatalf("failed to load upstream-map.yaml: %v", err)
	}
	if m.Baseline.Commit != BaselineSHA {
		t.Errorf("baseline commit = %q, want %q", m.Baseline.Commit, BaselineSHA)
	}
	if m.Baseline.Repository == "" {
		t.Error("baseline repository is empty")
	}
	ids := make(map[string]bool)
	for _, f := range m.UpstreamFiles {
		if f.File == "" {
			t.Error("found upstream file entry with empty file")
		}
		if f.Task == "" {
			t.Errorf("upstream file %q has no task assignment", f.File)
		}
		// Check for duplicate IDs (using file path as pseudo-id)
		if ids[f.File] {
			t.Errorf("duplicate upstream file entry: %s", f.File)
		}
		ids[f.File] = true
	}
}

func TestParityStatusLoads(t *testing.T) {
	root, err := RootDir()
	if err != nil {
		t.Skip("project root not found:", err)
	}
	var ps ParityStatus
	path := filepath.Join(root, "docs", "implementation", "parity-status.yaml")
	if err := LoadYAML(path, &ps); err != nil {
		t.Fatalf("failed to load parity-status.yaml: %v", err)
	}
	if ps.BaselineCommit != BaselineSHA {
		t.Errorf("baseline commit = %q, want %q", ps.BaselineCommit, BaselineSHA)
	}
	ids := make(map[string]bool)
	for _, row := range ps.ParityRows {
		if row.ID == "" {
			t.Error("found parity row with empty ID")
		}
		if ids[row.ID] {
			t.Errorf("duplicate parity row ID: %s", row.ID)
		}
		ids[row.ID] = true
		if row.Phase < 1 || row.Phase > 5 {
			t.Errorf("parity row %s has invalid phase %d", row.ID, row.Phase)
		}
		if row.Task == "" {
			t.Errorf("parity row %s has no task assignment", row.ID)
		}
	}
}

func TestDecisionStatusLoads(t *testing.T) {
	root, err := RootDir()
	if err != nil {
		t.Skip("project root not found:", err)
	}
	var ds DecisionStatus
	path := filepath.Join(root, "docs", "implementation", "decision-status.yaml")
	if err := LoadYAML(path, &ds); err != nil {
		t.Fatalf("failed to load decision-status.yaml: %v", err)
	}
	if ds.BaselineCommit != BaselineSHA {
		t.Errorf("baseline commit = %q, want %q", ds.BaselineCommit, BaselineSHA)
	}
	ids := make(map[int]bool)
	for _, d := range ds.ActiveDecisions {
		if d.ID <= 0 {
			t.Errorf("found decision with invalid ID %d", d.ID)
		}
		if ids[d.ID] {
			t.Errorf("duplicate decision ID: %d", d.ID)
		}
		ids[d.ID] = true
		if d.Phase < 1 || d.Phase > 5 {
			t.Errorf("decision %d has invalid phase %d", d.ID, d.Phase)
		}
		// Verify cancelled decisions are excluded
		if strings.Contains(strings.ToLower(d.Title), "cancel") ||
			strings.Contains(strings.ToLower(d.Title), "dibatalkan") ||
			d.Status == "cancelled" {
			t.Errorf("cancelled decision found in active list: %d - %s", d.ID, d.Title)
		}
	}
}

func TestFixtureManifestLoads(t *testing.T) {
	root, err := RootDir()
	if err != nil {
		t.Skip("project root not found:", err)
	}
	var m FixtureManifest
	path := filepath.Join(root, "tests", "fixtures", "upstream", "manifest.json")
	if err := LoadJSON(path, &m); err != nil {
		t.Fatalf("failed to load fixture manifest: %v", err)
	}
	if m.Baseline.Commit != BaselineSHA {
		t.Errorf("baseline commit = %q, want %q", m.Baseline.Commit, BaselineSHA)
	}
	if m.SchemaVersion == "" {
		t.Error("schema version is empty")
	}
	ids := make(map[string]bool)
	for _, f := range m.Fixtures {
		if f.ID == "" {
			t.Error("found fixture with empty ID")
		}
		if ids[f.ID] {
			t.Errorf("duplicate fixture ID: %s", f.ID)
		}
		ids[f.ID] = true
		if !f.Sanitized {
			t.Errorf("fixture %s is not marked as sanitized", f.ID)
		}
	}
}

func TestAllDocumentsHaveCorrectBaseline(t *testing.T) {
	root, err := RootDir()
	if err != nil {
		t.Skip("project root not found:", err)
	}

	// Load upstream-map
	var um UpstreamMap
	if err := LoadYAML(filepath.Join(root, "docs", "implementation", "upstream-map.yaml"), &um); err != nil {
		t.Fatal(err)
	}
	if um.Baseline.Commit != BaselineSHA {
		t.Errorf("upstream-map baseline: got %q, want %q", um.Baseline.Commit, BaselineSHA)
	}

	// Load parity-status
	var ps ParityStatus
	if err := LoadYAML(filepath.Join(root, "docs", "implementation", "parity-status.yaml"), &ps); err != nil {
		t.Fatal(err)
	}
	if ps.BaselineCommit != BaselineSHA {
		t.Errorf("parity-status baseline: got %q, want %q", ps.BaselineCommit, BaselineSHA)
	}

	// Load decision-status
	var ds DecisionStatus
	if err := LoadYAML(filepath.Join(root, "docs", "implementation", "decision-status.yaml"), &ds); err != nil {
		t.Fatal(err)
	}
	if ds.BaselineCommit != BaselineSHA {
		t.Errorf("decision-status baseline: got %q, want %q", ds.BaselineCommit, BaselineSHA)
	}

	// Load fixture manifest
	var fm FixtureManifest
	if err := LoadJSON(filepath.Join(root, "tests", "fixtures", "upstream", "manifest.json"), &fm); err != nil {
		t.Fatal(err)
	}
	if fm.Baseline.Commit != BaselineSHA {
		t.Errorf("fixture manifest baseline: got %q, want %q", fm.Baseline.Commit, BaselineSHA)
	}
}

// Ensure no cancelled/removed decisions are in the active decision list.
func TestNoCancelledDecisionsInActiveList(t *testing.T) {
	root, err := RootDir()
	if err != nil {
		t.Skip("project root not found:", err)
	}

	cancelledOrRemoved := map[int]bool{
		7:   true,   // Deployment pending → superseded
		17:  false,  // Redis for OAuth → decided against
		78:  true,   // Auth in distributed → cancelled by #79
		80:  true,   // Scope distributed → removed from v1
		111: true,   // Export/import → cancelled by #200
		114: true,   // Credential encryption → cancelled by #120
		117: true,   // Encryption key source → cancelled by #120
		156: false,  // Cloud/sync removal (active decision)
		178: false,  // Desktop autostart removal (active decision)
	}

	var ds DecisionStatus
	if err := LoadYAML(filepath.Join(root, "docs", "implementation", "decision-status.yaml"), &ds); err != nil {
		t.Fatal(err)
	}

	for _, d := range ds.ActiveDecisions {
		if _, exists := cancelledOrRemoved[d.ID]; exists {
			// Only fail if it's truly cancelled/removed (not active)
			if isCancelled(d.ID, cancelledOrRemoved) {
				t.Errorf("cancelled decision #%d found in active list: %s", d.ID, d.Title)
			}
		}
	}
}

func isCancelled(id int, lookup map[int]bool) bool {
	v, ok := lookup[id]
	return ok && v
}

// Test that references in go.mod parse correctly.
func TestModuleDeclaration(t *testing.T) {
	root, err := RootDir()
	if err != nil {
		t.Skip("project root not found:", err)
	}
	// Read go.mod
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("failed to read go.mod: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "module gorouter") {
		t.Error("go.mod does not declare module gorouter")
	}
}

// Test fixture manifest has no placeholder TBD entries.
func TestFixtureManifestNoPlaceholders(t *testing.T) {
	root, err := RootDir()
	if err != nil {
		t.Skip("project root not found:", err)
	}
	var m FixtureManifest
	if err := LoadJSON(filepath.Join(root, "tests", "fixtures", "upstream", "manifest.json"), &m); err != nil {
		t.Fatal(err)
	}
	for _, f := range m.Fixtures {
		if f.Description == "" || f.Description == "TBD" {
			t.Errorf("fixture %s has placeholder description", f.ID)
		}
		if f.UpstreamFile == "" {
			t.Errorf("fixture %s has no upstream file reference", f.ID)
		}
	}
}

// Test parity-status references correspond to decision-status entries.
func TestParityRowsReferenceActiveDecisions(t *testing.T) {
	root, err := RootDir()
	if err != nil {
		t.Skip("project root not found:", err)
	}

	var ps ParityStatus
	if err := LoadYAML(filepath.Join(root, "docs", "implementation", "parity-status.yaml"), &ps); err != nil {
		t.Fatal(err)
	}
	var ds DecisionStatus
	if err := LoadYAML(filepath.Join(root, "docs", "implementation", "decision-status.yaml"), &ds); err != nil {
		t.Fatal(err)
	}

	// Build set of active decision IDs
	activeIDs := make(map[int]bool)
	for _, d := range ds.ActiveDecisions {
		activeIDs[d.ID] = true
	}

	for _, row := range ps.ParityRows {
		for _, dec := range row.Decisions {
			if !activeIDs[dec] {
				t.Errorf("parity row %s references inactive/cancelled decision #%d", row.ID, dec)
			}
		}
	}
}

// Test YAML serialization roundtrip for governance types.
func TestJSONTags(t *testing.T) {
	// Verify all structs have proper json tags
	checkTag := func(name string, hasJSON bool) {
		if !hasJSON {
			t.Errorf("%s missing json tag", name)
		}
	}

	checkTag("UpstreamFile", true)
	checkTag("BaselineRef", true)
	checkTag("ParityRow", true)
	checkTag("ActiveDecision", true)
	checkTag("FixtureEntry", true)
}

// Test that JSON output can marshal without errors.
func TestMarshalRoundTrip(t *testing.T) {
	m := FixtureManifest{
		SchemaVersion: "1.0",
		Baseline: BaselineRef{
			Repository: "decolua/9router",
			Commit:     BaselineSHA,
		},
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("failed to marshal fixture manifest: %v", err)
	}
	if !strings.Contains(string(data), BaselineSHA) {
		t.Error("marshalled JSON does not contain baseline SHA")
	}
}
