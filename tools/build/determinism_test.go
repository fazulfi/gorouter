package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDeterminismTwoRuns(t *testing.T) {
	frontendDir := t.TempDir()
	setupFrontend(t, frontendDir)

	optsA := &options{
		commit:      "5b6452ea2c099dbc6b47ef359c807331b9c872c0",
		version:     "v0.0.0-t01",
		os:          runtime.GOOS,
		arch:        runtime.GOARCH,
		outDir:      t.TempDir(),
		frontendDir: frontendDir,
		target:      "./testdata/fixture",
		goBinary:    "go",
	}
	optsB := &options{
		commit:      "5b6452ea2c099dbc6b47ef359c807331b9c872c0",
		version:     "v0.0.0-t01",
		os:          runtime.GOOS,
		arch:        runtime.GOARCH,
		outDir:      t.TempDir(),
		frontendDir: frontendDir,
		target:      "./testdata/fixture",
		goBinary:    "go",
	}

	mA, err := build(context.Background(), optsA, defaultRunner)
	if err != nil {
		t.Fatalf("build A failed: %v", err)
	}
	mB, err := build(context.Background(), optsB, defaultRunner)
	if err != nil {
		t.Fatalf("build B failed: %v", err)
	}

	if mA.PayloadSHA256 != mB.PayloadSHA256 {
		t.Errorf("payload sha256 differs between runs: %s vs %s", mA.PayloadSHA256, mB.PayloadSHA256)
	}
	if mA.FrontendHash != mB.FrontendHash {
		t.Errorf("frontend hash differs between runs: %s vs %s", mA.FrontendHash, mB.FrontendHash)
	}

	manifestA, err := os.ReadFile(filepath.Join(optsA.outDir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifestB, err := os.ReadFile(filepath.Join(optsB.outDir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(manifestA) != string(manifestB) {
		t.Errorf("manifest files differ between runs:\nA=%s\nB=%s", manifestA, manifestB)
	}

	payloadA, err := os.ReadFile(filepath.Join(optsA.outDir, payloadName(optsA.os)))
	if err != nil {
		t.Fatal(err)
	}
	payloadB, err := os.ReadFile(filepath.Join(optsB.outDir, payloadName(optsB.os)))
	if err != nil {
		t.Fatal(err)
	}
	if string(payloadA) != string(payloadB) {
		t.Error("payloads differ between runs")
	}
}
