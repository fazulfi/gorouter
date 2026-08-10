package updater

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	ErrMissingSignature   = errors.New("update signature is required")
	ErrInvalidSignature   = errors.New("update signature is invalid")
	ErrHealthCheck        = errors.New("update health check failed")
	ErrSchemaIncompatible = errors.New("update schema is incompatible")
)

type Manifest struct {
	Version          string `json:"version"`
	Platform         string `json:"platform"`
	Checksum         string `json:"checksum"`
	SignatureURL     string `json:"signature_url"`
	MinSchemaVersion int    `json:"min_schema_version"`
	CompatWindowDays int    `json:"compat_window_days"`
}
type Faults struct{ Download, Swap, Start, Health, Migration error }

func VerifySignature(payload, signature []byte, key ed25519.PublicKey) error {
	if len(key) != ed25519.PublicKeySize || len(signature) == 0 {
		return ErrMissingSignature
	}
	if !ed25519.Verify(key, payload, signature) {
		return ErrInvalidSignature
	}
	return nil
}
func VerifyChecksum(payload []byte, expected string) error {
	got := fmt.Sprintf("%x", sha256.Sum256(payload))
	if expected == "" || got != expected {
		return fmt.Errorf("checksum mismatch")
	}
	return nil
}

type DrainController struct {
	mu     sync.Mutex
	active int
	done   chan struct{}
}

func NewDrainController() *DrainController { return &DrainController{done: make(chan struct{})} }
func (d *DrainController) Begin() func() {
	d.mu.Lock()
	d.active++
	d.mu.Unlock()
	return func() {
		d.mu.Lock()
		d.active--
		if d.active == 0 {
			close(d.done)
		}
		d.mu.Unlock()
	}
}
func (d *DrainController) Drain(ctx context.Context, maxWait time.Duration) error {
	d.mu.Lock()
	active := d.active
	done := d.done
	d.mu.Unlock()
	if active == 0 {
		return nil
	}
	if maxWait <= 0 {
		select {
		case <-done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	timer := time.NewTimer(maxWait)
	defer timer.Stop()
	select {
	case <-done:
		return nil
	case <-timer.C:
		return context.DeadlineExceeded
	case <-ctx.Done():
		return ctx.Err()
	}
}

type HealthGate struct {
	Startup   func(context.Context) error
	Readiness func(context.Context) error
	Liveness  func(context.Context) error
}

func (h HealthGate) StartupOnly(ctx context.Context) error {
	if h.Startup == nil {
		return nil
	}
	return h.Startup(ctx)
}
func (h HealthGate) StartupOnlyChecks(ctx context.Context) error { return h.StartupOnly(ctx) }
func (h HealthGate) Ready(ctx context.Context) bool {
	return h.Readiness == nil || h.Readiness(ctx) == nil
}
func (h HealthGate) Live(ctx context.Context) bool {
	return h.Liveness == nil || h.Liveness(ctx) == nil
}

type Updater struct {
	InstallDir      string
	Current         string
	SchemaVersion   int
	Faults          Faults
	Health          func(context.Context, string) error
	Start           func(context.Context, string) error
	Migrate         func(context.Context, int) error
	MigrateRollback func(context.Context) error
	backup          []byte
	backupMode      os.FileMode
	backupExists    bool
}

func (u *Updater) Apply(ctx context.Context, m Manifest, payload, signature []byte, key ed25519.PublicKey) error {
	if u.Faults.Download != nil {
		return u.Faults.Download
	}
	if m.MinSchemaVersion > u.SchemaVersion {
		return ErrSchemaIncompatible
	}
	if err := VerifySignature(payload, signature, key); err != nil {
		return err
	}
	if err := VerifyChecksum(payload, m.Checksum); err != nil {
		return err
	}
	if err := u.snapshot(); err != nil {
		return err
	}
	if u.Migrate != nil {
		if u.Faults.Migration != nil {
			return u.Faults.Migration
		}
		if err := u.Migrate(ctx, m.MinSchemaVersion); err != nil {
			if u.MigrateRollback != nil {
				_ = u.MigrateRollback(ctx)
			}
			return err
		}
	}
	if err := u.swap(payload); err != nil {
		return err
	}
	if u.Start != nil {
		if u.Faults.Start != nil {
			return u.failWithRollback(u.Faults.Start)
		}
		if err := u.Start(ctx, u.Current); err != nil {
			return u.failWithRollback(err)
		}
	}
	if u.Health != nil {
		if u.Faults.Health != nil {
			return u.failWithRollback(u.Faults.Health)
		}
		if err := u.Health(ctx, u.Current); err != nil {
			return u.failWithRollback(ErrHealthCheck)
		}
	}
	u.backup = nil
	u.backupMode = 0
	u.backupExists = false
	return nil
}
func (u *Updater) targetPath() (string, error) {
	if u.InstallDir == "" {
		return "", errors.New("install directory is required")
	}
	if u.Current == "" || filepath.IsAbs(u.Current) {
		return "", errors.New("current target must be relative")
	}
	root, err := filepath.Abs(u.InstallDir)
	if err != nil {
		return "", err
	}
	target, err := filepath.Abs(filepath.Join(root, filepath.Clean(u.Current)))
	if err != nil {
		return "", err
	}
	prefix := root + string(os.PathSeparator)
	if target != root && !strings.HasPrefix(target, prefix) {
		return "", errors.New("current target escapes install directory")
	}
	return target, nil
}
func (u *Updater) swap(payload []byte) error {
	if u.Faults.Swap != nil {
		return u.Faults.Swap
	}
	if u.InstallDir == "" {
		return errors.New("install directory is required")
	}
	if err := os.MkdirAll(u.InstallDir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(u.InstallDir, ".update-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = tmp.Write(payload); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	target, err := u.targetPath()
	if err != nil {
		return err
	}
	return os.Rename(name, target)
}
func (u *Updater) snapshot() error {
	if u.InstallDir == "" {
		return errors.New("install directory is required")
	}
	target, err := u.targetPath()
	if err != nil {
		return err
	}
	info, err := os.Stat(target)
	if errors.Is(err, os.ErrNotExist) {
		u.backup = nil
		u.backupExists = false
		return nil
	}
	if err != nil {
		return err
	}
	data, err := os.ReadFile(target) // #nosec G304 -- target is validated as the updater's installation path.
	if err != nil {
		return err
	}
	u.backup, u.backupMode, u.backupExists = data, info.Mode(), true
	return nil
}
func (u *Updater) failWithRollback(cause error) error {
	if err := u.rollback(); err != nil {
		return fmt.Errorf("%w: rollback failed: %v", cause, err)
	}
	return cause
}
func (u *Updater) rollback() error {
	if u.InstallDir == "" {
		return errors.New("install directory is required")
	}
	target, err := u.targetPath()
	if err != nil {
		return err
	}
	if !u.backupExists {
		err := os.Remove(target)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	tmp, err := os.CreateTemp(u.InstallDir, ".rollback-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = tmp.Write(u.backup); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Chmod(u.backupMode.Perm()); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, target)
}
func Copy(dst io.Writer, src io.Reader) error { _, err := io.Copy(dst, src); return err }
