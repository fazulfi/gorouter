package governance

import (
	"encoding/json"
	"testing"
	"time"
)

func allPassingChecks() []GateCheck {
	return []GateCheck{
		{Name: "go-vet", Status: "pass", Output: "ok", DurationSeconds: 1.0},
		{Name: "go-test", Status: "pass", Output: "ok", DurationSeconds: 2.0},
		{Name: "go-race", Status: "pass", Output: "ok", DurationSeconds: 3.0},
		{Name: "frontend-build", Status: "pass", Output: "ok", DurationSeconds: 1.0},
		{Name: "frontend-test", Status: "pass", Output: "ok", DurationSeconds: 1.0},
		{Name: "code-coverage", Status: "pass", Output: "86.8%", DurationSeconds: 2.0},
		{Name: "coverage-threshold", Status: "pass", Output: "86.8% meets threshold 80.0%", DurationSeconds: 1.0},
		{Name: "traceability-docs", Status: "pass", Output: "ok", DurationSeconds: 0.5},
		{Name: "security-tests", Status: "pass", Output: "ok", DurationSeconds: 1.0},
		{Name: "security-audit", Status: "pass", Output: "clean", DurationSeconds: 2.0},
		{Name: "parity-tests", Status: "pass", Output: "ok", DurationSeconds: 0.5},
		{Name: "middleware-tests", Status: "pass", Output: "ok", DurationSeconds: 1.0},
		{Name: "auth-tests", Status: "pass", Output: "ok", DurationSeconds: 1.0},
		{Name: "observability-tests", Status: "pass", Output: "ok", DurationSeconds: 1.0},
		{Name: "migration-schema", Status: "pass", Output: "ok", DurationSeconds: 1.0},
		{Name: "postgres-pool", Status: "pass", Output: "ok", DurationSeconds: 1.0},
		{Name: "pg-repositories", Status: "pass", Output: "ok", DurationSeconds: 1.0},
	}
}

func failOnCheck(name string) []GateCheck {
	checks := allPassingChecks()
	for i := range checks {
		if checks[i].Name == name {
			checks[i].Status = "fail"
		}
	}
	return checks
}

func TestGateEvidence_ValidPass(t *testing.T) {
	ge := &GateEvidence{
		Phase:           "1",
		Status:          "pass",
		Timestamp:       time.Now().UTC(),
		DurationSeconds: 12.34,
		Checks:          allPassingChecks(),
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
		Checks:          failOnCheck("go-test"),
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
		Checks: allPassingChecks(),
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
			{Name: "go-race", Status: "pass", Output: "ok"},
			{Name: "frontend-build", Status: "pass", Output: "ok"},
			{Name: "frontend-test", Status: "pass", Output: "ok"},
			{Name: "code-coverage", Status: "pass", Output: "ok"},
			{Name: "coverage-threshold", Status: "pass", Output: "ok"},
			{Name: "traceability-docs", Status: "pass", Output: "ok"},
			{Name: "security-tests", Status: "pass", Output: "ok"},
			{Name: "security-audit", Status: "pass", Output: "ok"},
			{Name: "parity-tests", Status: "pass", Output: "ok"},
			{Name: "middleware-tests", Status: "pass", Output: "ok"},
			{Name: "auth-tests", Status: "pass", Output: "ok"},
			{Name: "observability-tests", Status: "pass", Output: "ok"},
			{Name: "migration-schema", Status: "pass", Output: "ok"},
			{Name: "postgres-pool", Status: "pass", Output: "ok"},
			// pg-repositories is missing
		},
	}
	err := ValidateGateEvidence(ge)
	if err == nil {
		t.Fatal("ValidateGateEvidence() expected error for missing required check, got nil")
	}
}

func TestGateEvidence_CheckInvalidStatus(t *testing.T) {
	// Check 0 has invalid status "maybe"
	checks := allPassingChecks()
	checks[0] = GateCheck{Name: "go-vet", Status: "maybe", Output: "confused"}
	ge := &GateEvidence{
		Phase:           "1",
		Status:          "pass",
		Timestamp:       time.Now().UTC(),
		DurationSeconds: 5.0,
		Checks:          checks,
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
		Checks:          allPassingChecks(),
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
	expected := []string{
		"go-vet", "go-test", "go-race",
		"frontend-build", "frontend-test",
		"code-coverage",
		"coverage-threshold",
		"traceability-docs",
		"security-tests", "security-audit",
		"parity-tests",
		"middleware-tests",
		"auth-tests",
		"observability-tests",
		"migration-schema",
		"postgres-pool", "pg-repositories",
	}
	for _, name := range expected {
		if _, ok := requiredPhase1Checks[name]; !ok {
			t.Errorf("requiredPhase1Checks missing %q", name)
		}
	}
	if len(requiredPhase1Checks) != len(expected) {
		t.Errorf("requiredPhase1Checks has %d entries, want %d", len(requiredPhase1Checks), len(expected))
	}
}

// TestInformationalPhase1CheckNames verifies the informational check names.
func TestInformationalPhase1CheckNames(t *testing.T) {
	expected := []string{"vps-deployment", "sudoers-hardening", "sbom-generate", "license-check"}
	for _, name := range expected {
		if _, ok := informationalPhase1Checks[name]; !ok {
			t.Errorf("informationalPhase1Checks missing %q", name)
		}
	}
	if len(informationalPhase1Checks) != len(expected) {
		t.Errorf("informationalPhase1Checks has %d entries, want %d", len(informationalPhase1Checks), len(expected))
	}
}

// RED Test: TestGateEvidencePassedWithInformationalFailures verifies that
// GateEvidencePassed returns true when all required checks pass but
// informational checks fail. This tests the informational-check exemption.
func TestGateEvidencePassedWithInformationalFailures(t *testing.T) {
	ge := &GateEvidence{
		Phase:           "1",
		Status:          "pass",
		Timestamp:       time.Now().UTC(),
		DurationSeconds: 10.0,
		Checks: []GateCheck{
			{Name: "go-vet", Status: "pass", Output: "ok"},
			{Name: "go-test", Status: "pass", Output: "ok"},
			{Name: "go-race", Status: "pass", Output: "ok"},
			{Name: "frontend-build", Status: "pass", Output: "ok"},
			{Name: "frontend-test", Status: "pass", Output: "ok"},
			{Name: "code-coverage", Status: "pass", Output: "86.7%"},
			{Name: "coverage-threshold", Status: "pass", Output: "86.7% meets threshold 80.0%"},
			{Name: "traceability-docs", Status: "pass", Output: "ok"},
			{Name: "security-tests", Status: "pass", Output: "ok"},
			{Name: "security-audit", Status: "pass", Output: "clean"},
			{Name: "parity-tests", Status: "pass", Output: "ok"},
			{Name: "middleware-tests", Status: "pass", Output: "ok"},
			{Name: "auth-tests", Status: "pass", Output: "ok"},
			{Name: "observability-tests", Status: "pass", Output: "ok"},
			{Name: "migration-schema", Status: "pass", Output: "ok"},
			{Name: "postgres-pool", Status: "pass", Output: "ok"},
			{Name: "pg-repositories", Status: "pass", Output: "ok"},
			// Informational checks — these may fail without affecting gate
			{Name: "vps-deployment", Status: "fail", Output: "no VPS configured"},
			{Name: "sudoers-hardening", Status: "fail", Output: "zero sudo granted"},
			{Name: "sbom-generate", Status: "fail", Output: "requires Go 1.25+"},
			{Name: "license-check", Status: "fail", Output: "go-licenses crashes on stdlib"},
		},
	}
	if err := ValidateGateEvidence(ge); err != nil {
		t.Fatalf("ValidateGateEvidence() unexpected error: %v", err)
	}
	if !GateEvidencePassed(ge) {
		t.Error("RED: GateEvidencePassed() = false, want true when informational checks fail but required checks pass")
	}
}

// RED Test: TestValidGateEvidenceWithInformationalOnly proves informational
// checks are NOT required for gate validation to pass.
func TestValidGateEvidenceWithInformationalOnly(t *testing.T) {
	// Only include informational checks, none of the required ones.
	ge := &GateEvidence{
		Phase:           "1",
		Status:          "pass",
		Timestamp:       time.Now().UTC(),
		DurationSeconds: 5.0,
		Checks: []GateCheck{
			{Name: "vps-deployment", Status: "pass", Output: "ok"},
			{Name: "sudoers-hardening", Status: "pass", Output: "ok"},
		},
	}
	err := ValidateGateEvidence(ge)
	if err == nil {
		t.Fatal("RED: ValidateGateEvidence() should FAIL when only informational checks are present (no required checks)")
	}
	// Must specifically mention a required check is missing
	if err != nil {
		t.Logf("CORRECT: gate rejected informational-only evidence: %v", err)
	}
}

// RED Test: TestMissingRequiredCheckAmongFullSet proves that even with many
// informational checks, a missing required check causes validation failure.
func TestMissingRequiredCheckAmongFullSet(t *testing.T) {
	ge := &GateEvidence{
		Phase:           "1",
		Status:          "pass",
		Timestamp:       time.Now().UTC(),
		DurationSeconds: 30.0,
		Checks: []GateCheck{
			{Name: "go-vet", Status: "pass", Output: "ok"},
			{Name: "go-test", Status: "pass", Output: "ok"},
			{Name: "go-race", Status: "pass", Output: "ok"},
			{Name: "frontend-build", Status: "pass", Output: "ok"},
			{Name: "frontend-test", Status: "pass", Output: "ok"},
			{Name: "code-coverage", Status: "pass", Output: "ok"},
			{Name: "coverage-threshold", Status: "pass", Output: "ok"},
			{Name: "traceability-docs", Status: "pass", Output: "ok"},
			{Name: "security-tests", Status: "pass", Output: "ok"},
			{Name: "security-audit", Status: "pass", Output: "ok"},
			{Name: "parity-tests", Status: "pass", Output: "ok"},
			{Name: "middleware-tests", Status: "pass", Output: "ok"},
			{Name: "auth-tests", Status: "pass", Output: "ok"},
			{Name: "observability-tests", Status: "pass", Output: "ok"},
			{Name: "migration-schema", Status: "pass", Output: "ok"},
			{Name: "postgres-pool", Status: "pass", Output: "ok"},
			// pg-repositories is MISSING
			{Name: "vps-deployment", Status: "pass", Output: "ok"},
			{Name: "sudoers-hardening", Status: "pass", Output: "ok"},
		},
	}
	err := ValidateGateEvidence(ge)
	if err == nil {
		t.Fatal("RED: ValidateGateEvidence() should FAIL when pg-repositories is missing")
	}
	t.Logf("CORRECT: gate rejected evidence missing required check: %v", err)
}

// RED Test: TestGatePassesWithAllRequired verifies the gate passes with all
// required checks present, regardless of informational check presence.
func TestGatePassesWithAllRequired(t *testing.T) {
	ge := &GateEvidence{
		Phase:           "1",
		Status:          "pass",
		Timestamp:       time.Now().UTC(),
		DurationSeconds: 30.0,
		Checks: []GateCheck{
			{Name: "go-vet", Status: "pass", Output: "ok", DurationSeconds: 1.0},
			{Name: "go-test", Status: "pass", Output: "ok", DurationSeconds: 2.0},
			{Name: "go-race", Status: "pass", Output: "ok", DurationSeconds: 3.0},
			{Name: "frontend-build", Status: "pass", Output: "ok", DurationSeconds: 1.0},
			{Name: "frontend-test", Status: "pass", Output: "ok", DurationSeconds: 1.0},
			{Name: "code-coverage", Status: "pass", Output: "86.8%", DurationSeconds: 2.0},
			{Name: "coverage-threshold", Status: "pass", Output: "86.8% meets threshold 80.0%", DurationSeconds: 1.0},
			{Name: "traceability-docs", Status: "pass", Output: "ok", DurationSeconds: 0.5},
			{Name: "security-tests", Status: "pass", Output: "ok", DurationSeconds: 1.0},
			{Name: "security-audit", Status: "pass", Output: "clean", DurationSeconds: 2.0},
			{Name: "parity-tests", Status: "pass", Output: "ok", DurationSeconds: 0.5},
			{Name: "middleware-tests", Status: "pass", Output: "ok", DurationSeconds: 1.0},
			{Name: "auth-tests", Status: "pass", Output: "ok", DurationSeconds: 1.0},
			{Name: "observability-tests", Status: "pass", Output: "ok", DurationSeconds: 1.0},
			{Name: "migration-schema", Status: "pass", Output: "ok", DurationSeconds: 1.0},
			{Name: "postgres-pool", Status: "pass", Output: "ok", DurationSeconds: 1.0},
			{Name: "pg-repositories", Status: "pass", Output: "ok", DurationSeconds: 1.0},
			// No informational checks — should still pass
		},
	}
	if err := ValidateGateEvidence(ge); err != nil {
		t.Fatalf("RED: ValidateGateEvidence() unexpected error when all required checks present: %v", err)
	}
	if !GateEvidencePassed(ge) {
		t.Error("GateEvidencePassed() should be true when all checks pass")
	}
}

// RED Test: TestAllRequiredChecksMustBePresent ensures every required check
// is individually tested for enforcement.
func TestAllRequiredChecksMustBePresent(t *testing.T) {
	allRequired := RequiredPhase1CheckNames()
	// Base set that covers all required checks
	for _, missing := range allRequired {
		t.Run("missing_"+missing, func(t *testing.T) {
			ge := &GateEvidence{
				Phase:     "1",
				Status:    "pass",
				Timestamp: time.Now().UTC(),
			}
			for _, name := range allRequired {
				if name == missing {
					continue
				}
				ge.Checks = append(ge.Checks, GateCheck{
					Name: name, Status: "pass", Output: "ok",
				})
			}
			err := ValidateGateEvidence(ge)
			if err == nil {
				t.Fatalf("RED: ValidateGateEvidence should fail when %q is missing", missing)
			}
		})
	}
}
