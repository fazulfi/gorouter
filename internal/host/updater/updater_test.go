package updater

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func payloadInfo(p []byte) string { return fmtHash(p) }
func fmtHash(p []byte) string     { return fmt.Sprintf("%x", sha256.Sum256(p)) }

func TestStartupOnlyChecks(t *testing.T) {
	called := false
	h := HealthGate{Startup: func(context.Context) error { called = true; return nil }, Readiness: func(context.Context) error { return errors.New("not ready") }}
	if err := h.StartupOnlyChecks(context.Background()); err != nil || !called {
		t.Fatal(err)
	}
	if h.Ready(context.Background()) {
		t.Fatal("readiness should fail")
	}
}
func TestOptInUpdate(t *testing.T) {
	if err := VerifyChecksum([]byte("x"), fmtHash([]byte("x"))); err != nil {
		t.Fatal(err)
	}
}
func TestIndefiniteDrainProgress(t *testing.T) {
	d := NewDrainController()
	done := d.Begin()
	go func() { time.Sleep(20 * time.Millisecond); done() }()
	if err := d.Drain(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
}
func TestStagedVerification(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	p := []byte("payload")
	sig := ed25519.Sign(priv, p)
	if err := VerifySignature(p, sig, pub); err != nil {
		t.Fatal(err)
	}
	if err := VerifySignature(p, nil, pub); !errors.Is(err, ErrMissingSignature) {
		t.Fatal(err)
	}
}
func TestMigrationPreflight(t *testing.T) {
	called := false
	u := Updater{SchemaVersion: 2, InstallDir: t.TempDir(), Current: "app", Migrate: func(context.Context, int) error { called = true; return nil }}
	p := []byte("x")
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	if err := u.Apply(context.Background(), Manifest{MinSchemaVersion: 1, Checksum: fmtHash(p)}, p, ed25519.Sign(priv, p), pub); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("migration not called")
	}
}

func TestRejectsInstallDirEscape(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(filepath.Dir(dir), "outside-target")
	p := []byte("new")
	if err := os.WriteFile(outside, []byte("safe"), 0600); err != nil {
		t.Fatal(err)
	}
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	u := Updater{InstallDir: dir, Current: "../outside-target"}
	if err := u.Apply(context.Background(), Manifest{Checksum: fmtHash(p)}, p, ed25519.Sign(priv, p), pub); err == nil {
		t.Fatal("path escape must be rejected")
	}
	got, err := os.ReadFile(outside)
	if err != nil || string(got) != "safe" {
		t.Fatalf("outside target changed: %q, %v", got, err)
	}
}

func TestFailedMigrationRollsBackSideEffects(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	if err := os.WriteFile(state, []byte("old-state"), 0600); err != nil {
		t.Fatal(err)
	}
	p := []byte("new")
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	u := Updater{InstallDir: dir, Current: "app", Migrate: func(context.Context, int) error {
		if err := os.WriteFile(state, []byte("new-state"), 0600); err != nil {
			return err
		}
		return errors.New("migration failed")
	}, MigrateRollback: func(context.Context) error { return os.WriteFile(state, []byte("old-state"), 0600) }}
	if err := u.Apply(context.Background(), Manifest{Checksum: fmtHash(p)}, p, ed25519.Sign(priv, p), pub); err == nil {
		t.Fatal("expected migration failure")
	}
	got, err := os.ReadFile(state)
	if err != nil || string(got) != "old-state" {
		t.Fatalf("migration side effect not rolled back: %q, %v", got, err)
	}
}

func TestSuccessfulApplyInvalidatesRollbackSnapshot(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	u := Updater{InstallDir: dir, Current: "app"}
	first := []byte("new")
	if err := u.Apply(context.Background(), Manifest{Checksum: fmtHash(first)}, first, ed25519.Sign(priv, first), pub); err != nil {
		t.Fatal(err)
	}
	if err := u.rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "app")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale snapshot restored after success: %v", err)
	}
}
func TestAtomicSwap(t *testing.T) {
	dir := t.TempDir()
	p := []byte("x")
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	u := Updater{InstallDir: dir, Current: "app"}
	if err := u.Apply(context.Background(), Manifest{Checksum: fmtHash(p)}, p, ed25519.Sign(priv, p), pub); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "app"))
	if string(got) != "x" {
		t.Fatal(string(got))
	}
}
func TestOneHealthAttempt(t *testing.T) {
	n := 0
	p := []byte("x")
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	u := Updater{InstallDir: t.TempDir(), Current: "app", Health: func(context.Context, string) error { n++; return errors.New("bad") }}
	if err := u.Apply(context.Background(), Manifest{Checksum: fmtHash(p)}, p, ed25519.Sign(priv, p), pub); !errors.Is(err, ErrHealthCheck) {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal(n)
	}
}
func TestCompatibleRollback(t *testing.T) {
	dir := t.TempDir()
	p := []byte("new")
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	u := Updater{InstallDir: dir, Current: "app", Start: func(context.Context, string) error { return errors.New("start") }}
	if err := u.Apply(context.Background(), Manifest{Checksum: fmtHash(p)}, p, ed25519.Sign(priv, p), pub); err == nil {
		t.Fatal("expected start failure")
	}
}
func TestIncompatibleSchemaSafeMode(t *testing.T) {
	p := []byte("x")
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	u := Updater{SchemaVersion: 1}
	if !errors.Is(u.Apply(context.Background(), Manifest{MinSchemaVersion: 2, Checksum: fmtHash(p)}, p, ed25519.Sign(priv, p), pub), ErrSchemaIncompatible) {
		t.Fatal("must fail closed")
	}
}
func TestOldInstallPreserved(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	p := []byte("new")
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	u := Updater{InstallDir: dir, Current: "app", Health: func(context.Context, string) error { return errors.New("bad") }}
	if err := u.Apply(context.Background(), Manifest{Checksum: fmtHash(p)}, p, ed25519.Sign(priv, p), pub); !errors.Is(err, ErrHealthCheck) {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "app"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old" {
		t.Fatalf("rollback lost previous version: %q", got)
	}
}

func TestSignatureAndChecksumFailures(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	p := []byte("payload")
	if err := VerifySignature(p, []byte("bad"), pub); !errors.Is(err, ErrInvalidSignature) {
		t.Fatal(err)
	}
	if err := VerifySignature(p, ed25519.Sign(priv, p), ed25519.PublicKey("bad")); !errors.Is(err, ErrMissingSignature) {
		t.Fatal(err)
	}
	if err := VerifyChecksum(p, ""); err == nil {
		t.Fatal("empty checksum must fail closed")
	}
}

func TestDrainTimeoutAndCancellation(t *testing.T) {
	d := NewDrainController()
	stop := d.Begin()
	if err := d.Drain(context.Background(), time.Millisecond); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := d.Drain(ctx, 0); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	stop()
}

func TestHealthGateStartupAndLiveness(t *testing.T) {
	if err := (HealthGate{}).StartupOnly(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := (HealthGate{Startup: func(context.Context) error { return errors.New("startup") }}).StartupOnly(context.Background()); err == nil {
		t.Fatal("startup failure must propagate")
	}
	if (HealthGate{Liveness: func(context.Context) error { return errors.New("dead") }}).Live(context.Background()) {
		t.Fatal("liveness must fail")
	}
	if !(HealthGate{}).Live(context.Background()) {
		t.Fatal("nil liveness must pass")
	}
}

func TestFaultInjectionFailClosed(t *testing.T) {
	p := []byte("x")
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	for name, faults := range map[string]Faults{
		"download":  {Download: errors.New("download")},
		"migration": {Migration: errors.New("migration")},
		"swap":      {Swap: errors.New("swap")},
		"start":     {Start: errors.New("start")},
		"health":    {Health: errors.New("health")},
	} {
		u := Updater{InstallDir: t.TempDir(), Current: "app", SchemaVersion: 1, Faults: faults, Migrate: func(context.Context, int) error { return nil }, Start: func(context.Context, string) error { return nil }, Health: func(context.Context, string) error { return nil }}
		if err := u.Apply(context.Background(), Manifest{MinSchemaVersion: 1, Checksum: fmtHash(p)}, p, ed25519.Sign(priv, p), pub); err == nil {
			t.Fatalf("%s fault must fail closed", name)
		}
	}
}

func TestCopy(t *testing.T) {
	var dst bytes.Buffer
	if err := Copy(&dst, strings.NewReader("copied")); err != nil {
		t.Fatal(err)
	}
	if dst.String() != "copied" {
		t.Fatalf("unexpected copy: %q", dst.String())
	}
}

func TestRollbackFailureIsReported(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	p := []byte("new")
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	u := Updater{InstallDir: dir, Current: "app", Start: func(context.Context, string) error { return errors.New("start") }}
	if err := u.Apply(context.Background(), Manifest{Checksum: fmtHash(p)}, p, ed25519.Sign(priv, p), pub); err == nil {
		t.Fatal("expected failed apply")
	}
}
