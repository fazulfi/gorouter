// Command gate runs Phase 1 gate verification and outputs JSON evidence.
//
// It executes all Phase 1 required checks (go vet, go test, go race, frontend,
// coverage, traceability, security, parity, middleware, auth, observability,
// migrations, postgres) and informational checks (deployment, hardening), then
// writes a structured JSON report to stdout.
//
// Exit code 0 means all required gates passed; exit code 1 means one or more
// required checks failed or the runner itself encountered an error.
//
// Usage:
//
//	go run ./tools/gate/verify.go
//
// Output:
//
//	{"phase":"1","status":"pass|fail","timestamp":"...","duration_seconds":...,"checks":[...]}
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// GateEvidence is the top-level gate verification report.
type GateEvidence struct {
	Phase           string      `json:"phase"`
	Status          string      `json:"status"`
	Timestamp       time.Time   `json:"timestamp"`
	DurationSeconds float64     `json:"duration_seconds"`
	Checks          []GateCheck `json:"checks"`
}

// GateCheck is a single verification check result.
type GateCheck struct {
	Name            string  `json:"name"`
	Status          string  `json:"status"`
	Output          string  `json:"output"`
	DurationSeconds float64 `json:"duration_seconds"`
}

// requiredChecks lists the check names that must pass for the gate to pass.
var requiredChecks = map[string]bool{
	"go-vet":              true,
	"go-test":             true,
	"go-race":             true,
	"frontend-build":      true,
	"frontend-test":       true,
	"code-coverage":       true,
	"coverage-threshold":  true,
	"traceability-docs":   true,
	"security-tests":      true,
	"security-audit":      true,
	"parity-tests":        true,
	"middleware-tests":    true,
	"auth-tests":          true,
	"observability-tests": true,
	"migration-schema":    true,
	"postgres-pool":       true,
	"pg-repositories":     true,
}

// informationalChecks lists check names that are recorded but do not affect
// the overall gate pass/fail status.
var informationalChecks = map[string]bool{
	"vps-deployment":    true,
	"sudoers-hardening": true,
	"sbom-generate":     true,
	"license-check":     true,
}

func runCheck(name string, fn func() (string, error)) GateCheck {
	start := time.Now()
	out, err := fn()
	duration := time.Since(start).Seconds()
	status := "pass"
	if err != nil {
		status = "fail"
	}
	return GateCheck{
		Name:            name,
		Status:          status,
		Output:          out,
		DurationSeconds: duration,
	}
}

func main() {
	os.Exit(run())
}

func run() int {
	rootDir, err := findRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error finding project root: %v\n", err)
		return 1
	}

	frontendDir := filepath.Join(rootDir, "frontend")
	start := time.Now()

	checks := []GateCheck{
		// ── Go toolchain ──────────────────────────────────────────────
		runCheck("go-vet", func() (string, error) {
			return runCmd(rootDir, "go", "vet", "./...")
		}),
		runCheck("go-test", func() (string, error) {
			return runCmd(rootDir, "go", "test", "-short", "-count=1", "./...")
		}),
		runCheck("go-race", func() (string, error) {
			return runCmd(rootDir, "go", "test", "-race", "-short", "-count=1", "./...")
		}),

		// ── Frontend ─────────────────────────────────────────────────
		runCheck("frontend-build", func() (string, error) {
			out, err := runCmd(frontendDir, "npm", "ci")
			if err != nil {
				return out, err
			}
			return runCmd(frontendDir, "npm", "run", "build")
		}),
		runCheck("frontend-test", func() (string, error) {
			return runCmd(frontendDir, "npm", "test")
		}),

		// ── Code coverage ────────────────────────────────────────────
		runCheck("code-coverage", func() (string, error) {
			return runCmd(rootDir, "go", "test", "-short", "-count=1", "-coverprofile=coverage.out", "-covermode=atomic", "./...")
		}),
		runCheck("coverage-threshold", func() (string, error) {
			out, err := runCmd(rootDir, "go", "tool", "cover", "-func=coverage.out")
			if err != nil {
				return out, err
			}
			// Parse total coverage from last line: "total:\t(statements)\tXX.X%"
			var pct float64
			for _, line := range strings.Split(out, "\n") {
				if strings.HasPrefix(line, "total:") {
					fields := strings.Fields(line)
					if len(fields) >= 3 {
						pctStr := strings.TrimSuffix(fields[len(fields)-1], "%")
						pct, err = strconv.ParseFloat(pctStr, 64)
						if err != nil {
							return out, fmt.Errorf("parse coverage %%: %w", err)
						}
					}
				}
			}
			if pct < 80.0 {
				return out, fmt.Errorf("coverage %.1f%% below threshold 80.0%%", pct)
			}
			return fmt.Sprintf("coverage %.1f%% meets threshold 80.0%%", pct), nil
		}),

		// ── Governance / traceability ────────────────────────────────
		runCheck("traceability-docs", func() (string, error) {
			return runCmd(rootDir, "go", "test", "./internal/governance/...", "-run", "TestBaselineSHA|TestUpstreamMap|TestParityStatus|TestDecisionStatus|TestFixtureManifest|TestAllDocumentsHaveCorrectBaseline", "-v")
		}),

		// ── Security ─────────────────────────────────────────────────
		runCheck("security-tests", func() (string, error) {
			return runCmd(rootDir, "go", "test", "./tests/security/...", "-v")
		}),
		runCheck("security-audit", func() (string, error) {
			// Real govulncheck scan — fails (non-zero exit) when reachable
			// vulnerabilities are found.  No fallback: if govulncheck is not
			// installed the gate correctly reports failure.
			return runCmd(rootDir, "govulncheck", "./...")
		}),

		// ── Parity ───────────────────────────────────────────────────
		runCheck("parity-tests", func() (string, error) {
			return runCmd(rootDir, "go", "test", "./tests/parity/...", "-v")
		}),

		// ── Middleware ───────────────────────────────────────────────
		runCheck("middleware-tests", func() (string, error) {
			return runCmd(rootDir, "go", "test", "./internal/transport/middleware/...", "-v")
		}),

		// ── Auth ─────────────────────────────────────────────────────
		runCheck("auth-tests", func() (string, error) {
			return runCmd(rootDir, "go", "test", "./internal/domain/auth/...", "./internal/app/auth/...", "./internal/app/keys/...", "./internal/domain/keys/...", "-v")
		}),

		// ── Observability ────────────────────────────────────────────
		runCheck("observability-tests", func() (string, error) {
			return runCmd(rootDir, "go", "test", "./internal/observability/...", "./internal/transport/httpserver/health/...", "-v")
		}),

		// ── Migrations ───────────────────────────────────────────────
		runCheck("migration-schema", func() (string, error) {
			return runCmd(rootDir, "go", "test", "./internal/persistence/postgres/migrations/...", "-v")
		}),

		// ── PostgreSQL ───────────────────────────────────────────────
		runCheck("postgres-pool", func() (string, error) {
			return runCmd(rootDir, "go", "test", "./internal/persistence/postgres/", "-v", "-count=1")
		}),
		runCheck("pg-repositories", func() (string, error) {
			return runCmd(rootDir, "go", "test", "./internal/persistence/postgres/repositories/...", "-v")
		}),
	}

	// ── Informational checks ─────────────────────────────────────────
	// These checks run but do not affect the overall gate status.
	infoChecks := []GateCheck{
		runCheck("vps-deployment", func() (string, error) {
			// Ping the configured VPS host; skip if no env var set
			host := os.Getenv("GOROUTER_VPS_HOST")
			if host == "" {
				return "GOROUTER_VPS_HOST not set; skipping", nil
			}
			return runCmd(rootDir, "ssh", "-o", "ConnectTimeout=5", "-o", "StrictHostKeyChecking=accept-new", host, "pg_isready")
		}),
		runCheck("sudoers-hardening", func() (string, error) {
			host := os.Getenv("GOROUTER_VPS_HOST")
			if host == "" {
				return "GOROUTER_VPS_HOST not set; skipping", nil
			}
			return runCmd(rootDir, "ssh", "-o", "ConnectTimeout=5", host, "sudo -l -U gorouter 2>/dev/null || echo 'sudo check failed'")
		}),
		runCheck("sbom-generate", func() (string, error) {
			return runCmd(rootDir, "cyclonedx-gomod", "mod", "-licenses", "-json", "-output", "gorouter.sbom.json", ".")
		}),
		runCheck("license-check", func() (string, error) {
			return runCmd(rootDir, "go-licenses", "check", "./...")
		}),
	}

	allChecks := append(checks, infoChecks...)
	duration := time.Since(start).Seconds()

	// Gate passes only if all required checks pass
	status := "pass"
	for _, c := range checks {
		if c.Status == "fail" {
			status = "fail"
			break
		}
	}

	evidence := GateEvidence{
		Phase:           "1",
		Status:          status,
		Timestamp:       time.Now().UTC(),
		DurationSeconds: duration,
		Checks:          allChecks,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(evidence); err != nil {
		fmt.Fprintf(os.Stderr, "error encoding gate evidence: %v\n", err)
		return 1
	}

	if status == "fail" {
		fmt.Fprintf(os.Stderr, "\ngate: some required checks failed (see evidence above)\n")
		return 1
	}

	fmt.Fprintf(os.Stderr, "\ngate: all required checks passed\n")
	return 0
}

// runCmd executes a command in the given directory and returns its combined
// output. If the command fails, the output is still returned alongside the error.
func runCmd(dir, name string, args ...string) (string, error) {
	switch name {
	case "go", "npm", "govulncheck", "ssh", "cyclonedx-gomod", "go-licenses":
		// Commands and arguments originate from the fixed Phase 1 gate definitions
		// above, not from request or environment input.
	default:
		return "", fmt.Errorf("gate command %q is not allowed", name)
	}

	cmd := exec.Command(name, args...) // #nosec G204,G702 -- fixed gate commands validated by the allowlist above
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// findRoot walks up from the current directory looking for go.mod.
func findRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getwd: %w", err)
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found from %s", cwd)
		}
		dir = parent
	}
}
