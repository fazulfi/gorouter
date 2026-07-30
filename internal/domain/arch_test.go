// Package domain enforces architectural boundaries for the domain layer.
// Tests here verify that domain packages remain pure and free from framework
// and infrastructure imports.
package domain

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// modulePrefix is the Go module path for this project.
const modulePrefix = "gorouter"

// projectRoot returns the absolute path to the project root by walking up
// from the source file location until it finds go.mod.
func projectRoot() string {
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Dir(file)
	for {
		if _, err := filepath.Glob(filepath.Join(dir, "go.mod")); err == nil {
			if fi, _ := filepath.Glob(filepath.Join(dir, "go.mod")); len(fi) > 0 {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// findImports parses all .go files under the given directory (non-recursively or
// recursively based on recurse) and returns a list of import paths that match any
// of the forbidden prefixes.
func findImports(dir string, forbiddenPrefixes []string, recurse bool, excludePrefixes ...string) []string {
	var violations []string
	root := projectRoot()
	if root == "" {
		return nil
	}
	base := filepath.Join(root, dir)

	// Collect .go files
	var files []string
	if recurse {
		filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				return nil
			}
			if strings.HasSuffix(path, "_test.go") {
				return nil
			}
			if strings.HasSuffix(path, ".go") {
				// Normalize path for exclusion check (ToSlash for Windows compat)
				rel := filepath.ToSlash(path)
				rootSlash := filepath.ToSlash(root)
				relShort, _ := strings.CutPrefix(rel, rootSlash+"/")
				for _, ex := range excludePrefixes {
					if strings.HasPrefix(relShort, ex) {
						return nil
					}
				}
				files = append(files, path)
			}
			return nil
		})
	} else {
		entries, err := filepath.Glob(filepath.Join(base, "*.go"))
		if err != nil {
			return nil
		}
		for _, e := range entries {
			if !strings.HasSuffix(e, "_test.go") {
				rel := filepath.ToSlash(e)
				rootSlash := filepath.ToSlash(root)
				relShort, _ := strings.CutPrefix(rel, rootSlash+"/")
				skip := false
				for _, ex := range excludePrefixes {
					if strings.HasPrefix(relShort, ex) {
						skip = true
						break
					}
				}
				if !skip {
					files = append(files, e)
				}
			}
		}
	}

	for _, file := range files {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, file, nil, parser.ImportsOnly)
		if err != nil {
			continue
		}
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			for _, forbidden := range forbiddenPrefixes {
				if strings.HasPrefix(path, forbidden) {
					violations = append(violations,
						file+": imports "+path)
				}
			}
		}
	}
	return violations
}

// TestDomainDoesNotImportPostgres verifies no domain package imports the
// postgres persistence layer.
func TestDomainDoesNotImportPostgres(t *testing.T) {
	violations := findImports("internal/domain", []string{
		modulePrefix + "/internal/persistence",
		"github.com/jackc/pgx",
	}, true)
	if len(violations) > 0 {
		t.Errorf("domain packages must not import persistence layer:\n%s",
			strings.Join(violations, "\n"))
	}
}

// TestDomainDoesNotImportChi verifies domain packages do not depend on chi.
func TestDomainDoesNotImportChi(t *testing.T) {
	violations := findImports("internal/domain", []string{
		"github.com/go-chi",
	}, true)
	if len(violations) > 0 {
		t.Errorf("domain packages must not import chi:\n%s",
			strings.Join(violations, "\n"))
	}
}

// TestDomainDoesNotImportHTTP verifies domain packages do not import net/http.
// The domain/provider/transport sub-package is exempted because it implements
// the RoundTripper interface which requires net/http types.
func TestDomainDoesNotImportHTTP(t *testing.T) {
	violations := findImports("internal/domain", []string{
		"net/http",
	}, true, "internal/domain/provider/transport")
	if len(violations) > 0 {
		t.Errorf("domain packages must not import net/http:\n%s",
			strings.Join(violations, "\n"))
	}
}

// TestTransportDoesNotImportDomainDirectly verifies the transport layer does
// NOT import domain packages directly — it must go through the application
// layer (internal/app). The internal/transport/httpserver/api, "internal/transport/middleware" package is
// exempted because API handlers must reference domain types (stream, engine
// interfaces) for request handling; a future refactor should route these
// through a dedicated internal/app/api bridge.
func TestTransportDoesNotImportDomainDirectly(t *testing.T) {
	// Only check non-test Go files under internal/transport
	violations := findImports("internal/transport", []string{
		modulePrefix + "/internal/domain",
	}, true, "internal/transport/httpserver/api", "internal/transport/middleware")
	if len(violations) > 0 {
		t.Errorf("transport layer must not import domain directly (violations: %v); "+
			"route through internal/app instead", violations)
	} else {
		t.Log("OK: transport layer has no direct domain imports")
	}
}

// TestDomainDoesNotImportTransport verifies domain packages don't import
// the transport layer at all.
func TestDomainDoesNotImportTransport(t *testing.T) {
	violations := findImports("internal/domain", []string{
		modulePrefix + "/internal/transport",
	}, true)
	if len(violations) > 0 {
		t.Errorf("domain packages must not import transport:\n%s",
			strings.Join(violations, "\n"))
	}
}
