package harness_test

import (
	"testing"

	"gorouter/tests/providers/harness"
)

func TestEnvVarNameConvention(t *testing.T) {
	cases := []struct {
		providerID, want string
	}{
		{"openai", "GOROUTER_LIVE_OPENAI_KEY"},
		{"black-forest-labs", "GOROUTER_LIVE_BLACK_FOREST_LABS_KEY"},
		{"codebuddy-cn", "GOROUTER_LIVE_CODEBUDDY_CN_KEY"},
		{"aws-polly", "GOROUTER_LIVE_AWS_POLLY_KEY"},
		{"vercel-ai-gateway", "GOROUTER_LIVE_VERCEL_AI_GATEWAY_KEY"},
		{"youcom", "GOROUTER_LIVE_YOUCOM_KEY"},
	}
	for _, c := range cases {
		if got := harness.EnvVarName(c.providerID); got != c.want {
			t.Errorf("EnvVarName(%q) = %q, want %q", c.providerID, got, c.want)
		}
	}
}

func TestLiveBaseURLEnvVar(t *testing.T) {
	if got, want := harness.LiveBaseURLEnvVar("black-forest-labs"), "GOROUTER_LIVE_BLACK_FOREST_LABS_BASE_URL"; got != want {
		t.Errorf("LiveBaseURLEnvVar = %q, want %q", got, want)
	}
}

func TestPlaceholderCredentialIsInertAndDistinct(t *testing.T) {
	a, b := harness.PlaceholderCredential("openai"), harness.PlaceholderCredential("anthropic")
	if a == b {
		t.Fatalf("placeholders must be distinct per provider, got %q twice", a)
	}
	if harness.PlaceholderCredential("openai") != a {
		t.Error("PlaceholderCredential must be deterministic per provider id")
	}
}

func TestEnvSourceLookup(t *testing.T) {
	t.Setenv("GOROUTER_LIVE_OPENAI_KEY", "env-secret-value")
	src := harness.EnvSource{}
	if v, ok := src.Lookup("GOROUTER_LIVE_OPENAI_KEY"); !ok || v != "env-secret-value" {
		t.Errorf("EnvSource lookup = (%q, %v), want (env-secret-value, true)", v, ok)
	}
	if _, ok := src.Lookup("GOROUTER_LIVE_ANTHROPIC_KEY"); ok {
		t.Error("EnvSource must report missing env vars as not found")
	}
}

func TestMemSourceLookup(t *testing.T) {
	src := harness.MemSource{"GOROUTER_LIVE_ANTHROPIC_KEY": "store-secret"}
	if v, ok := src.Lookup("GOROUTER_LIVE_ANTHROPIC_KEY"); !ok || v != "store-secret" {
		t.Errorf("MemSource lookup = (%q, %v), want (store-secret, true)", v, ok)
	}
	if _, ok := src.Lookup("GOROUTER_LIVE_OPENAI_KEY"); ok {
		t.Error("MemSource must report absent keys as not found")
	}
}
