package harness_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorouter/internal/domain/engine"
	"gorouter/tests/providers/harness"
)

func writeManifest(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return path
}

func TestLoadManifest(t *testing.T) {
	dir := t.TempDir()
	path := writeManifest(t, dir, "openai.yaml", `
provider_id: openai
provider_type: openai
auth_type: apikey
credential_env_var: GOROUTER_LIVE_OPENAI_KEY
timeout: 30s
retries: 2
mock_fixtures:
  FormatOpenAIChat: tests/providers/harness/testdata/fixtures/openai_chat.json
  FormatOpenAICompat: tests/providers/harness/testdata/fixtures/openai_compat.json
`)
	m, err := harness.LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if m.ProviderID != "openai" || m.ProviderType != "openai" || m.AuthType != "apikey" {
		t.Errorf("identity fields wrong: %+v", m)
	}
	if m.CredentialEnvVar != "GOROUTER_LIVE_OPENAI_KEY" {
		t.Errorf("credential env var = %q", m.CredentialEnvVar)
	}
	if m.Timeout != 30*time.Second {
		t.Errorf("timeout = %v, want 30s", m.Timeout)
	}
	if m.Retries != 2 {
		t.Errorf("retries = %d, want 2", m.Retries)
	}
	if len(m.MockFixtures) != 2 {
		t.Errorf("mock fixtures = %v, want 2 entries", m.MockFixtures)
	}
}

func TestLoadAllManifestsSorted(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, "youcom.yaml", "provider_id: youcom\nprovider_type: youcom\nauth_type: apikey\ncredential_env_var: GOROUTER_LIVE_YOUCOM_KEY\ntimeout: 30s\nretries: 2\nmock_fixtures:\n  FormatOpenAICompat: a.json\n")
	writeManifest(t, dir, "alicode.yaml", "provider_id: alicode\nprovider_type: alicode\nauth_type: apikey\ncredential_env_var: GOROUTER_LIVE_ALICODE_KEY\ntimeout: 30s\nretries: 2\nmock_fixtures:\n  FormatOpenAICompat: a.json\n")
	writeManifest(t, dir, "readme.txt", "not a manifest")
	ms, err := harness.LoadAllManifests(dir)
	if err != nil {
		t.Fatalf("LoadAllManifests: %v", err)
	}
	if len(ms) != 2 {
		t.Fatalf("got %d manifests, want 2", len(ms))
	}
	if ms[0].ProviderID != "alicode" || ms[1].ProviderID != "youcom" {
		t.Errorf("manifests must be sorted by provider id, got %s, %s", ms[0].ProviderID, ms[1].ProviderID)
	}
}

func TestManifestValidateErrors(t *testing.T) {
	valid := "provider_id: openai\nprovider_type: openai\nauth_type: apikey\ncredential_env_var: GOROUTER_LIVE_OPENAI_KEY\ntimeout: 30s\nretries: 2\nmock_fixtures:\n  FormatOpenAIChat: fixtures/openai_chat.json\n"
	cases := []struct {
		name    string
		old, kw string
	}{
		{"empty-provider-id", "provider_id: openai", "provider_id: \"\""},
		{"empty-provider-type", "provider_type: openai", "provider_type: \"\""},
		{"unknown-auth-type", "auth_type: apikey", "auth_type: magic"},
		{"zero-timeout", "timeout: 30s", "timeout: 0s"},
		{"negative-retries", "retries: 2", "retries: -1"},
		{"excessive-retries", "retries: 2", "retries: 9"},
		{"no-fixtures", "mock_fixtures:\n  FormatOpenAIChat: fixtures/openai_chat.json", "mock_fixtures: {}"},
		{"unknown-format-key", "FormatOpenAIChat: fixtures/openai_chat.json", "FormatBogus: x.json"},
		{"env-var-mismatch", "GOROUTER_LIVE_OPENAI_KEY", "GOROUTER_LIVE_WRONG_KEY"},
		{"none-auth-with-env-var", "auth_type: apikey", "auth_type: none"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			content := strings.Replace(valid, c.old, c.kw, 1)
			path := writeManifest(t, dir, "openai.yaml", content)
			if _, err := harness.LoadManifest(path); err == nil {
				t.Error("invalid manifest must fail validation")
			}
		})
	}
}

func TestManifestValidateNoneAuth(t *testing.T) {
	dir := t.TempDir()
	path := writeManifest(t, dir, "searxng.yaml", `
provider_id: searxng
provider_type: searxng
auth_type: none
timeout: 30s
retries: 2
mock_fixtures:
  FormatOpenAICompat: tests/providers/harness/testdata/fixtures/openai_compat.json
`)
	m, err := harness.LoadManifest(path)
	if err != nil {
		t.Fatalf("none-auth manifest must validate: %v", err)
	}
	if m.CredentialEnvVar != "" {
		t.Errorf("none-auth credential env var must be empty, got %q", m.CredentialEnvVar)
	}
}

func TestManifestPrimaryFormatAndFixturePath(t *testing.T) {
	m := &harness.Manifest{
		ProviderID:   "openai",
		ProviderType: "openai",
		AuthType:     "apikey",
		Timeout:      30 * time.Second,
		Retries:      2,
		MockFixtures: map[string]string{
			"FormatOpenAICompat": "compat.json",
			"FormatOpenAIChat":   "chat.json",
		},
	}
	primary, err := m.PrimaryFormat()
	if err != nil {
		t.Fatalf("PrimaryFormat: %v", err)
	}
	if primary != engine.FormatOpenAIChat {
		t.Errorf("primary format = %s, want FormatOpenAIChat (canonical order)", primary)
	}
	path, err := m.FixturePath(engine.FormatOpenAIChat)
	if err != nil || path != "chat.json" {
		t.Errorf("FixturePath = %q, %v", path, err)
	}
	if _, err := m.FixturePath(engine.FormatGemini); err == nil {
		t.Error("FixturePath must fail for formats without a fixture")
	}
}

func TestFormatName(t *testing.T) {
	names := map[engine.RequestFormat]string{
		engine.FormatOpenAIChat:     "FormatOpenAIChat",
		engine.FormatOpenAICompat:   "FormatOpenAICompat",
		engine.FormatCodexResponses: "FormatCodexResponses",
		engine.FormatAnthropic:      "FormatAnthropic",
		engine.FormatGemini:         "FormatGemini",
	}
	for format, want := range names {
		if got := harness.FormatName(format); got != want {
			t.Errorf("FormatName(%s) = %q, want %q", format, got, want)
		}
	}
	if got := harness.FormatName(engine.RequestFormat("bogus")); got != "" {
		t.Errorf("FormatName(bogus) = %q, want empty", got)
	}
}

func TestLoadManifestMissingFile(t *testing.T) {
	if _, err := harness.LoadManifest(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Error("missing manifest file must error")
	}
}
