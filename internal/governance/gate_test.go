package governance

import (
	"encoding/json"
	"testing"
	"time"
)

func TestGateEvidence_ValidPass(t *testing.T) {
	ge := &GateEvidence{
		Phase:           "1",
		Status:          "pass",
		Timestamp:       time.Now().UTC(),
		DurationSeconds: 12.34,
		Checks: []GateCheck{
			{Name: "go-vet", Status: "pass", Output: "ok", DurationSeconds: 2.0},
			{Name: "go-test", Status: "pass", Output: "ok", DurationSeconds: 5.0},
			{Name: "frontend-build", Status: "pass", Output: "ok", DurationSeconds: 3.0},
			{Name: "frontend-test", Status: "pass", Output: "ok", DurationSeconds: 2.34},
		},
	}

	if err := ValidateGateEvidence(ge); err != nil {
		t.Fatalf("ValidateGateEvidence() unexpected error: %v", err)
	}
	if !GateEvidencePassed(ge) {
		t.Error("GateEvidencePassed() = false, want true")
	}
}

func TestGateEvidence_ValidFail(t *testing.T) {
	ge := &GateEvidence{
		Phase:           "1",
		Status:          "fail",
		Timestamp:       time.Now().UTC(),
		DurationSeconds: 8.0,
		Checks: []GateCheck{
			{Name: "go-vet", Status: "pass", Output: "ok", DurationSeconds: 1.0},
			{Name: "go-test", Status: "fail", Output: "test failed", DurationSeconds: 4.0},
			{Name: "frontend-build", Status: "pass", Output: "ok", DurationSeconds: 2.0},
			{Name: "frontend-test", Status: "pass", Output: "ok", DurationSeconds: 1.0},
		},
	}

	if err := ValidateGateEvidence(ge); err != nil {
		t.Fatalf("ValidateGateEvidence() unexpected error: %v", err)
	}
	if GateEvidencePassed(ge) {
		t.Error("GateEvidencePassed() = true, want false")
	}
}

func TestGateEvidence_WrongPhase(t *testing.T) {
	ge := &GateEvidence{
		Phase: "2",
	}
	err := ValidateGateEvidence(ge)
	if err == nil {
		t.Fatal("ValidateGateEvidence() expected error for wrong phase, got nil")
	}
}

func TestGateEvidence_InvalidStatus(t *testing.T) {
	ge := &GateEvidence{
		Phase:  "1",
		Status: "unknown",
	}
	err := ValidateGateEvidence(ge)
	if err == nil {
		t.Fatal("ValidateGateEvidence() expected error for invalid status, got nil")
	}
}

func TestGateEvidence_MissingTimestamp(t *testing.T) {
	ge := &GateEvidence{
		Phase:  "1",
		Status: "pass",
		Checks: []GateCheck{
			{Name: "go-vet", Status: "pass", Output: "ok"},
			{Name: "go-test", Status: "pass", Output: "ok"},
			{Name: "frontend-build", Status: "pass", Output: "ok"},
			{Name: "frontend-test", Status: "pass", Output: "ok"},
		},
	}
	err := ValidateGateEvidence(ge)
	if err == nil {
		t.Fatal("ValidateGateEvidence() expected error for missing timestamp, got nil")
	}
}

func TestGateEvidence_NoChecks(t *testing.T) {
	ge := &GateEvidence{
		Phase:     "1",
		Status:    "pass",
		Timestamp: time.Now().UTC(),
	}
	err := ValidateGateEvidence(ge)
	if err == nil {
		t.Fatal("ValidateGateEvidence() expected error for no checks, got nil")
	}
}

func TestGateEvidence_MissingRequiredCheck(t *testing.T) {
	ge := &GateEvidence{
		Phase:           "1",
		Status:          "pass",
		Timestamp:       time.Now().UTC(),
		DurationSeconds: 5.0,
		Checks: []GateCheck{
			{Name: "go-vet", Status: "pass", Output: "ok"},
			{Name: "go-test", Status: "pass", Output: "ok"},
			{Name: "frontend-build", Status: "pass", Output: "ok"},
			// frontend-test is missing
		},
	}
	err := ValidateGateEvidence(ge)
	if err == nil {
		t.Fatal("ValidateGateEvidence() expected error for missing required check, got nil")
	}
}

func TestGateEvidence_CheckInvalidStatus(t *testing.T) {
	ge := &GateEvidence{
		Phase:           "1",
		Status:          "pass",
		Timestamp:       time.Now().UTC(),
		DurationSeconds: 5.0,
		Checks: []GateCheck{
			{Name: "go-vet", Status: "maybe", Output: "confused"},
			{Name: "go-test", Status: "pass", Output: "ok"},
			{Name: "frontend-build", Status: "pass", Output: "ok"},
			{Name: "frontend-test", Status: "pass", Output: "ok"},
		},
	}
	err := ValidateGateEvidence(ge)
	if err == nil {
		t.Fatal("ValidateGateEvidence() expected error for invalid check status, got nil")
	}
}

func TestGateEvidence_JSONMarshalUnmarshal(t *testing.T) {
	orig := &GateEvidence{
		Phase:           "1",
		Status:          "pass",
		Timestamp:       time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC),
		DurationSeconds: 15.5,
		Checks: []GateCheck{
			{Name: "go-vet", Status: "pass", Output: "ok", DurationSeconds: 2.0},
			{Name: "go-test", Status: "pass", Output: "all tests passed", DurationSeconds: 6.0},
			{Name: "frontend-build", Status: "pass", Output: "build success", DurationSeconds: 4.0},
			{Name: "frontend-test", Status: "pass", Output: "tests passed", DurationSeconds: 3.5},
		},
	}

	data, err := MarshalGateEvidence(orig)
	if err != nil {
		t.Fatalf("MarshalGateEvidence() error: %v", err)
	}

	// Verify it's valid JSON
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal() error: %v", err)
	}

	// Verify fields survived round-trip
	if raw["phase"] != "1" {
		t.Errorf("phase = %v, want \"1\"", raw["phase"])
	}
	if raw["status"] != "pass" {
		t.Errorf("status = %v, want \"pass\"", raw["status"])
	}

	// Unmarshal back
	restored, err := UnmarshalGateEvidence(data)
	if err != nil {
		t.Fatalf("UnmarshalGateEvidence() error: %v", err)
	}

	if restored.Phase != orig.Phase {
		t.Errorf("Phase = %q, want %q", restored.Phase, orig.Phase)
	}
	if restored.Status != orig.Status {
		t.Errorf("Status = %q, want %q", restored.Status, orig.Status)
	}
	if len(restored.Checks) != len(orig.Checks) {
		t.Errorf("len(Checks) = %d, want %d", len(restored.Checks), len(orig.Checks))
	}
}

func TestGateEvidence_AllChecksPassedHelper(t *testing.T) {
	// GateEvidencePassed returns false when status is "pass" but a check fails
	ge := &GateEvidence{
		Phase:  "1",
		Status: "pass",
		Checks: []GateCheck{
			{Name: "go-vet", Status: "fail", Output: "vet error"},
		},
	}
	if GateEvidencePassed(ge) {
		t.Error("GateEvidencePassed() = true, want false when individual check fails")
	}
}

func TestGateEvidence_InvalidCheckOutput(t *testing.T) {
	// Check that unmarshaling with extra fields is tolerated or fails correctly
	data := `{
		"phase": "1",
		"status": "pass",
		"timestamp": "2026-07-29T12:00:00Z",
		"duration_seconds": 10,
		"checks": [
			{"name": "go-vet", "status": "pass", "output": "ok"}
		]
	}`
	ge, err := UnmarshalGateEvidence([]byte(data))
	if err != nil {
		t.Fatalf("UnmarshalGateEvidence() error: %v", err)
	}
	if ge.Phase != "1" {
		t.Errorf("Phase = %q, want \"1\"", ge.Phase)
	}
}

func TestRequiredPhase1CheckNames(t *testing.T) {
	expected := []string{"go-vet", "go-test", "frontend-build", "frontend-test"}
	for _, name := range expected {
		if _, ok := requiredPhase1Checks[name]; !ok {
			t.Errorf("requiredPhase1Checks missing %q", name)
		}
	}
	if len(requiredPhase1Checks) != len(expected) {
		t.Errorf("requiredPhase1Checks has %d entries, want %d", len(requiredPhase1Checks), len(expected))
	}
}
