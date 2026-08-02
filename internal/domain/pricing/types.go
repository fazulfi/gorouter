// Package pricing defines the pure domain contract for model pricing
// overrides. The current override set is materialized in the existing
// gorouter_settings KV row "pricing:overrides"; the authoritative change
// history is the append-only audit series (decisions #148/#149, audit/02):
// stored request costs are never recomputed when pricing changes, and every
// override application is recorded with a sanitized before/after diff.
package pricing

import (
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// PriceOverride replaces the built-in price of one model within one provider
// for future requests. The identity of an override is its (provider, model)
// key; applying an override for an existing key replaces the previous value
// in the materialized set while the audit series keeps the full history.
type PriceOverride struct {
	ModelID     string    `json:"model_id"`
	ProviderID  string    `json:"provider_id"`
	InputPrice  float64   `json:"input_price"`
	OutputPrice float64   `json:"output_price"`
	UpdatedBy   uuid.UUID `json:"updated_by"`
	UpdatedAt   time.Time `json:"updated_at"`
}

var (
	// ErrModelIDRequired reports an override without a model identifier.
	ErrModelIDRequired = errors.New("pricing: model_id is required")
	// ErrProviderIDRequired reports an override without a provider identifier.
	ErrProviderIDRequired = errors.New("pricing: provider_id is required")
	// ErrNegativePrice reports an override with a negative price. Zero prices
	// are allowed: a configured zero is a verified free tier, distinct from
	// the unconfigured-zero fallback of decision #148.
	ErrNegativePrice = errors.New("pricing: prices must not be negative")
)

// Validate checks the override invariants. UpdatedBy and UpdatedAt are
// stamped by the application service, so they are not validated here.
func (o PriceOverride) Validate() error {
	if strings.TrimSpace(o.ModelID) == "" {
		return ErrModelIDRequired
	}
	if strings.TrimSpace(o.ProviderID) == "" {
		return ErrProviderIDRequired
	}
	if o.InputPrice < 0 || o.OutputPrice < 0 {
		return ErrNegativePrice
	}
	return nil
}

// Key returns the stable identity of the override within the override set.
// The set holds at most one override per key; applying an override with an
// existing key replaces the previous entry.
func (o PriceOverride) Key() string {
	return o.ProviderID + "/" + o.ModelID
}

// SortOverrides orders the set deterministically by provider then model so
// the JSONB encoding of the materialized set is stable and independent of
// insertion order.
func SortOverrides(overrides []PriceOverride) {
	sort.SliceStable(overrides, func(i, j int) bool {
		if overrides[i].ProviderID != overrides[j].ProviderID {
			return overrides[i].ProviderID < overrides[j].ProviderID
		}
		return overrides[i].ModelID < overrides[j].ModelID
	})
}
