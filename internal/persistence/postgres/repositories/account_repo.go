package repositories

import (
	"context"
	"errors"

	"gorouter/internal/domain/provider"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// accountRepo implements provider.AccountRepository backed by pgx.
type accountRepo struct {
	tx pgx.Tx
}

func scanAccount(row pgx.Row) (*provider.Account, error) {
	a := &provider.Account{}
	var modelFilters []string
	err := row.Scan(&a.ID, &a.ProviderID, &a.Label, &a.AuthType, &a.CredentialRef,
		&a.Priority, &a.IsEnabled, &a.MaxConcurrent, &modelFilters,
		&a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	a.ModelFilters = modelFilters
	return a, nil
}

func (r *accountRepo) FindByID(ctx context.Context, id uuid.UUID) (*provider.Account, error) {
	row := r.tx.QueryRow(ctx,
		`SELECT id, provider_id, label, auth_type, credential_ref, priority, is_enabled,
		 max_concurrent, model_filters, created_at, updated_at
		 FROM gorouter_provider_accounts WHERE id = $1`, id)
	return scanAccount(row)
}

func (r *accountRepo) FindByProviderID(ctx context.Context, providerID uuid.UUID) ([]provider.Account, error) {
	rows, err := r.tx.Query(ctx,
		`SELECT id, provider_id, label, auth_type, credential_ref, priority, is_enabled,
		 max_concurrent, model_filters, created_at, updated_at
		 FROM gorouter_provider_accounts WHERE provider_id = $1 ORDER BY priority ASC, label`, providerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []provider.Account
	for rows.Next() {
		var a provider.Account
		var modelFilters []string
		if err := rows.Scan(&a.ID, &a.ProviderID, &a.Label, &a.AuthType, &a.CredentialRef,
			&a.Priority, &a.IsEnabled, &a.MaxConcurrent, &modelFilters,
			&a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		a.ModelFilters = modelFilters
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *accountRepo) Create(ctx context.Context, a *provider.Account) error {
	modelFilters := a.ModelFilters
	if modelFilters == nil {
		modelFilters = []string{}
	}
	_, err := r.tx.Exec(ctx,
		`INSERT INTO gorouter_provider_accounts (id, provider_id, label, auth_type, credential_ref,
		 priority, is_enabled, max_concurrent, model_filters, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		a.ID, a.ProviderID, a.Label, a.AuthType, a.CredentialRef,
		a.Priority, a.IsEnabled, a.MaxConcurrent, modelFilters,
		a.CreatedAt, a.UpdatedAt)
	return err
}

func (r *accountRepo) Update(ctx context.Context, a *provider.Account) error {
	modelFilters := a.ModelFilters
	if modelFilters == nil {
		modelFilters = []string{}
	}
	_, err := r.tx.Exec(ctx,
		`UPDATE gorouter_provider_accounts SET label=$1, auth_type=$2, credential_ref=$3,
		 priority=$4, is_enabled=$5, max_concurrent=$6, model_filters=$7, updated_at=$8
		 WHERE id=$9`,
		a.Label, a.AuthType, a.CredentialRef,
		a.Priority, a.IsEnabled, a.MaxConcurrent, modelFilters,
		a.UpdatedAt, a.ID)
	return err
}

func (r *accountRepo) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.tx.Exec(ctx, `DELETE FROM gorouter_provider_accounts WHERE id = $1`, id)
	return err
}
