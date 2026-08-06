package repositories

import (
	"context"
	"encoding/json"
	"errors"

	"gorouter/internal/domain/pricing"

	"github.com/jackc/pgx/v5"
)

// pricingSettingsKey is the gorouter_settings KV key holding the current
// materialized pricing override set as a JSONB array. No new table or
// migration is introduced: overrides persist in the existing KV row, and the
// immutable history is the audit series.
const pricingSettingsKey = "pricing:overrides"

// pricingRepo implements pricing.PricingRepository against the existing
// gorouter_settings KV row.
type pricingRepo struct {
	tx pgx.Tx
}

// NewPricingRepo creates a pricing.PricingRepository backed by the given
// transaction.
func NewPricingRepo(tx pgx.Tx) pricing.PricingRepository {
	return &pricingRepo{tx: tx}
}

// LoadOverrides reads and decodes the current override set. An absent key
// yields an empty set rather than an error.
func (r *pricingRepo) LoadOverrides(ctx context.Context) ([]pricing.PriceOverride, error) {
	var raw []byte
	err := r.tx.QueryRow(ctx,
		`SELECT value FROM gorouter_settings WHERE key = $1`, pricingSettingsKey).Scan(&raw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return []pricing.PriceOverride{}, nil
		}
		return nil, err
	}
	out := []pricing.PriceOverride{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, err
		}
	}
	if out == nil {
		out = []pricing.PriceOverride{}
	}
	return out, nil
}

// SaveOverrides replaces the entire current set. The encoding is
// deterministic: the set is sorted by provider then model before marshaling,
// and the caller's slice is never mutated.
func (r *pricingRepo) SaveOverrides(ctx context.Context, overrides []pricing.PriceOverride) error {
	sorted := make([]pricing.PriceOverride, len(overrides))
	copy(sorted, overrides)
	pricing.SortOverrides(sorted)
	payload, err := json.Marshal(sorted)
	if err != nil {
		return err
	}
	_, err = r.tx.Exec(ctx,
		`INSERT INTO gorouter_settings (key, value, created_at, updated_at)
		 VALUES ($1, $2, NOW(), NOW())
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()`,
		pricingSettingsKey, payload)
	return err
}

// Compile-time interface check.
var _ pricing.PricingRepository = (*pricingRepo)(nil)
