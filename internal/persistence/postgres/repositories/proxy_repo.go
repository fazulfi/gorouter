package repositories

import (
	"context"
	"errors"

	"gorouter/internal/domain/provider"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// proxyRepo implements provider.ProxyRepository backed by pgx.
type proxyRepo struct {
	tx pgx.Tx
}

func scanProxyConfig(row pgx.Row) (*provider.ProxyConfig, error) {
	p := &provider.ProxyConfig{}
	err := row.Scan(&p.ID, &p.AccountID, &p.URL, &p.Username, &p.Password,
		&p.IsEnabled, &p.Priority, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return p, nil
}

func (r *proxyRepo) FindByAccountID(ctx context.Context, accountID uuid.UUID) ([]provider.ProxyConfig, error) {
	rows, err := r.tx.Query(ctx,
		`SELECT id, account_id, url, username, password, is_enabled, priority, created_at, updated_at
		 FROM gorouter_proxy_configs WHERE account_id = $1 ORDER BY priority ASC`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []provider.ProxyConfig
	for rows.Next() {
		var p provider.ProxyConfig
		if err := rows.Scan(&p.ID, &p.AccountID, &p.URL, &p.Username, &p.Password,
			&p.IsEnabled, &p.Priority, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *proxyRepo) Create(ctx context.Context, p *provider.ProxyConfig) error {
	_, err := r.tx.Exec(ctx,
		`INSERT INTO gorouter_proxy_configs (id, account_id, url, username, password, is_enabled, priority, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		p.ID, p.AccountID, p.URL, p.Username, p.Password,
		p.IsEnabled, p.Priority, p.CreatedAt, p.UpdatedAt)
	return err
}

func (r *proxyRepo) Update(ctx context.Context, p *provider.ProxyConfig) error {
	_, err := r.tx.Exec(ctx,
		`UPDATE gorouter_proxy_configs SET url=$1, username=$2, password=$3, is_enabled=$4,
		 priority=$5, updated_at=$6 WHERE id=$7`,
		p.URL, p.Username, p.Password, p.IsEnabled, p.Priority, p.UpdatedAt, p.ID)
	return err
}

func (r *proxyRepo) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.tx.Exec(ctx, `DELETE FROM gorouter_proxy_configs WHERE id = $1`, id)
	return err
}
