package routing

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"gorouter/internal/app/tx"
	"gorouter/internal/domain/modelref"
	"gorouter/internal/domain/provider"
)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

type fakePgxTx struct{ pgx.Tx }

func (f *fakePgxTx) Commit(ctx context.Context) error   { return nil }
func (f *fakePgxTx) Rollback(ctx context.Context) error { return nil }

type fakeProviderRepo struct {
	listFn func(ctx context.Context) ([]provider.Provider, error)
}

func (f *fakeProviderRepo) List(ctx context.Context) ([]provider.Provider, error) {
	return f.listFn(ctx)
}
func (f *fakeProviderRepo) FindByID(ctx context.Context, id uuid.UUID) (*provider.Provider, error) {
	return nil, errors.New("unexpected")
}
func (f *fakeProviderRepo) FindByType(ctx context.Context, ptype provider.ProviderType) ([]provider.Provider, error) {
	return nil, errors.New("unexpected")
}
func (f *fakeProviderRepo) Create(ctx context.Context, p *provider.Provider) error {
	return errors.New("unexpected")
}
func (f *fakeProviderRepo) Update(ctx context.Context, p *provider.Provider) error {
	return errors.New("unexpected")
}
func (f *fakeProviderRepo) Delete(ctx context.Context, id uuid.UUID) error {
	return errors.New("unexpected")
}

type fakeModelRepo struct {
	getModelsByProviderFn func(ctx context.Context, providerID uuid.UUID) ([]tx.ProviderModel, error)
}

func (f *fakeModelRepo) GetModelsByProvider(ctx context.Context, providerID uuid.UUID) ([]tx.ProviderModel, error) {
	return f.getModelsByProviderFn(ctx, providerID)
}
func (f *fakeModelRepo) GetModelByRef(ctx context.Context, providerID uuid.UUID, modelName string) (*tx.ProviderModel, error) {
	return nil, errors.New("unexpected")
}
func (f *fakeModelRepo) ListByCapability(ctx context.Context, capability string) ([]tx.ProviderModel, error) {
	return nil, errors.New("unexpected")
}

func fakeScope(providers provider.ProviderRepository, models tx.ModelRepository) *tx.TxScope {
	return tx.NewTxScope(
		&fakePgxTx{},
		nil, nil, nil, nil,
		providers, nil, nil,
		nil, nil,
		models,
		nil, nil, nil,
		nil, nil, nil, nil, nil, nil, nil,
		nil, nil,
	)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestResolver_Resolve_ByExplicitProviderName(t *testing.T) {
	prov := provider.Provider{
		ID:        uuid.MustParse("10000000-0000-0000-0000-000000000001"),
		Name:      "openai",
		Type:      provider.ProviderOpenAI,
		IsEnabled: true,
	}

	providerRepo := &fakeProviderRepo{
		listFn: func(ctx context.Context) ([]provider.Provider, error) {
			return []provider.Provider{prov}, nil
		},
	}

	modelRepo := &fakeModelRepo{
		getModelsByProviderFn: func(ctx context.Context, providerID uuid.UUID) ([]tx.ProviderModel, error) {
			return []tx.ProviderModel{
				{ModelName: "gpt-4", Capabilities: []string{"chat"}},
			}, nil
		},
	}

	r := NewResolver(func(ctx context.Context) (*tx.TxScope, error) {
		return fakeScope(providerRepo, modelRepo), nil
	})

	ref := modelref.ModelRef{Provider: "openai", Model: "gpt-4"}
	resolved, err := r.Resolve(context.Background(), ref)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved.ProviderID != prov.ID {
		t.Errorf("expected ProviderID %v, got %v", prov.ID, resolved.ProviderID)
	}
	if resolved.ModelName != "gpt-4" {
		t.Errorf("expected ModelName gpt-4, got %s", resolved.ModelName)
	}
}

func TestResolver_Resolve_ByProviderType(t *testing.T) {
	prov := provider.Provider{
		ID:        uuid.MustParse("20000000-0000-0000-0000-000000000002"),
		Name:      "azure-eastus",
		Type:      provider.ProviderAzure,
		IsEnabled: true,
	}

	providerRepo := &fakeProviderRepo{
		listFn: func(ctx context.Context) ([]provider.Provider, error) {
			return []provider.Provider{prov}, nil
		},
	}

	modelRepo := &fakeModelRepo{
		getModelsByProviderFn: func(ctx context.Context, providerID uuid.UUID) ([]tx.ProviderModel, error) {
			return []tx.ProviderModel{{ModelName: "gpt-4"}}, nil
		},
	}

	r := NewResolver(func(ctx context.Context) (*tx.TxScope, error) {
		return fakeScope(providerRepo, modelRepo), nil
	})

	ref := modelref.ModelRef{Provider: "azure", Model: "gpt-4"}
	resolved, err := r.Resolve(context.Background(), ref)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved.ProviderID != prov.ID {
		t.Errorf("expected ProviderID %v, got %v", prov.ID, resolved.ProviderID)
	}
}

func TestResolver_Resolve_NoExplicitProvider(t *testing.T) {
	enabledProv := provider.Provider{
		ID:        uuid.MustParse("30000000-0000-0000-0000-000000000003"),
		Name:      "enabled-provider",
		Type:      provider.ProviderOpenAI,
		IsEnabled: true,
	}
	disabledProv := provider.Provider{
		ID:        uuid.MustParse("40000000-0000-0000-0000-000000000004"),
		Name:      "disabled-provider",
		Type:      provider.ProviderOpenAI,
		IsEnabled: false,
	}

	providerRepo := &fakeProviderRepo{
		listFn: func(ctx context.Context) ([]provider.Provider, error) {
			return []provider.Provider{disabledProv, enabledProv}, nil
		},
	}

	modelRepo := &fakeModelRepo{
		getModelsByProviderFn: func(ctx context.Context, providerID uuid.UUID) ([]tx.ProviderModel, error) {
			return []tx.ProviderModel{{ModelName: "gpt-4"}}, nil
		},
	}

	r := NewResolver(func(ctx context.Context) (*tx.TxScope, error) {
		return fakeScope(providerRepo, modelRepo), nil
	})

	ref := modelref.ModelRef{Model: "gpt-4"}
	resolved, err := r.Resolve(context.Background(), ref)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved.ProviderID != enabledProv.ID {
		t.Errorf("expected enabled provider, got %v", resolved.ProviderID)
	}
}

func TestResolver_Resolve_NoModelSpecified(t *testing.T) {
	prov := provider.Provider{
		ID:        uuid.MustParse("50000000-0000-0000-0000-000000000005"),
		Name:      "test-provider",
		Type:      provider.ProviderOpenAI,
		IsEnabled: true,
	}

	providerRepo := &fakeProviderRepo{
		listFn: func(ctx context.Context) ([]provider.Provider, error) {
			return []provider.Provider{prov}, nil
		},
	}

	modelRepo := &fakeModelRepo{
		getModelsByProviderFn: func(ctx context.Context, providerID uuid.UUID) ([]tx.ProviderModel, error) {
			return []tx.ProviderModel{
				{ModelName: "gpt-4", Capabilities: []string{"chat"}},
				{ModelName: "gpt-3.5-turbo", Capabilities: []string{"chat"}},
			}, nil
		},
	}

	r := NewResolver(func(ctx context.Context) (*tx.TxScope, error) {
		return fakeScope(providerRepo, modelRepo), nil
	})

	ref := modelref.ModelRef{Provider: "test-provider"}
	resolved, err := r.Resolve(context.Background(), ref)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should pick the first model from the catalog.
	if resolved.ModelName != "gpt-4" {
		t.Errorf("expected ModelName gpt-4 (first model), got %s", resolved.ModelName)
	}
}

func TestResolver_Resolve_EmptyModelAndNoModels(t *testing.T) {
	prov := provider.Provider{
		ID:        uuid.MustParse("60000000-0000-0000-0000-000000000006"),
		Name:      "test-provider",
		Type:      provider.ProviderOpenAI,
		IsEnabled: true,
	}

	providerRepo := &fakeProviderRepo{
		listFn: func(ctx context.Context) ([]provider.Provider, error) {
			return []provider.Provider{prov}, nil
		},
	}

	modelRepo := &fakeModelRepo{
		getModelsByProviderFn: func(ctx context.Context, providerID uuid.UUID) ([]tx.ProviderModel, error) {
			return nil, nil
		},
	}

	r := NewResolver(func(ctx context.Context) (*tx.TxScope, error) {
		return fakeScope(providerRepo, modelRepo), nil
	})

	ref := modelref.ModelRef{Provider: "test-provider"}
	resolved, err := r.Resolve(context.Background(), ref)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should fallback to "gpt-4".
	if resolved.ModelName != "gpt-4" {
		t.Errorf("expected fallback ModelName gpt-4, got %s", resolved.ModelName)
	}
}

func TestResolver_Resolve_WithCapability(t *testing.T) {
	prov := provider.Provider{
		ID:        uuid.MustParse("70000000-0000-0000-0000-000000000007"),
		Name:      "test-provider",
		Type:      provider.ProviderOpenAI,
		IsEnabled: true,
	}

	providerRepo := &fakeProviderRepo{
		listFn: func(ctx context.Context) ([]provider.Provider, error) {
			return []provider.Provider{prov}, nil
		},
	}

	cap := "vision"
	modelRepo := &fakeModelRepo{
		getModelsByProviderFn: func(ctx context.Context, providerID uuid.UUID) ([]tx.ProviderModel, error) {
			return []tx.ProviderModel{
				{ModelName: "gpt-4", Capabilities: []string{"chat", "vision"}},
			}, nil
		},
	}

	r := NewResolver(func(ctx context.Context) (*tx.TxScope, error) {
		return fakeScope(providerRepo, modelRepo), nil
	})

	ref := modelref.ModelRef{Provider: "test-provider", Model: "gpt-4", Capability: &cap}
	resolved, err := r.Resolve(context.Background(), ref)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved.ModelName != "gpt-4" {
		t.Errorf("expected ModelName gpt-4, got %s", resolved.ModelName)
	}
}

func TestResolver_Resolve_CapabilityNotSupported(t *testing.T) {
	prov := provider.Provider{
		ID:        uuid.MustParse("80000000-0000-0000-0000-000000000008"),
		Name:      "test-provider",
		Type:      provider.ProviderOpenAI,
		IsEnabled: true,
	}

	providerRepo := &fakeProviderRepo{
		listFn: func(ctx context.Context) ([]provider.Provider, error) {
			return []provider.Provider{prov}, nil
		},
	}

	cap := "codex"
	modelRepo := &fakeModelRepo{
		getModelsByProviderFn: func(ctx context.Context, providerID uuid.UUID) ([]tx.ProviderModel, error) {
			return []tx.ProviderModel{
				{ModelName: "gpt-4", Capabilities: []string{"chat"}},
			}, nil
		},
	}

	r := NewResolver(func(ctx context.Context) (*tx.TxScope, error) {
		return fakeScope(providerRepo, modelRepo), nil
	})

	ref := modelref.ModelRef{Provider: "test-provider", Model: "gpt-4", Capability: &cap}
	_, err := r.Resolve(context.Background(), ref)
	if err == nil {
		t.Fatal("expected error for unsupported capability, got nil")
	}
	if !errors.Is(err, modelref.ErrCapNotSupported) {
		t.Fatalf("expected ErrCapNotSupported, got %v", err)
	}
}

func TestResolver_Resolve_ProviderNotFound(t *testing.T) {
	providerRepo := &fakeProviderRepo{
		listFn: func(ctx context.Context) ([]provider.Provider, error) {
			return []provider.Provider{}, nil
		},
	}

	modelRepo := &fakeModelRepo{
		getModelsByProviderFn: func(ctx context.Context, providerID uuid.UUID) ([]tx.ProviderModel, error) {
			return nil, nil
		},
	}

	r := NewResolver(func(ctx context.Context) (*tx.TxScope, error) {
		return fakeScope(providerRepo, modelRepo), nil
	})

	ref := modelref.ModelRef{Provider: "nonexistent", Model: "gpt-4"}
	_, err := r.Resolve(context.Background(), ref)
	if err == nil {
		t.Fatal("expected error for nonexistent provider, got nil")
	}
	if !errors.Is(err, modelref.ErrProviderNotFound) {
		t.Fatalf("expected ErrProviderNotFound, got %v", err)
	}
}

func TestResolver_Resolve_NoEnabledProviders(t *testing.T) {
	providerRepo := &fakeProviderRepo{
		listFn: func(ctx context.Context) ([]provider.Provider, error) {
			return []provider.Provider{
				{ID: uuid.MustParse("90000000-0000-0000-0000-000000000009"), Name: "disabled", IsEnabled: false},
			}, nil
		},
	}

	modelRepo := &fakeModelRepo{
		getModelsByProviderFn: func(ctx context.Context, providerID uuid.UUID) ([]tx.ProviderModel, error) {
			return nil, nil
		},
	}

	r := NewResolver(func(ctx context.Context) (*tx.TxScope, error) {
		return fakeScope(providerRepo, modelRepo), nil
	})

	ref := modelref.ModelRef{Model: "gpt-4"}
	_, err := r.Resolve(context.Background(), ref)
	if err == nil {
		t.Fatal("expected error for no enabled providers, got nil")
	}
	if !errors.Is(err, modelref.ErrProviderNotFound) {
		t.Fatalf("expected ErrProviderNotFound, got %v", err)
	}
}

func TestResolver_Resolve_TxProviderError(t *testing.T) {
	r := NewResolver(func(ctx context.Context) (*tx.TxScope, error) {
		return nil, errors.New("connection failed")
	})

	ref := modelref.ModelRef{Provider: "openai", Model: "gpt-4"}
	_, err := r.Resolve(context.Background(), ref)
	if err == nil {
		t.Fatal("expected error from txProvider, got nil")
	}
}

func TestResolver_Resolve_NilTxProvider(t *testing.T) {
	r := &Resolver{txProvider: nil}

	ref := modelref.ModelRef{Provider: "openai", Model: "gpt-4"}
	_, err := r.Resolve(context.Background(), ref)
	if err == nil {
		t.Fatal("expected error for nil txProvider, got nil")
	}
	if !errors.Is(err, modelref.ErrProviderNotFound) {
		t.Fatalf("expected ErrProviderNotFound, got %v", err)
	}
}

func TestResolver_Resolve_ListProvidersError(t *testing.T) {
	providerRepo := &fakeProviderRepo{
		listFn: func(ctx context.Context) ([]provider.Provider, error) {
			return nil, errors.New("db error")
		},
	}

	modelRepo := &fakeModelRepo{
		getModelsByProviderFn: func(ctx context.Context, providerID uuid.UUID) ([]tx.ProviderModel, error) {
			return nil, nil
		},
	}

	r := NewResolver(func(ctx context.Context) (*tx.TxScope, error) {
		return fakeScope(providerRepo, modelRepo), nil
	})

	ref := modelref.ModelRef{Provider: "openai", Model: "gpt-4"}
	_, err := r.Resolve(context.Background(), ref)
	if err == nil {
		t.Fatal("expected error from List, got nil")
	}
}
