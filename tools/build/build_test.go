package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func setupFrontend(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir+"/assets", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/index.html", []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/assets/app.js", []byte("console.log(1)"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/assets/other.txt", []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildSuccessEndToEnd(t *testing.T) {
	frontendDir := t.TempDir()
	setupFrontend(t, frontendDir)

	out := t.TempDir()
	opts := &options{
		commit:      "5b6452ea2c099dbc6b47ef359c807331b9c872c0",
		version:     "v0.0.0-t01",
		os:          runtime.GOOS,
		arch:        runtime.GOARCH,
		outDir:      out,
		frontendDir: frontendDir,
		target:      "./testdata/fixture",
		goBinary:    "go",
	}
	m, err := build(context.Background(), opts, defaultRunner)
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	if m == nil {
		t.Fatal("manifest is nil")
	}
	if m.Commit != "5b6452ea2c099dbc6b47ef359c807331b9c872c0" {
		t.Errorf("wrong commit in manifest")
	}
	if m.Version != "v0.0.0-t01" {
		t.Errorf("wrong version: %q", m.Version)
	}
	if !strings.HasPrefix(m.GoVersion, "go") {
		t.Errorf("missing go_version: %q", m.GoVersion)
	}
	if m.FrontendHash == "" {
		t.Error("frontend_hash must be recorded")
	}
	if m.PayloadSHA256 == "" {
		t.Error("payload_sha256 must be recorded")
	}
	if m.Signature != nil {
		t.Error("signature must be nil")
	}
	p := filepath.Join(out, payloadName(opts.os))
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("payload missing: %v", err)
	}
	mf := filepath.Join(out, "manifest.json")
	if _, err := os.Stat(mf); err != nil {
		t.Fatalf("manifest missing: %v", err)
	}
}

func TestBuildFakeRunner(t *testing.T) {
	frontendDir := t.TempDir()
	setupFrontend(t, frontendDir)

	out := t.TempDir()
	opts := &options{
		commit:      "abc123def456789012345678901234567890abcd",
		version:     "v1.2.3-alpha",
		os:          "linux",
		arch:        "amd64",
		outDir:      out,
		frontendDir: frontendDir,
		target:      "./nonexistent",
		goBinary:    "go",
	}

	runCalled := false
	var runArgs []string
	fakeR := func(ctx context.Context, goBin string, env, args []string) error {
		runCalled = true
		runArgs = args
		return os.WriteFile(filepath.Join(out, payloadName("linux")), []byte("fake-payload"), 0o755)
	}

	m, err := build(context.Background(), opts, fakeR)
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	if !runCalled {
		t.Fatal("runner was not invoked")
	}
	hasTrimpath := false
	hasBuildVcs := false
	for _, a := range runArgs {
		if a == "-trimpath" {
			hasTrimpath = true
		}
		if a == "-buildvcs=false" {
			hasBuildVcs = true
		}
	}
	if !hasTrimpath || !hasBuildVcs {
		t.Errorf("missing reproducible flags: trimpath=%v buildvcs=false=%v", hasTrimpath, hasBuildVcs)
	}
	if m.PayloadSHA256 == "" {
		t.Error("payload hash must be computed from written payload")
	}
}

func TestReproducibleBuildArgsDeterminism(t *testing.T) {
	a1 := reproducibleBuildArgs("/out/payload", "./pkg")
	a2 := reproducibleBuildArgs("/out/payload", "./pkg")
	if len(a1) != len(a2) {
		t.Fatalf("different lengths: %d vs %d", len(a1), len(a2))
	}
	for i := range a1 {
		if a1[i] != a2[i] {
			t.Errorf("args differ at index %d: %q vs %q", i, a1[i], a2[i])
		}
	}
	expected := []string{
		"build", "-tags", "prod", "-trimpath", "-buildvcs=false",
		"-ldflags=-s -w", "-o", "/out/payload", "./pkg",
	}
	for i, e := range expected {
		if i >= len(a1) {
			break
		}
		if a1[i] != e {
			t.Errorf("arg[%d] = %q, want %q", i, a1[i], e)
		}
	}
}

func TestBuildEnvDeterminism(t *testing.T) {
	e1 := buildEnv("linux", "amd64")
	e2 := buildEnv("linux", "amd64")
	if len(e1) != len(e2) {
		t.Fatalf("env different lengths: %d vs %d", len(e1), len(e2))
	}
	for i := range e1 {
		if e1[i] != e2[i] {
			t.Fatalf("env differs at %d: %q vs %q", i, e1[i], e2[i])
		}
	}
	envMap := make(map[string]string)
	for _, kv := range e1 {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		envMap[k] = v
	}
	if envMap["CGO_ENABLED"] != "0" {
		t.Errorf("CGO_ENABLED = %q, want 0", envMap["CGO_ENABLED"])
	}
	if envMap["GOOS"] != "linux" {
		t.Errorf("GOOS = %q, want linux", envMap["GOOS"])
	}
	if envMap["GOARCH"] != "amd64" {
		t.Errorf("GOARCH = %q, want amd64", envMap["GOARCH"])
	}
	if envMap["GOFLAGS"] != "" {
		t.Errorf("GOFLAGS cleared? %q", envMap["GOFLAGS"])
	}
}

func TestPayloadName(t *testing.T) {
	if payloadName("windows") != "payload.exe" {
		t.Error("windows should be payload.exe")
	}
	if payloadName("linux") != "payload" {
		t.Error("linux should be payload")
	}
	if payloadName("darwin") != "payload" {
		t.Error("darwin should be payload")
	}
}
