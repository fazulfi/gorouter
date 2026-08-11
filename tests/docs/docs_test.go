package docs

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestDocumentationAndLocaleCoverage(t *testing.T) {
	root := filepath.Join("..", "..")
	docs := []string{"README.md", "docs/public/quickstart.md", "docs/public/configuration.md", "docs/public/api.md", "docs/public/providers.md", "docs/public/oauth.md", "docs/public/deployment-checklist.md", "docs/public/backup.md", "docs/public/troubleshooting.md", "docs/public/migration.md", "docs/public/contributor.md", "docs/public/architecture.md"}
	for _, path := range docs {
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			t.Errorf("missing owned documentation %s: %v", path, err)
		}
	}
	en := loadCatalog(t, filepath.Join(root, "frontend/src/i18n/catalogs/en.json"))
	id := loadCatalog(t, filepath.Join(root, "frontend/src/i18n/catalogs/id.json"))
	if !sameKeys(en, id) {
		t.Fatal("locale catalogs have different key sets")
	}
	for _, locale := range []string{"en", "id"} {
		loadCatalog(t, filepath.Join(root, "packaging/locales", locale+".json"))
	}
	re := regexp.MustCompile(`(?i)\b(todo|fixme|tbd|coming soon|lorem ipsum|your[_ -]?api[_ -]?key)\b`)
	for _, path := range docs {
		b, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			continue
		}
		if re.Match(b) {
			t.Errorf("placeholder or secret-like copy in %s", path)
		}
		for _, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, "](") && strings.Contains(line, "docs/public/missing") {
				t.Errorf("broken internal link in %s", path)
			}
		}
	}
}

func TestInternalMarkdownAnchors(t *testing.T) {
	root := filepath.Join("..", "..")
	docs := []string{"README.md", "docs/public/quickstart.md", "docs/public/configuration.md", "docs/public/api.md", "docs/public/providers.md", "docs/public/oauth.md", "docs/public/deployment-checklist.md", "docs/public/backup.md", "docs/public/troubleshooting.md", "docs/public/migration.md", "docs/public/contributor.md", "docs/public/architecture.md"}
	for _, path := range docs {
		checkInternalLinks(t, root, path)
	}
}

func checkInternalLinks(t *testing.T, root, source string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, source))
	if err != nil {
		t.Fatalf("read %s: %v", source, err)
	}
	linkRE := regexp.MustCompile(`\[[^]]*\]\(([^)]+)\)`)
	for _, match := range linkRE.FindAllStringSubmatch(string(b), -1) {
		target, err := url.Parse(match[1])
		if err != nil || target.IsAbs() || target.Host != "" || target.Scheme != "" || strings.HasPrefix(target.Path, "//") {
			continue
		}
		anchor := strings.TrimPrefix(target.Fragment, "#")
		if anchor == "" {
			continue
		}
		resolved := filepath.Join(filepath.Dir(filepath.Join(root, source)), filepath.FromSlash(target.Path))
		if target.Path == "" {
			resolved = filepath.Join(root, source)
		}
		content, err := os.ReadFile(resolved)
		if err != nil {
			t.Errorf("broken internal link in %s: target %s: %v", source, match[1], err)
			continue
		}
		if !anchorExists(string(content), anchor) {
			t.Errorf("missing internal anchor in %s: #%s (target %s)", source, anchor, match[1])
		}
	}
}

func anchorExists(content, anchor string) bool {
	id := regexp.QuoteMeta(anchor)
	if regexp.MustCompile(`(?m)<a[^>]+(?:id|name)=["']` + id + `["']`).MatchString(content) {
		return true
	}
	for _, heading := range regexp.MustCompile(`(?m)^#{1,6}\s+(.+?)\s*#*\s*$`).FindAllStringSubmatch(content, -1) {
		text := strings.ToLower(strings.TrimSpace(heading[1]))
		text = regexp.MustCompile(`[^a-z0-9 -]`).ReplaceAllString(text, "")
		text = strings.ReplaceAll(text, " ", "-")
		if text == strings.ToLower(anchor) {
			return true
		}
	}
	return false
}

func loadCatalog(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var v map[string]any
	if json.Unmarshal(b, &v) != nil {
		t.Fatalf("invalid JSON catalog %s", path)
	}
	return v
}
func sameKeys(a, b map[string]any) bool {
	for k, av := range a {
		bv, ok := b[k]
		if !ok {
			return false
		}
		am, aok := av.(map[string]any)
		bm, bok := bv.(map[string]any)
		if aok != bok || (aok && !sameKeys(am, bm)) {
			return false
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			return false
		}
	}
	return true
}
