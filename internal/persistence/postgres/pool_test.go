package postgres

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPoolConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     PoolConfig
		wantErr string
	}{
		{
			name: "valid config",
			cfg: PoolConfig{
				DSN:                 "postgres://user:pass@localhost:5432/test",
				MaxConns:            10,
				HealthCheckInterval: 30 * time.Second,
				ConnTimeout:         10 * time.Second,
			},
			wantErr: "",
		},
		{
			name: "empty DSN",
			cfg: PoolConfig{
				DSN:                 "",
				MaxConns:            10,
				HealthCheckInterval: 30 * time.Second,
				ConnTimeout:         10 * time.Second,
			},
			wantErr: "dsn",
		},
		{
			name: "zero MaxConns uses default",
			cfg: PoolConfig{
				DSN:                 "postgres://user:pass@localhost:5432/test",
				MaxConns:            0,
				HealthCheckInterval: 30 * time.Second,
				ConnTimeout:         10 * time.Second,
			},
			wantErr: "",
		},
		{
			name: "negative MaxConns",
			cfg: PoolConfig{
				DSN:                 "postgres://user:pass@localhost:5432/test",
				MaxConns:            -5,
				HealthCheckInterval: 30 * time.Second,
				ConnTimeout:         10 * time.Second,
			},
			wantErr: "max",
		},
		{
			name: "zero health check interval uses default",
			cfg: PoolConfig{
				DSN:                 "postgres://user:pass@localhost:5432/test",
				MaxConns:            10,
				HealthCheckInterval: 0,
				ConnTimeout:         10 * time.Second,
			},
			wantErr: "",
		},
		{
			name: "negative health check interval",
			cfg: PoolConfig{
				DSN:                 "postgres://user:pass@localhost:5432/test",
				MaxConns:            10,
				HealthCheckInterval: -1 * time.Second,
				ConnTimeout:         10 * time.Second,
			},
			wantErr: "health",
		},
		{
			name: "zero conn timeout uses default",
			cfg: PoolConfig{
				DSN:                 "postgres://user:pass@localhost:5432/test",
				MaxConns:            10,
				HealthCheckInterval: 30 * time.Second,
				ConnTimeout:         0,
			},
			wantErr: "",
		},
		{
			name: "negative conn timeout",
			cfg: PoolConfig{
				DSN:                 "postgres://user:pass@localhost:5432/test",
				MaxConns:            10,
				HealthCheckInterval: 30 * time.Second,
				ConnTimeout:         -5 * time.Second,
			},
			wantErr: "timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("Validate() unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("Validate() expected error containing %q, got nil", tt.wantErr)
				} else if !contains(err.Error(), tt.wantErr) {
					t.Errorf("Validate() error = %q, want error containing %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

func TestPoolConfig_Defaults(t *testing.T) {
	cfg := PoolConfig{
		DSN: "postgres://user:pass@localhost:5432/test",
	}
	cfg.SetDefaults()

	if cfg.MaxConns != 10 {
		t.Errorf("MaxConns = %d, want 10", cfg.MaxConns)
	}
	if cfg.HealthCheckInterval != 30*time.Second {
		t.Errorf("HealthCheckInterval = %v, want 30s", cfg.HealthCheckInterval)
	}
	if cfg.ConnTimeout != 10*time.Second {
		t.Errorf("ConnTimeout = %v, want 10s", cfg.ConnTimeout)
	}
}

func TestPoolConfig_DefaultsOnlyZero(t *testing.T) {
	cfg := PoolConfig{
		DSN:                 "postgres://user:pass@localhost:5432/test",
		MaxConns:            5,
		HealthCheckInterval: 15 * time.Second,
		ConnTimeout:         5 * time.Second,
	}
	cfg.SetDefaults()

	if cfg.MaxConns != 5 {
		t.Errorf("MaxConns = %d, want 5 (should not override non-zero)", cfg.MaxConns)
	}
	if cfg.HealthCheckInterval != 15*time.Second {
		t.Errorf("HealthCheckInterval = %v, want 15s (should not override non-zero)", cfg.HealthCheckInterval)
	}
	if cfg.ConnTimeout != 5*time.Second {
		t.Errorf("ConnTimeout = %v, want 5s (should not override non-zero)", cfg.ConnTimeout)
	}
}

func TestOpen_CanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Immediately cancel

	_, err := Open(ctx, PoolConfig{
		DSN:                 "postgres://user:pass@localhost:5432/test",
		MaxConns:            10,
		HealthCheckInterval: 30 * time.Second,
		ConnTimeout:         10 * time.Second,
	})
	if err == nil {
		t.Error("Open() expected error with canceled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Open() error = %v, want context.Canceled", err)
	}
}

func TestOpen_InvalidDSN(t *testing.T) {
	ctx := context.Background()
	_, err := Open(ctx, PoolConfig{
		DSN:                 "not-a-valid-dsn",
		MaxConns:            10,
		HealthCheckInterval: 30 * time.Second,
		ConnTimeout:         1 * time.Second,
	})
	if err == nil {
		t.Error("Open() expected error with invalid DSN, got nil")
	}
}

func TestPoolClose_Idempotent(t *testing.T) {
	// Validate that Pool.Close() is safe to call multiple times
	// by testing on a nil pool (should not panic)
	var p *Pool = nil
	p.Close() // Should not panic

	// Also test on a zero-value pool
	p2 := &Pool{}
	p2.Close() // Should not panic
}

func TestPool_Close_AlreadyClosed(t *testing.T) {
	p := &Pool{closed: true}
	p.Close()
}

func TestPool_Ping_NilPool(t *testing.T) {
	var p *Pool = nil
	err := p.Ping(context.Background())
	if err == nil {
		t.Error("Ping() on nil pool expected error, got nil")
	}
}

func TestPool_Ping_EmptyPool(t *testing.T) {
	p := &Pool{}
	err := p.Ping(context.Background())
	if err == nil {
		t.Error("Ping() on empty pool expected error, got nil")
	}
}

func TestPool_Pool_Nil(t *testing.T) {
	var p *Pool = nil
	if got := p.Pool(); got != nil {
		t.Error("Pool() on nil pool should return nil")
	}
}

func TestPool_Pool_Empty(t *testing.T) {
	p := &Pool{}
	if got := p.Pool(); got != nil {
		t.Error("Pool() on empty pool should return nil")
	}
}

func TestOpen_InvalidConnString(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name string
		dsn  string
	}{
		{"plain string", "not-a-valid-dsn"},
		{"garbage", "://:"},
		{"missing protocol", "user:pass@localhost:5432/db"},
		{"invalid port", "postgres://localhost:abc/db"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Open(ctx, PoolConfig{
				DSN:                 tt.dsn,
				MaxConns:            10,
				HealthCheckInterval: 30 * time.Second,
				ConnTimeout:         1 * time.Second,
			})
			if err == nil {
				t.Errorf("Open() with DSN %q expected error, got nil", tt.dsn)
			}
		})
	}
}

func TestOpen_EmptyDSN(t *testing.T) {
	ctx := context.Background()
	_, err := Open(ctx, PoolConfig{
		DSN:                 "",
		MaxConns:            10,
		HealthCheckInterval: 30 * time.Second,
		ConnTimeout:         1 * time.Second,
	})
	if err == nil {
		t.Fatal("Open() with empty DSN expected error, got nil")
	}
	if !contains(err.Error(), "dsn is required") {
		t.Errorf("Open() error = %q, want dsn is required", err.Error())
	}
}

func TestPoolConfig_Validate_ZeroConfig(t *testing.T) {
	cfg := PoolConfig{}
	err := cfg.Validate()
	if err == nil {
		t.Error("Validate() on zero config expected error (empty DSN), got nil")
	}
	if !contains(err.Error(), "dsn") {
		t.Errorf("Validate() error = %q, want dsn error", err.Error())
	}
}

func TestPoolConfig_Validate_AllNegative(t *testing.T) {
	cfg := PoolConfig{
		DSN:                 "postgres://user:pass@localhost:5432/test",
		MaxConns:            -1,
		HealthCheckInterval: -1 * time.Second,
		ConnTimeout:         -1 * time.Second,
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() expected error with negative values")
	}
	if !contains(err.Error(), "max connections cannot be negative") {
		t.Errorf("Validate() expected MaxConns error, got: %v", err)
	}
}

func TestPool_Close_WithPgxPool(t *testing.T) {
	pool, err := brokenPool()
	if err != nil {
		t.Skipf("brokenPool: %v", err)
	}
	p := &Pool{pgxPool: pool}
	p.Close()
	if !p.closed {
		t.Error("expected closed = true after Close()")
	}
	p.Close()
}

func TestPool_Ping_BrokenPool(t *testing.T) {
	pool, err := brokenPool()
	if err != nil {
		t.Skipf("brokenPool: %v", err)
	}
	p := &Pool{pgxPool: pool}
	err = p.Ping(context.Background())
	if err == nil {
		t.Error("Ping() with broken pool expected error, got nil")
	}
}

func TestOpen_PingHealthCheckFails(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := Open(ctx, PoolConfig{
		DSN:                 "postgres://localhost:1/postgres",
		MaxConns:            10,
		HealthCheckInterval: 30 * time.Second,
		ConnTimeout:         1 * time.Second,
	})
	if err == nil {
		t.Error("Open() expected error with unreachable host, got nil")
	}
}

func TestPool_Pool_WithPgxPool(t *testing.T) {
	pool, err := brokenPool()
	if err != nil {
		t.Skipf("brokenPool: %v", err)
	}
	p := &Pool{pgxPool: pool}
	if got := p.Pool(); got == nil {
		t.Error("Pool() with pgxPool should return non-nil")
	}
}

func brokenPool() (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig("postgres://localhost:5432/postgres?connect_timeout=1")
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 1
	cfg.MinConns = 0
	cfg.HealthCheckPeriod = time.Hour
	cfg.MaxConnLifetime = time.Hour
	cfg.MaxConnIdleTime = time.Hour
	if cfg.ConnConfig != nil && cfg.ConnConfig.DialFunc == nil {
		cfg.ConnConfig.DialFunc = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return nil, errors.New("dial error")
		}
	}
	return pgxpool.NewWithConfig(context.Background(), cfg)
}

// contains is a helper for substring matching.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsStr(s, substr)
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
