package combos

import (
	"context"

	"gorouter/internal/domain/combo"
)

// AutoSwitchEngine selects a combo member whose model satisfies the request
// capabilities. Single-model requests (no capabilities) keep the configured
// order unchanged; when no member is compatible the configured fallback order
// is retained instead of dropping candidates.
type AutoSwitchEngine struct{}

// NewAutoSwitchEngine creates a capability auto-switch engine.
func NewAutoSwitchEngine() *AutoSwitchEngine { return &AutoSwitchEngine{} }

// Select returns the first active member supporting ALL requested
// capabilities. With no capabilities it returns the first active member.
// A catalog error for one member falls back to the configured order rather
// than failing the request.
func (e *AutoSwitchEngine) Select(ctx context.Context, members []combo.Member, capabilities []string, catalog CapabilityCatalog) (*combo.Member, error) {
	active := combo.ActiveMembers(members)
	if len(active) == 0 {
		return nil, ErrNoEligibleMember
	}
	if len(capabilities) == 0 {
		return &active[0], nil
	}
	for i := range active {
		ok, err := catalog.Supports(ctx, active[i], capabilities)
		if err != nil {
			continue
		}
		if ok {
			return &active[i], nil
		}
	}
	return &active[0], nil
}
