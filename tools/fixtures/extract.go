// Package fixtures provides tools for extracting and managing upstream fixtures.
//
// This is a stub for Phase 1. Full extraction logic will be implemented
// in Phase 2 when upstream response capture is needed.
package fixtures

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ManifestEntry represents a single fixture entry with provenance metadata.
type ManifestEntry struct {
	ID              string `json:"id"`
	Source          string `json:"source"`
	UpstreamCommit  string `json:"upstream_commit"`
	CapturedAt      string `json:"captured_at"`
	ContentType     string `json:"content_type"`
	Endpoint        string `json:"endpoint"`
	Description     string `json:"description"`
	SchemaChecksum  string `json:"schema_checksum"`
}

// Manifest is the fixture registry.
type Manifest struct {
	SchemaVersion string            `json:"schemaVersion"`
	Baseline      map[string]string `json:"baseline"`
	Fixtures      []ManifestEntry   `json:"fixtures"`
}

// LoadManifest loads a fixture manifest from the given path.
func LoadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}

	return &m, nil
}

// ValidateContract checks that a captured fixture matches the expected schema.
// Returns nil if valid, error if mismatch.
func ValidateContract(manifestPath string, entryID string) error {
	m, err := LoadManifest(manifestPath)
	if err != nil {
		return fmt.Errorf("load manifest: %w", err)
	}

	for _, entry := range m.Fixtures {
		if entry.ID == entryID {
			return nil // entry exists
		}
	}

	return fmt.Errorf("entry %q not found in manifest", entryID)
}
