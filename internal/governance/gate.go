// Package governance provides gate evidence types and validation for
// Phase 1, Phase 2, and Phase 3 CI, quality, security, and gate evidence
// automation.
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
// Phase 3 adds one check per P3 task family (P3-T01..P3-T14): matrices,
// formats, translators, compatible providers, specialized executors,
// OAuth flows, refresh, state checkpoints, catalog, combos, mutators,
// modalities, generated registry parity, and the provider harness.
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
	"3": {
		// Phase 1 baseline
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
		// Phase 2 additions
		"engine-tests":    false,
		"streaming-tests": false,
		"executor-tests":  false,
		"retry-tests":     false,
		"cooldown-tests":  false,
		"e2e-pipeline":    false,
		"gate-evidence":   false,
		"remediation-sha": false,
		"decision-trace":  false,
		"gate-schema":     false,
		// Phase 3 additions (P3-T01..P3-T14)
		"matrix-parity":              false,
		"format-tests":               false,
		"translator-tests":           false,
		"compatible-provider-tests":  false,
		"specialized-executor-tests": false,
		"oauth-tests":                false,
		"refresh-tests":              false,
		"state-checkpoint-tests":     false,
		"catalog-tests":              false,
		"combo-tests":                false,
		"mutator-tests":              false,
		"modality-tests":             false,
		"registry-parity":            false,
		"provider-harness":           false,
	},
}

// requiredPhase3CheckNames lists the Phase 3 task-family check additions in
// P3-T01..P3-T14 order, mirroring the evidence taxonomy of the approved
// Phase 3 design (Audit 5 and the Phase 3 exit gate).
var requiredPhase3CheckNames = []string{
	"matrix-parity",              // P3-T01
	"format-tests",               // P3-T02
	"translator-tests",           // P3-T03
	"compatible-provider-tests",  // P3-T04
	"specialized-executor-tests", // P3-T05
	"oauth-tests",                // P3-T06
	"refresh-tests",              // P3-T07
	"state-checkpoint-tests",     // P3-T08
	"catalog-tests",              // P3-T09
	"combo-tests",                // P3-T10
	"mutator-tests",              // P3-T11
	"modality-tests",             // P3-T12
	"registry-parity",            // P3-T13
	"provider-harness",           // P3-T14
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

// RequiredPhase3CheckNames returns the Phase 3 required check names: the
// full Phase 2 set plus the P3-T01..P3-T14 task-family additions.
func RequiredPhase3CheckNames() []string {
	names := make([]string, 0, len(phaseCheckRequirements["3"]))
	for name := range phaseCheckRequirements["3"] {
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

// validStatus reports whether status is acceptable for the given phase.
// "pending" is an honest state only for Phase 3, whose final gates and
// audits remain open; the closed Phase 1/2 gates accept only pass/fail.
func validStatus(phase, status string) bool {
	switch status {
	case "pass", "fail":
		return true
	case "pending":
		return phase == "3"
	default:
		return false
	}
}

// ValidateGateEvidence validates a GateEvidence struct against the
// requirements for the evidence's phase. It checks:
//   - Phase must be "1", "2", or "3"
//   - Status must be "pass" or "fail" (Phase 3 also accepts "pending")
//   - Timestamp must be set
//   - At least one check exists
//   - Each check has required fields
//   - All required checks for the phase are present (informational checks are optional)
func ValidateGateEvidence(ge *GateEvidence) error {
	required, ok := phaseCheckRequirements[ge.Phase]
	if !ok {
		return fmt.Errorf("gate_evidence: unsupported phase %q (supported: 1, 2, 3)", ge.Phase)
	}
	if !validStatus(ge.Phase, ge.Status) {
		if ge.Phase == "3" {
			return fmt.Errorf("gate_evidence: status must be \"pass\", \"fail\", or \"pending\", got %q", ge.Status)
		}
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
		if !validStatus(ge.Phase, check.Status) {
			if ge.Phase == "3" {
				return fmt.Errorf("gate_evidence: check %q: status must be \"pass\", \"fail\", or \"pending\", got %q", check.Name, check.Status)
			}
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
