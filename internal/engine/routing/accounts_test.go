package routing

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"

	"gorouter/internal/app/cooldown"
	"gorouter/internal/domain/provider"
)

// testCooldown creates a real cooldown.Registry with threshold=1 for tests
// that verify cooldown exclusion in account selection.
func testCooldown() *cooldown.Registry {
	return cooldown.New(cooldown.Config{
		DefaultCooldown:  10 * time.Minute,
		MaxCooldown:      10 * time.Minute,
		FailureThreshold: 1,
		EscalationFactor: 1.0,
		CleanupInterval:  time.Hour,
	})
}

// fakeAccountRepo implements provider.AccountRepository for tests.
type fakeAccountRepo struct {
	accounts []provider.Account
}

func (f *fakeAccountRepo) FindByID(_ context.Context, id uuid.UUID) (*provider.Account, error) {
	for i := range f.accounts {
		if f.accounts[i].ID == id {
			return &f.accounts[i], nil
		}
	}
	return nil, nil
}

func (f *fakeAccountRepo) FindByProviderID(_ context.Context, providerID uuid.UUID) ([]provider.Account, error) {
	var result []provider.Account
	for _, a := range f.accounts {
		if a.ProviderID == providerID {
			result = append(result, a)
		}
	}
	return result, nil
}

func (f *fakeAccountRepo) Create(_ context.Context, _ *provider.Account) error { return nil }
func (f *fakeAccountRepo) Update(_ context.Context, _ *provider.Account) error { return nil }
func (f *fakeAccountRepo) Delete(_ context.Context, _ uuid.UUID) error         { return nil }

func newTestSelector(accounts []provider.Account, state *RoutingStateManager) *AccountSelector {
	return NewEngineAccountSelector(&fakeAccountRepo{accounts: accounts}, state)
}

func TestAccountSelector_SelectAccount(t *testing.T) {
	providerID := uuid.New()
	state := NewRoutingStateManagerWithClock(frozenNow)

	accounts := []provider.Account{
		{ID: uuid.New(), ProviderID: providerID, Priority: 2, IsEnabled: true, ModelFilters: []string{"gpt-4"}},
		{ID: uuid.New(), ProviderID: providerID, Priority: 0, IsEnabled: true},
		{ID: uuid.New(), ProviderID: providerID, Priority: 1, IsEnabled: true},
	}

	selector := newTestSelector(accounts, state)

	// Should return highest priority (lowest number).
	best, candidates, err := selector.SelectAccount(context.Background(), providerID, "gpt-4")
	if err != nil {
		t.Fatalf("SelectAccount: %v", err)
	}
	if best.ID != accounts[1].ID {
		t.Errorf("best account = %v, want %v (highest priority)", best.ID, accounts[1].ID)
	}
	if len(candidates) != 3 {
		t.Errorf("got %d candidates, want 3", len(candidates))
	}

	// Candidates should be sorted by priority.
	if !sort.SliceIsSorted(candidates, func(i, j int) bool {
		return candidates[i].Priority < candidates[j].Priority
	}) {
		t.Error("candidates not sorted by priority")
	}
}

func TestAccountSelector_ModelFilter(t *testing.T) {
	providerID := uuid.New()
	state := NewRoutingStateManagerWithClock(frozenNow)

	accounts := []provider.Account{
		{ID: uuid.New(), ProviderID: providerID, Priority: 0, IsEnabled: true, ModelFilters: []string{"gpt-4"}},
		{ID: uuid.New(), ProviderID: providerID, Priority: 1, IsEnabled: true, ModelFilters: []string{"claude-3"}},
		{ID: uuid.New(), ProviderID: providerID, Priority: 2, IsEnabled: true, ModelFilters: nil},
	}

	selector := newTestSelector(accounts, state)

	// Should only match accounts with matching filter or no filter.
	t.Run("gpt-4", func(t *testing.T) {
		best, candidates, err := selector.SelectAccount(context.Background(), providerID, "gpt-4")
		if err != nil {
			t.Fatalf("SelectAccount: %v", err)
		}
		if best.ID != accounts[0].ID {
			t.Errorf("best account = %v, want %v", best.ID, accounts[0].ID)
		}
		if len(candidates) != 2 {
			t.Errorf("got %d candidates, want 2", len(candidates))
		}
	})

	t.Run("claude-3", func(t *testing.T) {
		best, candidates, err := selector.SelectAccount(context.Background(), providerID, "claude-3")
		if err != nil {
			t.Fatalf("SelectAccount: %v", err)
		}
		if best.ID != accounts[1].ID {
			t.Errorf("best account = %v, want %v", best.ID, accounts[1].ID)
		}
		if len(candidates) != 2 {
			t.Errorf("got %d candidates, want 2", len(candidates))
		}
	})

	t.Run("unknown model matches unfiltered account", func(t *testing.T) {
		best, _, err := selector.SelectAccount(context.Background(), providerID, "unknown-model")
		if err != nil {
			t.Fatalf("SelectAccount: %v", err)
		}
		if best.ID != accounts[2].ID {
			t.Errorf("best = %v, want %v (unfiltered account)", best.ID, accounts[2].ID)
		}
	})
}

func TestAccountSelector_DisabledAccount(t *testing.T) {
	providerID := uuid.New()
	state := NewRoutingStateManagerWithClock(frozenNow)

	accounts := []provider.Account{
		{ID: uuid.New(), ProviderID: providerID, Priority: 0, IsEnabled: false},
		{ID: uuid.New(), ProviderID: providerID, Priority: 1, IsEnabled: true},
	}
	selector := newTestSelector(accounts, state)

	best, _, err := selector.SelectAccount(context.Background(), providerID, "gpt-4")
	if err != nil {
		t.Fatalf("SelectAccount: %v", err)
	}
	if best.ID != accounts[1].ID {
		t.Errorf("best account = %v, want %v (first enabled)", best.ID, accounts[1].ID)
	}
}

func TestAccountSelector_CooldownAccount(t *testing.T) {
	providerID := uuid.New()
	cd := testCooldown()
	state := NewRoutingStateManagerWithClockAndAuthority(frozenNow, cd)

	cooldownID := uuid.New()
	state.RecordFailure(cooldownID, FailureRateLimit, 429, "rate limited", nil)

	accounts := []provider.Account{
		{ID: cooldownID, ProviderID: providerID, Priority: 0, IsEnabled: true},
		{ID: uuid.New(), ProviderID: providerID, Priority: 1, IsEnabled: true},
	}
	selector := newTestSelector(accounts, state)

	best, _, err := selector.SelectAccount(context.Background(), providerID, "gpt-4")
	if err != nil {
		t.Fatalf("SelectAccount: %v", err)
	}
	if best.ID == cooldownID {
		t.Error("should not select cooldown account")
	}
}

func TestAccountSelector_NoAccounts(t *testing.T) {
	state := NewRoutingStateManagerWithClock(frozenNow)
	selector := newTestSelector(nil, state)

	_, _, err := selector.SelectAccount(context.Background(), uuid.New(), "gpt-4")
	if err == nil {
		t.Fatal("expected error for no accounts")
	}
}

func TestAccountSelector_NilRepo(t *testing.T) {
	selector := &AccountSelector{accountRepo: nil, state: nil}
	_, _, err := selector.SelectAccount(context.Background(), uuid.New(), "gpt-4")
	if err == nil {
		t.Fatal("expected error for nil repo")
	}
}

func TestMatchesModelFilter(t *testing.T) {
	tests := []struct {
		name    string
		filters []string
		model   string
		want    bool
	}{
		{"nil filter", nil, "gpt-4", true},
		{"empty filter", []string{}, "gpt-4", true},
		{"matching filter", []string{"gpt-4"}, "gpt-4", true},
		{"non-matching filter", []string{"claude-3"}, "gpt-4", false},
		{"multi filter match", []string{"claude-3", "gpt-4"}, "gpt-4", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchesModelFilter(tt.filters, tt.model); got != tt.want {
				t.Errorf("matchesModelFilter(%v, %q) = %v, want %v", tt.filters, tt.model, got, tt.want)
			}
		})
	}
}

func TestAccountSelector_StablePriorityTie(t *testing.T) {
	providerID := uuid.New()
	state := NewRoutingStateManagerWithClock(frozenNow)

	a1 := provider.Account{ID: uuid.New(), ProviderID: providerID, Priority: 0, IsEnabled: true}
	a2 := provider.Account{ID: uuid.New(), ProviderID: providerID, Priority: 0, IsEnabled: true}
	a3 := provider.Account{ID: uuid.New(), ProviderID: providerID, Priority: 0, IsEnabled: true}

	accounts := []provider.Account{a1, a2, a3}
	selector := newTestSelector(accounts, state)

	best, candidates, err := selector.SelectAccount(context.Background(), providerID, "gpt-4")
	if err != nil {
		t.Fatalf("SelectAccount: %v", err)
	}

	// Best should be first in insertion order.
	if best.ID != a1.ID {
		t.Errorf("best account = %v, want %v (first inserted)", best.ID, a1.ID)
	}

	// Order should be preserved.
	if len(candidates) != 3 {
		t.Fatalf("got %d candidates, want 3", len(candidates))
	}
	if candidates[0].ID != a1.ID {
		t.Errorf("candidates[0] = %v, want %v", candidates[0].ID, a1.ID)
	}
	if candidates[1].ID != a2.ID {
		t.Errorf("candidates[1] = %v, want %v", candidates[1].ID, a2.ID)
	}
	if candidates[2].ID != a3.ID {
		t.Errorf("candidates[2] = %v, want %v", candidates[2].ID, a3.ID)
	}
}
