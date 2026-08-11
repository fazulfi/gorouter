package tray

import "testing"

func TestMenuActions(t *testing.T) {
	menu := New().Menu()
	if len(menu.Items) != 4 {
		t.Fatalf("items=%d", len(menu.Items))
	}
}
func TestRunningState(t *testing.T) {
	tr := New()
	tr.SetRunning(true)
	if !tr.Running() {
		t.Fatal("not running")
	}
}
