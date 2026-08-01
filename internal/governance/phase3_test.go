package governance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Phase 3 gate evidence tests.
//
// Phase 3 (Engine and Provider Matrix, P3-T01..P3-T14) evidence is written to
// docs/implementation/gate-evidence/phase-3.json by the documentation/evidence
// author after the final gates complete. The validator must:
//
//   - accept phase "3" evidence with the full cumulative required-check set
//     (all Phase 2 checks plus one check per P3 task family);
//   - accept honest "pending" statuses for Phase 3 while final gates and
//     audits are still open (the shipped document no longer uses pending
//     once all six final gates completed and the evidence flipped to "pass");
//   - keep rejecting "pending" for the closed Phase 1/2 gates;
//   - keep enforcing that every parity ledger row is owned (task assignment)
//     and that Phase 3 rows only reference the fourteen approved task IDs.

// phase3OnlyCheckNames are the Phase 3 additions keyed by P3 task family.
// They mirror the evidence taxonomy of the approved design
// (.sisyphus/plans/phase-3-design.md Audit 5 and the Phase 3 exit gate).
var phase3OnlyCheckNames = []string{
	"matrix-parity",              // P3-T01 provider/format/OAuth matrices
	"format-tests",               // P3-T02 format detection and codec families
	"translator-tests",           // P3-T03 translator graph and native passthrough
	"compatible-provider-tests",  // P3-T04 generic compatible providers
	"specialized-executor-tests", // P3-T05 specialized executors by protocol family
	"oauth-tests",                // P3-T06 OAuth and import flow families
	"refresh-tests",              // P3-T07 proactive/reactive refresh coordinator
	"state-checkpoint-tests",     // P3-T08 account state, cooldown, locks, checkpoints
	"catalog-tests",              // P3-T09 aliases, disabled models, model catalog
	"combo-tests",                // P3-T10 combo modes
	"mutator-tests",              // P3-T11 request mutators
	"modality-tests",             // P3-T12 modalities
	"registry-parity",            // P3-T13 generated registry parity
	"provider-harness",           // P3-T14 provider matrix harness
}

// phase3OnlyCheckSet returns the Phase 3 additions as a set.
func phase3OnlyCheckSet() map[string]bool {
	set := make(map[string]bool, len(phase3OnlyCheckNames))
	for _, name := range phase3OnlyCheckNames {
		set[name] = true
	}
	return set
}

// phase3PassingChecks builds a full passing check list for Phase 3: every
// Phase 2 required check plus every Phase 3 addition.
func phase3PassingChecks() []GateCheck {
	checks := phase2PassingChecks()
	for _, name := range phase3OnlyCheckNames {
		checks = append(checks, GateCheck{Name: name, Status: "pass", Output: "ok"})
	}
	return checks
}

// TestRequiredPhase3CheckNames pins the exact Phase 3 required-check set:
// the Phase 2 set plus exactly the fourteen P3 task-family checks.
func TestRequiredPhase3CheckNames(t *testing.T) {
	required := RequiredPhase3CheckNames()
	set := make(map[string]bool, len(required))
	for _, name := range required {
		set[name] = true
	}

	// Cumulative: every Phase 2 required check must be present.
	phase2 := phaseCheckRequirements["2"]
	for name := range phase2 {
		if !set[name] {
			t.Errorf("RequiredPhase3CheckNames missing Phase 2 check %q", name)
		}
	}

	// Exactly the fourteen P3 additions, and nothing else beyond phase 2.
	expectedTotal := len(phase2) + len(phase3OnlyCheckNames)
	if len(required) != expectedTotal {
		t.Errorf("RequiredPhase3CheckNames has %d entries, want %d (phase 2 %d + P3 %d)",
			len(required), expectedTotal, len(phase2), len(phase3OnlyCheckNames))
	}
	for _, name := range phase3OnlyCheckNames {
		if !set[name] {
			t.Errorf("RequiredPhase3CheckNames missing Phase 3 check %q", name)
		}
	}
	for name := range set {
		if _, ok := phase2[name]; ok {
			continue
		}
		if !phase3OnlyCheckSet()[name] {
			t.Errorf("RequiredPhase3CheckNames contains unexpected check %q", name)
		}
	}
}

// TestGateEvidence_Phase3_ValidPass validates that Phase 3 evidence with the
// full required check set and status "pass" is accepted and passes the gate.
func TestGateEvidence_Phase3_ValidPass(t *testing.T) {
	ge := &GateEvidence{
		Phase:           "3",
		Status:          "pass",
		Timestamp:       time.Now().UTC(),
		DurationSeconds: 30.0,
		Checks:          phase3PassingChecks(),
	}

	if err := ValidateGateEvidence(ge); err != nil {
		t.Fatalf("RED: ValidateGateEvidence() unexpected error for phase 3: %v", err)
	}
	if !GateEvidencePassed(ge) {
		t.Error("GateEvidencePassed() = false, want true for phase 3 with all checks passing")
	}
}

// TestGateEvidence_Phase3_AllPendingValid validates that Phase 3 evidence may
// honestly report "pending" for every required check while the final gates
// and audits remain open.
func TestGateEvidence_Phase3_AllPendingValid(t *testing.T) {
	checks := make([]GateCheck, 0, len(RequiredPhase3CheckNames()))
	for _, name := range RequiredPhase3CheckNames() {
		checks = append(checks, GateCheck{Name: name, Status: "pending", Output: "final gate pending"})
	}
	ge := &GateEvidence{
		Phase:           "3",
		Status:          "pending",
		Timestamp:       time.Now().UTC(),
		DurationSeconds: 0.0,
		Checks:          checks,
	}

	if err := ValidateGateEvidence(ge); err != nil {
		t.Fatalf("RED: ValidateGateEvidence() must accept honest pending phase 3 evidence: %v", err)
	}
	if GateEvidencePassed(ge) {
		t.Error("GateEvidencePassed() = true, want false while evidence is pending")
	}
}

// TestGateEvidence_Phase3_PendingCheckValidButNotPassed validates that a
// pending check is structurally valid but does not count as a passed gate.
func TestGateEvidence_Phase3_PendingCheckValidButNotPassed(t *testing.T) {
	checks := phase3PassingChecks()
	for i := range checks {
		if checks[i].Name == "oauth-tests" {
			checks[i] = GateCheck{Name: "oauth-tests", Status: "pending", Output: "e2e fake-provider run pending"}
		}
	}
	ge := &GateEvidence{
		Phase:           "3",
		Status:          "pass",
		Timestamp:       time.Now().UTC(),
		DurationSeconds: 25.0,
		Checks:          checks,
	}

	if err := ValidateGateEvidence(ge); err != nil {
		t.Fatalf("RED: ValidateGateEvidence() must accept a pending check in phase 3: %v", err)
	}
	if GateEvidencePassed(ge) {
		t.Error("GateEvidencePassed() = true, want false when a required check is pending")
	}
}

// TestGateEvidence_Phase3_MissingRequiredCheckFails validates that pending
// states never excuse a missing required check.
func TestGateEvidence_Phase3_MissingRequiredCheckFails(t *testing.T) {
	checks := phase3PassingChecks()
	filtered := checks[:0]
	for _, c := range checks {
		if c.Name == "combo-tests" {
			continue // combo-tests is missing
		}
		filtered = append(filtered, c)
	}
	ge := &GateEvidence{
		Phase:           "3",
		Status:          "pending",
		Timestamp:       time.Now().UTC(),
		DurationSeconds: 10.0,
		Checks:          filtered,
	}

	err := ValidateGateEvidence(ge)
	if err == nil {
		t.Fatal("RED: ValidateGateEvidence() must fail when a required phase 3 check is missing")
	}
	if !regexp.MustCompile(`"combo-tests"`).MatchString(err.Error()) {
		t.Errorf("RED: error should name the missing check, got: %v", err)
	}
}

// TestPhase3EvidenceFileValidates is the repository-owned machine validation
// path for the shipped gate-evidence document. It loads the real
// docs/implementation/gate-evidence/phase-3.json, unmarshals it, and runs the
// full ValidateGateEvidence requirements check. The shipped document must be
// structurally valid for phase "3" (every required Phase 1/2 inherited name and
// the fourteen P3-T01..P3-T14 additions present with honest statuses) and, on
// the final revision, must report GateEvidencePassed true with all six final
// release/audit/CI/merge/deploy/live gates satisfied. Run with:
//
//	go test ./internal/governance/ -run TestPhase3EvidenceFileValidates -count=1
func TestPhase3EvidenceFileValidates(t *testing.T) {
	root, err := RootDir()
	if err != nil {
		t.Skip("project root not found:", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "docs", "implementation", "gate-evidence", "phase-3.json"))
	if err != nil {
		t.Fatalf("failed to read phase-3.json: %v", err)
	}

	ge, err := UnmarshalGateEvidence(data)
	if err != nil {
		t.Fatalf("phase-3.json is not valid gate-evidence JSON: %v", err)
	}
	if ge.Phase != "3" {
		t.Fatalf("phase-3.json declares phase %q, want \"3\"", ge.Phase)
	}
	if err := ValidateGateEvidence(ge); err != nil {
		t.Fatalf("RED: shipped phase-3.json fails ValidateGateEvidence: %v", err)
	}
	if !GateEvidencePassed(ge) {
		t.Fatal("shipped phase-3.json must report GateEvidencePassed once all final gates are satisfied")
	}
	if ge.Status != "pass" {
		t.Errorf("overall phase-3.json status = %q, want \"pass\" once the final gates are closed", ge.Status)
	}

	// Every name returned by RequiredPhase3CheckNames() must be present in the
	// shipped document with an honest status.
	required := make(map[string]bool, len(RequiredPhase3CheckNames()))
	for _, name := range RequiredPhase3CheckNames() {
		required[name] = true
	}
	present := make(map[string]bool, len(ge.Checks))
	for _, c := range ge.Checks {
		present[c.Name] = true
		if c.Name == "" || c.Status == "" || c.Output == "" {
			t.Errorf("shipped check %q must carry name, status, and output", c.Name)
		}
	}
	for name := range required {
		if !present[name] {
			t.Errorf("shipped phase-3.json is missing required check %q", name)
		}
	}

	// The six final-gate checks must all be present and pass; no check may
	// remain pending or claim a pass it does not have.
	finalGates := map[string]bool{
		"gate-final-full-matrix": true,
		"gate-five-audits":       true,
		"gate-pr-ci":             true,
		"gate-merge":             true,
		"gate-deploy-rollback":   true,
		"gate-live-validation":   true,
	}
	pendingCount := 0
	finalGatePassCount := 0
	for _, c := range ge.Checks {
		if c.Status == "pending" {
			pendingCount++
			t.Errorf("unexpected pending check %q: all final gates are satisfied", c.Name)
		}
		if finalGates[c.Name] {
			if c.Status == "pass" {
				finalGatePassCount++
			} else {
				t.Errorf("final gate %q must have status pass, got %q", c.Name, c.Status)
			}
		}
	}
	if pendingCount != 0 {
		t.Errorf("shipped phase-3.json has %d pending checks, want 0 once all final gates are satisfied", pendingCount)
	}
	if finalGatePassCount != len(finalGates) {
		t.Errorf("shipped phase-3.json has %d/6 final gates passing, want all six", finalGatePassCount)
	}
}

// TestGateEvidence_Phase1_RejectsPending guards the closed Phase 1 gate:
// "pending" is only an honest state for Phase 3, never for completed gates.
func TestGateEvidence_Phase1_RejectsPending(t *testing.T) {
	ge := &GateEvidence{
		Phase:     "1",
		Status:    "pending",
		Timestamp: time.Now().UTC(),
		Checks:    allPassingChecks(),
	}
	if err := ValidateGateEvidence(ge); err == nil {
		t.Fatal("ValidateGateEvidence() must reject pending status for phase 1")
	}
}

// TestGateEvidence_Phase2_RejectsPending guards the closed Phase 2 gate.
func TestGateEvidence_Phase2_RejectsPending(t *testing.T) {
	ge := &GateEvidence{
		Phase:     "2",
		Status:    "pending",
		Timestamp: time.Now().UTC(),
		Checks:    phase2PassingChecks(),
	}
	if err := ValidateGateEvidence(ge); err == nil {
		t.Fatal("ValidateGateEvidence() must reject pending status for phase 2")
	}
}

// TestSchemaStatusEnumsAllowPending verifies the gate-evidence JSON schema
// accepts "pending" for evidence-level and check-level status so Phase 3
// evidence can be written honestly while final gates remain open.
func TestSchemaStatusEnumsAllowPending(t *testing.T) {
	root, err := RootDir()
	if err != nil {
		t.Skip("project root not found:", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "docs", "implementation", "gate-evidence", "schema.json"))
	if err != nil {
		t.Fatalf("failed to read schema.json: %v", err)
	}

	var schema map[string]interface{}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("schema.json is not valid JSON: %v", err)
	}

	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("schema missing 'properties'")
	}

	statusProp, ok := props["status"].(map[string]interface{})
	if !ok {
		t.Fatal("schema missing 'status' property")
	}
	statusEnum, ok := statusProp["enum"].([]interface{})
	if !ok {
		t.Fatal("schema status property missing 'enum'")
	}
	if !containsString(statusEnum, "pending") {
		t.Errorf("RED: schema status enum %v must allow \"pending\" for phase 3 evidence", statusEnum)
	}

	checksProp, ok := props["checks"].(map[string]interface{})
	if !ok {
		t.Fatal("schema missing 'checks' property")
	}
	items, ok := checksProp["items"].(map[string]interface{})
	if !ok {
		t.Fatal("schema checks.items missing")
	}
	itemProps, ok := items["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("schema checks.items.properties missing")
	}
	checkStatus, ok := itemProps["status"].(map[string]interface{})
	if !ok {
		t.Fatal("schema check item missing 'status' property")
	}
	checkEnum, ok := checkStatus["enum"].([]interface{})
	if !ok {
		t.Fatal("schema check status property missing 'enum'")
	}
	if !containsString(checkEnum, "pending") {
		t.Errorf("RED: schema check status enum %v must allow \"pending\"", checkEnum)
	}
}

func containsString(list []interface{}, want string) bool {
	for _, v := range list {
		if s, ok := v.(string); ok && s == want {
			return true
		}
	}
	return false
}

// TestParityRowsOwnedAndNoUnowned verifies every parity ledger row carries
// ownership (a task assignment) and a capability — no unowned rows may exist,
// regardless of phase. This must hold for the Phase 3 rows the documentation
// author adds later.
func TestParityRowsOwnedAndNoUnowned(t *testing.T) {
	root, err := RootDir()
	if err != nil {
		t.Skip("project root not found:", err)
	}
	var ps ParityStatus
	if err := LoadYAML(filepath.Join(root, "docs", "implementation", "parity-status.yaml"), &ps); err != nil {
		t.Fatalf("failed to load parity-status.yaml: %v", err)
	}

	taskPattern := regexp.MustCompile(`^P[1-5]-T\d+$`)
	unowned := map[string]bool{
		"": true, "none": true, "unowned": true,
		"TBD": true, "tbd": true, "to be determined": true,
	}
	for _, row := range ps.ParityRows {
		if row.ID == "" {
			t.Error("unowned row: empty id")
		}
		if row.Capability == "" {
			t.Errorf("row %s has no capability", row.ID)
		}
		if unowned[strings.ToLower(row.Task)] {
			t.Errorf("row %s is unowned: task=%q", row.ID, row.Task)
		}
		if row.Task != "" && !taskPattern.MatchString(row.Task) {
			t.Errorf("row %s task %q does not match ownership format P<phase>-T<task>", row.ID, row.Task)
		}
	}
}

// TestPhase3ParityRowsUseKnownTaskIDs enforces P3-T01..T14 ownership: any
// Phase 3 parity row must reference one of the fourteen approved task IDs
// from plans/phase-3-engine-provider-matrix.md.
func TestPhase3ParityRowsUseKnownTaskIDs(t *testing.T) {
	root, err := RootDir()
	if err != nil {
		t.Skip("project root not found:", err)
	}
	var ps ParityStatus
	if err := LoadYAML(filepath.Join(root, "docs", "implementation", "parity-status.yaml"), &ps); err != nil {
		t.Fatalf("failed to load parity-status.yaml: %v", err)
	}

	known := make(map[string]bool, 14)
	for i := 1; i <= 14; i++ {
		known[fmt.Sprintf("P3-T%02d", i)] = true
	}

	for _, row := range ps.ParityRows {
		if row.Phase != 3 {
			continue
		}
		if row.Task == "" {
			t.Errorf("phase 3 row %s has no task ownership", row.ID)
			continue
		}
		if !known[row.Task] {
			t.Errorf("phase 3 row %s references unknown task %q (approved: P3-T01..P3-T14)", row.ID, row.Task)
		}
	}
}
