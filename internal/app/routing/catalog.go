package routing

import (
	"context"
	"time"

	"github.com/google/uuid"

	enginerouting "gorouter/internal/engine/routing"
)

// CatalogService is the application-layer model catalog use-case: listing,
// discovery reconciliation (decision #192), and custom/disabled model
// management.
type CatalogService struct {
	store   enginerouting.CatalogStore
	catalog *enginerouting.ModelCatalog
}

// NewCatalogService creates a CatalogService over the given store.
func NewCatalogService(store enginerouting.CatalogStore) *CatalogService {
	return &CatalogService{
		store:   store,
		catalog: enginerouting.NewModelCatalog(store),
	}
}

// List returns the full catalog in stable order.
func (s *CatalogService) List(ctx context.Context) ([]enginerouting.CatalogModel, error) {
	return s.catalog.List(ctx)
}

// Get returns one catalog model by id.
func (s *CatalogService) Get(ctx context.Context, id uuid.UUID) (*enginerouting.CatalogModel, error) {
	all, err := s.catalog.List(ctx)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].ID == id {
			m := all[i]
			return &m, nil
		}
	}
	return nil, enginerouting.ErrModelNotFound
}

// Reconcile applies a discovery run for one provider. Custom models,
// admin-disabled models, and aliases are never deleted (decision #192).
func (s *CatalogService) Reconcile(ctx context.Context, providerID uuid.UUID, discovered []enginerouting.DiscoveredModel) (*enginerouting.ReconcileResult, error) {
	return s.catalog.Reconcile(ctx, providerID, discovered)
}

// SetModelEnabled toggles the admin enabled state of a catalog model.
func (s *CatalogService) SetModelEnabled(ctx context.Context, id uuid.UUID, enabled bool) error {
	if s.store == nil {
		return enginerouting.ErrModelNotFound
	}
	return s.store.SetEnabled(ctx, id, enabled)
}

// CreateCustomModel creates an admin-owned model (is_builtin=false). It is
// preserved by discovery until the admin deletes it (decision #192).
func (s *CatalogService) CreateCustomModel(ctx context.Context, providerID uuid.UUID, modelName string, capabilities []string, maxTokens int64) (*enginerouting.CatalogModel, error) {
	if s.store == nil {
		return nil, enginerouting.ErrModelNotFound
	}
	now := time.Now().UTC()
	m := &enginerouting.CatalogModel{
		ID:           uuid.New(),
		ProviderID:   providerID,
		ModelName:    modelName,
		Capabilities: capabilities,
		MaxTokens:    maxTokens,
		IsBuiltin:    false,
		IsEnabled:    true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.store.InsertCustom(ctx, m); err != nil {
		return nil, err
	}
	return m, nil
}

// DeleteModel explicitly removes a catalog model (admin action; the only
// deletion path for custom models, decision #192).
func (s *CatalogService) DeleteModel(ctx context.Context, id uuid.UUID) error {
	if s.store == nil {
		return enginerouting.ErrModelNotFound
	}
	return s.store.Delete(ctx, id)
}
