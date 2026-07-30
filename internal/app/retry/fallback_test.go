package retry

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"

	"gorouter/internal/domain/provider"
)

// mockCooldownRegistry implements CooldownRegistry for testing.
type mockCooldownRegistry struct {
	mu         sync.Mutex
	onCooldown map[uuid.UUID]bool
	failures   []uuid.UUID
	successes  []uuid.UUID
}

func newMockCooldownRegistry() *mockCooldownRegistry {
	return &mockCooldownRegistry{
		onCooldown: make(map[uuid.UUID]bool),
	}
}

func (m *mockCooldownRegistry) IsOnCooldown(_ context.Context, id uuid.UUID) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.onCooldown[id]
}

func (m *mockCooldownRegistry) RecordFailure(_ context.Context, id uuid.UUID, _ error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failures = append(m.failures, id)
}

func (m *mockCooldownRegistry) RecordSuccess(_ context.Context, id uuid.UUID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.successes = append(m.successes, id)
}

func TestNextAccountSelectsByPriority(t *testing.T) {
	ctx := context.Background()
	cooldown := newMockCooldownRegistry()
	sel := NewFallbackSelector(nil, cooldown)

	accounts := []provider.Account{
		{ID: uuid.MustParse("10000000-0000-0000-0000-000000000001"), Priority: 3, IsEnabled: true},
		{ID: uuid.MustParse("10000000-0000-0000-0000-000000000002"), Priority: 1, IsEnabled: true},
		{ID: uuid.MustParse("10000000-0000-0000-0000-000000000003"), Priority: 2, IsEnabled: true},
	}

	currentID := uuid.MustParse("10000000-0000-0000-0000-000000000001")
	next := sel.NextAccount(ctx, accounts, currentID)
	if next == nil {
		t.Fatal("expected a fallback account, got nil")
	}
	if next.Priority != 1 {
		t.Errorf("expected priority 1 (lowest number), got %d", next.Priority)
	}
}

func TestNextAccountSkipsCurrent(t *testing.T) {
	ctx := context.Background()
	cooldown := newMockCooldownRegistry()
	sel := NewFallbackSelector(nil, cooldown)

	currentID := uuid.MustParse("20000000-0000-0000-0000-000000000001")
	accounts := []provider.Account{
		{ID: currentID, Priority: 1, IsEnabled: true},
		{ID: uuid.MustParse("20000000-0000-0000-0000-000000000002"), Priority: 2, IsEnabled: true},
	}

	next := sel.NextAccount(ctx, accounts, currentID)
	if next == nil {
		t.Fatal("expected a fallback account, got nil")
	}
	if next.ID == currentID {
		t.Error("NextAccount returned the current (failed) account")
	}
}

func TestNextAccountSkipsCooldown(t *testing.T) {
	ctx := context.Background()
	cooldown := newMockCooldownRegistry()

	onCooldownID := uuid.MustParse("30000000-0000-0000-0000-000000000001")
	availableID := uuid.MustParse("30000000-0000-0000-0000-000000000002")
	cooldown.onCooldown[onCooldownID] = true

	sel := NewFallbackSelector(nil, cooldown)
	accounts := []provider.Account{
		{ID: onCooldownID, Priority: 1, IsEnabled: true},
		{ID: availableID, Priority: 2, IsEnabled: true},
	}

	currentID := uuid.MustParse("00000000-0000-0000-0000-000000000000")
	next := sel.NextAccount(ctx, accounts, currentID)
	if next == nil {
		t.Fatal("expected a fallback account, got nil")
	}
	if next.ID != availableID {
		t.Errorf("expected available account, got %v", next.ID)
	}
}

func TestNextAccountSkipsDisabled(t *testing.T) {
	ctx := context.Background()
	cooldown := newMockCooldownRegistry()
	sel := NewFallbackSelector(nil, cooldown)

	disabledID := uuid.MustParse("40000000-0000-0000-0000-000000000001")
	enabledID := uuid.MustParse("40000000-0000-0000-0000-000000000002")

	accounts := []provider.Account{
		{ID: disabledID, Priority: 1, IsEnabled: false},
		{ID: enabledID, Priority: 2, IsEnabled: true},
	}

	currentID := uuid.MustParse("00000000-0000-0000-0000-000000000000")
	next := sel.NextAccount(ctx, accounts, currentID)
	if next == nil {
		t.Fatal("expected a fallback account, got nil")
	}
	if next.ID != enabledID {
		t.Errorf("expected enabled account, got %v", next.ID)
	}
}

func TestNextAccountEmptyReturnsNil(t *testing.T) {
	ctx := context.Background()
	cooldown := newMockCooldownRegistry()
	sel := NewFallbackSelector(nil, cooldown)

	next := sel.NextAccount(ctx, nil, uuid.Nil)
	if next != nil {
		t.Errorf("expected nil for empty accounts, got %v", next.ID)
	}
}

func TestNextAccountAllOnCooldownReturnsNil(t *testing.T) {
	ctx := context.Background()
	cooldown := newMockCooldownRegistry()

	id1 := uuid.MustParse("50000000-0000-0000-0000-000000000001")
	id2 := uuid.MustParse("50000000-0000-0000-0000-000000000002")
	cooldown.onCooldown[id1] = true
	cooldown.onCooldown[id2] = true

	sel := NewFallbackSelector(nil, cooldown)
	accounts := []provider.Account{
		{ID: id1, Priority: 1, IsEnabled: true},
		{ID: id2, Priority: 2, IsEnabled: true},
	}

	next := sel.NextAccount(ctx, accounts, uuid.Nil)
	if next != nil {
		t.Errorf("expected nil when all accounts on cooldown, got %v", next.ID)
	}
}

func TestNextAccountAllDisabledReturnsNil(t *testing.T) {
	ctx := context.Background()
	cooldown := newMockCooldownRegistry()
	sel := NewFallbackSelector(nil, cooldown)

	accounts := []provider.Account{
		{ID: uuid.MustParse("60000000-0000-0000-0000-000000000001"), Priority: 1, IsEnabled: false},
		{ID: uuid.MustParse("60000000-0000-0000-0000-000000000002"), Priority: 2, IsEnabled: false},
	}

	next := sel.NextAccount(ctx, accounts, uuid.Nil)
	if next != nil {
		t.Errorf("expected nil when all accounts disabled, got %v", next.ID)
	}
}

func TestNextAccountOnlyCurrentReturnsNil(t *testing.T) {
	ctx := context.Background()
	cooldown := newMockCooldownRegistry()
	sel := NewFallbackSelector(nil, cooldown)

	currentID := uuid.MustParse("70000000-0000-0000-0000-000000000001")
	accounts := []provider.Account{
		{ID: currentID, Priority: 1, IsEnabled: true},
	}

	next := sel.NextAccount(ctx, accounts, currentID)
	if next != nil {
		t.Errorf("expected nil when only the current account exists, got %v", next.ID)
	}
}

func TestNextAccountNilCooldownRegistry(t *testing.T) {
	ctx := context.Background()
	sel := NewFallbackSelector(nil, nil)

	id1 := uuid.MustParse("80000000-0000-0000-0000-000000000001")
	id2 := uuid.MustParse("80000000-0000-0000-0000-000000000002")

	accounts := []provider.Account{
		{ID: id1, Priority: 2, IsEnabled: true},
		{ID: id2, Priority: 1, IsEnabled: true},
	}

	next := sel.NextAccount(ctx, accounts, id1)
	if next == nil {
		t.Fatal("expected fallback even with nil cooldown")
	}
	if next.ID != id2 {
		t.Errorf("expected id2 (priority 1), got %v", next.ID)
	}
}

func TestNextAccountPreservesPriorityOrder(t *testing.T) {
	ctx := context.Background()
	cooldown := newMockCooldownRegistry()
	sel := NewFallbackSelector(nil, cooldown)

	accounts := []provider.Account{
		{ID: uuid.MustParse("90000000-0000-0000-0000-000000000001"), Priority: 10, IsEnabled: true},
		{ID: uuid.MustParse("90000000-0000-0000-0000-000000000002"), Priority: 5, IsEnabled: true},
		{ID: uuid.MustParse("90000000-0000-0000-0000-000000000003"), Priority: 1, IsEnabled: true},
	}

	next := sel.NextAccount(ctx, accounts, uuid.Nil)
	if next == nil {
		t.Fatal("expected a fallback account")
	}
	if next.Priority != 1 {
		t.Errorf("expected priority 1 (best), got %d", next.Priority)
	}
}

func TestCooldownRegistryRecords(t *testing.T) {
	cooldown := newMockCooldownRegistry()
	id := uuid.MustParse("a0000000-0000-0000-0000-000000000001")

	cooldown.RecordFailure(context.Background(), id, nil)
	cooldown.RecordSuccess(context.Background(), id)

	if len(cooldown.failures) != 1 {
		t.Errorf("expected 1 failure record, got %d", len(cooldown.failures))
	}
	if len(cooldown.successes) != 1 {
		t.Errorf("expected 1 success record, got %d", len(cooldown.successes))
	}
}
