package soak

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

var ErrInvalidManifest = errors.New("invalid soak manifest")

type LabManifest struct {
	SchemaVersion  string         `json:"schema_version"`
	Task           string         `json:"task"`
	Status         string         `json:"status"`
	CandidateSHA   string         `json:"candidate_sha"`
	ReferenceEnv   ReferenceEnv   `json:"reference_environment"`
	Common         CommonSettings `json:"common"`
	Gates          []GateSpec     `json:"gates"`
	SampleInterval string         `json:"sample_interval"`
	OutputDir      string         `json:"output_dir"`
	EvidencePath   string         `json:"evidence_path"`
	Owners         string         `json:"owner"`
}

type ReferenceEnv struct {
	Host    string `json:"host"`
	CPU     string `json:"cpu"`
	Memory  string `json:"memory"`
	Storage string `json:"storage"`
	OS      string `json:"os"`
}

type CommonSettings struct {
	WorkingDir           string `json:"working_directory"`
	ThresholdEnforcement string `json:"threshold_enforcement"`
	OutputFormat         string `json:"output_format"`
}

type GateSpec struct {
	ID           string    `json:"id"`
	Metric       string    `json:"metric"`
	Command      string    `json:"command"`
	Tooling      string    `json:"tooling"`
	Load         LoadSpec  `json:"load"`
	Threshold    Threshold `json:"threshold"`
	PassCriteria string    `json:"pass_criteria"`
	EvidenceFile string    `json:"evidence_file"`
	Owner        string    `json:"owner"`
}

type LoadSpec struct {
	Duration    string  `json:"duration"`
	Concurrent  int     `json:"concurrency"`
	ErrorBudget float64 `json:"error_budget"`
}

type Threshold struct {
	Value string  `json:"value"`
	P95Ms float64 `json:"p95_ms,omitempty"`
}

func ReadLabManifest(path string) (*LabManifest, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	var m LabManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if m.SchemaVersion == "" || m.Task == "" {
		return nil, ErrInvalidManifest
	}
	return &m, nil
}

func WriteLabManifest(path string, m *LabManifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		os.MkdirAll(dir, 0700)
	}
	return os.WriteFile(path, append(data, '\n'), 0600)
}
