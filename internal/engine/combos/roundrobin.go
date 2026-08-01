package combos

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"gorouter/internal/domain/combo"
)

// RoundRobinEngine performs sticky round-robin selection. Rotation state is
// persisted through a StickyStore so selection survives restart; in-memory
// hot state is serialized under a mutex for race safety.
type RoundRobinEngine struct {
	store StickyStore
	mu    sync.Mutex
}

// NewRoundRobinEngine creates a round-robin engine over the given store.
func NewRoundRobinEngine(store StickyStore) *RoundRobinEngine {
	return &RoundRobinEngine{store: store}
}

// Select returns the next member in the sticky rotation, advancing and
// persisting the rotation checkpoint. stickyCount < 1 rotates every request.
// The rotation cursor advances only when the current stickiness window closes,
// so each member serves stickyCount consecutive requests.
func (e *RoundRobinEngine) Select(ctx context.Context, comboID uuid.UUID, members []combo.Member, stickyCount int) (*combo.Member, error) {
	active := combo.ActiveMembers(members)
	if len(active) == 0 {
		return nil, ErrNoEligibleMember
	}
	if stickyCount < 1 {
		stickyCount = 1
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	rot, err := e.store.GetRotation(ctx, comboID)
	if err != nil {
		return nil, fmt.Errorf("combos: get rotation: %w", err)
	}
	if rot == nil {
		rot = &Rotation{}
	}
	if rot.Index < 0 || rot.Index >= len(active) {
		rot.Index = 0
		rot.Stickiness = 0
	}

	selected := &active[rot.Index]
	next := Rotation{
		Index:     rot.Index,
		UpdatedAt: time.Now().UTC(),
	}
	if rot.Stickiness > 0 {
		next.Stickiness = rot.Stickiness - 1
	} else {
		next.Stickiness = stickyCount - 1
	}
	if next.Stickiness == 0 {
		next.Index = (rot.Index + 1) % len(active)
	}
	if err := e.store.SaveRotation(ctx, comboID, next); err != nil {
		return nil, fmt.Errorf("combos: save rotation: %w", err)
	}
	return selected, nil
}
