package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestContractClientMatchesSpec(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("findRepoRoot: %v", err)
	}

	outPath := filepath.Join(repoRoot, outputPath)
	yamlPath := filepath.Join(repoRoot, openapiPath)

	data, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("read %s: %v", yamlPath, err)
	}

	var doc openapiDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse %s: %v", yamlPath, err)
	}

	generated, err := generateTS(doc)
	if err != nil {
		t.Fatalf("generateTS: %v", err)
	}

	existing, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read %s: %v", outPath, err)
	}

	if !bytes.Equal(existing, generated) {
		t.Fatalf("diff gate FAIL: %s does not match regenerated output from %s\nRun: go run ./tools/contractgen", outputPath, openapiPath)
	}

	t.Logf("diff gate PASS: %s matches regenerated output (%d bytes from %s)", outputPath, len(generated), openapiPath)
}
