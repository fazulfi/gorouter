package release

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// testRoot creates a temporary directory with minimal lock-file stubs so that
// ComputeLockHashes and BuildFreezeRecord succeed without a real repository.
func testRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	// Each stub is a fixed-content file with a known SHA-256 so the test
	// can assert the exact lock hash.
	for _, lf := range []struct {
		relPath string
		content string
	}{
		{"go.mod", "module gorouter\ngo 1.25\n"},
		{"go.sum", "github.com/foo/bar v1.0.0 h1:abc123\n"},
		{"frontend/package-lock.json", `{"name":"frontend","lockfileVersion":3}` + "\n"},
	} {
		dir := filepath.Dir(filepath.Join(root, lf.relPath))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, lf.relPath), []byte(lf.content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestFreezeRecord_Validate_Nil(t *testing.T) {
	var r *FreezeRecord
	if err := r.Validate(); err == nil {
		t.Fatal("expected error for nil record")
	}
}

func TestFreezeRecord_Validate_InvalidSHA(t *testing.T) {
	r := &FreezeRecord{CandidateSHA: "not-a-sha"}
	if err := r.Validate(); err == nil {
		t.Fatal("expected error for invalid SHA")
	}
}

func TestFreezeRecord_Validate_NoLocks(t *testing.T) {
	r := &FreezeRecord{
		StableVersion: "v1.2.3",
		CandidateSHA:  "715916074067bed34fa4b44a3783775d041ef3dc",
	}
	if err := r.Validate(); err == nil {
		t.Fatal("expected error for missing locks")
	}
}

func TestFreezeRecord_Validate_InvalidLockHash(t *testing.T) {
	r := &FreezeRecord{
		StableVersion: "v1.2.3",
		CandidateSHA:  "715916074067bed34fa4b44a3783775d041ef3dc",
		Locks:         map[string]string{"go.mod": "not-hex"},
	}
	if err := r.Validate(); err == nil {
		t.Fatal("expected error for invalid lock hash")
	}
}

func TestFreezeRecord_Validate_StableEqualsBeta(t *testing.T) {
	r := &FreezeRecord{
		StableVersion: "v1.2.3-beta.1",
		CandidateSHA:  "715916074067bed34fa4b44a3783775d041ef3dc",
		Locks:         map[string]string{"go.mod": strings.Repeat("0", 64)},
	}
	if err := r.Validate(); err == nil {
		t.Fatal("expected error when stable version still has -beta suffix")
	}
}

func TestFreezeRecord_Validate_OK(t *testing.T) {
	r := &FreezeRecord{
		StableVersion: "v1.2.3",
		CandidateSHA:  "715916074067bed34fa4b44a3783775d041ef3dc",
		Locks:         map[string]string{"go.mod": strings.Repeat("a", 64)},
	}
	if err := r.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHashFile(t *testing.T) {
	root := testRoot(t)
	sum, err := HashFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(sum) {
		t.Fatalf("hash %q is not a 64-hex SHA-256", sum)
	}
}

func TestHashFile_Missing(t *testing.T) {
	_, err := HashFile(filepath.Join(os.TempDir(), "nolock.xxx"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestComputeLockHashes(t *testing.T) {
	root := testRoot(t)
	locks, err := ComputeLockHashes(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(locks) != 3 {
		t.Fatalf("expected 3 locks, got %d", len(locks))
	}
	for _, name := range []string{"go.mod", "go.sum", "frontend-package-lock.json"} {
		if _, ok := locks[name]; !ok {
			t.Fatalf("missing lock %q", name)
		}
	}
}

func TestComputeLockHashes_MissingLock(t *testing.T) {
	root := t.TempDir()
	// No go.sum — one of the locks is missing
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ComputeLockHashes(root)
	if err == nil {
		t.Fatal("expected error for missing lock file")
	}
}

func TestBuildFreezeRecord(t *testing.T) {
	root := testRoot(t)
	rec, err := BuildFreezeRecord(root,
		"715916074067bed34fa4b44a3783775d041ef3dc",
		"v1.2.3-beta.1",
		"v1.2.3",
		"No user-reported beta defects exist — stable-candidate freeze at same HEAD (see evidence).",
	)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Artifact != "stable-candidate-freeze" {
		t.Fatalf("artifact = %q", rec.Artifact)
	}
	if rec.Phase != "P5-T15" {
		t.Fatalf("phase = %q", rec.Phase)
	}
	if rec.StableVersion != "v1.2.3" {
		t.Fatalf("stable_version = %q", rec.StableVersion)
	}
	if rec.CandidateSHA != "715916074067bed34fa4b44a3783775d041ef3dc" {
		t.Fatalf("candidate_sha = %q", rec.CandidateSHA)
	}
	if rec.CandidateTag != "v1.2.3-beta.1" {
		t.Fatalf("candidate_tag = %q", rec.CandidateTag)
	}
	if err := rec.Validate(); err != nil {
		t.Fatalf("record validation failed: %v", err)
	}
}

func TestBuildFreezeRecord_InvalidSHA(t *testing.T) {
	root := testRoot(t)
	_, err := BuildFreezeRecord(root, "bad-sha", "v1.2.3-beta.1", "v1.2.3", "none")
	if err == nil {
		t.Fatal("expected error for bad SHA")
	}
}

func TestBuildFreezeRecord_InvalidVersion(t *testing.T) {
	root := testRoot(t)
	_, err := BuildFreezeRecord(root,
		"715916074067bed34fa4b44a3783775d041ef3dc",
		"v1.2.3-beta.1",
		"not-a-version",
		"none",
	)
	if err == nil {
		t.Fatal("expected error for bad version")
	}
}

func TestWriteFreezeRecord(t *testing.T) {
	root := testRoot(t)
	rec, err := BuildFreezeRecord(root,
		"715916074067bed34fa4b44a3783775d041ef3dc",
		"v1.2.3-beta.1",
		"v1.2.3",
		"No defects reported.",
	)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "freeze-manifest.json")
	if err := WriteFreezeRecord(rec, path); err != nil {
		t.Fatal(err)
	}
	// Read back and verify
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got FreezeRecord
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.CandidateSHA != rec.CandidateSHA {
		t.Fatalf("round-trip: candidate SHA = %q, want %q", got.CandidateSHA, rec.CandidateSHA)
	}
	if got.StableVersion != rec.StableVersion {
		t.Fatalf("round-trip: stable version = %q, want %q", got.StableVersion, rec.StableVersion)
	}
}

func TestWriteFreezeRecord_Invalid(t *testing.T) {
	r := &FreezeRecord{CandidateSHA: "bad"}
	if err := WriteFreezeRecord(r, os.DevNull); err == nil {
		t.Fatal("expected error for invalid record")
	}
}

func TestSortedLockNames(t *testing.T) {
	r := &FreezeRecord{
		Locks: map[string]string{
			"z": strings.Repeat("a", 64),
			"a": strings.Repeat("b", 64),
		},
	}
	got := SortedLockNames(r)
	if len(got) != 2 || got[0] != "a" || got[1] != "z" {
		t.Fatalf("sorted names = %v, want [a z]", got)
	}
}

func TestLockPathsOrder(t *testing.T) {
	if len(LockPaths) < 3 {
		t.Fatal("expected at least 3 lock paths")
	}
}

// Deterministic field ordering: the JSON marshalled output must have
// certain required fields so the T16 gate can parse them.
func TestFreezeRecord_JSONFieldNames(t *testing.T) {
	root := testRoot(t)
	rec, err := BuildFreezeRecord(root,
		"715916074067bed34fa4b44a3783775d041ef3dc",
		"v1.2.3-beta.1",
		"v1.2.3",
		"none",
	)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"artifact", "phase", "stable_version", "candidate_sha", "locks"} {
		if _, ok := raw[required]; !ok {
			t.Fatalf("missing required JSON field %q", required)
		}
	}
}
