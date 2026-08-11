package combos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorouter/internal/app/tx"
	"gorouter/internal/domain/auth"
	"gorouter/internal/domain/pricing"

	"github.com/google/uuid"
)

// PriceOverride is an alias for pricing.PriceOverride.
type PriceOverride = pricing.PriceOverride

// PricingScope is the transaction-scoped persistence surface used by
// PricingService. *tx.TxScope satisfies it, so the service can be wired
// directly to TransactionManager.Begin output.
type PricingScope interface {
	Pricing() pricing.PricingRepository
	AuditLog() tx.AuditLogRepository
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

var _ PricingScope = (*tx.TxScope)(nil)

// PricingScopeBeginner begins a pricing transaction scope. It is an
// interface so tests can drive the service with recording scopes.
type PricingScopeBeginner interface {
	Begin(ctx context.Context) (PricingScope, error)
}

// PricingScopeBeginnerFunc adapts a function to PricingScopeBeginner.
type PricingScopeBeginnerFunc func(ctx context.Context) (PricingScope, error)

// Begin implements PricingScopeBeginner.
func (f PricingScopeBeginnerFunc) Begin(ctx context.Context) (PricingScope, error) { return f(ctx) }

// errPricingActorRequired reports pricing mutations without an actor.
var errPricingActorRequired = errors.New("combos: actor is required for pricing overrides")

// PricingService applies model pricing overrides. Every mutation writes the
// materialized override set and an append-only audit entry (actor, target,
// sanitized before/after diff, result) in one transaction (audit/02); the
// audit series is immutable — there is no update or delete path.
type PricingService struct {
	beginner PricingScopeBeginner
}

// NewPricingService creates a PricingService over the given scope beginner.
func NewPricingService(beginner PricingScopeBeginner) *PricingService {
	return &PricingService{beginner: beginner}
}

// ApplyOverride validates and applies a pricing override, replacing any
// previous override for the same (provider, model) key in the materialized
// set. The caller's override value is never mutated; the returned copy
// carries the stamped actor and timestamp. History is preserved by the
// append-only audit entry written in the same transaction.
func (s *PricingService) ApplyOverride(ctx context.Context, actor *auth.Actor, override pricing.PriceOverride) (pricing.PriceOverride, error) {
	if actor == nil {
		return pricing.PriceOverride{}, errPricingActorRequired
	}
	if err := override.Validate(); err != nil {
		return pricing.PriceOverride{}, err
	}

	applied := override
	applied.UpdatedBy = actor.UserID
	applied.UpdatedAt = time.Now().UTC()

	scope, err := s.beginner.Begin(ctx)
	if err != nil {
		return pricing.PriceOverride{}, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = scope.Rollback(ctx) }()

	current, err := scope.Pricing().LoadOverrides(ctx)
	if err != nil {
		return pricing.PriceOverride{}, fmt.Errorf("load overrides: %w", err)
	}
	var before *pricing.PriceOverride
	set := make([]pricing.PriceOverride, 0, len(current)+1)
	for _, o := range current {
		if o.Key() == applied.Key() {
			cp := o
			before = &cp
			continue
		}
		set = append(set, o)
	}
	set = append(set, applied)

	if err := scope.Pricing().SaveOverrides(ctx, set); err != nil {
		return pricing.PriceOverride{}, fmt.Errorf("save overrides: %w", err)
	}
	if err := writePricingAudit(ctx, scope.AuditLog(), actor, before, &applied); err != nil {
		return pricing.PriceOverride{}, fmt.Errorf("audit pricing override: %w", err)
	}
	if err := scope.Commit(ctx); err != nil {
		return pricing.PriceOverride{}, fmt.Errorf("commit tx: %w", err)
	}
	return applied, nil
}

// ListOverrides returns the current override set in deterministic order
// (provider, then model).
func (s *PricingService) ListOverrides(ctx context.Context) ([]pricing.PriceOverride, error) {
	scope, err := s.beginner.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = scope.Rollback(ctx) }()
	list, err := scope.Pricing().LoadOverrides(ctx)
	if err != nil {
		return nil, fmt.Errorf("load overrides: %w", err)
	}
	pricing.SortOverrides(list)
	return list, nil
}

// pricingDiff is the sanitized before/after projection of an override:
// identity and prices only, no credentials or internal fields.
func pricingDiff(o pricing.PriceOverride) map[string]any {
	return map[string]any{
		"model_id":     o.ModelID,
		"provider_id":  o.ProviderID,
		"input_price":  o.InputPrice,
		"output_price": o.OutputPrice,
	}
}

// writePricingAudit appends the immutable audit entry for an override
// application. before is nil for a first-time override; the before diff is
// then omitted from the details.
func writePricingAudit(ctx context.Context, log tx.AuditLogRepository, actor *auth.Actor, before, after *pricing.PriceOverride) error {
	details := map[string]any{
		"target": map[string]string{
			"provider_id": after.ProviderID,
			"model_id":    after.ModelID,
		},
		"result": "applied",
	}
	if before != nil {
		details["before"] = pricingDiff(*before)
	}
	details["after"] = pricingDiff(*after)
	raw, err := json.Marshal(details)
	if err != nil {
		return err
	}
	actorID := actor.UserID
	entry := &tx.AuditLogEntry{
		ID:           uuid.New(),
		ActorID:      &actorID,
		Action:       "pricing.override.apply",
		ResourceType: "pricing_override",
		Details:      raw,
		OccurredAt:   time.Now().UTC(),
	}
	return log.Create(ctx, entry)
}
