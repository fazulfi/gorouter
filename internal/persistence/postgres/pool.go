package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PoolConfig defines the configuration for creating a PostgreSQL connection pool.
type PoolConfig struct {
	// DSN is the PostgreSQL connection string (required).
	DSN string

	// MaxConns is the maximum number of connections in the pool.
	// Default: 10 if zero.
	MaxConns int32

	// HealthCheckInterval is the frequency of health checks.
	// Default: 30s if zero.
	HealthCheckInterval time.Duration

	// ConnTimeout is the maximum time to wait for a connection.
	// Default: 10s if zero.
	ConnTimeout time.Duration
}

// Validate checks that the configuration is valid.
func (c PoolConfig) Validate() error {
	if c.DSN == "" {
		return fmt.Errorf("pool config: dsn is required")
	}
	if c.MaxConns < 0 {
		return fmt.Errorf("pool config: max connections cannot be negative: %d", c.MaxConns)
	}
	if c.HealthCheckInterval < 0 {
		return fmt.Errorf("pool config: health check interval cannot be negative: %v", c.HealthCheckInterval)
	}
	if c.ConnTimeout < 0 {
		return fmt.Errorf("pool config: connection timeout cannot be negative: %v", c.ConnTimeout)
	}
	return nil
}

// SetDefaults fills zero-valued fields with sensible defaults.
func (c *PoolConfig) SetDefaults() {
	if c.MaxConns == 0 {
		c.MaxConns = 10
	}
	if c.HealthCheckInterval == 0 {
		c.HealthCheckInterval = 30 * time.Second
	}
	if c.ConnTimeout == 0 {
		c.ConnTimeout = 10 * time.Second
	}
}

// Pool wraps a pgxpool.Pool with lifecycle management.
type Pool struct {
	pgxPool *pgxpool.Pool
	closed  bool
}

// Open creates a new PostgreSQL connection pool with the given configuration.
func Open(ctx context.Context, cfg PoolConfig) (*Pool, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	cfg.SetDefaults()

	poolCfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("parse pool config: %w", err)
	}

	poolCfg.MaxConns = cfg.MaxConns
	poolCfg.HealthCheckPeriod = cfg.HealthCheckInterval
	poolCfg.MaxConnLifetime = 0 // no max lifetime
	poolCfg.MaxConnIdleTime = cfg.HealthCheckInterval * 2

	// Apply connection timeout to the context
	connectCtx := ctx
	if cfg.ConnTimeout > 0 {
		var cancel context.CancelFunc
		connectCtx, cancel = context.WithTimeout(ctx, cfg.ConnTimeout)
		defer cancel()
	}

	pgxPool, err := pgxpool.NewWithConfig(connectCtx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	p := &Pool{
		pgxPool: pgxPool,
	}

	// Verify connectivity
	if err := p.Ping(connectCtx); err != nil {
		pgxPool.Close()
		return nil, fmt.Errorf("pool health check failed: %w", err)
	}

	return p, nil
}

// Ping verifies the pool can connect to the database.
func (p *Pool) Ping(ctx context.Context) error {
	if p == nil || p.pgxPool == nil {
		return fmt.Errorf("pool is not initialized")
	}
	conn, err := p.pgxPool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()
	return conn.Conn().Ping(ctx)
}

// Close gracefully shuts down the pool.
// Safe to call multiple times.
func (p *Pool) Close() {
	if p == nil || p.pgxPool == nil {
		return
	}
	if p.closed {
		return
	}
	p.closed = true
	p.pgxPool.Close()
}

// Pool returns the underlying pgxpool.Pool.
// Returns nil if the pool is not initialized.
func (p *Pool) Pool() *pgxpool.Pool {
	if p == nil {
		return nil
	}
	return p.pgxPool
}
