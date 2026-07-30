package routing

import (
	"context"
	"fmt"
	"sort"

	"github.com/google/uuid"

	"gorouter/internal/domain/provider"
)

// CooldownRegistry is the subset of cooldown operations the account selector
// needs. Defined here to avoid coupling to a specific cooldown implementation.
type CooldownRegistry interface {
	IsOnCooldown(ctx context.Context, accountID uuid.UUID) bool
	RecordFailure(ctx context.Context, accountID uuid.UUID, err error)
	RecordSuccess(ctx context.Context, accountID uuid.UUID)
}

// AccountSelector implements provider.AccountSelector by selecting the
// highest-priority non-cooldown enabled account for a given provider.
type AccountSelector struct {
	accountRepo provider.AccountRepository
	cooldown    CooldownRegistry
}

// NewAccountSelector creates an AccountSelector backed by the given
// repository and cooldown registry.
func NewAccountSelector(accountRepo provider.AccountRepository, cooldown CooldownRegistry) *AccountSelector {
	return &AccountSelector{accountRepo: accountRepo, cooldown: cooldown}
}

// SelectAccount returns the best account for the given provider and model,
// along with all qualifying accounts sorted by priority. Accounts on cooldown
// or disabled are excluded. The first returned account is the best match.
func (s *AccountSelector) SelectAccount(ctx context.Context, providerID uuid.UUID, model string) (*provider.Account, []provider.Account, error) {
	if s.accountRepo == nil {
		return nil, nil, fmt.Errorf("account selector: account repository not configured")
	}

	all, err := s.accountRepo.FindByProviderID(ctx, providerID)
	if err != nil {
		return nil, nil, fmt.Errorf("account selector: find by provider: %w", err)
	}

	if len(all) == 0 {
		return nil, nil, fmt.Errorf("account selector: no accounts for provider %s", providerID)
	}

	// Filter: only enabled, not on cooldown, matching model filter.
	var candidates []provider.Account
	for _, a := range all {
		if !a.IsEnabled {
			continue
		}
		if s.cooldown != nil && s.cooldown.IsOnCooldown(ctx, a.ID) {
			continue
		}
		if !matchesModelFilter(a.ModelFilters, model) {
			continue
		}
		candidates = append(candidates, a)
	}

	if len(candidates) == 0 {
		return nil, nil, fmt.Errorf("account selector: no available accounts for provider %s", providerID)
	}

	// Sort by priority ascending (lower = higher priority).
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Priority < candidates[j].Priority
	})

	best := candidates[0]
	return &best, candidates, nil
}

// matchesModelFilter checks whether the given model is allowed by the filter
// list. An empty or nil filter list allows all models.
func matchesModelFilter(filters []string, model string) bool {
	if len(filters) == 0 {
		return true
	}
	for _, f := range filters {
		if f == model {
			return true
		}
	}
	return false
}
