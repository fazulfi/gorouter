package cooldown

import (
	"context"

	"github.com/google/uuid"

	"gorouter/internal/domain/provider"
)

// CooldownAuthority is the sole authority for cooldown timing and eligibility
// decisions. All cooldown policy (threshold, escalation, max duration) lives
// in implementations of this interface — never in routing state managers,
// engine selectors, or fallback handlers.
//
// The *Registry type in this package implements CooldownAuthority, making it
// the canonical production implementation.
type CooldownAuthority interface {
	// IsOnCooldown checks whether the account is currently cooled down.
	IsOnCooldown(ctx context.Context, accountID uuid.UUID) bool

	// RecordFailure records a failure and updates cooldown state.
	RecordFailure(ctx context.Context, accountID uuid.UUID, err error)

	// RecordSuccess records a success and clears cooldown state.
	RecordSuccess(ctx context.Context, accountID uuid.UUID)

	// Status returns the current cooldown state for the account, or nil
	// if the account has no entry in the registry.
	Status(ctx context.Context, accountID uuid.UUID) *provider.AccountCooldown
}
