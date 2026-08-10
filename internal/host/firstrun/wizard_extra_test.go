package firstrun

import (
	"os"
	"testing"
)

func TestDefaultAndDetached(t *testing.T) {
	w := Default()
	if w.URL == "" {
		t.Fatal("default URL missing")
	}
	if w.TerminalDetached() {
		t.Fatal("unexpected detached")
	}
	if err := os.Setenv("GOROUTER_DETACHED", "1"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Unsetenv("GOROUTER_DETACHED") })
	if !w.TerminalDetached() {
		t.Fatal("detached marker ignored")
	}
}
func TestNoOpenAndInvalid(t *testing.T) {
	if err := (Wizard{}).Run(Options{}); err == nil {
		t.Fatal("expected URL error")
	}
	if err := (Wizard{URL: "http://x", Open: func(string) error { return nil }}).Run(Options{NoOpen: true}); err != nil {
		t.Fatal(err)
	}
}
