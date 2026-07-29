package postgres

import (
	"context"
	"errors"
	"testing"
	"time"
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
