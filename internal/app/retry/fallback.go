package retry

import (
	"context"
	"sort"

	"github.com/google/uuid"

	"gorouter/internal/domain/provider"
)

// CooldownRegistry tracks account cooldown state across retry attempts.
// The implementation lives outside this package.
type CooldownRegistry interface {
	IsOnCooldown(ctx context.Context, accountID uuid.UUID) bool
	RecordFailure(ctx context.Context, accountID uuid.UUID, err error)
	RecordSuccess(ctx context.Context, accountID uuid.UUID)
}

// FallbackSelector picks the next available account for same-model retries
// when the current account fails.
type FallbackSelector struct {
	selector provider.AccountSelector
	cooldown CooldownRegistry
}

// NewFallbackSelector creates a FallbackSelector.
func NewFallbackSelector(selector provider.AccountSelector, cooldown CooldownRegistry) *FallbackSelector {
	return &FallbackSelector{
		selector: selector,
		cooldown: cooldown,
	}
}

// NextAccount returns the best fallback account from the candidate list.
// It skips the current (failed) account and any account on cooldown.
// Accounts are returned in priority order (lowest Priority value first).
// Returns nil when no fallback is available.
func (s *FallbackSelector) NextAccount(ctx context.Context, accounts []provider.Account, currentID uuid.UUID) *provider.Account {
	if len(accounts) == 0 {
		return nil
	}

	var candidates []provider.Account
	for _, a := range accounts {
		if a.ID == currentID {
			continue
		}
		if !a.IsEnabled {
			continue
		}
		if s.cooldown != nil && s.cooldown.IsOnCooldown(ctx, a.ID) {
			continue
		}
		candidates = append(candidates, a)
	}

	if len(candidates) == 0 {
		return nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Priority < candidates[j].Priority
	})

	return &candidates[0]
}
