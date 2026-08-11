package postgresmanaged

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeAdapter struct {
	starts, stops int
	startErr      error
}

func (f *fakeAdapter) start(context.Context, Config) error           { f.starts++; return f.startErr }
func (f *fakeAdapter) stop(context.Context, Config) error            { f.stops++; return nil }
func (f *fakeAdapter) verify(_ context.Context, config Config) error { return verifyMarker(config) }

func TestFirstRunAndPreservedData(t *testing.T) {
	dir := t.TempDir()
	a := &fakeAdapter{}
	m, err := New(Config{DataDir: dir}, a)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Provision(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "application.data"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.Upgrade(context.Background(), "16", "17"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "application.data"))
	if err != nil || string(data) != "keep" {
		t.Fatalf("data not preserved: %q %v", data, err)
	}
}

func TestFailedSetupRecoveryAndNoAutoReset(t *testing.T) {
	dir := t.TempDir()
	a := &fakeAdapter{startErr: os.ErrPermission}
	m, _ := New(Config{DataDir: dir}, a)
	if err := m.Provision(context.Background()); err == nil {
		t.Fatal("expected setup failure")
	}
	if _, err := os.Stat(filepath.Join(dir, "PG_VERSION")); err != nil {
		t.Fatal(err)
	}
}

func TestRelocationOnlyWhenStopped(t *testing.T) {
	dir := t.TempDir()
	m, _ := New(Config{DataDir: dir}, &fakeAdapter{})
	if err := m.Provision(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.Upgrade(context.Background(), "16", "17"); err != ErrMustBeStopped {
		t.Fatalf("got %v", err)
	}
}

func TestPlatformConstructorsAndIntegrity(t *testing.T) {
	dir := t.TempDir()
	for name, constructor := range map[string]func(Config) (*Manager, error){
		"windows": NewWindows, "macos": NewMacOS, "linux": NewLinux,
	} {
		m, err := constructor(Config{DataDir: dir, Runner: &recordingRunner{}})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := m.Provision(context.Background()); err != nil {
			t.Fatalf("%s provision: %v", name, err)
		}
		if err := m.Stop(context.Background()); err != nil {
			t.Fatalf("%s stop: %v", name, err)
		}
		if err := m.VerifyDataIntegrity(context.Background()); err != nil {
			t.Fatalf("%s verify: %v", name, err)
		}
	}
}

func TestPlatformAdaptersExecuteLifecycleAndVerifyState(t *testing.T) {
	dir := t.TempDir()
	runner := &recordingRunner{}
	m, err := NewLinux(Config{DataDir: dir, Runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Provision(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(runner.commands) != 1 || runner.commands[0] != "pg_ctl start -D "+dir+" -w" {
		t.Fatalf("unexpected start command: %v", runner.commands)
	}
	if err := m.VerifyDataIntegrity(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(runner.commands) != 2 || runner.commands[1] != "pg_ctl stop -D "+dir+" -m fast -w" {
		t.Fatalf("unexpected stop command: %v", runner.commands)
	}
	if err := m.VerifyDataIntegrity(context.Background()); err != nil {
		t.Fatal(err)
	}
}

type recordingRunner struct{ commands []string }

func (r *recordingRunner) Run(_ context.Context, name string, args ...string) error {
	r.commands = append(r.commands, name+" "+strings.Join(args, " "))
	return nil
}

func TestValidationAndRunning(t *testing.T) {
	if _, err := New(Config{}, &fakeAdapter{}); err == nil {
		t.Fatal("expected data dir validation")
	}
	if _, err := New(Config{DataDir: t.TempDir()}, nil); err == nil {
		t.Fatal("expected adapter validation")
	}
	dir := t.TempDir()
	m, _ := New(Config{DataDir: dir}, &fakeAdapter{})
	if m.Running() {
		t.Fatal("manager should start stopped")
	}
	if err := m.Upgrade(context.Background(), "", "17"); err == nil {
		t.Fatal("expected version validation")
	}
}

func TestLifecycleErrorsAndIntegrity(t *testing.T) {
	dir := t.TempDir()
	m, _ := New(Config{DataDir: dir}, &fakeAdapter{})
	if err := m.Stop(context.Background()); err != ErrNotRunning {
		t.Fatal(err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background()); err != ErrAlreadyRunning {
		t.Fatal(err)
	}
}
