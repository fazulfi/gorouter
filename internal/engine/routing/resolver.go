package routing

import (
	"context"
	"time"

	"github.com/google/uuid"

	"gorouter/internal/domain/provider"
)

// ResolverConfig controls routing resolver behaviour.
type ResolverConfig struct {
	// DefaultModel is used when no model is specified. Default: "gpt-4".
	DefaultModel string

	// MaxFallbackAttempts is the maximum number of same-model fallback attempts.
	MaxFallbackAttempts int

	// FallbackWait is the initial wait before attempting a fallback.
	FallbackWait time.Duration

	// AttemptTimeout is the per-attempt timeout.
	AttemptTimeout time.Duration
}

// DefaultResolverConfig returns defaults for the routing resolver.
func DefaultResolverConfig() ResolverConfig {
	return ResolverConfig{
		DefaultModel:        "gpt-4",
		MaxFallbackAttempts: 3,
		FallbackWait:        10 * time.Second,
		AttemptTimeout:      30 * time.Second,
	}
}

// Resolver is the authority-correct routing resolver.
// It resolves model references, selects accounts, manages fallback,
// and coordinates with the state manager for cooldown/locks.
type Resolver struct {
	config     ResolverConfig
	providers  provider.ProviderRepository
	models     ModelRepository
	selector   *AccountSelector
	fallback   *FallbackHandler
	state      *RoutingStateManager
	checkpoint Checkpointer
}

// ModelRepository defines the model lookup interface needed by the resolver.
type ModelRepository interface {
	GetModelsByProvider(ctx context.Context, providerID uuid.UUID) ([]ModelInfo, error)
}

// ModelInfo represents basic model metadata for routing decisions.
type ModelInfo struct {
	ModelName    string
	Capabilities []string
	IsEnabled    bool
}

// NewResolver creates the engine-level routing resolver.
func NewResolver(
	config ResolverConfig,
	providers provider.ProviderRepository,
	models ModelRepository,
	selector *AccountSelector,
	fallback *FallbackHandler,
	state *RoutingStateManager,
	checkpoint Checkpointer,
) *Resolver {
	if config.DefaultModel == "" {
		config.DefaultModel = "gpt-4"
	}
	return &Resolver{
		config:     config,
		providers:  providers,
		models:     models,
		selector:   selector,
		fallback:   fallback,
		state:      state,
		checkpoint: checkpoint,
	}
}

// ResolveModel resolves a model string to a provider and account, handling
// account selection and fallback. It returns the selected provider, account,
// model name, and any remaining eligible account candidates.
func (r *Resolver) ResolveModel(ctx context.Context, model string) (*provider.Provider, *provider.Account, string, []provider.Account, error) {
	providers, err := r.providers.List(ctx)
	if err != nil {
		return nil, nil, "", nil, err
	}

	// Find the first enabled provider that has eligible accounts.
	for i := range providers {
		p := providers[i]
		if !p.IsEnabled {
			continue
		}

		best, candidates, err := r.selector.SelectAccount(ctx, p.ID, model)
		if err != nil {
			continue
		}

		return &p, best, model, candidates, nil
	}

	return nil, nil, "", nil, ErrNoEligibleAccount
}

// ResolveProviderModel resolves a provider+model combination to an account.
func (r *Resolver) ResolveProviderModel(ctx context.Context, providerID uuid.UUID, model string) (*provider.Account, []provider.Account, error) {
	return r.selector.SelectAccount(ctx, providerID, model)
}

// SelectAccounts returns all eligible accounts for a provider+model combination.
func (r *Resolver) SelectAccounts(ctx context.Context, providerID uuid.UUID, model string) ([]provider.Account, error) {
	_, candidates, err := r.selector.SelectAccount(ctx, providerID, model)
	return candidates, err
}

// EvaluateFallback determines whether and how to fall back.
func (r *Resolver) EvaluateFallback(
	attempt int,
	class FailureClass,
	accounts []provider.Account,
	currentID uuid.UUID,
) *FallbackDecision {
	return r.fallback.EvaluateFallback(context.Background(), attempt, class, accounts, currentID)
}

// RecordFailure records a failure and returns the updated account state.
func (r *Resolver) RecordFailure(accountID uuid.UUID, class FailureClass, httpStatus int, reason string, err error) *AccountState {
	return r.state.RecordFailure(accountID, class, httpStatus, reason, err)
}

// RecordSuccess records a success for an account.
func (r *Resolver) RecordSuccess(accountID uuid.UUID) {
	r.state.RecordSuccess(accountID)
}

// Checkpoint persists the current routing state.
func (r *Resolver) Checkpoint(ctx context.Context) error {
	if r.checkpoint == nil {
		return nil
	}
	snapshot := r.state.Checkpoint()
	return r.checkpoint.SaveCheckpoint(ctx, snapshot)
}

// Restore restores routing state from a checkpoint.
func (r *Resolver) Restore(ctx context.Context) error {
	if r.checkpoint == nil {
		return nil
	}
	snapshot, err := r.checkpoint.LoadCheckpoint(ctx)
	if err != nil {
		return err
	}
	if len(snapshot) > 0 {
		r.state.Restore(snapshot)
	}
	return nil
}

// StateManager returns the underlying state manager.
func (r *Resolver) StateManager() *RoutingStateManager {
	return r.state
}

// AccountSelectorResult holds the result of an account selection.
type AccountSelectorResult struct {
	Best       *provider.Account
	Candidates []provider.Account
}

// selectAccounts wraps AccountSelector.SelectAccount for convenience.
func (s *AccountSelector) selectAccounts(ctx context.Context, providerID uuid.UUID, model string) (*AccountSelectorResult, error) {
	best, candidates, err := s.SelectAccount(ctx, providerID, model)
	if err != nil {
		return nil, err
	}
	return &AccountSelectorResult{Best: best, Candidates: candidates}, nil
}
