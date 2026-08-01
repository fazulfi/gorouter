package routing

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"gorouter/internal/domain/provider"
	enginerouting "gorouter/internal/engine/routing"
)

// appFakeProviderRepo implements provider.ProviderRepository in memory.
type appFakeProviderRepo struct {
	providers []provider.Provider
}

func (f *appFakeProviderRepo) FindByID(_ context.Context, id uuid.UUID) (*provider.Provider, error) {
	for i := range f.providers {
		if f.providers[i].ID == id {
			return &f.providers[i], nil
		}
	}
	return nil, nil
}

func (f *appFakeProviderRepo) FindByType(_ context.Context, ptype provider.ProviderType) ([]provider.Provider, error) {
	var out []provider.Provider
	for _, p := range f.providers {
		if p.Type == ptype {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *appFakeProviderRepo) List(_ context.Context) ([]provider.Provider, error) {
	return f.providers, nil
}

func (f *appFakeProviderRepo) Create(_ context.Context, _ *provider.Provider) error { return nil }
func (f *appFakeProviderRepo) Update(_ context.Context, _ *provider.Provider) error { return nil }
func (f *appFakeProviderRepo) Delete(_ context.Context, _ uuid.UUID) error          { return nil }

// appFakeCatalogStore implements enginerouting.CatalogStore in memory.
type appFakeCatalogStore struct {
	models []enginerouting.CatalogModel
}

func (f *appFakeCatalogStore) ListAll(_ context.Context) ([]enginerouting.CatalogModel, error) {
	out := make([]enginerouting.CatalogModel, len(f.models))
	copy(out, f.models)
	return out, nil
}

func (f *appFakeCatalogStore) ListByProvider(_ context.Context, providerID uuid.UUID) ([]enginerouting.CatalogModel, error) {
	var out []enginerouting.CatalogModel
	for _, m := range f.models {
		if m.ProviderID == providerID {
			out = append(out, m)
		}
	}
	return out, nil
}

func (f *appFakeCatalogStore) GetByProviderModel(_ context.Context, providerID uuid.UUID, modelName string) (*enginerouting.CatalogModel, error) {
	for i := range f.models {
		if f.models[i].ProviderID == providerID && f.models[i].ModelName == modelName {
			m := f.models[i]
			return &m, nil
		}
	}
	return nil, nil
}

func (f *appFakeCatalogStore) UpsertDiscovered(_ context.Context, m enginerouting.CatalogModel) error {
	for i := range f.models {
		if f.models[i].ProviderID == m.ProviderID && f.models[i].ModelName == m.ModelName {
			prev := f.models[i]
			prev.Capabilities = m.Capabilities
			prev.MaxTokens = m.MaxTokens
			prev.IsBuiltin = true
			f.models[i] = prev
			return nil
		}
	}
	m.ID = uuid.New()
	m.IsBuiltin = true
	m.IsEnabled = true
	f.models = append(f.models, m)
	return nil
}

func (f *appFakeCatalogStore) InsertCustom(_ context.Context, m *enginerouting.CatalogModel) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	f.models = append(f.models, *m)
	return nil
}

func (f *appFakeCatalogStore) Update(_ context.Context, m *enginerouting.CatalogModel) error {
	for i := range f.models {
		if f.models[i].ID == m.ID {
			f.models[i] = *m
			return nil
		}
	}
	return enginerouting.ErrModelNotFound
}

func (f *appFakeCatalogStore) SetEnabled(_ context.Context, id uuid.UUID, enabled bool) error {
	for i := range f.models {
		if f.models[i].ID == id {
			f.models[i].IsEnabled = enabled
			return nil
		}
	}
	return enginerouting.ErrModelNotFound
}

func (f *appFakeCatalogStore) DeleteStale(_ context.Context, id uuid.UUID) error {
	for i := range f.models {
		if f.models[i].ID == id {
			if !f.models[i].IsBuiltin || !f.models[i].IsEnabled {
				return nil
			}
			f.models = append(f.models[:i], f.models[i+1:]...)
			return nil
		}
	}
	return enginerouting.ErrModelNotFound
}

func (f *appFakeCatalogStore) Delete(_ context.Context, id uuid.UUID) error {
	for i := range f.models {
		if f.models[i].ID == id {
			f.models = append(f.models[:i], f.models[i+1:]...)
			return nil
		}
	}
	return enginerouting.ErrModelNotFound
}

func appCatalogModel(providerID uuid.UUID, name string, builtin, enabled bool) enginerouting.CatalogModel {
	return enginerouting.CatalogModel{
		ID:           uuid.New(),
		ProviderID:   providerID,
		ModelName:    name,
		Capabilities: []string{"chat"},
		MaxTokens:    128000,
		IsBuiltin:    builtin,
		IsEnabled:    enabled,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
}

func newAppService(aliases []enginerouting.Alias) *Service {
	repo := &appFakeAliasRepo{aliases: aliases}
	return NewService(repo, nil, nil, nil)
}

// appFakeAliasRepo implements enginerouting.AliasRepository in memory.
type appFakeAliasRepo struct {
	aliases []enginerouting.Alias
}

func (f *appFakeAliasRepo) Create(_ context.Context, a *enginerouting.Alias) error {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	for _, e := range f.aliases {
		if e.Alias == a.Alias {
			return enginerouting.ErrAliasExists
		}
	}
	f.aliases = append(f.aliases, *a)
	return nil
}

func (f *appFakeAliasRepo) Update(_ context.Context, a *enginerouting.Alias) error {
	for i := range f.aliases {
		if f.aliases[i].ID == a.ID {
			f.aliases[i] = *a
			return nil
		}
	}
	return enginerouting.ErrAliasNotFound
}

func (f *appFakeAliasRepo) Delete(_ context.Context, id uuid.UUID) error {
	for i := range f.aliases {
		if f.aliases[i].ID == id {
			f.aliases = append(f.aliases[:i], f.aliases[i+1:]...)
			return nil
		}
	}
	return enginerouting.ErrAliasNotFound
}

func (f *appFakeAliasRepo) FindByAlias(_ context.Context, alias string) (*enginerouting.Alias, error) {
	for i := range f.aliases {
		if f.aliases[i].Alias == alias {
			a := f.aliases[i]
			return &a, nil
		}
	}
	return nil, nil
}

func (f *appFakeAliasRepo) FindByID(_ context.Context, id uuid.UUID) (*enginerouting.Alias, error) {
	for i := range f.aliases {
		if f.aliases[i].ID == id {
			a := f.aliases[i]
			return &a, nil
		}
	}
	return nil, nil
}

func (f *appFakeAliasRepo) List(_ context.Context) ([]enginerouting.Alias, error) {
	out := make([]enginerouting.Alias, len(f.aliases))
	copy(out, f.aliases)
	return out, nil
}

func (f *appFakeAliasRepo) ListActive(_ context.Context) ([]enginerouting.Alias, error) {
	var out []enginerouting.Alias
	for i := range f.aliases {
		if f.aliases[i].IsActive {
			out = append(out, f.aliases[i])
		}
	}
	return out, nil
}

func (f *appFakeAliasRepo) SetActive(_ context.Context, id uuid.UUID, active bool) error {
	for i := range f.aliases {
		if f.aliases[i].ID == id {
			f.aliases[i].IsActive = active
			return nil
		}
	}
	return enginerouting.ErrAliasNotFound
}

// TestService_CreateAliasRoundTrip verifies alias CRUD through the app
// service.
func TestService_CreateAliasRoundTrip(t *testing.T) {
	t.Parallel()
	svc := newAppService(nil)
	a, err := svc.CreateAlias(context.Background(), "my-model", "openai/gpt-4o", nil)
	if err != nil {
		t.Fatalf("CreateAlias: %v", err)
	}
	if a.Alias != "my-model" || a.Target != "openai/gpt-4o" {
		t.Errorf("created = %+v", a)
	}
	if !a.IsActive {
		t.Error("IsActive = false, want true (schema default)")
	}
	got, err := svc.GetAliasByName(context.Background(), "my-model")
	if err != nil || got == nil {
		t.Fatalf("GetAliasByName: %v", err)
	}
	if err := svc.DeleteAlias(context.Background(), a.ID); err != nil {
		t.Fatalf("DeleteAlias: %v", err)
	}
	if _, err := svc.GetAliasByName(context.Background(), "my-model"); !errors.Is(err, enginerouting.ErrAliasNotFound) {
		t.Errorf("GetAliasByName after delete = %v, want ErrAliasNotFound", err)
	}
}

// TestService_CreateAliasValidation verifies slashless alias enforcement at
// the app boundary (decision #31).
func TestService_CreateAliasValidation(t *testing.T) {
	t.Parallel()
	svc := newAppService(nil)
	if _, err := svc.CreateAlias(context.Background(), "bad/name", "openai/gpt-4o", nil); !errors.Is(err, enginerouting.ErrInvalidAlias) {
		t.Errorf("CreateAlias(bad/name) = %v, want ErrInvalidAlias", err)
	}
}

// TestService_SetAliasActive verifies enable/disable toggling (schema
// is_active).
func TestService_SetAliasActive(t *testing.T) {
	t.Parallel()
	svc := newAppService(nil)
	a, _ := svc.CreateAlias(context.Background(), "my-model", "openai/gpt-4o", nil)
	if err := svc.SetAliasActive(context.Background(), a.ID, false); err != nil {
		t.Fatalf("SetAliasActive: %v", err)
	}
	got, err := svc.GetAliasByName(context.Background(), "my-model")
	if err != nil || got == nil {
		t.Fatalf("GetAliasByName: %v", err)
	}
	if got.IsActive {
		t.Error("IsActive = true after disable")
	}
}

// TestService_ResolveModel verifies the app-level resolution entry point
// composes alias resolution with the catalog enabled-check.
func TestService_ResolveModel(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	store := &appFakeCatalogStore{models: []enginerouting.CatalogModel{
		appCatalogModel(pid, "gpt-4o", true, true),
	}}
	svc := NewService(
		&appFakeAliasRepo{aliases: []enginerouting.Alias{
			{Alias: "fast", Target: "openai/gpt-4o", IsActive: true},
		}},
		store,
		enginerouting.NewMemoryNodeStore(),
		&appFakeProviderRepo{providers: []provider.Provider{
			{ID: pid, Name: "openai", Type: provider.ProviderOpenAI},
		}},
	)

	got, err := svc.ResolveModel(context.Background(), "fast")
	if err != nil {
		t.Fatalf("ResolveModel: %v", err)
	}
	if got.ViaAlias != "fast" {
		t.Errorf("ViaAlias = %q", got.ViaAlias)
	}
	if got.Provider != "openai" || got.Model != "gpt-4o" {
		t.Errorf("got %s/%s, want openai/gpt-4o", got.Provider, got.Model)
	}
	if got.CatalogModel == nil {
		t.Error("CatalogModel = nil, want resolved catalog entry")
	}
}

// TestService_ResolveModelDisabled verifies disabled models are rejected at
// resolution (admin state honored).
func TestService_ResolveModelDisabled(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	svc := NewService(
		&appFakeAliasRepo{},
		&appFakeCatalogStore{models: []enginerouting.CatalogModel{
			appCatalogModel(pid, "gpt-4o", true, false),
		}},
		nil,
		&appFakeProviderRepo{providers: []provider.Provider{
			{ID: pid, Name: "openai", Type: provider.ProviderOpenAI},
		}},
	)
	_, err := svc.ResolveModel(context.Background(), "gpt-4o")
	if !errors.Is(err, enginerouting.ErrModelDisabled) {
		t.Errorf("ResolveModel = %v, want ErrModelDisabled", err)
	}
}

// TestService_NodeLifecycle verifies node management through the service.
func TestService_NodeLifecycle(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	svc := NewService(&appFakeAliasRepo{}, nil, enginerouting.NewMemoryNodeStore(), nil)
	if err := svc.SaveNode(context.Background(), enginerouting.ProviderNode{
		ID: "eu-1", ProviderID: pid, Region: "eu", IsActive: true,
	}); err != nil {
		t.Fatalf("SaveNode: %v", err)
	}
	n, err := svc.SelectNode(context.Background(), pid, "eu")
	if err != nil {
		t.Fatalf("SelectNode: %v", err)
	}
	if n.ID != "eu-1" {
		t.Errorf("selected %q, want eu-1", n.ID)
	}
	nodes, err := svc.ListNodes(context.Background())
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Errorf("len(ListNodes) = %d, want 1", len(nodes))
	}
}

// TestService_ResolveModelProviderSource verifies the service's provider
// alias source resolves providers by name/type (Phase 2 resolver behavior).
func TestService_ResolveModelProviderSource(t *testing.T) {
	t.Parallel()
	p := provider.Provider{ID: uuid.New(), Name: "openai", Type: provider.ProviderOpenAI, IsEnabled: true}
	svc := NewService(&appFakeAliasRepo{}, nil, nil, &appFakeProviderRepo{providers: []provider.Provider{p}})
	got, err := svc.ResolveModel(context.Background(), "openai/gpt-4o")
	if err != nil {
		t.Fatalf("ResolveModel: %v", err)
	}
	if got.Provider != "openai" || got.Model != "gpt-4o" {
		t.Errorf("got %s/%s, want openai/gpt-4o", got.Provider, got.Model)
	}
}
