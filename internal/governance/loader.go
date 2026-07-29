// Package governance provides traceability document loading and validation
// for gorouter's implementation against upstream baseline.
package governance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// BaselineSHA is the exact upstream commit this implementation is based on.
const BaselineSHA = "79918c7830695bbca4a45c9fea4a42c3e9fd73d1"

// UpstreamMap represents the docs/implementation/upstream-map.yaml content.
type UpstreamMap struct {
	Baseline     BaselineRef       `yaml:"baseline" json:"baseline"`
	UpstreamFiles []UpstreamFile   `yaml:"upstream_files" json:"upstream_files"`
	Entries      []json.RawMessage `yaml:"entries" json:"entries"`
}

// BaselineRef identifies the upstream baseline.
type BaselineRef struct {
	Repository string `yaml:"repository" json:"repository"`
	Commit     string `yaml:"commit" json:"commit"`
	Product    string `yaml:"product" json:"product"`
	Version    string `yaml:"version" json:"version"`
}

// UpstreamFile maps one upstream file to a gorouter package.
type UpstreamFile struct {
	File             string   `yaml:"file" json:"file"`
	Responsibility   string   `yaml:"responsibility" json:"responsibility"`
	GorouterPackage  *string  `yaml:"gorouter_package" json:"gorouter_package"`
	Task             string   `yaml:"task" json:"task"`
	Status           string   `yaml:"status" json:"status"`
	DecisionRefs     []int    `yaml:"decision_refs,omitempty" json:"decision_refs,omitempty"`
}

// ParityStatus represents the docs/implementation/parity-status.yaml content.
type ParityStatus struct {
	BaselineCommit string       `yaml:"baseline_commit" json:"baseline_commit"`
	ParityRows     []ParityRow  `yaml:"parity_rows" json:"parity_rows"`
	Entries        []json.RawMessage `yaml:"entries" json:"entries"`
}

// ParityRow represents one ledger row.
type ParityRow struct {
	Area             string   `yaml:"area" json:"area"`
	ID               string   `yaml:"id" json:"id"`
	Capability       string   `yaml:"capability" json:"capability"`
	Disposition      string   `yaml:"disposition" json:"disposition"`
	Decisions        []int    `yaml:"decisions" json:"decisions"`
	Phase            int      `yaml:"phase" json:"phase"`
	Task             string   `yaml:"task" json:"task"`
	Status           string   `yaml:"status" json:"status"`
	RequiredEvidence string   `yaml:"required_evidence" json:"required_evidence"`
}

// DecisionStatus represents the docs/implementation/decision-status.yaml content.
type DecisionStatus struct {
	BaselineCommit string           `yaml:"baseline_commit" json:"baseline_commit"`
	ActiveDecisions []ActiveDecision `yaml:"active_decisions" json:"active_decisions"`
	Entries         []json.RawMessage `yaml:"entries" json:"entries"`
}

// ActiveDecision represents one tracked decision.
type ActiveDecision struct {
	ID     int    `yaml:"id" json:"id"`
	Title  string `yaml:"title" json:"title"`
	Phase  int    `yaml:"phase" json:"phase"`
	Task   string `yaml:"task" json:"task"`
	Status string `yaml:"status" json:"status"`
}

// FixtureManifest represents tests/fixtures/upstream/manifest.json content.
type FixtureManifest struct {
	SchemaVersion string          `json:"schemaVersion"`
	Baseline      BaselineRef     `json:"baseline"`
	Fixtures      []FixtureEntry  `json:"fixtures"`
}

// FixtureEntry represents one fixture entry.
type FixtureEntry struct {
	ID           string `json:"id"`
	UpstreamFile string `json:"upstreamFile"`
	Description  string `json:"description"`
	Phase        int    `json:"phase"`
	Task         string `json:"task"`
	Type         string `json:"type"`
	Sanitized    bool   `json:"sanitized"`
}

// LoadYAML reads and unmarshals a YAML file.
func LoadYAML(path string, dest interface{}) error {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

// LoadJSON reads and unmarshals a JSON file.
func LoadJSON(path string, dest interface{}) error {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

// RootDir finds the project root by looking for go.mod.
func RootDir() (string, error) {
	// Start from the current directory or a reasonable default
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getwd: %w", err)
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found from %s", cwd)
		}
		dir = parent
	}
}
