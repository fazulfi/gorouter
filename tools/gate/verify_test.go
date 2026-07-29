package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ─── runCheck ───────────────────────────────────────────────────────────────

func TestRunCheck_Pass(t *testing.T) {
	gc := runCheck("pass-check", func() (string, error) {
		return "all good", nil
	})
	if gc.Name != "pass-check" {
		t.Errorf("Name = %q, want %q", gc.Name, "pass-check")
	}
	if gc.Status != "pass" {
		t.Errorf("Status = %q, want %q", gc.Status, "pass")
	}
	if gc.Output != "all good" {
		t.Errorf("Output = %q, want %q", gc.Output, "all good")
	}
	if gc.DurationSeconds < 0 {
		t.Errorf("DurationSeconds = %f, want >= 0", gc.DurationSeconds)
	}
}

func TestRunCheck_Fail(t *testing.T) {
	gc := runCheck("fail-check", func() (string, error) {
		return "error output", fmt.Errorf("something went wrong")
	})
	if gc.Name != "fail-check" {
		t.Errorf("Name = %q, want %q", gc.Name, "fail-check")
	}
	if gc.Status != "fail" {
		t.Errorf("Status = %q, want %q", gc.Status, "fail")
	}
	if gc.Output != "error output" {
		t.Errorf("Output = %q, want %q", gc.Output, "error output")
	}
	if gc.DurationSeconds < 0 {
		t.Errorf("DurationSeconds = %f, want >= 0", gc.DurationSeconds)
	}
}

func TestRunCheck_Empty(t *testing.T) {
	gc := runCheck("empty-check", func() (string, error) {
		return "", nil
	})
	if gc.Name != "empty-check" {
		t.Errorf("Name = %q, want %q", gc.Name, "empty-check")
	}
	if gc.Status != "pass" {
		t.Errorf("Status = %q, want %q", gc.Status, "pass")
	}
	if gc.Output != "" {
		t.Errorf("Output = %q, want empty string", gc.Output)
	}
}

func TestRunCheck_RecordsDuration(t *testing.T) {
	gc := runCheck("timed-check", func() (string, error) {
		time.Sleep(5 * time.Millisecond)
		return "done", nil
	})
	if gc.DurationSeconds < 0.001 {
		t.Errorf("DurationSeconds = %f, want > 0.001", gc.DurationSeconds)
	}
}

// ─── runCmd ────────────────────────────────────────────────────────────────

func TestRunCmd_Success(t *testing.T) {
	out, err := runCmd(".", "go", "version")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "go") {
		t.Errorf("output = %q, want substring %q", out, "go")
	}
}

func TestRunCmd_Error(t *testing.T) {
	_, err := runCmd(".", "command-that-does-not-exist-12345")
	if err == nil {
		t.Error("expected error for non-existent command")
	}
}

func TestRunCmd_Output(t *testing.T) {
	out, err := runCmd(".", "go", "help", "build")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "usage") {
		t.Errorf("output = %q, want substring %q", out, "usage")
	}
}

// ─── findRoot ──────────────────────────────────────────────────────────────

func TestFindRoot_Success(t *testing.T) {
	root, err := findRoot()
	if err != nil {
		t.Fatalf("findRoot() error = %v", err)
	}
	if root == "" {
		t.Fatal("findRoot() returned empty path")
	}
	info, err := os.Stat(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Errorf("go.mod not found at root %s: %v", root, err)
	}
	if info.IsDir() {
		t.Errorf("go.mod at %s is a directory", filepath.Join(root, "go.mod"))
	}
}

func TestFindRoot_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(origWd); err != nil {
			t.Fatal(err)
		}
	}()

	_, err = findRoot()
	if err == nil {
		t.Error("expected error when no go.mod exists")
	}
}

func TestFindRoot_FromSubdirectory(t *testing.T) {
	tmpDir := t.TempDir()
	goModContent := "module testmodule\n\ngo 1.23\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goModContent), 0644); err != nil {
		t.Fatal(err)
	}

	subDir := filepath.Join(tmpDir, "a", "b", "c")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(subDir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(origWd); err != nil {
			t.Fatal(err)
		}
	}()

	root, err := findRoot()
	if err != nil {
		t.Fatalf("findRoot() error = %v", err)
	}
	if root != tmpDir {
		t.Errorf("root = %s, want %s", root, tmpDir)
	}
}

// ─── GateEvidence / GateCheck ──────────────────────────────────────────────

func TestGateEvidence_MarshalUnmarshal(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	ev := GateEvidence{
		Phase:           "1",
		Status:          "pass",
		Timestamp:       now,
		DurationSeconds: 12.5,
		Checks: []GateCheck{
			{Name: "check-1", Status: "pass", Output: "ok", DurationSeconds: 2.0},
			{Name: "check-2", Status: "fail", Output: "error msg", DurationSeconds: 3.5},
		},
	}

	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("json.Marshal error = %v", err)
	}

	var decoded GateEvidence
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal error = %v", err)
	}

	if decoded.Phase != "1" {
		t.Errorf("Phase = %q, want %q", decoded.Phase, "1")
	}
	if decoded.Status != "pass" {
		t.Errorf("Status = %q, want %q", decoded.Status, "pass")
	}
	if !decoded.Timestamp.Equal(now) {
		t.Errorf("Timestamp = %v, want %v", decoded.Timestamp, now)
	}
	if decoded.DurationSeconds != 12.5 {
		t.Errorf("DurationSeconds = %f, want %f", decoded.DurationSeconds, 12.5)
	}
	if len(decoded.Checks) != 2 {
		t.Fatalf("len(Checks) = %d, want 2", len(decoded.Checks))
	}
	if decoded.Checks[0].Name != "check-1" {
		t.Errorf("Checks[0].Name = %q, want %q", decoded.Checks[0].Name, "check-1")
	}
	if decoded.Checks[0].Status != "pass" {
		t.Errorf("Checks[0].Status = %q, want %q", decoded.Checks[0].Status, "pass")
	}
	if decoded.Checks[0].Output != "ok" {
		t.Errorf("Checks[0].Output = %q, want %q", decoded.Checks[0].Output, "ok")
	}
	if decoded.Checks[0].DurationSeconds != 2.0 {
		t.Errorf("Checks[0].DurationSeconds = %f, want %f", decoded.Checks[0].DurationSeconds, 2.0)
	}
	if decoded.Checks[1].Name != "check-2" {
		t.Errorf("Checks[1].Name = %q, want %q", decoded.Checks[1].Name, "check-2")
	}
	if decoded.Checks[1].Status != "fail" {
		t.Errorf("Checks[1].Status = %q, want %q", decoded.Checks[1].Status, "fail")
	}
	if decoded.Checks[1].Output != "error msg" {
		t.Errorf("Checks[1].Output = %q, want %q", decoded.Checks[1].Output, "error msg")
	}
	if decoded.Checks[1].DurationSeconds != 3.5 {
		t.Errorf("Checks[1].DurationSeconds = %f, want %f", decoded.Checks[1].DurationSeconds, 3.5)
	}
}

func TestGateEvidenceSchema_Load(t *testing.T) {
	schemaPath := filepath.Join("..", "..", "docs", "implementation", "gate-evidence", "schema.json")
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("failed to read schema.json: %v", err)
	}

	var schema map[string]interface{}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("schema.json is not valid JSON: %v", err)
	}

	if schema["type"] != "object" {
		t.Errorf("schema type = %q, want %q", schema["type"], "object")
	}

	reqRaw, ok := schema["required"].([]interface{})
	if !ok {
		t.Fatal("schema missing 'required' array")
	}
	requiredFields := make(map[string]bool)
	for _, r := range reqRaw {
		requiredFields[r.(string)] = true
	}
	for _, f := range []string{"phase", "status", "timestamp", "duration_seconds", "checks"} {
		if !requiredFields[f] {
			t.Errorf("required field %q missing from schema", f)
		}
	}

	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("schema missing 'properties'")
	}
	for _, f := range []string{"phase", "status", "timestamp", "duration_seconds", "checks"} {
		if _, exists := props[f]; !exists {
			t.Errorf("property %q missing from schema", f)
		}
	}

	checksProp, ok := props["checks"].(map[string]interface{})
	if !ok {
		t.Fatal("checks property is not an object")
	}
	items, ok := checksProp["items"].(map[string]interface{})
	if !ok {
		t.Fatal("checks.items missing")
	}
	itemProps, ok := items["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("checks.items.properties missing")
	}
	for _, f := range []string{"name", "status", "output"} {
		if _, exists := itemProps[f]; !exists {
			t.Errorf("check item property %q missing", f)
		}
	}
}

func TestPhase1Evidence_Load(t *testing.T) {
	evPath := filepath.Join("..", "..", "docs", "implementation", "gate-evidence", "phase-1.json")
	data, err := os.ReadFile(evPath)
	if err != nil {
		t.Fatalf("failed to read phase-1.json: %v", err)
	}

	var ev GateEvidence
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatalf("phase-1.json does not match GateEvidence struct: %v", err)
	}

	if ev.Phase != "1" {
		t.Errorf("Phase = %q, want %q", ev.Phase, "1")
	}
	if ev.Status != "pass" {
		t.Errorf("Status = %q, want %q", ev.Status, "pass")
	}
	if ev.DurationSeconds <= 0 {
		t.Errorf("DurationSeconds = %f, want > 0", ev.DurationSeconds)
	}
	if ev.Timestamp.IsZero() {
		t.Error("Timestamp is zero")
	}
	if len(ev.Checks) == 0 {
		t.Fatal("Checks is empty")
	}

	for i, c := range ev.Checks {
		if c.Name == "" {
			t.Errorf("Checks[%d].Name is empty", i)
		}
		if c.Status != "pass" && c.Status != "fail" {
			t.Errorf("Checks[%d].Status = %q, want pass or fail", i, c.Status)
		}
	}
}

// ─── run() ─────────────────────────────────────────────────────────────────

func TestRun_ErrorNoGoMod(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(origWd); err != nil {
			t.Fatal(err)
		}
	}()

	var stderrBuf bytes.Buffer
	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w

	exitCode := run()

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(&stderrBuf, r); err != nil {
		t.Fatal(err)
	}
	os.Stderr = oldStderr

	if exitCode != 1 {
		t.Errorf("run() = %d, want 1", exitCode)
	}
	if !strings.Contains(stderrBuf.String(), "go.mod not found") {
		t.Errorf("stderr = %q, want substring %q", stderrBuf.String(), "go.mod not found")
	}
}

func TestRun_FullFlow(t *testing.T) {
	tmpDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module temp\n\ngo 1.23\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(origWd); err != nil {
			t.Fatal(err)
		}
	}()

	var stdoutBuf, stderrBuf bytes.Buffer

	oldStdout := os.Stdout
	oldStderr := os.Stderr
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = wOut
	os.Stderr = wErr

	exitCode := run()

	if err := wOut.Close(); err != nil {
		t.Fatal(err)
	}
	if err := wErr.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(&stdoutBuf, rOut); err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(&stderrBuf, rErr); err != nil {
		t.Fatal(err)
	}
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	if exitCode != 1 {
		t.Errorf("run() = %d, want 1 (frontend checks should fail)", exitCode)
	}

	stdoutData := stdoutBuf.Bytes()
	if len(stdoutData) == 0 {
		t.Fatal("stdout is empty, expected JSON GateEvidence")
	}

	var ev GateEvidence
	if err := json.Unmarshal(stdoutData, &ev); err != nil {
		t.Fatalf("stdout is not valid GateEvidence JSON:\n%s\n\nerror: %v", stdoutData, err)
	}
	if ev.Phase != "1" {
		t.Errorf("Phase = %q, want %q", ev.Phase, "1")
	}
	if ev.Status != "fail" {
		t.Errorf("Status = %q, want %q", ev.Status, "fail")
	}
	if ev.DurationSeconds <= 0 {
		t.Errorf("DurationSeconds = %f, want > 0", ev.DurationSeconds)
	}
	if ev.Timestamp.IsZero() {
		t.Error("Timestamp is zero")
	}
	if len(ev.Checks) != 4 {
		t.Errorf("len(Checks) = %d, want 4", len(ev.Checks))
	}

	checkNames := make(map[string]bool)
	for _, c := range ev.Checks {
		checkNames[c.Name] = true
	}
	for _, want := range []string{"go-vet", "go-test", "frontend-build", "frontend-test"} {
		if !checkNames[want] {
			t.Errorf("missing check %q", want)
		}
	}

	for _, c := range ev.Checks {
		if strings.HasPrefix(c.Name, "frontend") && c.Status != "fail" {
			t.Errorf("check %q should be fail, got %q", c.Name, c.Status)
		}
	}

	if !strings.Contains(stderrBuf.String(), "some checks failed") {
		t.Errorf("stderr = %q, want substring %q", stderrBuf.String(), "some checks failed")
	}
}
