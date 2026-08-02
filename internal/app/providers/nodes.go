package providers

import (
	"context"
	"fmt"

	"gorouter/internal/domain/auth"
	enginerouting "gorouter/internal/engine/routing"

	"github.com/rs/zerolog"
)

// NodeService manages provider nodes by delegating to the routing NodeStore
// (the PostgreSQL-backed store in production). Mutations audit with the
// acting user.
type NodeService struct {
	beginner ScopeBeginner
	logger   zerolog.Logger
}

// NewNodeService creates a NodeService over the given scope beginner.
func NewNodeService(beginner ScopeBeginner, logger zerolog.Logger) *NodeService {
	return &NodeService{beginner: beginner, logger: logger}
}

// List returns all provider nodes from the store.
func (s *NodeService) List(ctx context.Context) ([]enginerouting.ProviderNode, error) {
	scope, err := s.beginner.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = scope.Rollback(ctx) }()
	nodes, err := scope.Nodes().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	return nodes, nil
}

// Save upserts a provider node and audits the mutation with the acting user.
func (s *NodeService) Save(ctx context.Context, actor *auth.Actor, node enginerouting.ProviderNode) error {
	if err := requireActor(actor); err != nil {
		return err
	}
	scope, err := s.beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = scope.Rollback(ctx) }()
	if err := scope.Nodes().Save(ctx, node); err != nil {
		return fmt.Errorf("save node: %w", err)
	}
	details := map[string]any{
		"name":      node.Name,
		"base_url":  node.BaseURL,
		"region":    node.Region,
		"priority":  node.Priority,
		"is_active": node.IsActive,
	}
	if err := writeAudit(ctx, scope.AuditLog(), actor, "node.save", "provider_node", uuidOrNil(node.ID), details); err != nil {
		return fmt.Errorf("audit node save: %w", err)
	}
	return scope.Commit(ctx)
}

// Delete removes a provider node and audits the mutation with the acting user.
func (s *NodeService) Delete(ctx context.Context, actor *auth.Actor, id string) error {
	if err := requireActor(actor); err != nil {
		return err
	}
	scope, err := s.beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = scope.Rollback(ctx) }()
	if err := scope.Nodes().Delete(ctx, id); err != nil {
		return fmt.Errorf("delete node: %w", err)
	}
	if err := writeAudit(ctx, scope.AuditLog(), actor, "node.delete", "provider_node", uuidOrNil(id), map[string]any{"id": id}); err != nil {
		return fmt.Errorf("audit node delete: %w", err)
	}
	return scope.Commit(ctx)
}
