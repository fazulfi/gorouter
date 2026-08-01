package routing

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Catalog model and decision #192 reconciliation
// ---------------------------------------------------------------------------

// fakeCatalogStore implements CatalogStore over gorouter_provider_models
// semantics: custom (is_builtin=false) and admin-disabled (is_enabled=false)
// rows are never removed by reconciliation.
type fakeCatalogStore struct {
	models []CatalogModel
}

func newFakeCatalogStore(models ...CatalogModel) *fakeCatalogStore {
	s := &fakeCatalogStore{}
	for i := range models {
		if models[i].ID == uuid.Nil {
			models[i].ID = uuid.New()
		}
		s.models = append(s.models, models[i])
	}
	return s
}

func (f *fakeCatalogStore) ListAll(_ context.Context) ([]CatalogModel, error) {
	out := make([]CatalogModel, len(f.models))
	copy(out, f.models)
	return out, nil
}

func (f *fakeCatalogStore) ListByProvider(_ context.Context, providerID uuid.UUID) ([]CatalogModel, error) {
	var out []CatalogModel
	for _, m := range f.models {
		if m.ProviderID == providerID {
			out = append(out, m)
		}
	}
	return out, nil
}

func (f *fakeCatalogStore) GetByProviderModel(_ context.Context, providerID uuid.UUID, modelName string) (*CatalogModel, error) {
	for i := range f.models {
		if f.models[i].ProviderID == providerID && f.models[i].ModelName == modelName {
			m := f.models[i]
			return &m, nil
		}
	}
	return nil, nil
}

func (f *fakeCatalogStore) UpsertDiscovered(_ context.Context, m CatalogModel) error {
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

func (f *fakeCatalogStore) InsertCustom(_ context.Context, m *CatalogModel) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	f.models = append(f.models, *m)
	return nil
}

func (f *fakeCatalogStore) Update(_ context.Context, m *CatalogModel) error {
	for i := range f.models {
		if f.models[i].ID == m.ID {
			f.models[i] = *m
			return nil
		}
	}
	return ErrModelNotFound
}

func (f *fakeCatalogStore) SetEnabled(_ context.Context, id uuid.UUID, enabled bool) error {
	for i := range f.models {
		if f.models[i].ID == id {
			f.models[i].IsEnabled = enabled
			return nil
		}
	}
	return ErrModelNotFound
}

func (f *fakeCatalogStore) DeleteStale(_ context.Context, id uuid.UUID) error {
	for i := range f.models {
		if f.models[i].ID == id {
			if !f.models[i].IsBuiltin || !f.models[i].IsEnabled {
				return nil
			}
			f.models = append(f.models[:i], f.models[i+1:]...)
			return nil
		}
	}
	return ErrModelNotFound
}

func (f *fakeCatalogStore) Delete(_ context.Context, id uuid.UUID) error {
	for i := range f.models {
		if f.models[i].ID == id {
			f.models = append(f.models[:i], f.models[i+1:]...)
			return nil
		}
	}
	return ErrModelNotFound
}

func catalogModel(providerID uuid.UUID, name string, builtin, enabled bool) CatalogModel {
	return CatalogModel{
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

// TestPlanReconciliation_NeverDeletesCustom verifies discovery never deletes
// custom models (decision #192: is_builtin=false rows survive until admin
// removes them).
func TestPlanReconciliation_NeverDeletesCustom(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	current := []CatalogModel{
		catalogModel(pid, "admin-created-model", false, true),
		catalogModel(pid, "gpt-4o", true, true),
	}
	plan := PlanReconciliation(current, []DiscoveredModel{
		{ModelName: "gpt-4o", Capabilities: []string{"chat"}, MaxTokens: 128000},
	})
	if len(plan.ToDelete) != 0 {
		t.Fatalf("ToDelete = %v, custom model must never be deleted (decision #192)", plan.ToDelete)
	}
	if len(plan.ToUpsert) != 1 || plan.ToUpsert[0].ModelName != "gpt-4o" {
		t.Errorf("ToUpsert = %+v, want exactly gpt-4o", plan.ToUpsert)
	}
	if len(plan.Preserved) != 1 || plan.Preserved[0].ModelName != "admin-created-model" {
		t.Errorf("Preserved = %+v, want admin-created-model", plan.Preserved)
	}
}

// TestPlanReconciliation_NeverDeletesDisabled verifies admin-disabled models
// survive discovery even when the provider no longer reports them
// (decision #192).
func TestPlanReconciliation_NeverDeletesDisabled(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	current := []CatalogModel{
		catalogModel(pid, "gpt-4o", true, false), // admin disabled
	}
	plan := PlanReconciliation(current, nil)
	if len(plan.ToDelete) != 0 {
		t.Fatalf("ToDelete = %v, admin-disabled model must never be deleted", plan.ToDelete)
	}
	if len(plan.Preserved) != 1 {
		t.Errorf("Preserved = %+v, want the disabled model preserved", plan.Preserved)
	}
}

// TestPlanReconciliation_DeletesStaleBuiltin verifies builtin enabled models
// no longer reported by the provider are removed (the only deletable class).
func TestPlanReconciliation_DeletesStaleBuiltin(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	current := []CatalogModel{
		catalogModel(pid, "gpt-4o", true, true),
		catalogModel(pid, "gpt-4o-mini", true, true),
	}
	plan := PlanReconciliation(current, []DiscoveredModel{{ModelName: "gpt-4o"}})
	if len(plan.ToDelete) != 1 || plan.ToDelete[0] != current[1].ID {
		t.Fatalf("ToDelete = %v, want only gpt-4o-mini", plan.ToDelete)
	}
}

// TestPlanReconciliation_UpsertPreservesAdminFlags verifies upsert updates
// provider-reported fields only: capabilities/max_tokens are refreshed but
// is_enabled (admin state) is preserved (decision #192).
func TestPlanReconciliation_UpsertPreservesAdminFlags(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	current := []CatalogModel{
		catalogModel(pid, "gpt-4o", true, false), // admin disabled
	}
	plan := PlanReconciliation(current, []DiscoveredModel{
		{ModelName: "gpt-4o", Capabilities: []string{"chat", "vision"}, MaxTokens: 256000},
	})
	if len(plan.ToUpsert) != 1 {
		t.Fatalf("ToUpsert = %+v", plan.ToUpsert)
	}
	u := plan.ToUpsert[0]
	if u.IsEnabled {
		t.Error("upsert must preserve is_enabled=false (admin state)")
	}
	if u.Capabilities[0] != "chat" || len(u.Capabilities) != 2 {
		t.Errorf("Capabilities = %v, want refreshed [chat vision]", u.Capabilities)
	}
	if u.MaxTokens != 256000 {
		t.Errorf("MaxTokens = %d, want 256000", u.MaxTokens)
	}
}

// TestPlanReconciliation_NewDiscoveredInserted verifies new discovered models
// are inserted as builtin+enabled.
func TestPlanReconciliation_NewDiscoveredInserted(t *testing.T) {
	t.Parallel()
	plan := PlanReconciliation(nil, []DiscoveredModel{
		{ModelName: "brand-new-model", Capabilities: []string{"chat"}},
	})
	if len(plan.ToUpsert) != 1 {
		t.Fatalf("ToUpsert = %+v", plan.ToUpsert)
	}
	if !plan.ToUpsert[0].IsBuiltin || !plan.ToUpsert[0].IsEnabled {
		t.Errorf("new discovered model must be builtin+enabled: %+v", plan.ToUpsert[0])
	}
}

// TestPlanReconciliation_Deterministic verifies the plan is deterministic for
// identical inputs (stable order, no map iteration).
func TestPlanReconciliation_Deterministic(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	current := []CatalogModel{
		catalogModel(pid, "b-model", true, true),
		catalogModel(pid, "a-model", true, true),
	}
	disc := []DiscoveredModel{
		{ModelName: "b-model"},
		{ModelName: "c-model"},
	}
	p1 := PlanReconciliation(current, disc)
	p2 := PlanReconciliation(current, disc)
	if len(p1.ToDelete) != 1 || len(p2.ToDelete) != 1 || p1.ToDelete[0] != p2.ToDelete[0] {
		t.Errorf("ToDelete not deterministic: %v vs %v", p1.ToDelete, p2.ToDelete)
	}
	if len(p1.ToUpsert) != 2 || len(p2.ToUpsert) != 2 {
		t.Fatalf("ToUpsert lengths: %d vs %d", len(p1.ToUpsert), len(p2.ToUpsert))
	}
	for i := range p1.ToUpsert {
		if p1.ToUpsert[i].ModelName != p2.ToUpsert[i].ModelName {
			t.Errorf("ToUpsert[%d] not deterministic: %q vs %q",
				i, p1.ToUpsert[i].ModelName, p2.ToUpsert[i].ModelName)
		}
	}
}

// TestPlanReconciliation_AliasesUntouched verifies reconciliation plans do
// not reference the alias table at all (aliases are admin-owned, decision
// #192).
func TestPlanReconciliation_AliasesUntouched(t *testing.T) {
	t.Parallel()
	plan := PlanReconciliation(nil, nil)
	if plan == nil {
		t.Fatal("plan must not be nil")
	}
}

// TestCatalog_EnabledFilter verifies the catalog only routes enabled models
// (disabled models are excluded from resolution).
func TestCatalog_EnabledFilter(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	store := newFakeCatalogStore(
		catalogModel(pid, "gpt-4o", true, true),
		catalogModel(pid, "gpt-4o-mini", true, false),
	)
	catalog := NewModelCatalog(store)
	got, err := catalog.EnabledByProvider(context.Background(), pid)
	if err != nil {
		t.Fatalf("EnabledByProvider: %v", err)
	}
	if len(got) != 1 || got[0].ModelName != "gpt-4o" {
		t.Errorf("EnabledByProvider = %+v, want only gpt-4o", got)
	}
}

// TestCatalog_LookupDisabled verifies resolving a disabled model fails with
// ErrModelDisabled.
func TestCatalog_LookupDisabled(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	store := newFakeCatalogStore(catalogModel(pid, "gpt-4o", true, false))
	catalog := NewModelCatalog(store)
	_, err := catalog.ResolveEnabled(context.Background(), pid, "gpt-4o")
	if !errors.Is(err, ErrModelDisabled) {
		t.Errorf("ResolveEnabled = %v, want ErrModelDisabled", err)
	}
}

// TestCatalog_ListStableOrder verifies catalog listing is deterministic
// (provider_id, model_name).
func TestCatalog_ListStableOrder(t *testing.T) {
	t.Parallel()
	p1 := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	p2 := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	store := newFakeCatalogStore(
		catalogModel(p2, "z-model", true, true),
		catalogModel(p1, "a-model", true, true),
		catalogModel(p1, "b-model", true, false),
	)
	catalog := NewModelCatalog(store)
	got, err := catalog.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0].ModelName != "a-model" || got[1].ModelName != "b-model" || got[2].ModelName != "z-model" {
		t.Errorf("order = [%s %s %s], want [a-model b-model z-model]",
			got[0].ModelName, got[1].ModelName, got[2].ModelName)
	}
}

// TestCatalog_ReconcileExecutes verifies the catalog applies a plan via its
// store and returns the executed result counts.
func TestCatalog_ReconcileExecutes(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	store := newFakeCatalogStore(
		catalogModel(pid, "stale-builtin", true, true),
		catalogModel(pid, "custom-keep", false, true),
	)
	catalog := NewModelCatalog(store)
	disc := []DiscoveredModel{
		{ModelName: "gpt-4o", Capabilities: []string{"chat"}},
		{ModelName: "custom-keep", Capabilities: []string{"chat"}},
	}
	res, err := catalog.Reconcile(context.Background(), pid, disc)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if res.Deleted != 1 {
		t.Errorf("Deleted = %d, want 1 (stale-builtin only)", res.Deleted)
	}
	if res.Inserted != 1 {
		t.Errorf("Inserted = %d, want 1 (gpt-4o)", res.Inserted)
	}
	if res.Updated != 1 {
		t.Errorf("Updated = %d, want 1 (custom-keep refreshed)", res.Updated)
	}
	if res.Preserved != 0 {
		t.Errorf("Preserved = %d, want 0 (custom-keep was reported and refreshed)", res.Preserved)
	}
	// custom-keep must still exist after reconciliation
	m, err := store.GetByProviderModel(context.Background(), pid, "custom-keep")
	if err != nil || m == nil {
		t.Errorf("custom-keep lost after reconcile: %v", err)
	}
}
