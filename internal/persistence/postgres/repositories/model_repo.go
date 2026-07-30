package repositories

import (
	"context"
	"errors"

	"gorouter/internal/app/tx"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ModelRepository provides query helpers for the provider_models catalog table.
type ModelRepository struct {
	tx pgx.Tx
}

func NewModelRepository(pgTx pgx.Tx) *ModelRepository {
	return &ModelRepository{tx: pgTx}
}

func scanProviderModel(row pgx.Row) (*tx.ProviderModel, error) {
	m := &tx.ProviderModel{}
	err := row.Scan(&m.ID, &m.ProviderID, &m.ModelName, &m.Capabilities,
		&m.MaxTokens, &m.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return m, nil
}

func (r *ModelRepository) GetModelsByProvider(ctx context.Context, providerID uuid.UUID) ([]tx.ProviderModel, error) {
	rows, err := r.tx.Query(ctx,
		`SELECT id, provider_id, model_id, capabilities, max_tokens, created_at
		 FROM gorouter_provider_models WHERE provider_id = $1 ORDER BY model_id`, providerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []tx.ProviderModel
	for rows.Next() {
		var m tx.ProviderModel
		if err := rows.Scan(&m.ID, &m.ProviderID, &m.ModelName, &m.Capabilities,
			&m.MaxTokens, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *ModelRepository) GetModelByRef(ctx context.Context, providerID uuid.UUID, modelName string) (*tx.ProviderModel, error) {
	row := r.tx.QueryRow(ctx,
		`SELECT id, provider_id, model_id, capabilities, max_tokens, created_at
		 FROM gorouter_provider_models WHERE provider_id = $1 AND model_id = $2`, providerID, modelName)
	return scanProviderModel(row)
}

func (r *ModelRepository) ListByCapability(ctx context.Context, capability string) ([]tx.ProviderModel, error) {
	rows, err := r.tx.Query(ctx,
		`SELECT id, provider_id, model_id, capabilities, max_tokens, created_at
		 FROM gorouter_provider_models WHERE $1 = ANY(capabilities) ORDER BY provider_id, model_id`, capability)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []tx.ProviderModel
	for rows.Next() {
		var m tx.ProviderModel
		if err := rows.Scan(&m.ID, &m.ProviderID, &m.ModelName, &m.Capabilities,
			&m.MaxTokens, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
