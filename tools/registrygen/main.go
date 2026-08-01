// Command registrygen generates the provider registry and provider type
// constants from the authoritative P3-T01 manifest.
//
// The single source of truth is docs/implementation/provider-matrix.yaml
// (100 audited provider rows). No provider is hardcoded in this tool or in
// the generated output; every emitted row and constant is derived from the
// manifest in declaration order.
//
// Usage:
//
//	go run ./tools/registrygen/                  write generated files
//	go run ./tools/registrygen/ -stdout-types   print internal/domain/provider/types_generated.go
//	go run ./tools/registrygen/ -stdout-registry  print internal/engine/providers/registry/generated.go
//
// The generator fails loudly (non-zero exit) on:
//   - missing/unreadable/unparseable manifest
//   - a provider count that differs from the manifest metadata total (100)
//   - duplicate provider_id, empty required fields, or shorthand values
//   - executor_path values that do not match an audited engine executor home
//   - supported_formats values that are not baseline domain format constants
//   - two distinct provider_type values that would map to the same Go
//     constant name, or a constant name that collides with an identifier in
//     the target package
//
// Generated files carry a "Code generated ... DO NOT EDIT" header; CI
// (internal/engine/providers/registry/registry_test.go) regenerates and
// compares bytes so manual edits fail.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// manifestPaths are the relative (to the repository root) input and output
// paths. The repository root is resolved from this source file so the tool
// works from any working directory.
const (
	providerMatrixRel = "docs/implementation/provider-matrix.yaml"
	typesOutputRel    = "internal/domain/provider/types_generated.go"
	registryOutputRel = "internal/engine/providers/registry/generated.go"
)

// expectedTotal is the audited provider count. It must equal the manifest
// metadata "total" field and the number of provider rows.
const expectedTotal = 100

// providerEntry mirrors one row of provider-matrix.yaml.
type providerEntry struct {
	ProviderID       string   `yaml:"provider_id"`
	DisplayName      string   `yaml:"display_name"`
	ProviderType     string   `yaml:"provider_type"`
	Category         string   `yaml:"category"`
	AuthType         string   `yaml:"auth_type"`
	ExecutorPath     string   `yaml:"executor_path"`
	SupportedFormats []string `yaml:"supported_formats"`
	SourceCitation   string   `yaml:"source_citation"`
}

type providerManifest struct {
	Metadata struct {
		Total int `yaml:"total"`
	} `yaml:"metadata"`
	Entries []providerEntry `yaml:"providers"`
}

// auditedExecutorPaths are the executor homes that exist in
// internal/engine/providers/. A row may only reference one of them.
var auditedExecutorPaths = map[string]bool{
	"generic":                   true,
	"specialized/azure_openai":  true,
	"specialized/codex":         true,
	"specialized/cursor":        true,
	"specialized/deepseek":      true,
	"specialized/fireworks":     true,
	"specialized/gcp_vertex":    true,
	"specialized/gemini":        true,
	"specialized/github_models": true,
	"specialized/groq":          true,
	"specialized/kiro":          true,
	"specialized/mistral":       true,
	"specialized/perplexity":    true,
	"specialized/together":      true,
	"specialized/xai":           true,
}

// auditedFormats are the baseline format constants declared in
// internal/domain/engine/types.go.
var auditedFormats = map[string]bool{
	"FormatOpenAIChat":     true,
	"FormatOpenAICompat":   true,
	"FormatCodexResponses": true,
	"FormatAnthropic":      true,
	"FormatGemini":         true,
}

// baselineTypes are ProviderType constants already declared by hand in
// internal/domain/provider/types.go. The generator reuses them instead of
// redeclaring them.
var baselineTypes = map[string]string{
	"openai":    "ProviderOpenAI",
	"anthropic": "ProviderAnthropic",
	"azure":     "ProviderAzure",
	"custom":    "ProviderCustom",
}

var shorthandPatterns = []string{
	"all audited", "per registry", "SOURCE NEEDED", "TODO", "TBD", "...",
	"to be determined",
}

// repoRoot returns the repository root by walking up from the working
// directory until go.mod is found.
func repoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for d := wd; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d, nil
		}
		if filepath.Dir(d) == d {
			return "", fmt.Errorf("go.mod not found above %s", wd)
		}
	}
}

// validateRelPath rejects absolute paths and any path whose ".."
// components would escape the repository root.
func validateRelPath(rel string) error {
	if rel == "" {
		return fmt.Errorf("empty relative path")
	}
	rel = strings.ReplaceAll(rel, `\`, string(filepath.Separator))
	if filepath.IsAbs(rel) {
		return fmt.Errorf("absolute path %q is not allowed", rel)
	}
	clean := filepath.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path %q escapes the repository root", rel)
	}
	return nil
}

// safeReadWithinRoot opens a root-scoped handle on base and reads the
// repository-relative path rel. os.Root rejects any traversal that
// would escape base, so manifests are read only from the repository.
func safeReadWithinRoot(base, rel string) ([]byte, error) {
	if err := validateRelPath(rel); err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(base)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return r.ReadFile(rel)
}

func loadManifest(root string) (*providerManifest, error) {
	data, err := safeReadWithinRoot(root, providerMatrixRel)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", providerMatrixRel, err)
	}
	var m providerManifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", providerMatrixRel, err)
	}
	return &m, nil
}

// validate enforces every documented manifest contract. It returns a list of
// problems; the caller fails when non-empty.
func validate(m *providerManifest) []string {
	var problems []string

	if m.Metadata.Total != expectedTotal {
		problems = append(problems, fmt.Sprintf(
			"metadata total=%d want %d", m.Metadata.Total, expectedTotal))
	}
	if len(m.Entries) != expectedTotal {
		problems = append(problems, fmt.Sprintf(
			"provider rows=%d want %d", len(m.Entries), expectedTotal))
	}

	seenIDs := map[string]int{}
	seenTypes := map[string]string{} // provider_type -> constant name

	for _, e := range m.Entries {
		if strings.TrimSpace(e.ProviderID) == "" {
			problems = append(problems, "empty provider_id")
		} else if first := seenIDs[e.ProviderID]; first > 0 {
			problems = append(problems, fmt.Sprintf(
				"duplicate provider_id %q", e.ProviderID))
		}
		seenIDs[e.ProviderID]++

		for _, f := range []struct{ name, val string }{
			{"display_name", e.DisplayName},
			{"provider_type", e.ProviderType},
			{"category", e.Category},
			{"auth_type", e.AuthType},
			{"executor_path", e.ExecutorPath},
			{"source_citation", e.SourceCitation},
		} {
			if strings.TrimSpace(f.val) == "" {
				problems = append(problems, fmt.Sprintf(
					"provider %q empty %s", e.ProviderID, f.name))
			}
		}

		if !auditedExecutorPaths[e.ExecutorPath] {
			problems = append(problems, fmt.Sprintf(
				"provider %q unknown executor_path %q", e.ProviderID, e.ExecutorPath))
		}

		if len(e.SupportedFormats) == 0 {
			problems = append(problems, fmt.Sprintf(
				"provider %q empty supported_formats", e.ProviderID))
		}
		for _, f := range e.SupportedFormats {
			if !auditedFormats[f] {
				problems = append(problems, fmt.Sprintf(
					"provider %q unknown format %q", e.ProviderID, f))
			}
		}

		lower := strings.ToLower(e.ProviderID + " " + e.DisplayName + " " +
			e.ProviderType + " " + e.Category + " " + e.AuthType + " " +
			e.ExecutorPath + " " + e.SourceCitation + " " +
			strings.Join(e.SupportedFormats, " "))
		for _, pat := range shorthandPatterns {
			if strings.Contains(lower, pat) {
				problems = append(problems, fmt.Sprintf(
					"provider %q contains shorthand %q", e.ProviderID, pat))
			}
		}

		// Constant-name collision check: every distinct provider_type must
		// map to a unique Go identifier (no duplicate constants).
		name := constantName(e.ProviderType)
		if prev, ok := seenTypes[e.ProviderType]; ok {
			if prev != name {
				problems = append(problems, fmt.Sprintf(
					"provider_type %q maps to inconsistent constant %q (first %q)",
					e.ProviderType, name, prev))
			}
			continue
		}
		seenTypes[e.ProviderType] = name
	}

	// No emitted constant may collide with the hand-written baseline
	// identifiers; baseline provider_types themselves are skipped at
	// emission and therefore not checked.
	baselineNames := map[string]bool{}
	for _, n := range baselineTypes {
		baselineNames[n] = true
	}
	names := map[string]string{} // constant name -> provider_type
	for t, n := range seenTypes {
		if _, isBaseline := baselineTypes[t]; isBaseline {
			continue
		}
		if baselineNames[n] {
			problems = append(problems, fmt.Sprintf(
				"provider_type %q collides with baseline constant %q", t, n))
		}
		if prev, ok := names[n]; ok {
			problems = append(problems, fmt.Sprintf(
				"provider_type %q and %q map to the same constant %q", prev, t, n))
		}
		names[n] = t
	}

	return problems
}

// constantName converts a provider_type (snake_case or kebab-case) into a
// Go identifier: each dash/underscore-delimited segment is capitalised and
// the segments are joined with the "Provider" prefix. The rule is purely
// deterministic — no acronym special-casing — so output is stable across
// runs and versions. Collisions are rejected by validate.
func constantName(ptype string) string {
	segments := regexp.MustCompile(`[-_]`).Split(ptype, -1)
	var sb strings.Builder
	sb.WriteString("Provider")
	for _, seg := range segments {
		if seg == "" {
			continue
		}
		sb.WriteString(strings.ToUpper(seg[:1]))
		sb.WriteString(seg[1:])
	}
	return sb.String()
}

// providerConst resolves the domain constant name for a provider_type,
// preferring the hand-written baseline declaration when one exists.
func providerConst(ptype string) string {
	if n, ok := baselineTypes[ptype]; ok {
		return n
	}
	return constantName(ptype)
}

// genTypes emits internal/domain/provider/types_generated.go.
func genTypes(m *providerManifest) []byte {
	var b bytes.Buffer
	b.WriteString("// Code generated by tools/registrygen. DO NOT EDIT.\n")
	b.WriteString("//\n")
	b.WriteString("// Provider type constants for all audited providers. Source of\n")
	b.WriteString("// truth: docs/implementation/provider-matrix.yaml. The baseline\n")
	b.WriteString("// constants ProviderOpenAI, ProviderAnthropic, ProviderAzure and\n")
	b.WriteString("// ProviderCustom are declared by hand in types.go and are reused\n")
	b.WriteString("// here (not redeclared). Provider types are unique in the manifest\n")
	b.WriteString("// except \"anthropic\", which is shared by the anthropic and claude\n")
	b.WriteString("// rows; a single ProviderAnthropic constant serves both.\n")
	b.WriteString("//\n")
	b.WriteString("// Regenerate with: go run ./tools/registrygen/\n\n")
	b.WriteString("package provider\n\n")
	b.WriteString("const (\n")

	seen := map[string]bool{}
	for _, e := range m.Entries {
		if _, ok := baselineTypes[e.ProviderType]; ok {
			continue
		}
		if seen[e.ProviderType] {
			continue // duplicate provider_type (claude shares anthropic)
		}
		seen[e.ProviderType] = true
		fmt.Fprintf(&b, "\t%s ProviderType = %q // AUDITED: %s\n",
			constantName(e.ProviderType), e.ProviderType, e.SourceCitation)
	}

	b.WriteString(")\n")
	return b.Bytes()
}

// genRegistry emits internal/engine/providers/registry/generated.go.
func genRegistry(m *providerManifest) []byte {
	var b bytes.Buffer
	b.WriteString("// Code generated by tools/registrygen. DO NOT EDIT.\n")
	b.WriteString("//\n")
	b.WriteString("// Exhaustive provider registry derived from\n")
	b.WriteString("// docs/implementation/provider-matrix.yaml (P3-T01). Every audited\n")
	b.WriteString("// matrix row produces exactly one RegistryRows entry; the factory\n")
	b.WriteString("// index ProviderRegistry is keyed by provider type. The manifest\n")
	b.WriteString("// declares provider type \"anthropic\" twice (rows anthropic and\n")
	b.WriteString("// claude); both rows share the ProviderAnthropic index entry, so the\n")
	b.WriteString("// map holds 99 keys while RegistryRows holds 100 rows.\n")
	b.WriteString("//\n")
	b.WriteString("// Regenerate with: go run ./tools/registrygen/\n\n")
	b.WriteString("package registry\n\n")
	b.WriteString("import (\n")
	b.WriteString("\t\"gorouter/internal/domain/engine\"\n")
	b.WriteString("\t\"gorouter/internal/domain/provider\"\n")
	b.WriteString(")\n\n")

	b.WriteString("// ProviderEntry describes one audited provider row.\n")
	b.WriteString("type ProviderEntry struct {\n")
	b.WriteString("\tProviderID     string\n")
	b.WriteString("\tDisplayName    string\n")
	b.WriteString("\tCategory       string\n")
	b.WriteString("\tAuthType       string\n")
	b.WriteString("\tExecutorPath   string\n")
	b.WriteString("\tFormats        []engine.RequestFormat\n")
	b.WriteString("\tSourceCitation string\n")
	b.WriteString("}\n\n")

	b.WriteString("// SupportsFormat reports whether the entry handles the given format.\n")
	b.WriteString("func (e ProviderEntry) SupportsFormat(format engine.RequestFormat) bool {\n")
	b.WriteString("\tfor _, f := range e.Formats {\n")
	b.WriteString("\t\tif f == format {\n")
	b.WriteString("\t\t\treturn true\n")
	b.WriteString("\t\t}\n")
	b.WriteString("\t}\n")
	b.WriteString("\treturn false\n")
	b.WriteString("}\n\n")

	b.WriteString("// RegistryRows lists all 100 audited provider rows in matrix\n")
	b.WriteString("// declaration order.\n")
	b.WriteString("var RegistryRows = []ProviderEntry{\n")
	for _, e := range m.Entries {
		fmt.Fprintf(&b, "\t{ProviderID: %q, DisplayName: %q, Category: %q, AuthType: %q, ExecutorPath: %q, Formats: %s, SourceCitation: %q},\n",
			e.ProviderID, e.DisplayName, e.Category, e.AuthType, e.ExecutorPath,
			formatList(e.SupportedFormats), e.SourceCitation)
	}
	b.WriteString("}\n\n")

	b.WriteString("// ProviderRegistry indexes RegistryRows by provider type for\n")
	b.WriteString("// executor dispatch. Two matrix rows share provider type\n")
	b.WriteString("// \"anthropic\" (anthropic, claude) and therefore resolve to the same\n")
	b.WriteString("// entry (identical executor path and formats).\n")
	b.WriteString("var ProviderRegistry = map[provider.ProviderType]ProviderEntry{\n")
	seen := map[string]bool{}
	for _, e := range m.Entries {
		if seen[e.ProviderType] {
			continue
		}
		seen[e.ProviderType] = true
		fmt.Fprintf(&b, "\tprovider.%s: {ProviderID: %q, DisplayName: %q, Category: %q, AuthType: %q, ExecutorPath: %q, Formats: %s, SourceCitation: %q},\n",
			providerConst(e.ProviderType), e.ProviderID, e.DisplayName,
			e.Category, e.AuthType, e.ExecutorPath,
			formatList(e.SupportedFormats), e.SourceCitation)
	}
	b.WriteString("}\n")
	return b.Bytes()
}

// formatList renders a supported_formats slice as a Go literal.
func formatList(formats []string) string {
	quoted := make([]string, 0, len(formats))
	for _, f := range formats {
		quoted = append(quoted, "engine."+f)
	}
	return "[]engine.RequestFormat{" + strings.Join(quoted, ", ") + "}"
}

func writeIfChanged(root, rel string, content []byte) error {
	if err := validateRelPath(rel); err != nil {
		return fmt.Errorf("refusing to write %q: %w", rel, err)
	}
	formatted, err := format.Source(content)
	if err != nil {
		return fmt.Errorf("format generated output: %w", err)
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return fmt.Errorf("open repository root %s: %w", root, err)
	}
	defer r.Close()
	if old, err := r.ReadFile(rel); err == nil && bytes.Equal(old, formatted) {
		return nil
	}
	dir := filepath.Dir(rel)
	if dir != "." {
		if err := r.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create parent dirs %s: %w", dir, err)
		}
	}
	// 0o600/0o750 are the strictest modes gosec accepts; Git records
	// only the executable bit, so both map to a non-executable 100644
	// file and a 040000 directory in the committed tree.
	return r.WriteFile(rel, formatted, 0o600)
}

func stdoutFormatted(content []byte) error {
	formatted, err := format.Source(content)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(formatted)
	return err
}

func main() {
	stdoutTypes := flag.Bool("stdout-types", false,
		"print the generated provider types file to stdout")
	stdoutRegistry := flag.Bool("stdout-registry", false,
		"print the generated registry file to stdout")
	flag.Parse()

	root, err := repoRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "registrygen:", err)
		os.Exit(1)
	}

	m, err := loadManifest(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "registrygen:", err)
		os.Exit(1)
	}

	if problems := validate(m); len(problems) > 0 {
		sort.Strings(problems)
		for _, p := range problems {
			fmt.Fprintln(os.Stderr, "registrygen: invalid manifest:", p)
		}
		os.Exit(1)
	}

	typesContent := genTypes(m)
	registryContent := genRegistry(m)

	if *stdoutTypes {
		if err := stdoutFormatted(typesContent); err != nil {
			fmt.Fprintln(os.Stderr, "registrygen:", err)
			os.Exit(1)
		}
		return
	}
	if *stdoutRegistry {
		if err := stdoutFormatted(registryContent); err != nil {
			fmt.Fprintln(os.Stderr, "registrygen:", err)
			os.Exit(1)
		}
		return
	}

	written := 0
	for _, rel := range []string{typesOutputRel, registryOutputRel} {
		var content []byte
		if rel == typesOutputRel {
			content = typesContent
		} else {
			content = registryContent
		}
		if err := writeIfChanged(root, rel, content); err != nil {
			fmt.Fprintf(os.Stderr, "registrygen: write %s: %v\n", filepath.Join(root, rel), err)
			os.Exit(1)
		}
		written++
	}
	fmt.Fprintf(os.Stderr, "registrygen: wrote %d generated file(s) from %s\n",
		written, providerMatrixRel)
}
