// Package routing provides application-layer routing services: alias
// management, model resolution (alias + catalog), and provider nodes.
package routing

import (
	"context"

	"github.com/google/uuid"

	"gorouter/internal/domain/provider"
	enginerouting "gorouter/internal/engine/routing"
)

// Service is the application-layer routing use-case: alias CRUD, model
// resolution through the upstream resolution order, and provider-node
// management.
type Service struct {
	aliases   enginerouting.AliasRepository
	resolver  *enginerouting.AliasResolver
	catalog   *enginerouting.ModelCatalog
	nodes     *enginerouting.NodeManager
	nodeStore enginerouting.NodeStore
	providers provider.ProviderRepository
}

// NewService creates the routing service. All dependencies are optional;
// nil stores disable the corresponding capability.
func NewService(
	aliases enginerouting.AliasRepository,
	catalogStore enginerouting.CatalogStore,
	nodeStore enginerouting.NodeStore,
	providers provider.ProviderRepository,
) *Service {
	s := &Service{
		aliases:   aliases,
		providers: providers,
	}
	s.resolver = enginerouting.NewAliasResolver(aliases, enginerouting.NewProviderRepoAliasSource(providers))
	if catalogStore != nil {
		s.catalog = enginerouting.NewModelCatalog(catalogStore)
	}
	if nodeStore != nil {
		s.nodes = enginerouting.NewNodeManager(nodeStore)
		s.nodeStore = nodeStore
	}
	return s
}

// CreateAlias creates a new model alias. The name must be slashless
// (decision #31).
func (s *Service) CreateAlias(ctx context.Context, alias, target string, providerID *uuid.UUID) (*enginerouting.Alias, error) {
	if err := enginerouting.ValidateAlias(alias); err != nil {
		return nil, err
	}
	if s.aliases == nil {
		return nil, enginerouting.ErrInvalidAlias
	}
	a := &enginerouting.Alias{
		ID:         uuid.New(),
		Alias:      alias,
		Target:     target,
		ProviderID: providerID,
		IsActive:   true,
	}
	if err := s.aliases.Create(ctx, a); err != nil {
		return nil, err
	}
	return a, nil
}

// UpdateAlias updates an existing alias.
func (s *Service) UpdateAlias(ctx context.Context, a *enginerouting.Alias) error {
	if s.aliases == nil {
		return enginerouting.ErrAliasNotFound
	}
	return s.aliases.Update(ctx, a)
}

// DeleteAlias deletes an alias by id.
func (s *Service) DeleteAlias(ctx context.Context, id uuid.UUID) error {
	if s.aliases == nil {
		return enginerouting.ErrAliasNotFound
	}
	return s.aliases.Delete(ctx, id)
}

// SetAliasActive enables or disables an alias (schema is_active).
func (s *Service) SetAliasActive(ctx context.Context, id uuid.UUID, active bool) error {
	if s.aliases == nil {
		return enginerouting.ErrAliasNotFound
	}
	return s.aliases.SetActive(ctx, id, active)
}

// GetAliasByName returns an alias by name.
func (s *Service) GetAliasByName(ctx context.Context, name string) (*enginerouting.Alias, error) {
	if s.aliases == nil {
		return nil, enginerouting.ErrAliasNotFound
	}
	a, err := s.aliases.FindByAlias(ctx, name)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, enginerouting.ErrAliasNotFound
	}
	return a, nil
}

// ListAliases returns all aliases in stable original order.
func (s *Service) ListAliases(ctx context.Context) ([]enginerouting.Alias, error) {
	if s.aliases == nil {
		return nil, nil
	}
	return s.aliases.List(ctx)
}

// ResolvedModelResult is the outcome of Service.ResolveModel.
type ResolvedModelResult struct {
	enginerouting.ResolvedModel
	CatalogModel *enginerouting.CatalogModel
}

// ResolveModel resolves a model string through the upstream order (alias →
// built-in → inference) and validates the resolved model against the catalog
// when the provider is known.
func (s *Service) ResolveModel(ctx context.Context, modelStr string) (*ResolvedModelResult, error) {
	res, err := s.resolver.ResolveModel(ctx, modelStr)
	if err != nil {
		return nil, err
	}
	out := &ResolvedModelResult{ResolvedModel: *res}

	var providerID *uuid.UUID
	if res.ProviderID != nil {
		providerID = res.ProviderID
	} else if res.Provider != "" {
		providerID = s.findProviderID(ctx, res.Provider)
	}
	if providerID == nil || s.catalog == nil {
		return out, nil
	}
	m, err := s.catalog.ResolveEnabled(ctx, *providerID, res.Model)
	if err != nil {
		return nil, err
	}
	out.CatalogModel = m
	return out, nil
}

// findProviderID maps a provider name/type to a configured provider id.
func (s *Service) findProviderID(ctx context.Context, nameOrType string) *uuid.UUID {
	if s.providers == nil {
		return nil
	}
	all, err := s.providers.List(ctx)
	if err != nil {
		return nil
	}
	for i := range all {
		if all[i].Name == nameOrType || string(all[i].Type) == nameOrType {
			return &all[i].ID
		}
	}
	return nil
}

// ListNodes returns all provider nodes (defaults merged with runtime).
func (s *Service) ListNodes(ctx context.Context) ([]enginerouting.ProviderNode, error) {
	if s.nodeStore == nil {
		return nil, nil
	}
	var defaults []enginerouting.ProviderNode
	if s.providers != nil {
		all, err := s.providers.List(ctx)
		if err != nil {
			return nil, err
		}
		defaults = enginerouting.DefaultNodesFromProviders(all)
	}
	return enginerouting.MergeDefaultAndRuntime(defaults, s.nodeStore), nil
}

// SaveNode upserts a runtime provider node.
func (s *Service) SaveNode(ctx context.Context, node enginerouting.ProviderNode) error {
	if s.nodes == nil {
		return enginerouting.ErrNoProviderNode
	}
	return s.nodes.Save(ctx, node)
}

// DeleteNode removes a runtime provider node.
func (s *Service) DeleteNode(ctx context.Context, id string) error {
	if s.nodes == nil {
		return enginerouting.ErrNoProviderNode
	}
	return s.nodes.Delete(ctx, id)
}

// SelectNode picks the best node for a provider, preferring the region.
func (s *Service) SelectNode(ctx context.Context, providerID uuid.UUID, preferRegion string) (*enginerouting.ProviderNode, error) {
	if s.nodes == nil {
		return nil, enginerouting.ErrNoProviderNode
	}
	return s.nodes.SelectNode(ctx, providerID, preferRegion)
}
