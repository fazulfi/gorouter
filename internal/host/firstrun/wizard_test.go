package firstrun

import "testing"

func TestHeadlessNoOpen(t *testing.T) {
	called := false
	err := (Wizard{URL: "http://x", Open: func(string) error { called = true; return nil }}).Run(Options{Headless: true})
	if err != nil || called {
		t.Fatalf("err=%v called=%v", err, called)
	}
}
func TestBareRun(t *testing.T) {
	called := false
	err := (Wizard{URL: "http://x", Open: func(string) error { called = true; return nil }}).Run(Options{})
	if err != nil || !called {
		t.Fatalf("err=%v called=%v", err, called)
	}
}
