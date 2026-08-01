package harness_test

import (
	"strings"
	"testing"

	"gorouter/tests/providers/harness"
)

func TestRedactStripsCredentialClasses(t *testing.T) {
	cases := []struct {
		name, input string
	}{
		{"bearer-token", "Authorization: Bearer abcDEF123.ghiJKL456_mnoPQR789-xyz"},
		{"basic-auth", "Proxy-Authorization: Basic dXNlcjpwYXNzd29yZA=="},
		{"openai-key", "sk-proj-1234567890abcdef1234567890abcdef"},
		{"anthropic-key", "sk-ant-api03-abcdef1234567890abcdef1234567890"},
		{"github-pat", "ghp_1234567890abcdef1234567890abcdef"},
		{"github-oauth", "gho_1234567890abcdef1234567890abcdef"},
		{"gitlab-pat", "glpat-abcdef1234567890abcdef"},
		{"slack-token", "xoxb-1234567890-1234567890-abcdef123456"},
		{"google-api-key", "AIzaSyA1234567890abcdefghijklmnopqrstuv"},
		{"aws-access-key", "AKIA1234567890ABCDEF"},
		{"jwt", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ0ZXN0In0.abcdefghijklmnopqrstuvwxyz123456"},
		{"session-cookie", "session=8f14e45fceea167a5a36dedd4bea2543"},
		{"cookie-header", "Cookie: session=8f14e45fceea167a5a36dedd4bea2543; theme=dark"},
		{"x-api-key-header", "x-api-key: deadbeef1234567890"},
		{"x-goog-api-key-header", "x-goog-api-key: AIzaSyA1234567890abcdefghijklmnopqrstuv"},
		{"query-api-key", "https://example.com/v1?api_key=deadbeef1234567890"},
		{"query-token", "https://example.com/v1?token=deadbeef1234567890&n=1"},
	}
	red := harness.NewRedactor(nil, nil)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := red.Redact(c.input)
			if strings.Contains(got, c.input) {
				t.Errorf("Redact(%q) = %q — credential still present", c.input, got)
			}
			if !strings.Contains(got, harness.RedactedMarker) {
				t.Errorf("Redact(%q) = %q — missing %q marker", c.input, got, harness.RedactedMarker)
			}
		})
	}
}

func TestRedactExactSecrets(t *testing.T) {
	secret := "inert-credential-value-7f3a9c2e"
	red := harness.NewRedactor([]string{secret}, nil)
	got := red.Redact("provider used credential " + secret + " during probe")
	if strings.Contains(got, secret) || !strings.Contains(got, harness.RedactedMarker) {
		t.Errorf("Redact = %q — exact secret not stripped", got)
	}
}

func TestRedactBytes(t *testing.T) {
	secret := "sk-gorouter-mock-openai"
	red := harness.NewRedactor([]string{secret}, nil)
	got := red.RedactBytes([]byte(`{"credential":"` + secret + `"}`))
	if strings.Contains(string(got), secret) {
		t.Errorf("RedactBytes = %s — secret still present", got)
	}
}

func TestRedactLeavesPlainTextUntouched(t *testing.T) {
	red := harness.NewRedactor(nil, nil)
	plain := "provider=openai status=pass roundtrips=1 duration=12ms"
	if got := red.Redact(plain); got != plain {
		t.Errorf("plain text must pass through untouched, got %q", got)
	}
}

func TestRedactLongestSecretFirst(t *testing.T) {
	red := harness.NewRedactor([]string{"short", "longer-secret-value"}, nil)
	got := red.Redact("longer-secret-value")
	if strings.Contains(got, "longer-secret-value") {
		t.Errorf("longest secret must be matched before overlapping shorter ones, got %q", got)
	}
}

func TestDetectCredentialLeaks(t *testing.T) {
	secret := "sk-gorouter-mock-codex"
	text := "provider=codex credential=" + secret + " status=pass\nAuthorization: Bearer abcDEF123.ghiJKL456"
	leaks := harness.DetectCredentialLeaks(text, []string{secret})
	if len(leaks) < 2 {
		t.Errorf("DetectCredentialLeaks found %d leaks, want >= 2: %v", len(leaks), leaks)
	}
	for _, l := range leaks {
		if strings.Contains(l, harness.RedactedMarker) {
			t.Errorf("leak %q must be reported raw, not redacted", l)
		}
	}
}

func TestDetectCredentialLeaksCleanOutput(t *testing.T) {
	clean := "--- PASS: TestProviderMatrixMock (0.02s)\n" +
		"=== RUN   TestLiveDryRun_UnavailableCredentialsSkip\n" +
		"provider=openai credential=" + harness.RedactedMarker + " status=skip\n"
	if leaks := harness.DetectCredentialLeaks(clean, nil); len(leaks) != 0 {
		t.Errorf("clean output must have zero leaks, got %v", leaks)
	}
}

func TestDefaultCredentialPatterns(t *testing.T) {
	patterns := harness.DefaultCredentialPatterns()
	if len(patterns) == 0 {
		t.Fatal("DefaultCredentialPatterns must not be empty")
	}
	probe := "Bearer abcDEF123.ghiJKL456\nsk-proj-1234567890\nghp_1234567890abcdef"
	for _, p := range patterns {
		if p.MatchString(probe) {
			return
		}
	}
	t.Error("no default pattern matched a representative credential")
}
