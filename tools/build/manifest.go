package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// Manifest is the release manifest written next to every release payload. It
// deliberately carries no timestamp so two builds from identical inputs produce
// byte-identical manifest files.
type Manifest struct {
	Commit        string  `json:"commit"`
	Version       string  `json:"version"`
	GoVersion     string  `json:"go_version"`
	OS            string  `json:"os"`
	Arch          string  `json:"arch"`
	FrontendHash  string  `json:"frontend_hash"`
	PayloadSHA256 string  `json:"payload_sha256"`
	Signature     *string `json:"signature"`
}

var (
	commitRe  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	versionRe = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)
	goVerRe   = regexp.MustCompile(`^go[0-9]+(\.[0-9]+)*`)
	sha256Re  = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

var (
	validOS   = map[string]bool{"linux": true, "darwin": true, "windows": true}
	validArch = map[string]bool{"amd64": true, "arm64": true, "386": true}
)

// Marshal renders stable JSON: field order is fixed by the struct declaration
// and the manifest holds no wall-clock data, so output is byte-deterministic.
func (m *Manifest) Marshal() ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}

// ValidateManifest enforces the same constraints declared in
// build/manifest.schema.json so a malformed manifest is rejected before it is
// written.
func ValidateManifest(m *Manifest) error {
	if m == nil {
		return errors.New("manifest is nil")
	}
	if !commitRe.MatchString(m.Commit) {
		return fmt.Errorf("commit %q must be 40 lowercase hex characters", m.Commit)
	}
	if !versionRe.MatchString(m.Version) {
		return fmt.Errorf("version %q must be SemVer of the form vMAJOR.MINOR.PATCH[-pre][+build]", m.Version)
	}
	if !goVerRe.MatchString(m.GoVersion) {
		return fmt.Errorf("go_version %q must look like go1.25", m.GoVersion)
	}
	if !validOS[m.OS] {
		return fmt.Errorf("os %q is not a supported release platform", m.OS)
	}
	if !validArch[m.Arch] {
		return fmt.Errorf("arch %q is not a supported release architecture", m.Arch)
	}
	if !sha256Re.MatchString(m.FrontendHash) {
		return fmt.Errorf("frontend_hash %q must be 64 lowercase hex characters", m.FrontendHash)
	}
	if !sha256Re.MatchString(m.PayloadSHA256) {
		return fmt.Errorf("payload_sha256 %q must be 64 lowercase hex characters", m.PayloadSHA256)
	}
	return nil
}

func WriteManifest(m *Manifest, dir string) error {
	if err := ValidateManifest(m); err != nil {
		return err
	}
	data, err := m.Marshal()
	if err != nil {
		return err
	}
	data = append(data, '\n')
	manifestPath := filepath.Join(filepath.Clean(dir), "manifest.json")
	return os.WriteFile(manifestPath, data, 0o600)
}
