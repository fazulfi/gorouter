package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"gorouter/internal/domain/engine"
)

// Manifest describes one provider-matrix.yaml row for the harness.
// One YAML file per provider lives under tests/providers/manifests/.
// Each manifest defines the provider type, the mock/golden fixture
// paths per supported wire format, the live credential env var name,
// and the probe test timeout/retry policy (P3-T14).
type Manifest struct {
	ProviderID       string            `yaml:"provider_id"`
	ProviderType     string            `yaml:"provider_type"`
	AuthType         string            `yaml:"auth_type"`
	CredentialEnvVar string            `yaml:"credential_env_var"`
	Timeout          time.Duration     `yaml:"timeout"`
	Retries          int               `yaml:"retries"`
	MockFixtures     map[string]string `yaml:"mock_fixtures"`
}

var validAuthTypes = map[string]bool{
	"apikey": true,
	"oauth":  true,
	"cookie": true,
	"none":   true,
	"pat":    true,
}

// LoadManifest reads and validates one manifest file.
func LoadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("harness: read manifest %s: %w", path, err)
	}
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("harness: parse manifest %s: %w", path, err)
	}
	if err := m.Validate(); err != nil {
		return nil, fmt.Errorf("harness: manifest %s: %w", path, err)
	}
	return &m, nil
}

// LoadAllManifests reads every *.yaml manifest under dir, ordered by
// provider id for determinism.
func LoadAllManifests(dir string) ([]*Manifest, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("harness: read manifest dir %s: %w", dir, err)
	}
	var out []*Manifest
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		m, err := LoadManifest(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ProviderID < out[j].ProviderID })
	return out, nil
}

// Validate checks structural invariants of the manifest: non-empty ids,
// known auth type, positive timeout, bounded retries, at least one
// mock fixture for a known format, and credential env var consistency
// (empty for auth_type none, otherwise the GOROUTER_LIVE_ convention).
func (m *Manifest) Validate() error {
	if strings.TrimSpace(m.ProviderID) == "" {
		return fmt.Errorf("provider_id must not be empty")
	}
	if strings.TrimSpace(m.ProviderType) == "" {
		return fmt.Errorf("provider_type must not be empty")
	}
	if !validAuthTypes[m.AuthType] {
		return fmt.Errorf("auth_type %q must be one of apikey/oauth/cookie/none/pat", m.AuthType)
	}
	if m.Timeout <= 0 {
		return fmt.Errorf("timeout must be positive, got %v", m.Timeout)
	}
	if m.Retries < 0 || m.Retries > 5 {
		return fmt.Errorf("retries out of range [0,5]: %d", m.Retries)
	}
	if len(m.MockFixtures) == 0 {
		return fmt.Errorf("mock_fixtures must declare at least one format fixture")
	}
	known := map[string]bool{}
	for _, f := range CanonicalFormatOrder() {
		known[FormatName(f)] = true
	}
	for format, fixture := range m.MockFixtures {
		if !known[format] {
			return fmt.Errorf("mock_fixtures key %q is not a known format constant", format)
		}
		if strings.TrimSpace(fixture) == "" {
			return fmt.Errorf("mock_fixtures[%s] must be a non-empty path", format)
		}
	}
	if m.AuthType == "none" {
		if m.CredentialEnvVar != "" {
			return fmt.Errorf("auth_type none must not declare credential_env_var %q", m.CredentialEnvVar)
		}
	} else if m.CredentialEnvVar != EnvVarName(m.ProviderID) {
		return fmt.Errorf("credential_env_var %q does not match convention %q", m.CredentialEnvVar, EnvVarName(m.ProviderID))
	}
	return nil
}

// PrimaryFormat returns the probe format for the manifest: the first
// supported format in canonical order that has a mock fixture.
func (m *Manifest) PrimaryFormat() (engine.RequestFormat, error) {
	for _, f := range CanonicalFormatOrder() {
		if _, ok := m.MockFixtures[FormatName(f)]; ok {
			return f, nil
		}
	}
	return "", fmt.Errorf("no fixture declared for any supported format")
}

// FixturePath returns the repo-relative mock/golden fixture path for
// the given format.
func (m *Manifest) FixturePath(format engine.RequestFormat) (string, error) {
	path, ok := m.MockFixtures[FormatName(format)]
	if !ok {
		return "", fmt.Errorf("no fixture declared for format %s", format)
	}
	return path, nil
}
