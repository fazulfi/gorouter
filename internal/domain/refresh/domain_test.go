package refresh_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func projectRoot() string {
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Dir(file)
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	return ""
}

func TestDomainRefreshNoInfrastructure(t *testing.T) {
	t.Parallel()
	root := projectRoot()
	if root == "" {
		t.Skip("cannot find project root")
	}
	pkgDir := filepath.Join(root, "internal", "domain", "refresh")
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		t.Fatal(err)
	}

	forbidden := []string{
		"gorouter/internal/engine",
		"gorouter/internal/transport",
		"gorouter/internal/persistence",
		"net/http",
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		if strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, filepath.Join(pkgDir, entry.Name()), nil, parser.AllErrors)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, "\"")
			for _, forbid := range forbidden {
				if strings.HasPrefix(path, forbid) {
					t.Errorf("%s imports forbidden package: %s", entry.Name(), path)
				}
			}
		}
	}
}
