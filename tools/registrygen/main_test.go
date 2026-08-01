package main

import (
	"bytes"
	"flag"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "registrygen-bin-*")
	if err != nil {
		panic("MkdirTemp: " + err.Error())
	}
	bin := filepath.Join(dir, "registrygen")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		panic("go build registrygen: " + err.Error() + "\n" + string(out))
	}
	os.Setenv("REGISTRYGEN_BIN", bin)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func binPath(t *testing.T) string {
	t.Helper()
	p := os.Getenv("REGISTRYGEN_BIN")
	if p == "" {
		t.Fatal("REGISTRYGEN_BIN not set")
	}
	return p
}

// runBin executes the built registrygen binary in dir and returns its
// stdout, stderr and exit code. Only used for the os.Exit(1) error paths
// of main() which cannot be exercised in-process without killing the test
// binary.
func runBin(t *testing.T, dir string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(binPath(t), args...)
	cmd.Dir = dir
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("run %v: %v", args, err)
		}
	}
	return out.String(), errBuf.String(), code
}

// captureOutput runs fn while capturing os.Stdout and os.Stderr.
// Tests using it must not run in parallel.
func captureOutput(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stderr: %v", err)
	}
	os.Stdout, os.Stderr = wOut, wErr
	fn()
	wOut.Close()
	wErr.Close()
	os.Stdout, os.Stderr = oldOut, oldErr

	var outBuf, errBuf bytes.Buffer
	if _, err := outBuf.ReadFrom(rOut); err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if _, err := errBuf.ReadFrom(rErr); err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	rOut.Close()
	rErr.Close()
	return outBuf.String(), errBuf.String()
}

// withChdir runs fn with the process working directory set to dir and
// restores it afterwards. Tests using it must not run in parallel.
func withChdir(t *testing.T, dir string, fn func()) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	defer func() {
		if err := os.Chdir(old); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()
	fn()
}

// runMainInProc invokes main() in-process with the given args and working
// directory. flag.CommandLine is reset first because main() redefines its
// flags on the global set. Used for the success paths of main(); the
// os.Exit error paths are covered by runBin subprocess tests.
func runMainInProc(t *testing.T, dir string, args ...string) (stdout, stderr string) {
	t.Helper()
	oldArgs := os.Args
	os.Args = append([]string{"registrygen"}, args...)
	defer func() { os.Args = oldArgs }()
	flag.CommandLine = flag.NewFlagSet("registrygen", flag.ExitOnError)
	defer func() { flag.CommandLine = flag.NewFlagSet("registrygen", flag.ExitOnError) }()
	return captureOutput(t, func() {
		withChdir(t, dir, main)
	})
}

// realRoot resolves the repository root from the package working
// directory (go test runs with CWD set to the package dir).
func realRoot(t *testing.T) string {
	t.Helper()
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("repoRoot: %v", err)
	}
	return root
}

func loadRealManifest(t *testing.T) *providerManifest {
	t.Helper()
	m, err := loadManifest(realRoot(t))
	if err != nil {
		t.Fatalf("load real manifest: %v", err)
	}
	return m
}

func cloneManifest(m *providerManifest) *providerManifest {
	cp := *m
	cp.Entries = append([]providerEntry(nil), m.Entries...)
	return &cp
}

// tempRoot creates a temp directory that looks like a repository root
// (contains go.mod) so repoRoot() resolves to it.
func tempRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module fake\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	return root
}

func writeManifestFile(t *testing.T, root, content string) {
	t.Helper()
	dir := filepath.Join(root, "docs", "implementation")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "provider-matrix.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func copyRealManifest(t *testing.T, root string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(realRoot(t), "docs", "implementation", "provider-matrix.yaml"))
	if err != nil {
		t.Fatalf("read real manifest: %v", err)
	}
	writeManifestFile(t, root, string(data))
}

func hasSubstring(problems []string, want string) bool {
	for _, p := range problems {
		if strings.Contains(p, want) {
			return true
		}
	}
	return false
}

// --- repo root / manifest loading ---

func TestRepoRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	withChdir(t, nested, func() {
		got, err := repoRoot()
		if err != nil {
			t.Fatalf("repoRoot: %v", err)
		}
		if got != root {
			t.Errorf("repoRoot = %q, want %q", got, root)
		}
	})

	empty := t.TempDir()
	withChdir(t, empty, func() {
		if _, err := repoRoot(); err == nil {
			t.Errorf("repoRoot: expected error without go.mod above")
		} else if !strings.Contains(err.Error(), "go.mod not found above") {
			t.Errorf("repoRoot error = %v", err)
		}
	})
}

func TestLoadManifest(t *testing.T) {
	root := t.TempDir()
	if _, err := loadManifest(root); err == nil || !strings.Contains(err.Error(), "read docs/implementation/provider-matrix.yaml") {
		t.Errorf("missing manifest error = %v, want read error", err)
	}

	writeManifestFile(t, root, "{{{")
	if _, err := loadManifest(root); err == nil || !strings.Contains(err.Error(), "parse docs/implementation/provider-matrix.yaml") {
		t.Errorf("unparseable manifest error = %v, want parse error", err)
	}

	writeManifestFile(t, root, "metadata:\n  total: 2\nproviders:\n"+
		"  - provider_id: a\n    display_name: A\n    provider_type: a\n    category: apikey\n    auth_type: apikey\n    executor_path: generic\n    supported_formats:\n      - FormatOpenAIChat\n    source_citation: a.js\n"+
		"  - provider_id: b\n    display_name: B\n    provider_type: b\n    category: freeTier\n    auth_type: none\n    executor_path: generic\n    supported_formats:\n      - FormatGemini\n    source_citation: b.js\n")
	m, err := loadManifest(root)
	if err != nil {
		t.Fatalf("loadManifest valid: %v", err)
	}
	if m.Metadata.Total != 2 || len(m.Entries) != 2 {
		t.Fatalf("parsed total=%d rows=%d, want 2/2", m.Metadata.Total, len(m.Entries))
	}
	if m.Entries[0].ProviderID != "a" || m.Entries[0].SupportedFormats[0] != "FormatOpenAIChat" {
		t.Errorf("entry a = %+v", m.Entries[0])
	}
	if m.Entries[1].ExecutorPath != "generic" || m.Entries[1].SourceCitation != "b.js" {
		t.Errorf("entry b = %+v", m.Entries[1])
	}
}

// --- validate: failure table ---

func TestValidateFailureTable(t *testing.T) {
	base := loadRealManifest(t)
	if base.Metadata.Total != 100 || len(base.Entries) != 100 {
		t.Fatalf("real manifest total=%d rows=%d, want 100/100", base.Metadata.Total, len(base.Entries))
	}
	cases := []struct {
		name string
		mut  func(*providerManifest)
		want []string
	}{
		{"metadata total mismatch",
			func(m *providerManifest) { m.Metadata.Total = 99 },
			[]string{"metadata total=99 want 100"}},
		{"row count mismatch",
			func(m *providerManifest) { m.Entries = m.Entries[:99] },
			[]string{"provider rows=99 want 100"}},
		{"empty provider_id",
			func(m *providerManifest) { m.Entries[0].ProviderID = " " },
			[]string{"empty provider_id"}},
		{"duplicate provider_id",
			func(m *providerManifest) { m.Entries[1].ProviderID = m.Entries[0].ProviderID },
			[]string{`duplicate provider_id "` + base.Entries[0].ProviderID + `"`}},
		{"empty display_name",
			func(m *providerManifest) { m.Entries[2].DisplayName = "" },
			[]string{`provider "` + base.Entries[2].ProviderID + `" empty display_name`}},
		{"empty provider_type",
			func(m *providerManifest) { m.Entries[3].ProviderType = " " },
			[]string{"empty provider_type"}},
		{"empty category",
			func(m *providerManifest) { m.Entries[4].Category = "" },
			[]string{"empty category"}},
		{"empty auth_type",
			func(m *providerManifest) { m.Entries[5].AuthType = "  " },
			[]string{"empty auth_type"}},
		{"empty executor_path",
			func(m *providerManifest) { m.Entries[6].ExecutorPath = "" },
			[]string{"empty executor_path"}},
		{"empty source_citation",
			func(m *providerManifest) { m.Entries[7].SourceCitation = " " },
			[]string{"empty source_citation"}},
		{"unknown executor_path",
			func(m *providerManifest) { m.Entries[8].ExecutorPath = "specialized/bogus" },
			[]string{`unknown executor_path "specialized/bogus"`}},
		{"empty supported_formats",
			func(m *providerManifest) { m.Entries[9].SupportedFormats = nil },
			[]string{"empty supported_formats"}},
		{"unknown format",
			func(m *providerManifest) { m.Entries[10].SupportedFormats = []string{"FormatNope"} },
			[]string{`unknown format "FormatNope"`}},
		{"shorthand all audited",
			func(m *providerManifest) { m.Entries[12].SourceCitation = "ALL AUDITED entries" },
			[]string{`contains shorthand "all audited"`}},
		{"shorthand per registry",
			func(m *providerManifest) { m.Entries[13].SourceCitation = "per registry values" },
			[]string{`contains shorthand "per registry"`}},
		{"shorthand ellipsis",
			func(m *providerManifest) { m.Entries[14].SourceCitation = "audit pending..." },
			[]string{`contains shorthand "..."`}},
		{"shorthand to be determined",
			func(m *providerManifest) { m.Entries[15].SourceCitation = "to be determined by audit" },
			[]string{`contains shorthand "to be determined"`}},
		{"baseline constant collision",
			func(m *providerManifest) { m.Entries[16].ProviderType = "OpenAI" },
			[]string{`collides with baseline constant "ProviderOpenAI"`}},
		{"same constant collision",
			func(m *providerManifest) {
				m.Entries[17].ProviderType = "zap-zap"
				m.Entries[18].ProviderType = "zap_zap"
			},
			[]string{`map to the same constant "ProviderZapZap"`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := cloneManifest(base)
			tc.mut(m)
			problems := validate(m)
			for _, want := range tc.want {
				if !hasSubstring(problems, want) {
					t.Errorf("missing problem %q in %v", want, problems)
				}
			}
		})
	}
}

func TestValidateRealManifestClean(t *testing.T) {
	m := loadRealManifest(t)
	if problems := validate(m); len(problems) != 0 {
		t.Errorf("real manifest problems: %v", problems)
	}
}

// --- constant generation ---

func TestConstantName(t *testing.T) {
	for ptype, want := range map[string]string{
		"gemini-cli":        "ProviderGeminiCli",
		"black_forest_labs": "ProviderBlackForestLabs",
		"black-forest-labs": "ProviderBlackForestLabs",
		"openai":            "ProviderOpenai",
		"a-b_c":             "ProviderABC",
		"-leading":          "ProviderLeading",
		"trailing-":         "ProviderTrailing",
		"UPPER":             "ProviderUPPER",
		"x":                 "ProviderX",
		"gemini_cli":        "ProviderGeminiCli",
	} {
		if got := constantName(ptype); got != want {
			t.Errorf("constantName(%q) = %q, want %q", ptype, got, want)
		}
	}
	if got := constantName(""); got != "Provider" {
		t.Errorf("constantName(\"\") = %q, want %q", got, "Provider")
	}
}

func TestProviderConst(t *testing.T) {
	for ptype, want := range map[string]string{
		"openai":     "ProviderOpenAI",
		"anthropic":  "ProviderAnthropic",
		"azure":      "ProviderAzure",
		"custom":     "ProviderCustom",
		"gemini-cli": "ProviderGeminiCli",
	} {
		if got := providerConst(ptype); got != want {
			t.Errorf("providerConst(%q) = %q, want %q", ptype, got, want)
		}
	}
}

// --- deterministic byte outputs ---

func TestGenTypesExactBytes(t *testing.T) {
	m := &providerManifest{}
	m.Metadata.Total = 3
	m.Entries = []providerEntry{
		{ProviderID: "openai", DisplayName: "OpenAI", ProviderType: "openai", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAIChat"}, SourceCitation: "registry/openai.js"},
		{ProviderID: "gemini-cli", DisplayName: "Gemini CLI", ProviderType: "gemini-cli", Category: "free", AuthType: "none", ExecutorPath: "specialized/gemini", SupportedFormats: []string{"FormatGemini"}, SourceCitation: "registry/gemini-cli.js"},
		{ProviderID: "gemini-cli-cn", DisplayName: "Gemini CLI CN", ProviderType: "gemini-cli", Category: "free", AuthType: "none", ExecutorPath: "specialized/gemini", SupportedFormats: []string{"FormatGemini"}, SourceCitation: "registry/gemini-cli-cn.js"},
	}
	got := genTypes(m)
	want := "// Code generated by tools/registrygen. DO NOT EDIT.\n" +
		"//\n" +
		"// Provider type constants for all audited providers. Source of\n" +
		"// truth: docs/implementation/provider-matrix.yaml. The baseline\n" +
		"// constants ProviderOpenAI, ProviderAnthropic, ProviderAzure and\n" +
		"// ProviderCustom are declared by hand in types.go and are reused\n" +
		"// here (not redeclared). Provider types are unique in the manifest\n" +
		"// except \"anthropic\", which is shared by the anthropic and claude\n" +
		"// rows; a single ProviderAnthropic constant serves both.\n" +
		"//\n" +
		"// Regenerate with: go run ./tools/registrygen/\n\n" +
		"package provider\n\n" +
		"const (\n" +
		"\tProviderGeminiCli ProviderType = \"gemini-cli\" // AUDITED: registry/gemini-cli.js\n" +
		")\n"
	if !bytes.Equal(got, []byte(want)) {
		t.Errorf("genTypes bytes:\n%s\nwant:\n%s", got, want)
	}
}

func TestGenRegistryExactBytes(t *testing.T) {
	m := &providerManifest{}
	m.Metadata.Total = 3
	m.Entries = []providerEntry{
		{ProviderID: "openai", DisplayName: "OpenAI", ProviderType: "openai", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAIChat"}, SourceCitation: "registry/openai.js"},
		{ProviderID: "gemini-cli", DisplayName: "Gemini CLI", ProviderType: "gemini-cli", Category: "free", AuthType: "none", ExecutorPath: "specialized/gemini", SupportedFormats: []string{"FormatGemini"}, SourceCitation: "registry/gemini-cli.js"},
		{ProviderID: "gemini-cli-cn", DisplayName: "Gemini CLI CN", ProviderType: "gemini-cli", Category: "free", AuthType: "none", ExecutorPath: "specialized/gemini", SupportedFormats: []string{"FormatGemini"}, SourceCitation: "registry/gemini-cli-cn.js"},
	}
	got := genRegistry(m)
	want := "// Code generated by tools/registrygen. DO NOT EDIT.\n" +
		"//\n" +
		"// Exhaustive provider registry derived from\n" +
		"// docs/implementation/provider-matrix.yaml (P3-T01). Every audited\n" +
		"// matrix row produces exactly one RegistryRows entry; the factory\n" +
		"// index ProviderRegistry is keyed by provider type. The manifest\n" +
		"// declares provider type \"anthropic\" twice (rows anthropic and\n" +
		"// claude); both rows share the ProviderAnthropic index entry, so the\n" +
		"// map holds 99 keys while RegistryRows holds 100 rows.\n" +
		"//\n" +
		"// Regenerate with: go run ./tools/registrygen/\n\n" +
		"package registry\n\n" +
		"import (\n" +
		"\t\"gorouter/internal/domain/engine\"\n" +
		"\t\"gorouter/internal/domain/provider\"\n" +
		")\n\n" +
		"// ProviderEntry describes one audited provider row.\n" +
		"type ProviderEntry struct {\n" +
		"\tProviderID     string\n" +
		"\tDisplayName    string\n" +
		"\tCategory       string\n" +
		"\tAuthType       string\n" +
		"\tExecutorPath   string\n" +
		"\tFormats        []engine.RequestFormat\n" +
		"\tSourceCitation string\n" +
		"}\n\n" +
		"// SupportsFormat reports whether the entry handles the given format.\n" +
		"func (e ProviderEntry) SupportsFormat(format engine.RequestFormat) bool {\n" +
		"\tfor _, f := range e.Formats {\n" +
		"\t\tif f == format {\n" +
		"\t\t\treturn true\n" +
		"\t\t}\n" +
		"\t}\n" +
		"\treturn false\n" +
		"}\n\n" +
		"// RegistryRows lists all 100 audited provider rows in matrix\n" +
		"// declaration order.\n" +
		"var RegistryRows = []ProviderEntry{\n" +
		"\t{ProviderID: \"openai\", DisplayName: \"OpenAI\", Category: \"apikey\", AuthType: \"apikey\", ExecutorPath: \"generic\", Formats: []engine.RequestFormat{engine.FormatOpenAIChat}, SourceCitation: \"registry/openai.js\"},\n" +
		"\t{ProviderID: \"gemini-cli\", DisplayName: \"Gemini CLI\", Category: \"free\", AuthType: \"none\", ExecutorPath: \"specialized/gemini\", Formats: []engine.RequestFormat{engine.FormatGemini}, SourceCitation: \"registry/gemini-cli.js\"},\n" +
		"\t{ProviderID: \"gemini-cli-cn\", DisplayName: \"Gemini CLI CN\", Category: \"free\", AuthType: \"none\", ExecutorPath: \"specialized/gemini\", Formats: []engine.RequestFormat{engine.FormatGemini}, SourceCitation: \"registry/gemini-cli-cn.js\"},\n" +
		"}\n\n" +
		"// ProviderRegistry indexes RegistryRows by provider type for\n" +
		"// executor dispatch. Two matrix rows share provider type\n" +
		"// \"anthropic\" (anthropic, claude) and therefore resolve to the same\n" +
		"// entry (identical executor path and formats).\n" +
		"var ProviderRegistry = map[provider.ProviderType]ProviderEntry{\n" +
		"\tprovider.ProviderOpenAI: {ProviderID: \"openai\", DisplayName: \"OpenAI\", Category: \"apikey\", AuthType: \"apikey\", ExecutorPath: \"generic\", Formats: []engine.RequestFormat{engine.FormatOpenAIChat}, SourceCitation: \"registry/openai.js\"},\n" +
		"\tprovider.ProviderGeminiCli: {ProviderID: \"gemini-cli\", DisplayName: \"Gemini CLI\", Category: \"free\", AuthType: \"none\", ExecutorPath: \"specialized/gemini\", Formats: []engine.RequestFormat{engine.FormatGemini}, SourceCitation: \"registry/gemini-cli.js\"},\n" +
		"}\n"
	if !bytes.Equal(got, []byte(want)) {
		t.Errorf("genRegistry bytes:\n%s\nwant:\n%s", got, want)
	}
}

func TestGenTypesRealManifest(t *testing.T) {
	m := loadRealManifest(t)
	got := genTypes(m)
	for _, b := range []string{"ProviderOpenAI", "ProviderAnthropic", "ProviderAzure", "ProviderCustom"} {
		if bytes.Contains(got, []byte(b+" ProviderType")) {
			t.Errorf("genTypes re-emitted baseline constant %s", b)
		}
	}
	distinct := map[string]bool{}
	for _, e := range m.Entries {
		distinct[e.ProviderType] = true
	}
	want := 0
	for ptype := range distinct {
		if _, isBase := baselineTypes[ptype]; !isBase {
			want++
		}
	}
	if n := bytes.Count(got, []byte(" ProviderType = ")); n != want {
		t.Errorf("genTypes emitted %d constants, want %d", n, want)
	}
	if !bytes.Contains(got, []byte(`ProviderGeminiCli ProviderType = "gemini_cli"`)) {
		t.Errorf("genTypes missing ProviderGeminiCli constant")
	}
	if !bytes.Equal(got, genTypes(m)) {
		t.Errorf("genTypes not deterministic")
	}
}

func TestGenRegistryRealManifest(t *testing.T) {
	m := loadRealManifest(t)
	got := genRegistry(m)
	if !bytes.HasPrefix(got, []byte("// Code generated by tools/registrygen. DO NOT EDIT.")) {
		t.Errorf("missing generated-code header")
	}
	if rows := bytes.Count(got, []byte("\t{ProviderID: ")); rows != 100 {
		t.Errorf("RegistryRows = %d, want 100", rows)
	}
	if !bytes.Contains(got, []byte(`{ProviderID: "alicode",`)) {
		t.Errorf("RegistryRows missing first manifest row alicode")
	}
	distinct := map[string]bool{}
	for _, e := range m.Entries {
		distinct[e.ProviderType] = true
	}
	if n := bytes.Count(got, []byte("\n\tprovider.")); n != len(distinct) {
		t.Errorf("ProviderRegistry keys = %d, want %d", n, len(distinct))
	}
	for _, k := range []string{"provider.ProviderOpenAI:", "provider.ProviderGeminiCli:"} {
		if !bytes.Contains(got, []byte(k)) {
			t.Errorf("ProviderRegistry missing key %s", k)
		}
	}
	if !bytes.Equal(got, genRegistry(m)) {
		t.Errorf("genRegistry not deterministic")
	}
}

// --- format literal rendering ---

func TestFormatList(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{nil, "[]engine.RequestFormat{}"},
		{[]string{"FormatGemini"}, "[]engine.RequestFormat{engine.FormatGemini}"},
		{[]string{"FormatOpenAIChat", "FormatAnthropic"}, "[]engine.RequestFormat{engine.FormatOpenAIChat, engine.FormatAnthropic}"},
	}
	for _, tc := range cases {
		if got := formatList(tc.in); got != tc.want {
			t.Errorf("formatList(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// --- write-if-changed ---

func TestWriteIfChanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "out.go")
	content := []byte("package p\n\nconst X = 1\n")
	if err := writeIfChanged(path, content); err != nil {
		t.Fatalf("first write: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "package p\n\nconst X = 1\n" {
		t.Errorf("written bytes = %q", string(data))
	}
	fi1, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	if err := writeIfChanged(path, content); err != nil {
		t.Fatalf("identical rewrite: %v", err)
	}
	fi2, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !fi1.ModTime().Equal(fi2.ModTime()) {
		t.Errorf("identical content rewrote file")
	}

	time.Sleep(5 * time.Millisecond)
	if err := writeIfChanged(path, []byte("package p\n\nconst X = 2\n")); err != nil {
		t.Fatalf("changed rewrite: %v", err)
	}
	data, _ = os.ReadFile(path)
	if string(data) != "package p\n\nconst X = 2\n" {
		t.Errorf("rewritten bytes = %q", string(data))
	}

	if err := writeIfChanged(filepath.Join(dir, "bad.go"), []byte("not go {")); err == nil {
		t.Errorf("invalid content: expected format error")
	} else if !strings.Contains(err.Error(), "format generated output") {
		t.Errorf("invalid content error = %v", err)
	}
}

func TestStdoutFormatted(t *testing.T) {
	content := []byte("package p\n\nconst X = 1\n")
	out, _ := captureOutput(t, func() {
		if err := stdoutFormatted(content); err != nil {
			t.Errorf("stdoutFormatted: %v", err)
		}
	})
	if out != "package p\n\nconst X = 1\n" {
		t.Errorf("stdout = %q", out)
	}
	if err := stdoutFormatted([]byte("{{{")); err == nil {
		t.Errorf("expected format error for invalid content")
	}
}

// --- main(): in-process success paths ---

func TestMainDefaultWrite(t *testing.T) {
	root := tempRoot(t)
	copyRealManifest(t, root)
	_, errOut := runMainInProc(t, root)
	if !strings.Contains(errOut, "registrygen: wrote 2 generated file(s) from docs/implementation/provider-matrix.yaml") {
		t.Errorf("stderr = %q", errOut)
	}
	for _, rel := range []string{
		"internal/domain/provider/types_generated.go",
		"internal/engine/providers/registry/generated.go",
	} {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Errorf("missing generated file %s: %v", rel, err)
			continue
		}
		if !bytes.HasPrefix(data, []byte("// Code generated by tools/registrygen. DO NOT EDIT.")) {
			t.Errorf("%s missing generated-code header", rel)
		}
	}
}

func TestMainStdoutTypes(t *testing.T) {
	root := tempRoot(t)
	copyRealManifest(t, root)
	out, errOut := runMainInProc(t, root, "-stdout-types")
	if errOut != "" {
		t.Errorf("stderr = %q, want empty", errOut)
	}
	m := loadRealManifest(t)
	want, err := format.Source(genTypes(m))
	if err != nil {
		t.Fatalf("format.Source: %v", err)
	}
	if out != string(want) {
		t.Errorf("stdout-types bytes differ from genTypes output")
	}
}

func TestMainStdoutRegistry(t *testing.T) {
	root := tempRoot(t)
	copyRealManifest(t, root)
	out, errOut := runMainInProc(t, root, "-stdout-registry")
	if errOut != "" {
		t.Errorf("stderr = %q, want empty", errOut)
	}
	m := loadRealManifest(t)
	want, err := format.Source(genRegistry(m))
	if err != nil {
		t.Fatalf("format.Source: %v", err)
	}
	if out != string(want) {
		t.Errorf("stdout-registry bytes differ from genRegistry output")
	}
}

// --- main(): os.Exit(1) error paths via subprocess ---

func TestMainExitRepoRootError(t *testing.T) {
	dir := t.TempDir()
	_, errOut, code := runBin(t, dir)
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut, "go.mod not found above") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestMainExitMissingManifest(t *testing.T) {
	root := tempRoot(t)
	_, errOut, code := runBin(t, root)
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut, "read docs/implementation/provider-matrix.yaml") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestMainExitUnparseableManifest(t *testing.T) {
	root := tempRoot(t)
	writeManifestFile(t, root, "{{{")
	_, errOut, code := runBin(t, root)
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut, "parse docs/implementation/provider-matrix.yaml") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestMainExitInvalidManifest(t *testing.T) {
	root := tempRoot(t)
	writeManifestFile(t, root, "metadata:\n  total: 99\nproviders: []\n")
	_, errOut, code := runBin(t, root)
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut, "registrygen: invalid manifest:") {
		t.Errorf("stderr = %q", errOut)
	}
	if !strings.Contains(errOut, "metadata total=99 want 100") {
		t.Errorf("stderr = %q", errOut)
	}
}
