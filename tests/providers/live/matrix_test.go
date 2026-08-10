package live_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"gorouter/tests/providers/harness"
)

type matrixCell struct {
	ProviderID string
	Format     string
	OAuthFlow  string
}

type matrixRow struct {
	ProviderID       string   `yaml:"provider_id"`
	AuthType         string   `yaml:"auth_type"`
	SupportedFormats []string `yaml:"supported_formats"`
}

type providerMatrix struct {
	Providers []matrixRow `yaml:"providers"`
}

func repoRoot() string { return filepath.Join("..", "..", "..") }

func loadDeclaredMatrix(t *testing.T) ([]matrixCell, []matrixRow) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(), "docs", "implementation", "provider-matrix.yaml"))
	if err != nil {
		t.Fatalf("read provider matrix: %v", err)
	}
	var matrix providerMatrix
	if err := yaml.Unmarshal(data, &matrix); err != nil {
		t.Fatalf("parse provider matrix: %v", err)
	}
	cells := make([]matrixCell, 0)
	for _, row := range matrix.Providers {
		flow := ""
		if row.AuthType == "oauth" {
			flow = row.AuthType
		}
		for _, format := range row.SupportedFormats {
			cells = append(cells, matrixCell{ProviderID: row.ProviderID, Format: format, OAuthFlow: flow})
		}
	}
	return cells, matrix.Providers
}

func cellSet(cells []matrixCell) map[string]struct{} {
	out := make(map[string]struct{}, len(cells))
	for _, cell := range cells {
		out[cell.ProviderID+"\x00"+cell.Format] = struct{}{}
	}
	return out
}

func assertMatrixParity(declared, represented []matrixCell) error {
	want, got := cellSet(declared), cellSet(represented)
	var missing, extra []string
	for key := range want {
		if _, ok := got[key]; !ok {
			missing = append(missing, key)
		}
	}
	for key := range got {
		if _, ok := want[key]; !ok {
			extra = append(extra, key)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 || len(extra) > 0 {
		return fmt.Errorf("matrix cell parity mismatch: missing=%v extra=%v", missing, extra)
	}
	return nil
}

func assertMatrixCoverage(declared []matrixCell, manifests []harness.Manifest) error {
	represented := make([]matrixCell, 0)
	for _, manifest := range manifests {
		for format := range manifest.MockFixtures {
			represented = append(represented, matrixCell{ProviderID: manifest.ProviderID, Format: format})
		}
	}
	if err := assertMatrixParity(declared, represented); err != nil {
		return err
	}
	declaredOAuth := map[string]bool{}
	representedOAuth := map[string]bool{}
	for _, cell := range declared {
		if cell.OAuthFlow != "" {
			declaredOAuth[cell.OAuthFlow] = true
		}
	}
	for _, manifest := range manifests {
		if manifest.AuthType == "oauth" {
			representedOAuth[manifest.AuthType] = true
		}
	}
	for flow := range declaredOAuth {
		if !representedOAuth[flow] {
			return fmt.Errorf("OAuth flow %q has no harness fixture/reference", flow)
		}
	}
	return nil
}

func classifyResult(result harness.Result, runErr error) string {
	if runErr != nil {
		return "FAIL"
	}
	switch result.Status {
	case harness.ResultPass:
		return "PASS"
	case harness.ResultFail:
		return "FAIL"
	default:
		return "SKIP"
	}
}

func TestProviderMatrixOffline(t *testing.T) {
	declared, rows := loadDeclaredMatrix(t)
	manifests, err := harness.LoadAllManifests(filepath.Join(repoRoot(), "tests", "providers", "manifests"))
	if err != nil {
		t.Fatal(err)
	}
	represented := make([]matrixCell, 0)
	for _, manifest := range manifests {
		for format := range manifest.MockFixtures {
			represented = append(represented, matrixCell{ProviderID: manifest.ProviderID, Format: format})
		}
	}
	if err := assertMatrixParity(declared, represented); err != nil {
		t.Fatal(err)
	}
	if err := assertMatrixCoverage(declared, manifestsToValues(manifests)); err != nil {
		t.Fatal(err)
	}
	for _, manifest := range manifests {
		result, runErr := harness.RunProvider(context.Background(), manifest, harness.Options{Mode: harness.ModeDryRun})
		classification := classifyResult(result, runErr)
		if runErr != nil {
			t.Errorf("cell=%s: %v", manifest.ProviderID, runErr)
		}
		if result.Status != harness.ResultSkip {
			t.Errorf("cell=%s: offline live matrix must skip, got %s", manifest.ProviderID, result.Status)
		}
		t.Logf("cell=%s status=%s classification=%s reason=%s", manifest.ProviderID, result.Status, classification, result.SkipReason)
	}
	t.Logf("matrix-cells=%d oauth-flows-covered=true wire-formats-covered=true", len(rows))
}

func manifestsToValues(in []*harness.Manifest) []harness.Manifest {
	out := make([]harness.Manifest, 0, len(in))
	for _, manifest := range in {
		out = append(out, *manifest)
	}
	return out
}

func TestMatrixClassificationPass(t *testing.T) {
	if got := classifyResult(harness.Result{Status: harness.ResultPass}, nil); got != "PASS" {
		t.Fatalf("classification = %q, want PASS", got)
	}
}

func TestMatrixClassificationFail(t *testing.T) {
	if got := classifyResult(harness.Result{Status: harness.ResultFail}, nil); got != "FAIL" {
		t.Fatalf("classification = %q, want FAIL", got)
	}
}

func TestMatrixCoverageAssertions(t *testing.T) {
	declared, _ := loadDeclaredMatrix(t)
	manifests, err := harness.LoadAllManifests(filepath.Join(repoRoot(), "tests", "providers", "manifests"))
	if err != nil {
		t.Fatal(err)
	}
	if err := assertMatrixCoverage(declared, manifestsToValues(manifests)); err != nil {
		t.Fatal(err)
	}
}

func Example() {
	fmt.Println(strings.Join([]string{"PASS", "FAIL", "SKIP"}, " | "))
	// Output: PASS | FAIL | SKIP
}
