package harness_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"gopkg.in/yaml.v3"

	"gorouter/internal/engine/providers/registry"
	"gorouter/tests/providers/harness"
)

func repoRoot(elem ...string) string {
	return filepath.Join(append([]string{"..", "..", ".."}, elem...)...)
}

type matrixRow struct {
	ProviderID       string   `yaml:"provider_id"`
	ProviderType     string   `yaml:"provider_type"`
	AuthType         string   `yaml:"auth_type"`
	SupportedFormats []string `yaml:"supported_formats"`
}

type matrixManifest struct {
	Entries []matrixRow `yaml:"providers"`
}

func loadMatrixRows(t *testing.T) []matrixRow {
	t.Helper()
	data, err := os.ReadFile(repoRoot("docs", "implementation", "provider-matrix.yaml"))
	if err != nil {
		t.Fatalf("read provider-matrix.yaml: %v", err)
	}
	var m matrixManifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		t.Fatalf("parse provider-matrix.yaml: %v", err)
	}
	return m.Entries
}

func loadManifests(t *testing.T) []*harness.Manifest {
	t.Helper()
	ms, err := harness.LoadAllManifests(repoRoot("tests", "providers", "manifests"))
	if err != nil {
		t.Fatalf("load manifests: %v", err)
	}
	return ms
}

func allPlaceholderSecrets(ms []*harness.Manifest) []string {
	secrets := make([]string, 0, len(ms))
	for _, m := range ms {
		secrets = append(secrets, harness.PlaceholderCredential(m.ProviderID))
	}
	return secrets
}

// TestManifestInventory asserts that the harness manifests are an
// exact 1:1 cover of the checked-in Phase 3 matrices and the
// generated registry: every provider-matrix.yaml row and every
// generated RegistryRows entry has exactly one manifest, with
// consistent identity, auth type, format coverage and credential
// naming.
func TestManifestInventory(t *testing.T) {
	rows := loadMatrixRows(t)
	ms := loadManifests(t)

	if len(rows) != 100 {
		t.Fatalf("matrix rows = %d, want 100", len(rows))
	}
	if len(registry.RegistryRows) != 100 {
		t.Fatalf("generated registry rows = %d, want 100", len(registry.RegistryRows))
	}
	if len(ms) != 100 {
		t.Fatalf("manifests = %d, want 100 (one per matrix row)", len(ms))
	}

	byID := make(map[string]*harness.Manifest, len(ms))
	for _, m := range ms {
		if _, dup := byID[m.ProviderID]; dup {
			t.Errorf("duplicate manifest for provider %s", m.ProviderID)
		}
		byID[m.ProviderID] = m
	}

	if len(byID) != len(rows) {
		t.Fatalf("manifest id set = %d rows, want %d (no orphans, no duplicates)", len(byID), len(rows))
	}

	for i, row := range rows {
		m, ok := byID[row.ProviderID]
		if !ok {
			t.Errorf("missing manifest for matrix row %s", row.ProviderID)
			continue
		}
		if m.ProviderType != row.ProviderType {
			t.Errorf("provider %s: manifest type %q != matrix type %q", row.ProviderID, m.ProviderType, row.ProviderType)
		}
		if m.AuthType != row.AuthType {
			t.Errorf("provider %s: manifest auth_type %q != matrix auth_type %q", row.ProviderID, m.AuthType, row.AuthType)
		}
		if row.ProviderID != registry.RegistryRows[i].ProviderID {
			t.Errorf("registry row %d provider id %q != matrix row provider id %q", i, registry.RegistryRows[i].ProviderID, row.ProviderID)
		}
		if m.AuthType == "none" {
			if m.CredentialEnvVar != "" {
				t.Errorf("provider %s: auth_type none must not declare credential_env_var, got %q", row.ProviderID, m.CredentialEnvVar)
			}
		} else if m.CredentialEnvVar != harness.EnvVarName(row.ProviderID) {
			t.Errorf("provider %s: credential env var %q != convention %q", row.ProviderID, m.CredentialEnvVar, harness.EnvVarName(row.ProviderID))
		}
		if len(m.MockFixtures) != len(row.SupportedFormats) {
			t.Errorf("provider %s: fixture count %d != supported format count %d", row.ProviderID, len(m.MockFixtures), len(row.SupportedFormats))
		}
		for _, format := range row.SupportedFormats {
			fixture, ok := m.MockFixtures[format]
			if !ok {
				t.Errorf("provider %s: missing fixture for format %s", row.ProviderID, format)
				continue
			}
			full := repoRoot(fixture)
			data, err := os.ReadFile(full)
			if err != nil {
				t.Errorf("provider %s: fixture %s unreadable: %v", row.ProviderID, fixture, err)
				continue
			}
			var doc any
			if err := json.Unmarshal(data, &doc); err != nil {
				t.Errorf("provider %s: fixture %s is not valid JSON: %v", row.ProviderID, fixture, err)
			}
		}
		if m.Timeout <= 0 {
			t.Errorf("provider %s: timeout must be positive, got %v", row.ProviderID, m.Timeout)
		}
		if m.Retries < 0 || m.Retries > 5 {
			t.Errorf("provider %s: retries out of range: %d", row.ProviderID, m.Retries)
		}
	}
}

func mockServerFor(t *testing.T, m *harness.Manifest, fixture []byte) *httptest.Server {
	t.Helper()
	cred := harness.PlaceholderCredential(m.ProviderID)
	wantHeaders := harness.AuthHeaders(m, cred)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(wantHeaders) != 0 {
			for name, want := range wantHeaders {
				if got := r.Header.Get(name); got != want {
					t.Errorf("provider %s: auth header %s = %q, want %q", m.ProviderID, name, got, want)
					http.Error(w, `{"error":{"message":"unauthorized"}}`, http.StatusUnauthorized)
					return
				}
			}
		} else {
			for _, name := range []string{"Authorization", "X-Api-Key", "X-Goog-Api-Key", "Cookie"} {
				if got := r.Header.Get(name); got != "" {
					t.Errorf("provider %s: unexpected auth header %s = %q (auth_type none)", m.ProviderID, name, got)
					http.Error(w, `{"error":{"message":"unauthorized"}}`, http.StatusUnauthorized)
					return
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(fixture)
	}))
}

// TestProviderMatrixMock runs the full mock/golden matrix: every
// manifest provider is probed through its own controlled mock server,
// isolated by its inert placeholder credential, and its artifact is
// compared against the golden fixture for its primary format. Providers
// run concurrently through a bounded worker pool; each run is
// independent and a failure in one provider never affects another.
func TestProviderMatrixMock(t *testing.T) {
	ms := loadManifests(t)
	secrets := allPlaceholderSecrets(ms)
	red := harness.NewRedactor(secrets, nil)

	var wg sync.WaitGroup
	sem := make(chan struct{}, 16)
	var passes atomic.Int32
	for _, m := range ms {
		wg.Add(1)
		go func(m *harness.Manifest) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			format, err := m.PrimaryFormat()
			if err != nil {
				t.Errorf("provider %s: primary format: %v", m.ProviderID, err)
				return
			}
			fixturePath, err := m.FixturePath(format)
			if err != nil {
				t.Errorf("provider %s: fixture path: %v", m.ProviderID, err)
				return
			}
			fixture, err := os.ReadFile(repoRoot(fixturePath))
			if err != nil {
				t.Errorf("provider %s: read fixture: %v", m.ProviderID, err)
				return
			}
			srv := mockServerFor(t, m, fixture)
			defer srv.Close()

			res, err := harness.RunProvider(context.Background(), m, harness.Options{
				Mode:     harness.ModeMock,
				BaseURL:  srv.URL,
				BaseDir:  repoRoot(),
				Redactor: red,
			})
			if err != nil {
				t.Errorf("provider %s: mock run failed: %v", m.ProviderID, err)
				return
			}
			if res.Status != harness.ResultPass {
				t.Errorf("provider %s: status = %s, want pass", m.ProviderID, res.Status)
				return
			}
			if err := harness.CompareArtifacts(res.Artifact, fixture); err != nil {
				t.Errorf("provider %s: artifact mismatch: %v", m.ProviderID, err)
				return
			}
			t.Logf("provider=%s credential=%s status=%s roundtrips=%d duration=%s",
				m.ProviderID, red.Redact(harness.PlaceholderCredential(m.ProviderID)),
				res.Status, res.RoundTrips, res.Duration)
			passes.Add(1)
		}(m)
	}
	wg.Wait()
	if got := int(passes.Load()); got != len(ms) {
		t.Errorf("passed %d/%d providers", got, len(ms))
	}
}

func TestMatrixCoverageConsistency(t *testing.T) {
	rows := loadMatrixRows(t)
	ms := loadManifests(t)
	matrixFormats := map[string]map[string]bool{}
	for _, row := range rows {
		set := map[string]bool{}
		for _, f := range row.SupportedFormats {
			set[f] = true
		}
		matrixFormats[row.ProviderID] = set
	}
	manifestByID := map[string]*harness.Manifest{}
	for _, m := range ms {
		manifestByID[m.ProviderID] = m
	}
	var bad []string
	for id, want := range matrixFormats {
		m, ok := manifestByID[id]
		if !ok {
			continue
		}
		got := map[string]bool{}
		for f := range m.MockFixtures {
			got[f] = true
		}
		if !reflect.DeepEqual(got, want) {
			bad = append(bad, fmt.Sprintf("%s: fixtures %v != formats %v", id, got, want))
		}
	}
	if len(bad) > 0 {
		t.Errorf("fixture/format coverage mismatches:\n%s", strings.Join(bad, "\n"))
	}
}
