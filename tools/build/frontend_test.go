package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

const testSHA = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestFrontendHashDeterminism(t *testing.T) {
	dir := t.TempDir()
	setupFrontend(t, dir)

	h1, err := HashFrontendDir(dir)
	if err != nil {
		t.Fatalf("hash 1: %v", err)
	}
	h2, err := HashFrontendDir(dir)
	if err != nil {
		t.Fatalf("hash 2: %v", err)
	}
	if h1 != h2 {
		t.Errorf("hashes differ: %s vs %s", h1, h2)
	}
}

func TestFrontendHashDifferentContent(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	setupFrontend(t, dir1)
	os.WriteFile(filepath.Join(dir2, "index.html"), []byte("<html>different</html>"), 0o644)
	os.WriteFile(filepath.Join(dir2, "assets/app.js"), []byte("console.log(2)"), 0o644)

	h1, err := HashFrontendDir(dir1)
	if err != nil {
		t.Fatalf("hash 1: %v", err)
	}
	h2, err := HashFrontendDir(dir2)
	if err != nil {
		t.Fatalf("hash 2: %v", err)
	}
	if h1 == h2 {
		t.Error("hashes should differ for different content")
	}
}

func TestFrontendHashMissingDir(t *testing.T) {
	_, err := HashFrontendDir("/nonexistent/path/xyz")
	if err == nil {
		t.Fatal("expected error for missing dir")
	}
}

func TestFrontendHashEmptyDir(t *testing.T) {
	dir := t.TempDir()
	_, err := HashFrontendDir(dir)
	if err == nil {
		t.Fatal("empty dir should error")
	}
}

func TestManifestMarshalDeterminism(t *testing.T) {
	m1 := Manifest{
		Commit:        "5b6452ea2c099dbc6b47ef359c807331b9c872c0",
		Version:       "v1.2.3",
		GoVersion:     "go1.25.0",
		OS:            "linux",
		Arch:          "amd64",
		FrontendHash:  testSHA,
		PayloadSHA256: testSHA,
		Signature:     nil,
	}

	b1, err := m1.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	b2, err := m1.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if string(b1) != string(b2) {
		t.Error("manifest marshal not deterministic")
	}
}

func TestManifestMarshalIncludesNullSignature(t *testing.T) {
	m := Manifest{
		Commit:        "aabbccdd11223344556677889900aabbccdd1122",
		Version:       "v0.0.0-test",
		GoVersion:     "go1.25.0",
		OS:            "darwin",
		Arch:          "arm64",
		FrontendHash:  testSHA,
		PayloadSHA256: testSHA,
		Signature:     nil,
	}
	data, err := m.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	var j map[string]interface{}
	if err := json.Unmarshal(data, &j); err != nil {
		t.Fatal(err)
	}
	if _, ok := j["signature"]; !ok {
		t.Error("signature field must be present")
	}
}

func TestValidateManifestValid(t *testing.T) {
	validCases := []Manifest{
		{
			Commit:        "5b6452ea2c099dbc6b47ef359c807331b9c872c0",
			Version:       "v1.2.3",
			GoVersion:     "go1.25.0",
			OS:            "linux",
			Arch:          "amd64",
			FrontendHash:  testSHA,
			PayloadSHA256: testSHA,
		},
		{
			Commit:        "0000000000000000000000000000000000000000",
			Version:       "v0.0.0-alpha+build",
			GoVersion:     "go1.25",
			OS:            "windows",
			Arch:          "386",
			FrontendHash:  testSHA,
			PayloadSHA256: testSHA,
		},
	}
	for i, m := range validCases {
		if err := ValidateManifest(&m); err != nil {
			t.Errorf("case %d valid manifest rejected: %v", i, err)
		}
	}
}

func TestValidateManifestInvalid(t *testing.T) {
	cases := []struct {
		desc string
		m    Manifest
	}{
		{desc: "commit too short", m: Manifest{Commit: "short"}},
		{desc: "version bad format", m: Manifest{Version: "invalid"}},
		{desc: "unsupported os", m: Manifest{OS: "unknown"}},
		{desc: "frontend_hash wrong length", m: Manifest{
			Commit: "abcd0000000000000000000000000000000000000", Version: "v1.2.3",
			GoVersion: "go1.25", OS: "linux", Arch: "amd64", FrontendHash: "short"}},
	}
	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			err := ValidateManifest(&c.m)
			if err == nil {
				t.Error("expected validation error")
			}
		})
	}
}

func TestParseFlagsSuccess(t *testing.T) {
	args := []string{"--commit=abc0000000000000000000000000000000000", "--version=v1.2.3", "--out=/tmp/x"}
	opts, err := parseFlags(args, os.Stderr)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if opts.commit != "abc0000000000000000000000000000000000" {
		t.Error("wrong commit")
	}
	if opts.version != "v1.2.3" {
		t.Error("wrong version")
	}
	if opts.outDir != "/tmp/x" {
		t.Error("wrong out")
	}
}

func TestParseFlagsMissingRequired(t *testing.T) {
	cases := [][]string{
		{},
		{"--commit=x0000000000000000000000000000000000000"},
		{"--version=v1.2.3"},
	}
	for _, args := range cases {
		_, err := parseFlags(args, os.Stderr)
		if err == nil {
			t.Fatalf("expected error for args: %v", args)
		}
	}
}

func TestWriteManifestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	m := &Manifest{
		Commit:        "abcd000000000000000000000000000000000000",
		Version:       "v0.0.1-beta.1",
		GoVersion:     "go1.25.0",
		OS:            "linux",
		Arch:          "arm64",
		FrontendHash:  testSHA,
		PayloadSHA256: testSHA,
	}
	if err := WriteManifest(m, dir); err != nil {
		t.Fatalf("WriteManifest failed: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m2 Manifest
	if err := json.Unmarshal(b, &m2); err != nil {
		t.Fatal(err)
	}
	if m2.Commit != m.Commit {
		t.Error("manifest round-trip failed")
	}
}
