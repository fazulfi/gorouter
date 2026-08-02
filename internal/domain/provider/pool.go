package provider

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ProxyPool represents a named group of proxy configurations.
type ProxyPool struct {
	ID          uuid.UUID
	Name        string
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// PoolMember links a proxy configuration to a pool with an ordinal position.
type PoolMember struct {
	PoolID        uuid.UUID
	ProxyConfigID uuid.UUID
	Position      int
}

// PoolRepository defines persistence operations for proxy pools.
type PoolRepository interface {
	List(ctx context.Context) ([]ProxyPool, error)
	Create(ctx context.Context, pool *ProxyPool) error
	Update(ctx context.Context, pool *ProxyPool) error
	Delete(ctx context.Context, id uuid.UUID) error
	Members(ctx context.Context, poolID uuid.UUID) ([]PoolMember, error)
	SetMembers(ctx context.Context, poolID uuid.UUID, members []PoolMember) error
}
