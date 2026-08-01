package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "upstreammap-bin-*")
	if err != nil {
		panic("MkdirTemp: " + err.Error())
	}
	bin := filepath.Join(dir, "upstreammap")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		panic("go build upstreammap: " + err.Error() + "\n" + string(out))
	}
	os.Setenv("UPSTREAMMAP_BIN", bin)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func binPath(t *testing.T) string {
	t.Helper()
	p := os.Getenv("UPSTREAMMAP_BIN")
	if p == "" {
		t.Fatal("UPSTREAMMAP_BIN not set")
	}
	return p
}

// runBin executes the built upstreammap binary in dir and returns its
// stdout, stderr and exit code. Only used for the os.Exit(1) error paths
// of main() (writeYAML failure, validation failure) which cannot be
// exercised in-process without killing the test binary.
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

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	out, _ := captureOutput(t, fn)
	return out
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

// tempRoot creates a temp directory that looks like a repository root
// (contains go.mod) so findRepoRoot() resolves to it.
func tempRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module fake\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	return root
}

// runMainInProc invokes main() in-process with the given args and working
// directory. Used for the success paths of main(); the os.Exit error paths
// are covered by runBin subprocess tests.
func runMainInProc(t *testing.T, dir string, args ...string) (stdout, stderr string) {
	t.Helper()
	oldArgs := os.Args
	os.Args = append([]string{"upstreammap"}, args...)
	defer func() { os.Args = oldArgs }()
	return captureOutput(t, func() {
		withChdir(t, dir, main)
	})
}

// --- Authority counts: the exact 100 / 20 / 5 baseline must never drift ---

func TestAuthorityCounts(t *testing.T) {
	if len(providerRegistry) != 100 {
		t.Errorf("providerRegistry = %d entries, want 100", len(providerRegistry))
	}
	if len(formatRegistry) != 5 {
		t.Errorf("formatRegistry = %d entries, want 5", len(formatRegistry))
	}
	if len(oauthRegistry) != 20 {
		t.Errorf("oauthRegistry = %d entries, want 20", len(oauthRegistry))
	}

	seen := map[string]bool{}
	for _, p := range providerRegistry {
		if seen[p.ProviderID] {
			t.Errorf("duplicate provider_id %q in providerRegistry", p.ProviderID)
		}
		seen[p.ProviderID] = true
	}

	// Audited category split: 64 apikey, 16 oauth, 13 freeTier, 5 free, 2 webCookie.
	byCat := map[string]int{}
	for _, p := range providerRegistry {
		byCat[p.Category]++
	}
	for cat, want := range map[string]int{"apikey": 64, "oauth": 16, "freeTier": 13, "free": 5, "webCookie": 2} {
		if got := byCat[cat]; got != want {
			t.Errorf("category %q = %d, want %d", cat, got, want)
		}
	}

	for _, f := range oauthRegistry {
		if f.FlowID == "" || f.Mechanism == "" || f.SourceCitation == "" {
			t.Errorf("oauth flow incomplete: %+v", f)
		}
		if f.RestartPolicy != "cancel_all_pending" {
			t.Errorf("flow %s restart_policy = %q, want cancel_all_pending", f.FlowID, f.RestartPolicy)
		}
	}
}

// --- Manifest builders ---

func TestBuildProviderManifestData(t *testing.T) {
	got := buildProviderManifestData()
	meta, ok := got["metadata"].(map[string]interface{})
	if !ok {
		t.Fatalf("metadata missing or wrong type %T", got["metadata"])
	}
	if meta["title"] != "Provider Matrix" {
		t.Errorf("metadata.title = %v", meta["title"])
	}
	if meta["total"] != 100 {
		t.Errorf("metadata.total = %v, want 100", meta["total"])
	}
	desc, _ := meta["description"].(string)
	if !strings.Contains(desc, "79918c7830695bbca4a45c9fea4a42c3e9fd73d1") {
		t.Errorf("metadata.description missing pinned commit: %q", desc)
	}
	src, _ := meta["source"].(string)
	if !strings.Contains(src, "audit/02-provider-matrix.md") {
		t.Errorf("metadata.source = %q", src)
	}

	providers, ok := got["providers"].([]providerEntry)
	if !ok {
		t.Fatalf("providers wrong type %T", got["providers"])
	}
	if len(providers) != 100 {
		t.Fatalf("providers = %d, want 100", len(providers))
	}
	for i := 1; i < len(providers); i++ {
		if providers[i-1].ProviderID >= providers[i].ProviderID {
			t.Errorf("providers not sorted at %d: %q >= %q", i, providers[i-1].ProviderID, providers[i].ProviderID)
		}
	}
	for _, p := range providers {
		if p.ProviderType == "" {
			t.Errorf("provider %q empty provider_type", p.ProviderID)
		}
	}
	for id, want := range map[string]string{
		"claude":            "anthropic",
		"github":            "github_models",
		"gemini-cli":        "gemini_cli",
		"black-forest-labs": "black_forest_labs",
		"openai":            "openai",
	} {
		found := false
		for _, p := range providers {
			if p.ProviderID == id {
				found = true
				if p.ProviderType != want {
					t.Errorf("%s provider_type = %q, want %q", id, p.ProviderType, want)
				}
			}
		}
		if !found {
			t.Errorf("provider %q missing from manifest data", id)
		}
	}
}

func TestBuildFormatManifestData(t *testing.T) {
	got := buildFormatManifestData()
	meta, ok := got["metadata"].(map[string]interface{})
	if !ok {
		t.Fatalf("metadata missing or wrong type %T", got["metadata"])
	}
	if meta["total"] != 5 {
		t.Errorf("metadata.total = %v, want 5", meta["total"])
	}
	formats, ok := got["formats"].([]formatEntry)
	if !ok {
		t.Fatalf("formats wrong type %T", got["formats"])
	}
	if len(formats) != 5 {
		t.Fatalf("formats = %d, want 5", len(formats))
	}
	seen := map[string]bool{}
	for _, f := range formats {
		seen[f.FormatConstant] = true
		if f.CodecPackage == "" || f.Detection == "" || f.TerminalEvent == "" || f.SourceCitation == "" {
			t.Errorf("format %s has incomplete fields", f.FormatConstant)
		}
	}
	for _, want := range []string{
		"FormatOpenAIChat", "FormatOpenAICompat", "FormatCodexResponses",
		"FormatAnthropic", "FormatGemini",
	} {
		if !seen[want] {
			t.Errorf("format %s missing from manifest data", want)
		}
	}
}

func TestBuildOAuthManifestData(t *testing.T) {
	got := buildOAuthManifestData()
	meta, ok := got["metadata"].(map[string]interface{})
	if !ok {
		t.Fatalf("metadata missing or wrong type %T", got["metadata"])
	}
	if meta["total"] != 20 {
		t.Errorf("metadata.total = %v, want 20", meta["total"])
	}
	flows, ok := got["oauth_flows"].([]oauthEntry)
	if !ok {
		t.Fatalf("oauth_flows wrong type %T", got["oauth_flows"])
	}
	if len(flows) != 20 {
		t.Fatalf("oauth_flows = %d, want 20", len(flows))
	}
	seen := map[string]bool{}
	for _, f := range flows {
		seen[f.FlowID] = true
		if f.Mechanism == "" || f.PortBehavior == "" || f.PKCE == "" || f.SourceCitation == "" {
			t.Errorf("flow %s has incomplete fields", f.FlowID)
		}
	}
	for _, want := range []string{
		"claude", "codex", "openai", "gemini-cli", "antigravity", "qwen",
		"github", "kiro", "kimchi", "cursor", "xai",
	} {
		if !seen[want] {
			t.Errorf("oauth flow %s missing from manifest data", want)
		}
	}
}

// --- Provider-type mapping ---

func TestProviderTypeForID(t *testing.T) {
	for id, want := range map[string]string{
		"claude":            "anthropic",
		"black-forest-labs": "black_forest_labs",
		"brave-search":      "brave_search",
		"cloudflare-ai":     "cloudflare_ai",
		"codebuddy-cn":      "codebuddy_cn",
		"edge-tts":          "edge_tts",
		"fal-ai":            "fal_ai",
		"gemini-cli":        "gemini_cli",
		"github":            "github_models",
		"glm-cn":            "glm_cn",
		"google-pse":        "google_pse",
		"google-tts":        "google_tts",
		"grok-cli":          "grok_cli",
		"grok-web":          "grok_web",
		"jina-ai":           "jina_ai",
		"jina-reader":       "jina_reader",
		"local-device":      "local_device",
		"mimo-free":         "mimo_free",
		"minimax-cn":        "minimax_cn",
		"ollama-local":      "ollama_local",
		"opencode-go":       "opencode_go",
		"perplexity-agent":  "perplexity_agent",
		"perplexity-web":    "perplexity_web",
		"stability-ai":      "stability_ai",
		"vercel-ai-gateway": "vercel_ai_gateway",
		"vertex-partner":    "vertex_partner",
		"voyage-ai":         "voyage_ai",
		"volcengine-ark":    "volcengine_ark",
	} {
		if got := providerTypeForID(id); got != want {
			t.Errorf("providerTypeForID(%q) = %q, want %q", id, got, want)
		}
	}
	for _, id := range []string{"openai", "anthropic", "gemini", "mmf", "opencode", "tavily"} {
		if got := providerTypeForID(id); got != id {
			t.Errorf("providerTypeForID(%q) = %q, want %q", id, got, id)
		}
	}
}

// --- Duplicate-provider dedup ---

func TestDeduplicateProviders(t *testing.T) {
	base := providerEntry{
		ProviderID: "dup", DisplayName: "Dup", Category: "apikey", AuthType: "apikey",
		ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"},
		SourceCitation: "registry/dup.js",
	}
	uniq := providerEntry{
		ProviderID: "uniq", DisplayName: "Uniq", Category: "apikey", AuthType: "apikey",
		ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"},
		SourceCitation: "registry/uniq.js",
	}

	_, errOut := captureOutput(t, func() {
		got := deduplicateProviders([]providerEntry{base, base, uniq})
		if len(got) != 2 {
			t.Errorf("deduplicateProviders len = %d, want 2", len(got))
		}
		if got[0].ProviderID != "dup" || got[1].ProviderID != "uniq" {
			t.Errorf("dedup order = [%s %s], want [dup uniq]", got[0].ProviderID, got[1].ProviderID)
		}
	})
	if !strings.Contains(errOut, `WARN: duplicate provider_id "dup" skipped`) {
		t.Errorf("stderr missing duplicate warning: %q", errOut)
	}

	got := deduplicateProviders([]providerEntry{base, uniq})
	if len(got) != 2 || got[0].ProviderID != "dup" || got[1].ProviderID != "uniq" {
		t.Errorf("duplicate-free input altered: %+v", got)
	}
}

// --- Repo-root discovery ---

func TestFindRepoRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	withChdir(t, nested, func() {
		if got := findRepoRoot(); got != root {
			t.Errorf("findRepoRoot = %q, want %q", got, root)
		}
	})

	empty := t.TempDir()
	withChdir(t, empty, func() {
		if got := findRepoRoot(); got != empty {
			t.Errorf("findRepoRoot fallback = %q, want %q", got, empty)
		}
	})
}

// --- YAML validation ---

func TestValidateYAMLFile(t *testing.T) {
	dir := t.TempDir()

	missing := filepath.Join(dir, "missing.yaml")
	_, errOut := captureOutput(t, func() {
		if got := validateYAMLFile(missing, "provider"); got != 1 {
			t.Errorf("missing file result = %d, want 1", got)
		}
	})
	if !strings.Contains(errOut, "ERROR reading") || !strings.Contains(errOut, missing) {
		t.Errorf("missing file stderr = %q", errOut)
	}

	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(bad, []byte("{{{"), 0o644); err != nil {
		t.Fatalf("write bad.yaml: %v", err)
	}
	_, errOut = captureOutput(t, func() {
		if got := validateYAMLFile(bad, "format"); got != 1 {
			t.Errorf("bad yaml result = %d, want 1", got)
		}
	})
	if !strings.Contains(errOut, "ERROR unmarshalling") {
		t.Errorf("bad yaml stderr = %q", errOut)
	}

	good := filepath.Join(dir, "good.yaml")
	content := "a: 1\n"
	if err := os.WriteFile(good, []byte(content), 0o644); err != nil {
		t.Fatalf("write good.yaml: %v", err)
	}
	out, _ := captureOutput(t, func() {
		if got := validateYAMLFile(good, "oauth"); got != 0 {
			t.Errorf("valid yaml result = %d, want 0", got)
		}
	})
	if want := "OK: " + good + " is valid YAML (5 bytes)\n"; out != want {
		t.Errorf("valid yaml stdout = %q, want %q", out, want)
	}
}

// --- Manifest writing: temp dirs only; exact file contents ---

func TestWriteYAMLExactBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.yaml")
	writeYAML(path, map[string]interface{}{
		"b": 2,
		"a": []string{"x", "y"},
	})
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	want := "a:\n  - x\n  - \"y\"\nb: 2\n"
	if string(data) != want {
		t.Errorf("yaml bytes = %q, want %q", string(data), want)
	}
}

func TestWriteProviderManifest(t *testing.T) {
	dir := t.TempDir()
	out := captureStdout(t, func() { writeProviderManifest(dir) })
	if !strings.Contains(out, "Wrote 100 provider entries.\n") {
		t.Errorf("stdout = %q", out)
	}
	path := filepath.Join(dir, "provider-matrix.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var doc struct {
		Metadata struct {
			Total int `yaml:"total"`
		} `yaml:"metadata"`
		Providers []providerEntry `yaml:"providers"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse written manifest: %v", err)
	}
	if doc.Metadata.Total != 100 {
		t.Errorf("metadata.total = %d, want 100", doc.Metadata.Total)
	}
	if len(doc.Providers) != 100 {
		t.Errorf("providers = %d, want 100", len(doc.Providers))
	}
	for i := 1; i < len(doc.Providers); i++ {
		if doc.Providers[i-1].ProviderID >= doc.Providers[i].ProviderID {
			t.Errorf("written providers not sorted at %d", i)
		}
	}
}

func TestWriteFormatManifest(t *testing.T) {
	dir := t.TempDir()
	out := captureStdout(t, func() { writeFormatManifest(dir) })
	if !strings.Contains(out, "Wrote 5 format entries.\n") {
		t.Errorf("stdout = %q", out)
	}
	data, err := os.ReadFile(filepath.Join(dir, "format-matrix.yaml"))
	if err != nil {
		t.Fatalf("read format manifest: %v", err)
	}
	var doc struct {
		Metadata struct {
			Total int `yaml:"total"`
		} `yaml:"metadata"`
		Formats []formatEntry `yaml:"formats"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse written format manifest: %v", err)
	}
	if doc.Metadata.Total != 5 || len(doc.Formats) != 5 {
		t.Errorf("format manifest total=%d rows=%d, want 5/5", doc.Metadata.Total, len(doc.Formats))
	}
}

func TestWriteOAuthManifest(t *testing.T) {
	dir := t.TempDir()
	out := captureStdout(t, func() { writeOAuthManifest(dir) })
	if !strings.Contains(out, "Wrote 20 OAuth flow entries.\n") {
		t.Errorf("stdout = %q", out)
	}
	data, err := os.ReadFile(filepath.Join(dir, "oauth-matrix.yaml"))
	if err != nil {
		t.Fatalf("read oauth manifest: %v", err)
	}
	var doc struct {
		Metadata struct {
			Total int `yaml:"total"`
		} `yaml:"metadata"`
		Flows []oauthEntry `yaml:"oauth_flows"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse written oauth manifest: %v", err)
	}
	if doc.Metadata.Total != 20 || len(doc.Flows) != 20 {
		t.Errorf("oauth manifest total=%d rows=%d, want 20/20", doc.Metadata.Total, len(doc.Flows))
	}
}

// --- main(): in-process success paths ---

// TestMainStdoutMatchesWriteOutput proves the --stdout modes emit exactly
// the same bytes as the corresponding write-mode file for the same data.
func TestMainStdoutMatchesWriteOutput(t *testing.T) {
	cases := []struct {
		flag     string
		writeFn  func(string)
		filename string
	}{
		{"--stdout", writeProviderManifest, "provider-matrix.yaml"},
		{"--stdout-format", writeFormatManifest, "format-matrix.yaml"},
		{"--stdout-oauth", writeOAuthManifest, "oauth-matrix.yaml"},
	}
	for _, tc := range cases {
		t.Run(tc.flag, func(t *testing.T) {
			dir := t.TempDir()
			captureStdout(t, func() { tc.writeFn(dir) })
			wantBytes, err := os.ReadFile(filepath.Join(dir, tc.filename))
			if err != nil {
				t.Fatalf("read %s: %v", tc.filename, err)
			}
			root := tempRoot(t)
			out, errOut := runMainInProc(t, root, tc.flag)
			if errOut != "" {
				t.Errorf("stderr = %q, want empty", errOut)
			}
			if string(wantBytes) != out {
				t.Errorf("%s bytes differ from write-mode output", tc.flag)
			}
		})
	}
}

func TestMainWriteMode(t *testing.T) {
	root := tempRoot(t)
	docDir := filepath.Join(root, "docs", "implementation")
	if err := os.MkdirAll(docDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	out, errOut := runMainInProc(t, root, "--write")
	if errOut != "" {
		t.Errorf("stderr = %q, want empty", errOut)
	}
	if !strings.Contains(out, "Wrote 100 provider entries.\n") ||
		!strings.Contains(out, "Wrote 5 format entries.\n") ||
		!strings.Contains(out, "Wrote 20 OAuth flow entries.\n") ||
		!strings.Contains(out, "Manifests written successfully.\n") {
		t.Errorf("stdout = %q", out)
	}
	for _, f := range []string{"provider-matrix.yaml", "format-matrix.yaml", "oauth-matrix.yaml"} {
		data, err := os.ReadFile(filepath.Join(root, "docs", "implementation", f))
		if err != nil {
			t.Errorf("missing written manifest %s: %v", f, err)
			continue
		}
		if len(data) == 0 {
			t.Errorf("written manifest %s is empty", f)
		}
	}
}

func TestMainValidateMode(t *testing.T) {
	root := tempRoot(t)
	docDir := filepath.Join(root, "docs", "implementation")
	if err := os.MkdirAll(docDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	captureStdout(t, func() {
		writeProviderManifest(docDir)
		writeFormatManifest(docDir)
		writeOAuthManifest(docDir)
	})
	out, _ := runMainInProc(t, root)
	if !strings.Contains(out, "All manifests validated successfully.\n") {
		t.Errorf("validate stdout = %q", out)
	}
}

// --- main(): os.Exit(1) error paths via subprocess ---

func TestMainWriteModeUnwritableDir(t *testing.T) {
	root := tempRoot(t)
	docs := filepath.Join(root, "docs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docs, "implementation"), []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, errOut, code := runBin(t, root, "--write")
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut, "ERROR creating") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestMainValidateModeMissingManifests(t *testing.T) {
	root := tempRoot(t)
	out, errOut, code := runBin(t, root)
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut, "ERROR reading") {
		t.Errorf("stderr = %q", errOut)
	}
	if !strings.Contains(out, "Validation FAILED with 3 errors.") {
		t.Errorf("stdout = %q", out)
	}
}

// TestValidateRelPathRejectsEscapes covers the containment guard used
// by writeYAML and validateYAMLFile.
func TestValidateRelPathRejectsEscapes(t *testing.T) {
	good := []string{
		"provider-matrix.yaml",
		"a/b/format-matrix.yaml",
		"./x.yaml",
		"a/../b.yaml",
	}
	for _, rel := range good {
		if err := validateRelPath(rel); err != nil {
			t.Errorf("validateRelPath(%q) = %v, want nil", rel, err)
		}
	}
	bad := []string{
		"", "..", "../x.yaml", "a/../../x.yaml", "/etc/passwd", `..\x`, "./../x",
	}
	for _, rel := range bad {
		if err := validateRelPath(rel); err == nil {
			t.Errorf("validateRelPath(%q) = nil, want escape rejection", rel)
		}
	}
}

// TestWriteYAMLRejectsEscape proves the manifest writer's containment
// guard refuses traversal names. The os.Exit error paths are covered by
// the subprocess tests, so the exit-free guard is asserted directly.
func TestWriteYAMLRejectsEscape(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"..", `..\x`} {
		if err := validateRelPath(name); err == nil {
			t.Errorf("validateRelPath(%q) = nil, want escape rejection", name)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "upstreammap-escape.yaml")); err == nil {
		t.Errorf("escape write leaked outside the target directory")
	}
}

// TestValidateYAMLFileRejectsEscape proves manifest validation never
// reads outside the target directory.
func TestValidateYAMLFileRejectsEscape(t *testing.T) {
	dir := t.TempDir()
	for _, rel := range []string{"..", "../does-not-matter.yaml", "/etc/hostname"} {
		_, errOut := captureOutput(t, func() {
			validateYAMLFile(filepath.Join(dir, rel), "provider")
		})
		if !strings.Contains(errOut, "ERROR reading") {
			t.Errorf("validateYAMLFile(rel=%q) stderr = %q, want ERROR reading", rel, errOut)
		}
	}
}
