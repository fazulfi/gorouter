// Package routing provides concrete implementations of domain routing interfaces.
// It implements modelref.Resolver and provider.AccountSelector using
// PostgreSQL-backed repositories, satisfying the architecture's application-layer
// contract: transport → app → domain.
package routing

import (
	"context"
	"fmt"

	"gorouter/internal/app/tx"
	"gorouter/internal/domain/modelref"
	"gorouter/internal/domain/provider"
)

// Resolver implements modelref.Resolver by looking up providers and models
// from the database via a TxScope-scoped repository set.
type Resolver struct {
	txProvider func(ctx context.Context) (*tx.TxScope, error)
}

// NewResolver creates a Resolver that uses the given function to obtain a
// transaction-scoped repository set. This indirection lets the caller control
// transaction lifecycle without coupling Resolver to a specific pool type.
func NewResolver(txProvider func(ctx context.Context) (*tx.TxScope, error)) *Resolver {
	return &Resolver{txProvider: txProvider}
}

// Resolve resolves a parsed ModelRef against the persisted provider and model
// configuration. Resolution follows the order defined in the domain interface:
//
//  1. If the provider is explicitly named, look it up by name/type.
//  2. If no model is specified, use the provider's default model.
//  3. Validate that the resolved model supports the requested capability.
func (r *Resolver) Resolve(ctx context.Context, ref modelref.ModelRef) (*modelref.ResolvedRef, error) {
	if r.txProvider == nil {
		return nil, fmt.Errorf("%w: resolver database provider not configured", modelref.ErrProviderNotFound)
	}

	scope, err := r.txProvider(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolver: begin tx: %w", err)
	}
	defer scope.Rollback(ctx)

	providers := scope.Providers()

	// 1. Resolve the provider.
	var prov *provider.Provider
	if ref.Provider != "" {
		// Look up by name or type.
		all, err := providers.List(ctx)
		if err != nil {
			return nil, fmt.Errorf("resolver: list providers: %w", err)
		}
		for i := range all {
			if all[i].Name == ref.Provider || string(all[i].Type) == ref.Provider {
				p := all[i]
				prov = &p
				break
			}
		}
		if prov == nil {
			return nil, fmt.Errorf("%w: provider %q not found", modelref.ErrProviderNotFound, ref.Provider)
		}
	} else {
		// No explicit provider — use the first enabled provider.
		all, err := providers.List(ctx)
		if err != nil {
			return nil, fmt.Errorf("resolver: list providers: %w", err)
		}
		for i := range all {
			if all[i].IsEnabled {
				p := all[i]
				prov = &p
				break
			}
		}
		if prov == nil {
			return nil, fmt.Errorf("%w: no enabled providers found", modelref.ErrProviderNotFound)
		}
	}

	// 2. Resolve the model name.
	modelName := ref.Model
	if modelName == "" {
		// Try to get the provider's default model from the model catalog.
		models, err := scope.Models().GetModelsByProvider(ctx, prov.ID)
		if err == nil && len(models) > 0 {
			modelName = models[0].ModelName
		}
		if modelName == "" {
			modelName = "gpt-4" // fallback default
		}
	}

	if ref.Capability != nil && *ref.Capability != "" {
		models, err := scope.Models().GetModelsByProvider(ctx, prov.ID)
		if err == nil {
			found := false
			for _, m := range models {
				if m.ModelName == modelName {
					for _, cap := range m.Capabilities {
						if cap == *ref.Capability {
							found = true
							break
						}
					}
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("%w: model %q does not support capability %q",
					modelref.ErrCapNotSupported, modelName, *ref.Capability)
			}
		}
	}

	return &modelref.ResolvedRef{
		Ref:        ref,
		ProviderID: prov.ID,
		ModelName:  modelName,
		Provider:   prov,
	}, nil
}
