package modelref

import (
	"context"

	"github.com/google/uuid"
	"gorouter/internal/domain/provider"
)

// ResolvedRef is the concrete resolution of a ModelRef after the resolver has
// looked up the provider, account, model, and capability against the
// configured state.
type ResolvedRef struct {
	// Ref is the parsed model reference that was resolved.
	Ref ModelRef

	// ProviderID is the database ID of the resolved provider.
	ProviderID uuid.UUID

	// AccountID is the optional account ID selected for this reference. When
	// nil the resolver chose the highest-priority non-cooldown account or no
	// account was required.
	AccountID *uuid.UUID

	// ModelName is the resolved model identifier (e.g. "gpt-4", "claude-3-opus").
	ModelName string

	// Capability is the resolved capability selector, if any.
	Capability *string

	// Provider is the full provider configuration for the resolved provider.
	Provider *provider.Provider
}

// Resolver resolves a parsed ModelRef into a ResolvedRef by looking up
// provider, account, model, and capability against the system configuration.
//
// Resolution order:
//  1. If the provider is explicitly named in the reference, look it up by
//     name or type. If unqualified, infer the provider from context (e.g.
//     default provider, or the provider associated with the calling API key).
//  2. If an account is specified in the reference, select that account.
//  3. If no account is specified, select the highest-priority non-cooldown
//     account for the provider.
//  4. If no model is specified in the reference, use the provider's default
//     model or infer from context (e.g. a model key's configured model).
//  5. Validate that the resolved model supports the requested capability
//     (if any). Return ErrCapNotSupported if it does not.
type Resolver interface {
	// Resolve takes a parsed ModelRef and returns the fully resolved concrete
	// reference, or an error if any step of the resolution fails.
	Resolve(ctx context.Context, ref ModelRef) (*ResolvedRef, error)
}
