package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func isolatedManager(t *testing.T) *Manager {
	t.Helper()
	t.Setenv("GOROUTER_STATE_FILE", filepath.Join(t.TempDir(), "lifecycle.json"))
	return NewManager(8080, "service")
}
func TestSeparateManagersShareLifecycleState(t *testing.T) {
	a := isolatedManager(t)
	if e := a.RunFirstRun(); e != nil {
		t.Fatal(e)
	}
	b := NewManager(8080, "service")
	s, e := b.Status()
	if e != nil || !s.Running || s.PID == 0 {
		t.Fatalf("separate manager did not observe running state: %#v %v", s, e)
	}
	if e = b.Stop(Graceful); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(os.Getenv("GOROUTER_STATE_FILE")); !os.IsNotExist(e) {
		t.Fatalf("state file should be removed after stop, err=%v", e)
	}
}
func TestStatusFields(t *testing.T) {
	m := isolatedManager(t)
	s, e := m.Status()
	if e != nil || s.Port != 8080 || s.Mode != "service" || s.Running {
		t.Fatalf("unexpected status: %#v %v", s, e)
	}
}
func TestGracefulForceExit(t *testing.T) {
	m := isolatedManager(t)
	if e := m.RunFirstRun(); e != nil {
		t.Fatal(e)
	}
	if e := m.Stop(Graceful); e != nil {
		t.Fatal(e)
	}
	if e := m.Stop(Force); e == nil {
		t.Fatal("expected not running")
	}
}
func TestAutostartPolicy(t *testing.T) {
	m := isolatedManager(t)
	if m.AutostartEnabled() {
		t.Fatal("default autostart must be disabled")
	}
	if e := m.RunFirstRun(); e != nil {
		t.Fatal(e)
	}
	m.SetAutostart(true)
	if !NewManager(8080, "service").AutostartEnabled() {
		t.Fatal("autostart not durable")
	}
}
func TestAlreadyRunning(t *testing.T) {
	m := isolatedManager(t)
	_ = m.RunFirstRun()
	if e := m.RunFirstRun(); e == nil {
		t.Fatal("expected already running")
	}
}
func TestLiveLogsFlags(t *testing.T) {
	m := isolatedManager(t)
	m.AddLog("hello")
	ch, e := m.Logs(Tail(1))
	if e != nil {
		t.Fatal(e)
	}
	if got := <-ch; got == "" {
		t.Fatal("empty log")
	}
}
func TestFollowReadsSeparateManagerWrites(t *testing.T) {
	a := isolatedManager(t)
	ch, e := NewManager(8080, "service").Logs(Follow())
	if e != nil {
		t.Fatal(e)
	}
	time.Sleep(15 * time.Millisecond)
	a.AddLog("shared")
	select {
	case got := <-ch:
		if got != "shared\n" {
			t.Fatalf("got %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("follow did not observe shared log")
	}
}
