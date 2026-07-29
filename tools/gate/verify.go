// Command gate runs Phase 1 gate verification and outputs JSON evidence.
//
// It executes go vet, go test (short), frontend build, and frontend test,
// then writes a structured JSON report to stdout. Exit code 0 means all
// gates passed; exit code 1 means one or more checks failed.
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

// checkResult runs a named check and returns a GateCheck.
type checkResult struct {
	name string
	out  string
	err  error
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

	start := time.Now()

	checks := []GateCheck{
		runCheck("go-vet", func() (string, error) {
			return runCmd(rootDir, "go", "vet", "./...")
		}),
		runCheck("go-test", func() (string, error) {
			return runCmd(rootDir, "go", "test", "-short", "-count=1", "./...")
		}),
		runCheck("frontend-build", func() (string, error) {
			frontendDir := filepath.Join(rootDir, "frontend")
			out, err := runCmd(frontendDir, "npm", "ci")
			if err != nil {
				return out, err
			}
			return runCmd(frontendDir, "npm", "run", "build")
		}),
		runCheck("frontend-test", func() (string, error) {
			return runCmd(filepath.Join(rootDir, "frontend"), "npm", "test")
		}),
	}

	duration := time.Since(start).Seconds()

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
		Checks:          checks,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(evidence); err != nil {
		fmt.Fprintf(os.Stderr, "error encoding gate evidence: %v\n", err)
		return 1
	}

	if status == "fail" {
		fmt.Fprintf(os.Stderr, "\ngate: some checks failed (see evidence above)\n")
		return 1
	}

	fmt.Fprintf(os.Stderr, "\ngate: all checks passed\n")
	return 0
}

// runCmd executes a command in the given directory and returns its combined
// output. If the command fails, the output is still returned alongside the error.
func runCmd(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
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
