package harness_test

import (
	"os/exec"
	"strings"
	"testing"

	"gorouter/tests/providers/harness"
)

// TestLeakFailGate is the leak-fail CI gate (P3-T14, design §14.2.9 /
// §15.4). It runs the harness mock matrix and live dry-run in a
// subprocess, scans every byte of stdout/stderr for credential
// patterns and exact placeholder secret values, and rejects the run if
// any credential-like string appears. The redaction marker must also
// be present in the output, proving the redactor was actually
// exercised.
func TestLeakFailGate(t *testing.T) {
	if testing.Short() {
		t.Skip("leak-fail gate runs in full CI mode")
	}
	ms := loadManifests(t)
	secrets := allPlaceholderSecrets(ms)

	cmd := exec.Command("go", "test", "-count=1", "-v",
		"-run", "TestProviderMatrixMock|TestLiveDryRun_UnavailableCredentialsSkip",
		"-timeout", "180s",
		"./tests/providers/harness/")
	cmd.Dir = repoRoot()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("harness subprocess failed: %v\n%s", err, out)
	}
	text := string(out)

	if leaks := harness.DetectCredentialLeaks(text, secrets); len(leaks) > 0 {
		t.Fatalf("credential leak detected in harness output (%d matches): %v", len(leaks), leaks)
	}
	if !strings.Contains(text, harness.RedactedMarker) {
		t.Errorf("redaction marker %q missing from harness output — redactor was not exercised", harness.RedactedMarker)
	}
}

// TestLeakFailGateDetectsLeaks is the positive control for the gate
// scanner: output containing credentials must be rejected, so the gate
// cannot pass vacuously.
func TestLeakFailGateDetectsLeaks(t *testing.T) {
	text := "provider=openai credential=sk-gorouter-mock-openai status=pass\n" +
		"Authorization: Bearer abcDEF123.ghiJKL456\n" +
		"Cookie: session=8f14e45fceea167a5a36dedd4bea2543\n"
	leaks := harness.DetectCredentialLeaks(text, []string{"sk-gorouter-mock-openai"})
	if len(leaks) < 3 {
		t.Errorf("gate scanner found %d leaks, want >= 3: %v", len(leaks), leaks)
	}
}
