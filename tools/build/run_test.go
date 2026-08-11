package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRunSuccess(t *testing.T) {
	frontendDir := t.TempDir()
	setupFrontend(t, frontendDir)
	out := t.TempDir()

	args := []string{
		"--commit=5b6452ea2c099dbc6b47ef359c807331b9c872c0",
		"--version=v0.0.0-t01",
		"--os=" + runtime.GOOS,
		"--arch=" + runtime.GOARCH,
		"--out=" + out,
		"--frontend-dir=" + frontendDir,
		"--target=./testdata/fixture",
	}
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run exit = %d, want 0; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "payload_sha256=") {
		t.Errorf("stdout missing payload_sha256: %s", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(out, "manifest.json")); err != nil {
		t.Errorf("manifest missing: %v", err)
	}
}

func TestRunMissingFlags(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("run exit = %d, want 2 for missing flags", code)
	}
}

func TestRunBuildFailure(t *testing.T) {
	frontendDir := t.TempDir()
	setupFrontend(t, frontendDir)
	out := t.TempDir()

	args := []string{
		"--commit=5b6452ea2c099dbc6b47ef359c807331b9c872c0",
		"--version=v0.0.0-t01",
		"--out=" + out,
		"--frontend-dir=" + frontendDir,
		"--target=./nonexistent-package-xyz",
	}
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr)
	if code != 1 {
		t.Errorf("run exit = %d, want 1 for failed build; stderr=%s", code, stderr.String())
	}
}

func TestBuildFrontendHashError(t *testing.T) {
	out := t.TempDir()
	opts := &options{
		commit:      "5b6452ea2c099dbc6b47ef359c807331b9c872c0",
		version:     "v0.0.0-t01",
		os:          runtime.GOOS,
		arch:        runtime.GOARCH,
		outDir:      out,
		frontendDir: "/nonexistent/frontend/dir",
		target:      "./testdata/fixture",
		goBinary:    "go",
	}
	_, err := build(context.Background(), opts, defaultRunner)
	if err == nil {
		t.Fatal("expected error for missing frontend dir")
	}
}

func TestBuildRunnerError(t *testing.T) {
	frontendDir := t.TempDir()
	setupFrontend(t, frontendDir)
	out := t.TempDir()

	opts := &options{
		commit:      "5b6452ea2c099dbc6b47ef359c807331b9c872c0",
		version:     "v0.0.0-t01",
		os:          "linux",
		arch:        "amd64",
		outDir:      out,
		frontendDir: frontendDir,
		target:      "./testdata/fixture",
		goBinary:    "go",
	}
	failRunner := func(ctx context.Context, goBin string, env, args []string) error {
		return errors.New("simulated build failure")
	}
	_, err := build(context.Background(), opts, failRunner)
	if err == nil {
		t.Fatal("expected error from failing runner")
	}
	if !strings.Contains(err.Error(), "simulated build failure") {
		t.Errorf("error should wrap runner error, got: %v", err)
	}
}

func TestValidateManifestNil(t *testing.T) {
	if err := ValidateManifest(nil); err == nil {
		t.Error("expected error for nil manifest")
	}
}

func TestValidateManifestBadGoVersion(t *testing.T) {
	m := &Manifest{
		Commit:        "5b6452ea2c099dbc6b47ef359c807331b9c872c0",
		Version:       "v1.2.3",
		GoVersion:     "not-a-go-version",
		OS:            "linux",
		Arch:          "amd64",
		FrontendHash:  testSHA,
		PayloadSHA256: testSHA,
	}
	if err := ValidateManifest(m); err == nil {
		t.Error("expected error for bad go_version")
	}
}

func TestValidateManifestBadArch(t *testing.T) {
	m := &Manifest{
		Commit:        "5b6452ea2c099dbc6b47ef359c807331b9c872c0",
		Version:       "v1.2.3",
		GoVersion:     "go1.25.0",
		OS:            "linux",
		Arch:          "mips",
		FrontendHash:  testSHA,
		PayloadSHA256: testSHA,
	}
	if err := ValidateManifest(m); err == nil {
		t.Error("expected error for unsupported arch")
	}
}

func TestValidateManifestBadPayloadHash(t *testing.T) {
	m := &Manifest{
		Commit:        "5b6452ea2c099dbc6b47ef359c807331b9c872c0",
		Version:       "v1.2.3",
		GoVersion:     "go1.25.0",
		OS:            "linux",
		Arch:          "amd64",
		FrontendHash:  testSHA,
		PayloadSHA256: "tooshort",
	}
	if err := ValidateManifest(m); err == nil {
		t.Error("expected error for bad payload_sha256")
	}
}

func TestWriteManifestValidationError(t *testing.T) {
	dir := t.TempDir()
	m := &Manifest{Commit: "invalid"}
	if err := WriteManifest(m, dir); err == nil {
		t.Error("expected WriteManifest to reject invalid manifest")
	}
}

func TestHashFileError(t *testing.T) {
	if _, err := hashFile("/nonexistent/file.bin"); err == nil {
		t.Error("expected error hashing missing file")
	}
}
