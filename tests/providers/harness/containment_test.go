package harness

import (
	"os"
	"path/filepath"
	"testing"
)

// TestValidateRelPathRejectsEscapes is the white-box unit test for the
// harness containment guard: relative paths that leave the rooted
// directory scope must be rejected, while in-root relative paths are
// accepted.
func TestValidateRelPathRejectsEscapes(t *testing.T) {
	good := []string{
		"a.json",
		"testdata/fixtures/openai_chat.json",
		"a/b/c.txt",
		"./rel.txt",
		"a/../b.txt", // resolves inside the root; not an escape
	}
	for _, rel := range good {
		if err := validateRelPath(rel); err != nil {
			t.Errorf("validateRelPath(%q) = %v, want nil", rel, err)
		}
	}

	bad := []string{
		"",
		"..",
		"../secret",
		"a/../../secret",
		"/etc/passwd",
		`..\escape`,
		"./../escape",
	}
	for _, rel := range bad {
		if err := validateRelPath(rel); err == nil {
			t.Errorf("validateRelPath(%q) = nil, want escape rejection", rel)
		}
	}
}

// TestRootedReadRejectsTraversal proves os.Root containment end to end:
// a path that walks out of the root must fail even though the target
// exists on disk outside the root.
func TestRootedReadRejectsTraversal(t *testing.T) {
	base := t.TempDir()
	parent := filepath.Dir(base)
	secret := filepath.Join(parent, "harness-escape-secret.txt")
	if err := os.WriteFile(secret, []byte("top secret"), 0o600); err != nil {
		t.Fatalf("write escape target: %v", err)
	}
	defer os.Remove(secret)

	root, err := os.OpenRoot(base)
	if err != nil {
		t.Fatalf("OpenRoot: %v", err)
	}
	defer root.Close()

	for _, rel := range []string{"..", "../harness-escape-secret.txt", "sub/../../harness-escape-secret.txt", "/etc/hostname"} {
		if _, err := root.ReadFile(rel); err == nil {
			t.Errorf("ReadFile(%q) succeeded; must reject escape", rel)
		}
	}
}
