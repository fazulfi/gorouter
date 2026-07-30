// Package governance provides gate evidence types and validation for
// Phase 1 and Phase 2 CI, quality, security, and gate evidence automation.
package governance

import (
	"encoding/json"
	"fmt"
	"time"
)

// GateEvidence represents the full output of a gate verification run.
type GateEvidence struct {
	Phase           string      `json:"phase"`
	Status          string      `json:"status"`
	Timestamp       time.Time   `json:"timestamp"`
	DurationSeconds float64     `json:"duration_seconds"`
	Checks          []GateCheck `json:"checks"`
}

// GateCheck represents a single verification check within a gate run.
type GateCheck struct {
	Name            string  `json:"name"`
	Status          string  `json:"status"`
	Output          string  `json:"output"`
	DurationSeconds float64 `json:"duration_seconds"`
}

// phaseCheckRequirements maps each phase to its required check names.
// Phase 1 covers governance, bootstrap, config, PostgreSQL, security
// middleware, auth primitives, and contracts. Phase 2 adds engine,
// streaming, executor, retry, cooldown, and end-to-end pipeline checks.
var phaseCheckRequirements = map[string]map[string]bool{
	"1": {
		"go-vet":              false,
		"go-test":             false,
		"go-race":             false,
		"frontend-build":      false,
		"frontend-test":       false,
		"code-coverage":       false,
		"coverage-threshold":  false,
		"traceability-docs":   false,
		"security-tests":      false,
		"security-audit":      false,
		"parity-tests":        false,
		"middleware-tests":    false,
		"auth-tests":          false,
		"observability-tests": false,
		"migration-schema":    false,
		"postgres-pool":       false,
		"pg-repositories":     false,
	},
	"2": {
		"go-vet":              false,
		"go-test":             false,
		"go-race":             false,
		"frontend-build":      false,
		"frontend-test":       false,
		"code-coverage":       false,
		"coverage-threshold":  false,
		"traceability-docs":   false,
		"security-tests":      false,
		"security-audit":      false,
		"parity-tests":        false,
		"middleware-tests":    false,
		"auth-tests":          false,
		"observability-tests": false,
		"migration-schema":    false,
		"postgres-pool":       false,
		"pg-repositories":     false,
		"engine-tests":        false,
		"streaming-tests":     false,
		"executor-tests":      false,
		"retry-tests":         false,
		"cooldown-tests":      false,
		"e2e-pipeline":        false,
		"gate-evidence":       false,
		"remediation-sha":     false,
		"decision-trace":      false,
		"gate-schema":         false,
	},
}

// requiredPhase1Checks lists the checks that must be present for Phase 1.
// These are the mandatory quality, security, and coverage checks that gate
// passage depends on.
var requiredPhase1Checks = phaseCheckRequirements["1"]

// informationalPhase1Checks lists checks that are informational only and
// do not block gate passage. These typically include deployment evidence
// and operational hardening records.
var informationalPhase1Checks = map[string]bool{
	"vps-deployment":    false,
	"sudoers-hardening": false,
	"sbom-generate":     false,
	"license-check":     false,
}

// RequiredPhase1CheckNames returns the sorted list of required check names.
func RequiredPhase1CheckNames() []string {
	names := make([]string, 0, len(requiredPhase1Checks))
	for name := range requiredPhase1Checks {
		names = append(names, name)
	}
	return names
}

// InformationalPhase1CheckNames returns the sorted list of informational check names.
func InformationalPhase1CheckNames() []string {
	names := make([]string, 0, len(informationalPhase1Checks))
	for name := range informationalPhase1Checks {
		names = append(names, name)
	}
	return names
}

// AllPhase1CheckNames returns all known Phase 1 check names (required + informational).
func AllPhase1CheckNames() []string {
	names := RequiredPhase1CheckNames()
	names = append(names, InformationalPhase1CheckNames()...)
	return names
}

// ValidateGateEvidence validates a GateEvidence struct against the
// requirements for the evidence's phase. It checks:
//   - Phase must be "1" or "2"
//   - Status must be "pass" or "fail"
//   - Timestamp must be set
//   - At least one check exists
//   - Each check has required fields
//   - All required checks for the phase are present (informational checks are optional)
func ValidateGateEvidence(ge *GateEvidence) error {
	required, ok := phaseCheckRequirements[ge.Phase]
	if !ok {
		return fmt.Errorf("gate_evidence: unsupported phase %q (supported: 1, 2)", ge.Phase)
	}
	if ge.Status != "pass" && ge.Status != "fail" {
		return fmt.Errorf("gate_evidence: status must be \"pass\" or \"fail\", got %q", ge.Status)
	}
	if ge.Timestamp.IsZero() {
		return fmt.Errorf("gate_evidence: timestamp is required")
	}
	if len(ge.Checks) == 0 {
		return fmt.Errorf("gate_evidence: at least one check is required")
	}

	found := copyMap(required)
	for i, check := range ge.Checks {
		if check.Name == "" {
			return fmt.Errorf("gate_evidence: check %d: name is required", i)
		}
		if check.Status != "pass" && check.Status != "fail" {
			return fmt.Errorf("gate_evidence: check %q: status must be \"pass\" or \"fail\", got %q", check.Name, check.Status)
		}
		if _, ok := found[check.Name]; ok {
			found[check.Name] = true
		}
		// Informational checks do not count toward the required set
	}

	for name, present := range found {
		if !present {
			return fmt.Errorf("gate_evidence: required check %q is missing for phase %s", name, ge.Phase)
		}
	}

	return nil
}

// GateEvidencePassed returns true if the evidence status is "pass" and all
// individual required checks also have status "pass". Informational checks
// (vps-deployment, sudoers-hardening, sbom-generate, license-check) do not
// affect the pass/fail determination.
func GateEvidencePassed(ge *GateEvidence) bool {
	if ge.Status != "pass" {
		return false
	}
	for _, c := range ge.Checks {
		if _, ok := informationalPhase1Checks[c.Name]; ok {
			continue // informational checks do not block gate passage
		}
		if c.Status != "pass" {
			return false
		}
	}
	return true
}

// MarshalGateEvidence serializes gate evidence to indented JSON.
func MarshalGateEvidence(ge *GateEvidence) ([]byte, error) {
	return json.MarshalIndent(ge, "", "  ")
}

// UnmarshalGateEvidence deserializes gate evidence from JSON bytes.
func UnmarshalGateEvidence(data []byte) (*GateEvidence, error) {
	var ge GateEvidence
	if err := json.Unmarshal(data, &ge); err != nil {
		return nil, fmt.Errorf("unmarshal gate evidence: %w", err)
	}
	return &ge, nil
}

// copyMap returns a shallow copy of a string-bool map.
func copyMap(src map[string]bool) map[string]bool {
	dst := make(map[string]bool, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
