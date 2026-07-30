package provider

import (
	"context"

	"github.com/google/uuid"
)

// AccountSelector selects the best account for a given provider and model, and
// returns the remaining fallback accounts ordered by priority.
//
// The first returned Account is the best match. The []Account slice contains
// all qualifying accounts (including the best) sorted by priority ascending
// (lower priority value = higher priority).
type AccountSelector interface {
	SelectAccount(ctx context.Context, providerID uuid.UUID, model string) (*Account, []Account, error)
}
