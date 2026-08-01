package routing

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"
)

// ErrModelDisabled is returned when a model exists but is admin-disabled.
var ErrModelDisabled = errors.New("routing: model disabled")

// ErrModelNotFound is returned when a model does not exist in the catalog.
var ErrModelNotFound = errors.New("routing: model not found")

// CatalogModel mirrors the gorouter_provider_models table columns used for
// routing: id, provider_id, model_id, display_name, capabilities, max_tokens,
// is_builtin, is_enabled, created_at, updated_at.
type CatalogModel struct {
	ID           uuid.UUID
	ProviderID   uuid.UUID
	ModelName    string
	DisplayName  string
	Capabilities []string
	MaxTokens    int64
	IsBuiltin    bool
	IsEnabled    bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// DiscoveredModel is a model reported by provider discovery (e.g. GET
// /v1/models on compatible providers).
type DiscoveredModel struct {
	ModelName    string
	Capabilities []string
	MaxTokens    int64
}

// CatalogStore defines the persistence boundary of the model catalog.
type CatalogStore interface {
	ListAll(ctx context.Context) ([]CatalogModel, error)
	ListByProvider(ctx context.Context, providerID uuid.UUID) ([]CatalogModel, error)
	GetByProviderModel(ctx context.Context, providerID uuid.UUID, modelName string) (*CatalogModel, error)
	UpsertDiscovered(ctx context.Context, m CatalogModel) error
	InsertCustom(ctx context.Context, m *CatalogModel) error
	Update(ctx context.Context, m *CatalogModel) error
	SetEnabled(ctx context.Context, id uuid.UUID, enabled bool) error
	DeleteStale(ctx context.Context, id uuid.UUID) error
	Delete(ctx context.Context, id uuid.UUID) error
}

// ReconcilePlan is the deterministic outcome of planning a discovery run.
type ReconcilePlan struct {
	ToUpsert  []CatalogModel
	ToDelete  []uuid.UUID
	Preserved []CatalogModel
}

// ReconcileResult reports the executed reconciliation counts.
type ReconcileResult struct {
	Inserted  int
	Updated   int
	Deleted   int
	Preserved int
}

// PlanReconciliation plans a discovery run against the current catalog.
// Decision #192: discovery refreshes provider-reported models but never
// removes custom models (is_builtin=false) or admin-disabled models
// (is_enabled=false); those survive until the admin changes or deletes them.
// Only builtin+enabled models no longer reported are deleted. Upserts refresh
// provider-reported fields (capabilities, max_tokens, display_name) and never
// flip admin flags. The plan is deterministic: inputs are iterated in order.
func PlanReconciliation(current []CatalogModel, discovered []DiscoveredModel) *ReconcilePlan {
	plan := &ReconcilePlan{}
	reported := make(map[string]bool, len(discovered))
	for _, d := range discovered {
		reported[d.ModelName] = true
	}
	seen := make(map[string]bool, len(current))

	for i := range current {
		m := current[i]
		seen[m.ModelName] = true
		if reported[m.ModelName] {
			plan.ToUpsert = append(plan.ToUpsert, refreshed(m, discovered))
			continue
		}
		if !m.IsBuiltin || !m.IsEnabled {
			plan.Preserved = append(plan.Preserved, m)
			continue
		}
		plan.ToDelete = append(plan.ToDelete, m.ID)
	}

	for i := range discovered {
		d := discovered[i]
		if seen[d.ModelName] {
			continue
		}
		plan.ToUpsert = append(plan.ToUpsert, CatalogModel{
			ModelName:    d.ModelName,
			Capabilities: d.Capabilities,
			MaxTokens:    d.MaxTokens,
			IsBuiltin:    true,
			IsEnabled:    true,
		})
	}
	return plan
}

// refreshed copies provider-reported fields onto an existing catalog row,
// keeping IsEnabled (admin state) intact.
func refreshed(m CatalogModel, discovered []DiscoveredModel) CatalogModel {
	for i := range discovered {
		if discovered[i].ModelName != m.ModelName {
			continue
		}
		m.Capabilities = discovered[i].Capabilities
		m.MaxTokens = discovered[i].MaxTokens
		m.IsBuiltin = true
		return m
	}
	return m
}

// ModelCatalog is the routing-facing catalog facade. It delegates persistence
// to a CatalogStore and applies reconciliation with decision #192 semantics.
type ModelCatalog struct {
	store CatalogStore
}

// NewModelCatalog creates a ModelCatalog over the given store.
func NewModelCatalog(store CatalogStore) *ModelCatalog {
	return &ModelCatalog{store: store}
}

// List returns the full catalog sorted by provider_id then model_name.
func (c *ModelCatalog) List(ctx context.Context) ([]CatalogModel, error) {
	if c.store == nil {
		return nil, nil
	}
	models, err := c.store.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(models, func(i, j int) bool {
		if models[i].ProviderID != models[j].ProviderID {
			return models[i].ProviderID.String() < models[j].ProviderID.String()
		}
		return models[i].ModelName < models[j].ModelName
	})
	return models, nil
}

// EnabledByProvider returns enabled models for a provider sorted by name.
func (c *ModelCatalog) EnabledByProvider(ctx context.Context, providerID uuid.UUID) ([]CatalogModel, error) {
	if c.store == nil {
		return nil, nil
	}
	models, err := c.store.ListByProvider(ctx, providerID)
	if err != nil {
		return nil, err
	}
	var out []CatalogModel
	for _, m := range models {
		if m.IsEnabled {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModelName < out[j].ModelName })
	return out, nil
}

// ResolveEnabled looks up a model and rejects it when admin-disabled.
func (c *ModelCatalog) ResolveEnabled(ctx context.Context, providerID uuid.UUID, modelName string) (*CatalogModel, error) {
	if c.store == nil {
		return nil, nil
	}
	m, err := c.store.GetByProviderModel(ctx, providerID, modelName)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, ErrModelNotFound
	}
	if !m.IsEnabled {
		return nil, ErrModelDisabled
	}
	return m, nil
}

// Reconcile plans and executes a discovery run for one provider.
func (c *ModelCatalog) Reconcile(ctx context.Context, providerID uuid.UUID, discovered []DiscoveredModel) (*ReconcileResult, error) {
	if c.store == nil {
		return &ReconcileResult{}, nil
	}
	current, err := c.store.ListByProvider(ctx, providerID)
	if err != nil {
		return nil, err
	}
	plan := PlanReconciliation(current, discovered)

	res := &ReconcileResult{}
	for i := range plan.ToUpsert {
		m := plan.ToUpsert[i]
		m.ProviderID = providerID
		existing, err := c.store.GetByProviderModel(ctx, providerID, m.ModelName)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			m.ID = existing.ID
			res.Updated++
		} else {
			res.Inserted++
		}
		if err := c.store.UpsertDiscovered(ctx, m); err != nil {
			return nil, err
		}
	}
	for _, id := range plan.ToDelete {
		if err := c.store.DeleteStale(ctx, id); err != nil {
			return nil, err
		}
		res.Deleted++
	}
	res.Preserved = len(plan.Preserved)
	return res, nil
}
