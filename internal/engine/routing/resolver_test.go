package routing

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"gorouter/internal/domain/provider"
)

// fakeProviderRepo implements provider.ProviderRepository for tests.
type fakeProviderRepo struct {
	providers []provider.Provider
}

func (f *fakeProviderRepo) FindByID(_ context.Context, id uuid.UUID) (*provider.Provider, error) {
	for i := range f.providers {
		if f.providers[i].ID == id {
			return &f.providers[i], nil
		}
	}
	return nil, nil
}

func (f *fakeProviderRepo) FindByType(_ context.Context, ptype provider.ProviderType) ([]provider.Provider, error) {
	var result []provider.Provider
	for _, p := range f.providers {
		if p.Type == ptype {
			result = append(result, p)
		}
	}
	return result, nil
}

func (f *fakeProviderRepo) List(_ context.Context) ([]provider.Provider, error) {
	return f.providers, nil
}

func (f *fakeProviderRepo) Create(_ context.Context, _ *provider.Provider) error { return nil }
func (f *fakeProviderRepo) Update(_ context.Context, _ *provider.Provider) error { return nil }
func (f *fakeProviderRepo) Delete(_ context.Context, _ uuid.UUID) error          { return nil }

// fakeModelRepo implements ModelRepository for tests.
type fakeModelRepo struct {
	models map[uuid.UUID][]ModelInfo
}

func (f *fakeModelRepo) GetModelsByProvider(_ context.Context, providerID uuid.UUID) ([]ModelInfo, error) {
	if f.models == nil {
		return nil, nil
	}
	return f.models[providerID], nil
}

func newTestResolver(providers []provider.Provider, accounts []provider.Account) *Resolver {
	providerRepo := &fakeProviderRepo{providers: providers}
	state := NewRoutingStateManagerWithClock(frozenNow)
	selector := NewEngineAccountSelector(&fakeAccountRepo{accounts: accounts}, state)
	handler := NewFallbackHandler(DefaultFallbackConfig(), state)

	return NewResolver(
		DefaultResolverConfig(),
		providerRepo,
		&fakeModelRepo{},
		selector,
		handler,
		state,
		NewFakeCheckpointer(),
	)
}

func TestResolver_ResolveModel(t *testing.T) {
	providerID := uuid.New()
	accountID := uuid.New()

	providers := []provider.Provider{
		{ID: providerID, Name: "openai", Type: provider.ProviderOpenAI, IsEnabled: true},
		{ID: uuid.New(), Name: "disabled", Type: provider.ProviderCustom, IsEnabled: false},
	}
	accounts := []provider.Account{
		{ID: accountID, ProviderID: providerID, Priority: 0, IsEnabled: true},
	}

	resolver := newTestResolver(providers, accounts)

	resolvedP, resolvedA, model, _, err := resolver.ResolveModel(context.Background(), "gpt-4")
	if err != nil {
		t.Fatalf("ResolveModel: %v", err)
	}
	if resolvedP.ID != providerID {
		t.Errorf("provider = %v, want %v", resolvedP.ID, providerID)
	}
	if resolvedA.ID != accountID {
		t.Errorf("account = %v, want %v", resolvedA.ID, accountID)
	}
	if model != "gpt-4" {
		t.Errorf("model = %q, want %q", model, "gpt-4")
	}
}

func TestResolver_ResolveModel_NoEligibleProvider(t *testing.T) {
	resolver := newTestResolver(nil, nil)

	_, _, _, _, err := resolver.ResolveModel(context.Background(), "gpt-4")
	if err == nil {
		t.Fatal("expected error for no eligible provider")
	}
}

func TestResolver_ResolveModel_DisabledProvider(t *testing.T) {
	providerID := uuid.New()
	providers := []provider.Provider{
		{ID: providerID, Name: "disabled", Type: provider.ProviderOpenAI, IsEnabled: false},
	}
	accounts := []provider.Account{
		{ID: uuid.New(), ProviderID: providerID, Priority: 0, IsEnabled: true},
	}
	resolver := newTestResolver(providers, accounts)

	_, _, _, _, err := resolver.ResolveModel(context.Background(), "gpt-4")
	if err == nil {
		t.Fatal("expected error for disabled provider")
	}
}

func TestResolver_ResolveProviderModel(t *testing.T) {
	providerID := uuid.New()
	accountID := uuid.New()

	accounts := []provider.Account{
		{ID: accountID, ProviderID: providerID, Priority: 0, IsEnabled: true},
		{ID: uuid.New(), ProviderID: providerID, Priority: 1, IsEnabled: true},
	}

	resolver := newTestResolver(nil, accounts)
	best, candidates, err := resolver.ResolveProviderModel(context.Background(), providerID, "gpt-4")
	if err != nil {
		t.Fatalf("ResolveProviderModel: %v", err)
	}
	if best.ID != accountID {
		t.Errorf("best = %v, want %v", best.ID, accountID)
	}
	if len(candidates) != 2 {
		t.Errorf("got %d candidates, want 2", len(candidates))
	}
}

func TestResolver_RecordFailureAndSuccess(t *testing.T) {
	accountID := uuid.New()
	resolver := newTestResolver(nil, nil)

	state := resolver.RecordFailure(accountID, FailureAuth, 401, "bad key", nil)
	if state == nil {
		t.Fatal("RecordFailure returned nil")
	}
	if state.Status != provider.AccountStatusDisabled {
		t.Errorf("Status = %q, want %q", state.Status, provider.AccountStatusDisabled)
	}

	resolver.RecordSuccess(accountID)
	state2 := resolver.StateManager().GetAccount(accountID)
	if state2 == nil {
		t.Fatal("GetAccount returned nil")
	}
	if state2.Status != provider.AccountStatusActive {
		t.Errorf("Status after success = %q, want %q", state2.Status, provider.AccountStatusActive)
	}
}

func TestResolver_CheckpointRestore(t *testing.T) {
	accountID := uuid.New()
	state := NewRoutingStateManagerWithClock(frozenNow)
	checkpointer := NewFakeCheckpointer()

	old := state.RecordFailure(accountID, FailureAuth, 401, "bad key", nil)
	if old == nil {
		t.Fatal("RecordFailure returned nil")
	}

	snapshot := state.Checkpoint()
	err := checkpointer.SaveCheckpoint(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}

	restoredState := NewRoutingStateManagerWithClock(frozenNow)
	loaded, err := checkpointer.LoadCheckpoint(context.Background())
	if err != nil {
		t.Fatalf("LoadCheckpoint: %v", err)
	}
	restoredState.Restore(loaded)

	state2 := restoredState.GetAccount(accountID)
	if state2 == nil {
		t.Fatal("restored account should exist")
	}
	if state2.Status != provider.AccountStatusDisabled {
		t.Errorf("restored status = %q, want %q", state2.Status, provider.AccountStatusDisabled)
	}
}

func TestResolver_EvaluateFallback(t *testing.T) {
	accountID := uuid.New()
	resolver := newTestResolver(nil, nil)

	accounts := []provider.Account{
		{ID: accountID, IsEnabled: true},
		{ID: uuid.New(), IsEnabled: true},
	}

	decision := resolver.EvaluateFallback(1, FailureUpstream, accounts, accountID)
	if !decision.ShouldFallback {
		t.Fatal("expected fallback for upstream failure")
	}
}

func TestResolver_SelectAccounts(t *testing.T) {
	providerID := uuid.New()
	accounts := []provider.Account{
		{ID: uuid.New(), ProviderID: providerID, Priority: 0, IsEnabled: true},
		{ID: uuid.New(), ProviderID: providerID, Priority: 1, IsEnabled: false},
		{ID: uuid.New(), ProviderID: providerID, Priority: 2, IsEnabled: true},
	}

	resolver := newTestResolver(nil, accounts)
	candidates, err := resolver.SelectAccounts(context.Background(), providerID, "gpt-4")
	if err != nil {
		t.Fatalf("SelectAccounts: %v", err)
	}
	if len(candidates) != 2 {
		t.Errorf("got %d candidates, want 2 (should exclude disabled)", len(candidates))
	}
}
