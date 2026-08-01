package routing

import (
	"context"
	"sort"

	"github.com/google/uuid"

	"gorouter/internal/domain/provider"
)

// AccountFilterFunc is a predicate for filtering candidate accounts during selection.
type AccountFilterFunc func(a provider.Account) bool

// AccountSelector implements provider.AccountSelector with authority-correct
// priority selection, model filtering, cooldown awareness, and concurrency gating.
// It provides deterministic tie-breaking (original insertion order for equal priority)
// and supports account state management via RoutingStateManager.
type AccountSelector struct {
	accountRepo provider.AccountRepository
	state       *RoutingStateManager
}

// NewEngineAccountSelector creates a new AccountSelector.
func NewEngineAccountSelector(accountRepo provider.AccountRepository, state *RoutingStateManager) *AccountSelector {
	return &AccountSelector{
		accountRepo: accountRepo,
		state:       state,
	}
}

// SelectAccount returns the best account for the given provider and model,
// along with all qualifying accounts sorted by priority (stable order).
// Accounts on cooldown, disabled, or exceeding concurrency limits are excluded.
func (s *AccountSelector) SelectAccount(ctx context.Context, providerID uuid.UUID, model string) (*provider.Account, []provider.Account, error) {
	if s.accountRepo == nil {
		return nil, nil, errAccountRepoNotConfigured
	}

	all, err := s.accountRepo.FindByProviderID(ctx, providerID)
	if err != nil {
		return nil, nil, errAccountFind(err)
	}

	if len(all) == 0 {
		return nil, nil, errNoAccountsForProvider(providerID)
	}

	candidates := s.filterCandidates(ctx, all, model)
	if len(candidates) == 0 {
		return nil, nil, errNoAvailableAccounts(providerID)
	}

	// Stable sort by priority ascending (lower = higher).
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].Priority < candidates[j].Priority
	})

	best := candidates[0]
	return &best, candidates, nil
}

// filterCandidates applies eligibility filters to the account list.
func (s *AccountSelector) filterCandidates(ctx context.Context, accounts []provider.Account, model string) []provider.Account {
	var candidates []provider.Account
	for _, a := range accounts {
		if !a.IsEnabled {
			continue
		}
		if s.state != nil && !s.state.IsEligible(a.ID) {
			continue
		}
		if !matchesModelFilter(a.ModelFilters, model) {
			continue
		}
		candidates = append(candidates, a)
	}
	return candidates
}

// matchesModelFilter checks whether the given model is allowed by the filter list.
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
