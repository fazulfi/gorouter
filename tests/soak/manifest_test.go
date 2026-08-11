package soak

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadLabManifestMissingFile(t *testing.T) {
	if _, err := ReadLabManifest("does-not-exist.json"); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestWriteAndReadLabManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lab-manifest.json")
	m := &LabManifest{
		SchemaVersion:  "p5-t13-lab-manifest.v1",
		Task:           "P5-T13",
		Status:         "pending",
		CandidateSHA:   "ffffffffffffffffffffffffffffffffffffffff",
		SampleInterval: "5m",
		OutputDir:      "artifacts/p5-t13",
		EvidencePath:   "artifacts/p5-t13/trend.json",
		ReferenceEnv: ReferenceEnv{
			Host: "143.198.86.164", CPU: "4", Memory: "7.8 GiB", Storage: "153 GiB", OS: "Ubuntu 24.04",
		},
		Common: CommonSettings{WorkingDir: "repository root", ThresholdEnforcement: "SOAK_ENFORCE_THRESHOLDS=1", OutputFormat: "signed JSON manifest"},
		Gates: []GateSpec{
			{ID: "soak-72h", Metric: "72h mixed load with zero prohibited events", Command: "go test ./tests/soak/... -run TestSoakSmokeRun -args -soak-duration=72h -soak-sample-interval=5m -soak-concurrency=256 -soak-output=artifacts/p5-t13/trend.json", Tooling: "Go test harness", Load: LoadSpec{Duration: "72h", Concurrent: 256, ErrorBudget: 0.01}, PassCriteria: "exit 0 and signed envelope records zero crashes/deadlocks/leaks", EvidenceFile: "artifacts/p5-t13/trend.json + signed envelope", Owner: "T13 owner"},
		},
	}
	if err := WriteLabManifest(path, m); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := ReadLabManifest(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.Task != "P5-T13" {
		t.Fatalf("task = %q", got.Task)
	}
	if got.CandidateSHA != "ffffffffffffffffffffffffffffffffffffffff" {
		t.Fatalf("sha mismatch")
	}
	if len(got.Gates) != 1 {
		t.Fatalf("gates = %d", len(got.Gates))
	}
	if got.Gates[0].ID != "soak-72h" {
		t.Fatalf("gate id = %q", got.Gates[0].ID)
	}
}

func TestReadLabManifestInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte(`{"foo":"bar"}`), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := ReadLabManifest(path); err == nil {
		t.Fatal("expected error for invalid manifest")
	}
}
