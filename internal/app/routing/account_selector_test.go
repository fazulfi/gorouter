package routing

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"gorouter/internal/domain/provider"
)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

type fakeAccountRepo struct {
	findByProviderIDFn func(ctx context.Context, providerID uuid.UUID) ([]provider.Account, error)
}

func (f *fakeAccountRepo) FindByID(ctx context.Context, id uuid.UUID) (*provider.Account, error) {
	return nil, errors.New("unexpected")
}
func (f *fakeAccountRepo) FindByProviderID(ctx context.Context, providerID uuid.UUID) ([]provider.Account, error) {
	return f.findByProviderIDFn(ctx, providerID)
}
func (f *fakeAccountRepo) Create(ctx context.Context, account *provider.Account) error {
	return errors.New("unexpected")
}
func (f *fakeAccountRepo) Update(ctx context.Context, account *provider.Account) error {
	return errors.New("unexpected")
}
func (f *fakeAccountRepo) Delete(ctx context.Context, id uuid.UUID) error {
	return errors.New("unexpected")
}

type fakeCooldownRegistry struct {
	onCooldownFn func(ctx context.Context, accountID uuid.UUID) bool
}

func (f *fakeCooldownRegistry) IsOnCooldown(ctx context.Context, accountID uuid.UUID) bool {
	if f.onCooldownFn != nil {
		return f.onCooldownFn(ctx, accountID)
	}
	return false
}
func (f *fakeCooldownRegistry) RecordFailure(ctx context.Context, accountID uuid.UUID, err error) {}
func (f *fakeCooldownRegistry) RecordSuccess(ctx context.Context, accountID uuid.UUID)            {}

func testAccount(idOffset byte, priority int, isEnabled bool, filters []string) provider.Account {
	return provider.Account{
		ID:           uuid.MustParse("00000000-0000-0000-0000-0000000000" + string([]byte{idOffset/16 + 48, idOffset%16 + 48})),
		ProviderID:   uuid.MustParse("f0000000-0000-0000-0000-000000000000"),
		Label:        "test-account",
		AuthType:     "api_key",
		Priority:     priority,
		IsEnabled:    isEnabled,
		ModelFilters: filters,
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestAccountSelector_SelectAccount_ReturnsHighestPriority(t *testing.T) {
	repo := &fakeAccountRepo{
		findByProviderIDFn: func(ctx context.Context, providerID uuid.UUID) ([]provider.Account, error) {
			return []provider.Account{
				testAccount(1, 10, true, nil),
				testAccount(2, 5, true, nil),
				testAccount(3, 1, true, nil),
			}, nil
		},
	}

	sel := NewAccountSelector(repo, nil)
	best, all, err := sel.SelectAccount(context.Background(), uuid.MustParse("f0000000-0000-0000-0000-000000000000"), "gpt-4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if best.Priority != 1 {
		t.Errorf("expected best priority 1, got %d", best.Priority)
	}
	if len(all) != 3 {
		t.Errorf("expected 3 candidates, got %d", len(all))
	}
	// Verify ordering: lowest priority first.
	for i := 1; i < len(all); i++ {
		if all[i].Priority < all[i-1].Priority {
			t.Errorf("candidates not sorted by priority: all[%d].Priority=%d < all[%d].Priority=%d",
				i, all[i].Priority, i-1, all[i-1].Priority)
		}
	}
}

func TestAccountSelector_SelectAccount_FiltersDisabled(t *testing.T) {
	repo := &fakeAccountRepo{
		findByProviderIDFn: func(ctx context.Context, providerID uuid.UUID) ([]provider.Account, error) {
			return []provider.Account{
				testAccount(1, 0, false, nil),
				testAccount(2, 1, true, nil),
			}, nil
		},
	}

	sel := NewAccountSelector(repo, nil)
	best, all, err := sel.SelectAccount(context.Background(), uuid.MustParse("f0000000-0000-0000-0000-000000000000"), "gpt-4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if best.Priority != 1 {
		t.Errorf("expected best priority 1, got %d", best.Priority)
	}
	if len(all) != 1 {
		t.Errorf("expected 1 candidate, got %d", len(all))
	}
}

func TestAccountSelector_SelectAccount_FiltersCooldown(t *testing.T) {
	repo := &fakeAccountRepo{
		findByProviderIDFn: func(ctx context.Context, providerID uuid.UUID) ([]provider.Account, error) {
			return []provider.Account{
				testAccount(1, 0, true, nil),
				testAccount(2, 1, true, nil),
			}, nil
		},
	}

	acc1ID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	cooldown := &fakeCooldownRegistry{
		onCooldownFn: func(ctx context.Context, accountID uuid.UUID) bool {
			return accountID == acc1ID
		},
	}

	sel := NewAccountSelector(repo, cooldown)
	best, all, err := sel.SelectAccount(context.Background(), uuid.MustParse("f0000000-0000-0000-0000-000000000000"), "gpt-4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if best.Priority != 1 {
		t.Errorf("expected best priority 1 (the non-cooldown account), got %d", best.Priority)
	}
	if len(all) != 1 {
		t.Errorf("expected 1 candidate, got %d", len(all))
	}
}

func TestAccountSelector_SelectAccount_FiltersByModel(t *testing.T) {
	repo := &fakeAccountRepo{
		findByProviderIDFn: func(ctx context.Context, providerID uuid.UUID) ([]provider.Account, error) {
			return []provider.Account{
				testAccount(1, 0, true, []string{"gpt-3.5-turbo"}),
				testAccount(2, 1, true, []string{"gpt-4"}),
				testAccount(3, 2, true, nil),
			}, nil
		},
	}

	sel := NewAccountSelector(repo, nil)
	best, all, err := sel.SelectAccount(context.Background(), uuid.MustParse("f0000000-0000-0000-0000-000000000000"), "gpt-4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if best.Priority != 1 {
		t.Errorf("expected best priority 1 (matching gpt-4), got %d", best.Priority)
	}
	if len(all) != 2 {
		t.Errorf("expected 2 candidates (gpt-4 filter + no filter), got %d", len(all))
	}
}

func TestAccountSelector_SelectAccount_NoMatchingAccounts(t *testing.T) {
	repo := &fakeAccountRepo{
		findByProviderIDFn: func(ctx context.Context, providerID uuid.UUID) ([]provider.Account, error) {
			return []provider.Account{
				testAccount(1, 0, true, []string{"claude"}),
			}, nil
		},
	}

	sel := NewAccountSelector(repo, nil)
	_, _, err := sel.SelectAccount(context.Background(), uuid.MustParse("f0000000-0000-0000-0000-000000000000"), "gpt-4")
	if err == nil {
		t.Fatal("expected error for no matching accounts, got nil")
	}
}

func TestAccountSelector_SelectAccount_NoAccounts(t *testing.T) {
	repo := &fakeAccountRepo{
		findByProviderIDFn: func(ctx context.Context, providerID uuid.UUID) ([]provider.Account, error) {
			return nil, nil
		},
	}

	sel := NewAccountSelector(repo, nil)
	_, _, err := sel.SelectAccount(context.Background(), uuid.MustParse("f0000000-0000-0000-0000-000000000000"), "gpt-4")
	if err == nil {
		t.Fatal("expected error for no accounts, got nil")
	}
}

func TestAccountSelector_SelectAccount_AllOnCooldown(t *testing.T) {
	repo := &fakeAccountRepo{
		findByProviderIDFn: func(ctx context.Context, providerID uuid.UUID) ([]provider.Account, error) {
			return []provider.Account{
				testAccount(1, 0, true, nil),
				testAccount(2, 1, true, nil),
			}, nil
		},
	}

	cooldown := &fakeCooldownRegistry{
		onCooldownFn: func(ctx context.Context, accountID uuid.UUID) bool {
			return true
		},
	}

	sel := NewAccountSelector(repo, cooldown)
	_, _, err := sel.SelectAccount(context.Background(), uuid.MustParse("f0000000-0000-0000-0000-000000000000"), "gpt-4")
	if err == nil {
		t.Fatal("expected error when all accounts on cooldown, got nil")
	}
}

func TestAccountSelector_SelectAccount_RepoError(t *testing.T) {
	repo := &fakeAccountRepo{
		findByProviderIDFn: func(ctx context.Context, providerID uuid.UUID) ([]provider.Account, error) {
			return nil, errors.New("db error")
		},
	}

	sel := NewAccountSelector(repo, nil)
	_, _, err := sel.SelectAccount(context.Background(), uuid.MustParse("f0000000-0000-0000-0000-000000000000"), "gpt-4")
	if err == nil {
		t.Fatal("expected error from repo, got nil")
	}
}

func TestAccountSelector_SelectAccount_NilRepo(t *testing.T) {
	sel := &AccountSelector{accountRepo: nil}
	_, _, err := sel.SelectAccount(context.Background(), uuid.MustParse("f0000000-0000-0000-0000-000000000000"), "gpt-4")
	if err == nil {
		t.Fatal("expected error for nil repo, got nil")
	}
}

func TestMatchesModelFilter_EmptyFilter(t *testing.T) {
	if !matchesModelFilter(nil, "gpt-4") {
		t.Error("expected nil filter to match all models")
	}
	if !matchesModelFilter([]string{}, "gpt-4") {
		t.Error("expected empty filter to match all models")
	}
}

func TestMatchesModelFilter_Matching(t *testing.T) {
	if !matchesModelFilter([]string{"gpt-4", "gpt-3.5-turbo"}, "gpt-4") {
		t.Error("expected gpt-4 to match the filter list")
	}
}

func TestMatchesModelFilter_NonMatching(t *testing.T) {
	if matchesModelFilter([]string{"claude-3"}, "gpt-4") {
		t.Error("expected gpt-4 not to match claude-3 filter")
	}
}
