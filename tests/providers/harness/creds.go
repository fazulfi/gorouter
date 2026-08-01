package harness

import (
	"os"
	"strings"
)

// CredentialSource resolves a credential value from an environment
// variable or a secret store.
type CredentialSource interface {
	Lookup(envVar string) (string, bool)
}

// EnvSource reads credentials from the process environment.
type EnvSource struct{}

// Lookup implements CredentialSource for the process environment.
func (EnvSource) Lookup(envVar string) (string, bool) {
	return os.LookupEnv(envVar)
}

// MemSource is an in-memory secret store used by tests and by the
// protected lab when credentials are injected via a secrets manager.
type MemSource map[string]string

// Lookup implements CredentialSource for an in-memory store.
func (m MemSource) Lookup(envVar string) (string, bool) {
	v, ok := m[envVar]
	return v, ok
}

// EnvVarName returns the live credential env var for a provider id
// using the GOROUTER_LIVE_<PROVIDER_ID>_KEY convention, e.g.
// GOROUTER_LIVE_OPENAI_KEY or GOROUTER_LIVE_BLACK_FOREST_LABS_KEY.
func EnvVarName(providerID string) string {
	upper := strings.ToUpper(strings.ReplaceAll(providerID, "-", "_"))
	return "GOROUTER_LIVE_" + upper + "_KEY"
}

// LiveBaseURLEnvVar returns the optional live endpoint override env
// var for a provider id (GOROUTER_LIVE_<PROVIDER_ID>_BASE_URL).
func LiveBaseURLEnvVar(providerID string) string {
	upper := strings.ToUpper(strings.ReplaceAll(providerID, "-", "_"))
	return "GOROUTER_LIVE_" + upper + "_BASE_URL"
}

// PlaceholderCredential returns the inert mock-mode credential for a
// provider id. It is never a real secret and is always redacted from
// harness output by the leak-fail gate.
func PlaceholderCredential(providerID string) string {
	return "sk-gorouter-mock-" + providerID
}
