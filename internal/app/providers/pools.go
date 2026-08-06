package providers

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorouter/internal/domain/auth"
	"gorouter/internal/domain/provider"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// PoolService manages proxy pools. Every mutation runs in a transaction whose
// audit entry is written atomically with the change and attributed to the
// acting user (decision #59).
type PoolService struct {
	beginner ScopeBeginner
	logger   zerolog.Logger
}

// NewPoolService creates a PoolService over the given scope beginner.
func NewPoolService(beginner ScopeBeginner, logger zerolog.Logger) *PoolService {
	return &PoolService{beginner: beginner, logger: logger}
}

// List returns all proxy pools in persistence order.
func (s *PoolService) List(ctx context.Context) ([]provider.ProxyPool, error) {
	scope, err := s.beginner.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = scope.Rollback(ctx) }()
	pools, err := scope.Pools().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list pools: %w", err)
	}
	return pools, nil
}

// Create persists a new proxy pool, assigns identity and timestamps, and
// audits the mutation with the acting user. The caller's pool value is not
// mutated; the persisted copy is returned.
func (s *PoolService) Create(ctx context.Context, actor *auth.Actor, pool *provider.ProxyPool) (*provider.ProxyPool, error) {
	if err := requireActor(actor); err != nil {
		return nil, err
	}
	if pool == nil {
		return nil, errors.New("providers: pool is nil")
	}
	p := *pool
	now := time.Now().UTC()
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	p.CreatedAt, p.UpdatedAt = now, now

	scope, err := s.beginner.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = scope.Rollback(ctx) }()
	if err := scope.Pools().Create(ctx, &p); err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	if err := writeAudit(ctx, scope.AuditLog(), actor, "pool.create", "proxy_pool", &p.ID, poolDetails(&p)); err != nil {
		return nil, fmt.Errorf("audit pool create: %w", err)
	}
	if err := scope.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return &p, nil
}

// Update replaces a pool's name and description and audits a sanitized
// before/after diff with the acting user.
func (s *PoolService) Update(ctx context.Context, actor *auth.Actor, pool *provider.ProxyPool) (*provider.ProxyPool, error) {
	if err := requireActor(actor); err != nil {
		return nil, err
	}
	if pool == nil {
		return nil, errors.New("providers: pool is nil")
	}
	scope, err := s.beginner.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = scope.Rollback(ctx) }()

	before, err := findPool(ctx, scope.Pools(), pool.ID)
	if err != nil {
		return nil, err
	}
	p := *pool
	p.UpdatedAt = time.Now().UTC()
	if err := scope.Pools().Update(ctx, &p); err != nil {
		return nil, fmt.Errorf("update pool: %w", err)
	}
	details := map[string]any{
		"before": poolDetails(before),
		"after":  poolDetails(&p),
	}
	if err := writeAudit(ctx, scope.AuditLog(), actor, "pool.update", "proxy_pool", &p.ID, details); err != nil {
		return nil, fmt.Errorf("audit pool update: %w", err)
	}
	if err := scope.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return &p, nil
}

// Delete removes a proxy pool and audits the mutation with the acting user.
func (s *PoolService) Delete(ctx context.Context, actor *auth.Actor, id uuid.UUID) error {
	if err := requireActor(actor); err != nil {
		return err
	}
	scope, err := s.beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = scope.Rollback(ctx) }()
	if err := scope.Pools().Delete(ctx, id); err != nil {
		return fmt.Errorf("delete pool: %w", err)
	}
	if err := writeAudit(ctx, scope.AuditLog(), actor, "pool.delete", "proxy_pool", &id, nil); err != nil {
		return fmt.Errorf("audit pool delete: %w", err)
	}
	return scope.Commit(ctx)
}

// GetMembers returns a pool's members in ordinal position order.
func (s *PoolService) GetMembers(ctx context.Context, poolID uuid.UUID) ([]provider.PoolMember, error) {
	scope, err := s.beginner.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = scope.Rollback(ctx) }()
	members, err := scope.Pools().Members(ctx, poolID)
	if err != nil {
		return nil, fmt.Errorf("get pool members: %w", err)
	}
	return members, nil
}

// SetMembers atomically replaces a pool's member list and audits the change
// with the acting user.
func (s *PoolService) SetMembers(ctx context.Context, actor *auth.Actor, poolID uuid.UUID, members []provider.PoolMember) error {
	if err := requireActor(actor); err != nil {
		return err
	}
	scope, err := s.beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = scope.Rollback(ctx) }()
	if err := scope.Pools().SetMembers(ctx, poolID, members); err != nil {
		return fmt.Errorf("set pool members: %w", err)
	}
	if err := writeAudit(ctx, scope.AuditLog(), actor, "pool.set_members", "proxy_pool", &poolID, map[string]any{"count": len(members)}); err != nil {
		return fmt.Errorf("audit pool set members: %w", err)
	}
	return scope.Commit(ctx)
}

func poolDetails(p *provider.ProxyPool) map[string]any {
	return map[string]any{
		"name":        p.Name,
		"description": p.Description,
	}
}

func findPool(ctx context.Context, repo provider.PoolRepository, id uuid.UUID) (*provider.ProxyPool, error) {
	pools, err := repo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list pools: %w", err)
	}
	for i := range pools {
		if pools[i].ID == id {
			return &pools[i], nil
		}
	}
	return nil, fmt.Errorf("providers: pool %s not found", id)
}
