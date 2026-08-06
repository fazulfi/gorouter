package pricing

import "context"

// PricingRepository persists the current materialized override set. The set
// is stored as a JSONB array in the existing gorouter_settings KV row
// "pricing:overrides" (no new table or migration). History is not stored
// here: it is the append-only audit series, so this contract has no update
// or delete path.
type PricingRepository interface {
	// LoadOverrides returns the current override set, or an empty set when
	// no overrides have been applied yet.
	LoadOverrides(ctx context.Context) ([]PriceOverride, error)
	// SaveOverrides replaces the entire current set. Callers pass the
	// complete set; the repository encodes it deterministically.
	SaveOverrides(ctx context.Context, overrides []PriceOverride) error
}
