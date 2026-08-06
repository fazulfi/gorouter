package repositories

import (
	"context"

	"gorouter/internal/domain/provider"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Compile-time interface assertion.
var _ provider.PoolRepository = (*proxyPoolRepo)(nil)

type proxyPoolRepo struct {
	tx pgx.Tx
}

func (r *proxyPoolRepo) List(ctx context.Context) ([]provider.ProxyPool, error) {
	rows, err := r.tx.Query(ctx,
		`SELECT id, name, description, created_at, updated_at
		 FROM gorouter_proxy_pools ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []provider.ProxyPool
	for rows.Next() {
		var p provider.ProxyPool
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *proxyPoolRepo) Create(ctx context.Context, p *provider.ProxyPool) error {
	_, err := r.tx.Exec(ctx,
		`INSERT INTO gorouter_proxy_pools (id, name, description, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		p.ID, p.Name, p.Description, p.CreatedAt, p.UpdatedAt)
	return err
}

func (r *proxyPoolRepo) Update(ctx context.Context, p *provider.ProxyPool) error {
	_, err := r.tx.Exec(ctx,
		`UPDATE gorouter_proxy_pools SET name=$1, description=$2, updated_at=$3 WHERE id=$4`,
		p.Name, p.Description, p.UpdatedAt, p.ID)
	return err
}

func (r *proxyPoolRepo) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.tx.Exec(ctx, `DELETE FROM gorouter_proxy_pools WHERE id = $1`, id)
	return err
}

func (r *proxyPoolRepo) Members(ctx context.Context, poolID uuid.UUID) ([]provider.PoolMember, error) {
	rows, err := r.tx.Query(ctx,
		`SELECT pool_id, proxy_config_id, position
		 FROM gorouter_proxy_pool_members WHERE pool_id = $1 ORDER BY position, proxy_config_id`, poolID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []provider.PoolMember
	for rows.Next() {
		var m provider.PoolMember
		if err := rows.Scan(&m.PoolID, &m.ProxyConfigID, &m.Position); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *proxyPoolRepo) SetMembers(ctx context.Context, poolID uuid.UUID, members []provider.PoolMember) error {
	// Empty input clears all members.
	if len(members) == 0 {
		_, err := r.tx.Exec(ctx,
			`DELETE FROM gorouter_proxy_pool_members WHERE pool_id = $1`, poolID)
		return err
	}
	proxyIDs := make([]uuid.UUID, len(members))
	positions := make([]int, len(members))
	for i, m := range members {
		proxyIDs[i] = m.ProxyConfigID
		positions[i] = m.Position
	}
	// Single-statement atomic replacement: delete old members and insert
	// replacements in one CTE. If the INSERT fails (e.g. FK violation),
	// the DELETE is rolled back as part of the same atomic statement.
	_, err := r.tx.Exec(ctx,
		`WITH deleted AS (
			DELETE FROM gorouter_proxy_pool_members WHERE pool_id = $1
		)
		INSERT INTO gorouter_proxy_pool_members (pool_id, proxy_config_id, position)
		SELECT $1, proxy_config_id, position
		FROM unnest($2::uuid[], $3::int[]) AS t(proxy_config_id, position)`,
		poolID, proxyIDs, positions)
	return err
}
