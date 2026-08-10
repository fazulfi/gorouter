package postgresmanaged

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var (
	ErrAlreadyRunning = errors.New("postgresql is already running")
	ErrNotRunning     = errors.New("postgresql is not running")
	ErrMustBeStopped  = errors.New("postgresql must be stopped")
)

type CommandRunner interface {
	Run(context.Context, string, ...string) error
}
type Config struct {
	DataDir string
	Mode    string
	Runner  CommandRunner
}
type PostgresManager interface {
	Provision(context.Context) error
	Upgrade(context.Context, string, string) error
	Stop(context.Context) error
	Start(context.Context) error
	VerifyDataIntegrity(context.Context) error
}
type Manager struct {
	config   Config
	platform platformAdapter
	mu       sync.Mutex
	running  bool
}
type platformAdapter interface {
	start(context.Context, Config) error
	stop(context.Context, Config) error
	verify(context.Context, Config) error
}

func New(config Config, platform platformAdapter) (*Manager, error) {
	if config.DataDir == "" {
		return nil, errors.New("data directory is required")
	}
	if platform == nil {
		return nil, errors.New("platform adapter is required")
	}
	return &Manager{config: config, platform: platform}, nil
}
func runCommand(ctx context.Context, config Config, name string, args ...string) error {
	if config.Runner == nil {
		return errors.New("command runner is required")
	}
	return config.Runner.Run(ctx, name, args...)
}
func (m *Manager) Provision(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running {
		return ErrAlreadyRunning
	}
	if err := os.MkdirAll(m.config.DataDir, 0o700); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	marker := filepath.Join(m.config.DataDir, "PG_VERSION")
	if _, err := os.Stat(marker); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(marker, []byte("16\n"), 0o600); err != nil {
			return fmt.Errorf("initialize database: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("inspect database: %w", err)
	}
	if err := m.platform.start(ctx, m.config); err != nil {
		return fmt.Errorf("start after provision: %w", err)
	}
	m.running = true
	return nil
}
func (m *Manager) Upgrade(_ context.Context, from, to string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if from == "" || to == "" {
		return errors.New("upgrade versions are required")
	}
	if m.running {
		return ErrMustBeStopped
	}
	marker := filepath.Join(m.config.DataDir, "PG_VERSION")
	current, err := os.ReadFile(marker)
	if err != nil {
		return fmt.Errorf("read current version: %w", err)
	}
	if string(current) != from+"\n" {
		return fmt.Errorf("current version is %q, expected %q", string(current), from+"\n")
	}
	if err := os.WriteFile(marker, []byte(to+"\n"), 0o600); err != nil {
		return fmt.Errorf("write upgraded version: %w", err)
	}
	return nil
}
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.running {
		return ErrNotRunning
	}
	if err := m.platform.stop(ctx, m.config); err != nil {
		return fmt.Errorf("stop postgres: %w", err)
	}
	m.running = false
	return nil
}
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running {
		return ErrAlreadyRunning
	}
	if err := m.platform.start(ctx, m.config); err != nil {
		return fmt.Errorf("start postgres: %w", err)
	}
	m.running = true
	return nil
}
func (m *Manager) VerifyDataIntegrity(ctx context.Context) error {
	return m.platform.verify(ctx, m.config)
}
func (m *Manager) Running() bool { m.mu.Lock(); defer m.mu.Unlock(); return m.running }
func verifyMarker(config Config) error {
	_, err := os.Stat(filepath.Join(config.DataDir, "PG_VERSION"))
	if err != nil {
		return fmt.Errorf("database integrity check: %w", err)
	}
	return nil
}
