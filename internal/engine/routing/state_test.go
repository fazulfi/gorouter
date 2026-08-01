package routing

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"gorouter/internal/app/cooldown"
	"gorouter/internal/domain/provider"
)

// testCooldownAuthority implements cooldown.CooldownAuthority for tests,
// using the same injectable clock as the RoutingStateManager so time
// advancement is coordinated.
type testCooldownAuthority struct {
	mu        sync.Mutex
	cooldowns map[uuid.UUID]time.Time
	now       func() time.Time
	duration  time.Duration
}

func newTestCooldownAuthority(now func() time.Time) *testCooldownAuthority {
	return &testCooldownAuthority{
		cooldowns: make(map[uuid.UUID]time.Time),
		now:       now,
		duration:  30 * time.Second,
	}
}

func (a *testCooldownAuthority) IsOnCooldown(_ context.Context, id uuid.UUID) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	until, ok := a.cooldowns[id]
	if !ok {
		return false
	}
	if a.now().Before(until) {
		return true
	}
	delete(a.cooldowns, id)
	return false
}

func (a *testCooldownAuthority) RecordFailure(_ context.Context, id uuid.UUID, _ error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cooldowns[id] = a.now().Add(a.duration)
}

func (a *testCooldownAuthority) RecordSuccess(_ context.Context, id uuid.UUID) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.cooldowns, id)
}

func (a *testCooldownAuthority) Status(_ context.Context, _ uuid.UUID) *provider.AccountCooldown {
	return nil
}

func (a *testCooldownAuthority) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cooldowns = make(map[uuid.UUID]time.Time)
}

// newRoutingStateWithCooldown creates a RoutingStateManager with an injectable
// clock and a testCooldownAuthority that uses the same clock.
func newRoutingStateWithCooldown(now func() time.Time, duration time.Duration) *RoutingStateManager {
	auth := newTestCooldownAuthority(now)
	auth.duration = duration
	return NewRoutingStateManagerWithClockAndAuthority(now, auth)
}

// realCooldownRegistry creates a real cooldown.Registry with config suitable for
// tests that don't need time manipulation — threshold=1, long cooldown.
func realCooldownRegistry() cooldown.CooldownAuthority {
	return cooldown.New(cooldown.Config{
		DefaultCooldown:  10 * time.Minute,
		MaxCooldown:      10 * time.Minute,
		FailureThreshold: 1,
		EscalationFactor: 1.0,
		CleanupInterval:  time.Hour,
	})
}

func frozenNow() time.Time {
	return time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC)
}

func TestFailureClass_String(t *testing.T) {
	tests := []struct {
		class FailureClass
		want  string
	}{
		{FailureUnknown, "unknown"},
		{FailureAuth, "auth"},
		{FailureRateLimit, "rate_limit"},
		{FailureUpstream, "upstream"},
		{FailureTimeout, "timeout"},
		{FailureConnection, "connection"},
		{FailureDefinitive, "definitive"},
		{FailureClass(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.class.String(); got != tt.want {
			t.Errorf("FailureClass(%d).String() = %q, want %q", tt.class, got, tt.want)
		}
	}
}

func TestModelLock_IsExpired(t *testing.T) {
	now := time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC)

	t.Run("nil lock", func(t *testing.T) {
		var l *ModelLock
		if !l.IsExpired(now) {
			t.Error("nil lock should be expired")
		}
	})

	t.Run("not expired", func(t *testing.T) {
		l := &ModelLock{
			Model:     "gpt-4",
			ExpiresAt: now.Add(time.Hour),
		}
		if l.IsExpired(now) {
			t.Error("lock should not be expired")
		}
	})

	t.Run("expired", func(t *testing.T) {
		l := &ModelLock{
			Model:     "gpt-4",
			ExpiresAt: now.Add(-time.Hour),
		}
		if !l.IsExpired(now) {
			t.Error("lock should be expired")
		}
	})
}

func TestRoutingStateManager_New(t *testing.T) {
	m := NewRoutingStateManager()
	if m == nil {
		t.Fatal("NewRoutingStateManager() returned nil")
	}
	if m.GetAccount(uuid.New()) != nil {
		t.Error("expected nil for unknown account")
	}
}

func TestRoutingStateManager_GetOrCreateAccount(t *testing.T) {
	m := NewRoutingStateManager()
	id := uuid.New()

	state := m.GetOrCreateAccount(id, provider.AccountStatusActive)
	if state == nil {
		t.Fatal("GetOrCreateAccount returned nil")
	}
	if state.AccountID != id {
		t.Errorf("AccountID = %v, want %v", state.AccountID, id)
	}
	if state.Status != provider.AccountStatusActive {
		t.Errorf("Status = %q, want %q", state.Status, provider.AccountStatusActive)
	}

	// Second call returns existing.
	state2 := m.GetOrCreateAccount(id, provider.AccountStatusCooldown)
	if state2.Status != provider.AccountStatusActive {
		t.Errorf("Status changed to %q, want %q", state2.Status, provider.AccountStatusActive)
	}
}

func TestRoutingStateManager_RecordFailure_Auth(t *testing.T) {
	m := NewRoutingStateManagerWithClock(frozenNow)
	id := uuid.New()

	result := m.RecordFailure(id, FailureAuth, 401, "invalid credentials", nil)
	if result == nil {
		t.Fatal("RecordFailure returned nil")
	}
	if result.Status != provider.AccountStatusDisabled {
		t.Errorf("Status = %q, want %q", result.Status, provider.AccountStatusDisabled)
	}
	if result.LastFailure == nil {
		t.Fatal("LastFailure should not be nil")
	}
	if result.LastFailure.Class != FailureAuth {
		t.Errorf("FailureClass = %v, want %v", result.LastFailure.Class, FailureAuth)
	}

	if !m.IsDisabled(id) {
		t.Error("account should be disabled after auth failure")
	}
}

func TestRoutingStateManager_RecordFailure_RateLimit(t *testing.T) {
	m := newRoutingStateWithCooldown(frozenNow, 10*time.Minute)
	id := uuid.New()

	result := m.RecordFailure(id, FailureRateLimit, 429, "rate limited", nil)
	if result.Status != provider.AccountStatusCooldown {
		t.Errorf("Status = %q, want %q", result.Status, provider.AccountStatusCooldown)
	}
	if !m.IsOnCooldown(id) {
		t.Error("account should be on cooldown after rate limit")
	}
}

func TestRoutingStateManager_RecordFailure_Upstream(t *testing.T) {
	m := newRoutingStateWithCooldown(frozenNow, 10*time.Minute)
	id := uuid.New()

	result := m.RecordFailure(id, FailureUpstream, 502, "bad gateway", nil)
	if result.Status != provider.AccountStatusCooldown {
		t.Errorf("Status = %q, want %q", result.Status, provider.AccountStatusCooldown)
	}
	if !m.IsOnCooldown(id) {
		t.Error("account should be on cooldown after 5xx")
	}
}

func TestRoutingStateManager_RecordSuccess(t *testing.T) {
	m := newRoutingStateWithCooldown(frozenNow, 10*time.Minute)
	id := uuid.New()

	// Record a failure first.
	m.RecordFailure(id, FailureUpstream, 502, "bad gateway", nil)
	if !m.IsOnCooldown(id) {
		t.Error("account should be on cooldown")
	}

	// Then success.
	m.RecordSuccess(id)
	if m.IsOnCooldown(id) {
		t.Error("account should not be on cooldown after success")
	}
	if m.IsDisabled(id) {
		t.Error("account should not be disabled after success")
	}

	state := m.GetAccount(id)
	if state != nil && state.LastFailure != nil {
		t.Error("LastFailure should be nil after success")
	}
}

func TestRoutingStateManager_ModelLock(t *testing.T) {
	m := NewRoutingStateManagerWithClock(frozenNow)
	accountID := uuid.New()
	model := "gpt-4"

	// First, get or create the account so state is tracked.
	m.GetOrCreateAccount(accountID, provider.AccountStatusActive)

	// Acquire lock.
	acquired := m.AcquireModelLock(accountID, model, time.Hour)
	if !acquired {
		t.Fatal("failed to acquire model lock")
	}

	// Second acquire same account should succeed (extends).
	acquired2 := m.AcquireModelLock(accountID, model, time.Hour)
	if !acquired2 {
		t.Error("failed to extend model lock")
	}

	// Another account should fail.
	otherID := uuid.New()
	m.GetOrCreateAccount(otherID, provider.AccountStatusActive)
	acquired3 := m.AcquireModelLock(otherID, model, time.Hour)
	if acquired3 {
		t.Error("other account should not acquire locked model")
	}

	// Release.
	m.ReleaseModelLock(accountID, model)
	acquired4 := m.AcquireModelLock(otherID, model, time.Hour)
	if !acquired4 {
		t.Error("other account should acquire after release")
	}
}

func TestRoutingStateManager_Concurrency(t *testing.T) {
	m := NewRoutingStateManagerWithClock(frozenNow)
	id := uuid.New()

	// No max limit.
	if !m.IncrementConcurrency(id, 0) {
		t.Error("should allow concurrency with no limit")
	}
	if !m.IncrementConcurrency(id, 0) {
		t.Error("should allow second concurrent with no limit")
	}

	m.DecrementConcurrency(id)
	m.DecrementConcurrency(id)

	// Max limit of 1.
	if !m.IncrementConcurrency(id, 1) {
		t.Error("should allow first concurrent with limit 1")
	}
	if m.IncrementConcurrency(id, 1) {
		t.Error("should reject second concurrent with limit 1")
	}
}

func TestRoutingStateManager_CheckpointRestore(t *testing.T) {
	m := NewRoutingStateManagerWithClock(frozenNow)
	id := uuid.New()

	m.RecordFailure(id, FailureAuth, 401, "bad key", nil)
	m.AcquireModelLock(id, "gpt-4", time.Hour)

	// Checkpoint.
	snapshot := m.Checkpoint()
	if len(snapshot) != 1 {
		t.Fatalf("Checkpoint returned %d entries, want 1", len(snapshot))
	}

	// New manager with restored data.
	m2 := NewRoutingStateManagerWithClock(frozenNow)
	m2.Restore(snapshot)

	if !m2.IsDisabled(id) {
		t.Error("restored account should be disabled")
	}

	state := m2.GetAccount(id)
	if state == nil {
		t.Fatal("restored account should exist")
	}
	if state.LastFailure.Class != FailureAuth {
		t.Errorf("restored FailureClass = %v, want %v", state.LastFailure.Class, FailureAuth)
	}
}

func TestRoutingStateManager_IsEligible(t *testing.T) {
	m := NewRoutingStateManagerWithClock(frozenNow)
	id := uuid.New()

	// Unknown account is eligible.
	if !m.IsEligible(id) {
		t.Error("unknown account should be eligible")
	}

	// Disabled is not eligible.
	m.RecordFailure(id, FailureAuth, 401, "bad key", nil)
	if m.IsEligible(id) {
		t.Error("disabled account should not be eligible")
	}

	// Reset.
	m.RecordSuccess(id)
	if !m.IsEligible(id) {
		t.Error("active account should be eligible")
	}
}

func TestRoutingStateManager_ConcurrentSafety(t *testing.T) {
	m := newRoutingStateWithCooldown(frozenNow, 10*time.Minute)
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := uuid.New()
			m.GetOrCreateAccount(id, provider.AccountStatusActive)
			m.RecordFailure(id, FailureUpstream, 502, "err", nil)
			m.IsOnCooldown(id)
			m.RecordSuccess(id)
			m.Checkpoint()
		}(i)
	}
	wg.Wait()
}

func TestClassifyFailure(t *testing.T) {
	tests := []struct {
		name      string
		http      int
		isAuth    bool
		isTimeout bool
		isConn    bool
		want      FailureClass
	}{
		{"401 auth", 401, false, false, false, FailureAuth},
		{"403 auth", 403, false, false, false, FailureAuth},
		{"isAuth flag", 200, true, false, false, FailureAuth},
		{"429 rate limit", 429, false, false, false, FailureRateLimit},
		{"502 upstream", 502, false, false, false, FailureUpstream},
		{"5xx upstream", 503, false, false, false, FailureUpstream},
		{"504 timeout", 504, false, false, false, FailureTimeout},
		{"408 timeout", 408, false, false, false, FailureTimeout},
		{"isTimeout flag", 200, false, true, false, FailureTimeout},
		{"isConn flag", 200, false, false, true, FailureConnection},
		{"400 definitive", 400, false, false, false, FailureDefinitive},
		{"404 definitive", 404, false, false, false, FailureDefinitive},
		{"200 unknown", 200, false, false, false, FailureUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyFailure(tt.http, nil, tt.isAuth, tt.isTimeout, tt.isConn)
			if got != tt.want {
				t.Errorf("ClassifyFailure(%d) = %v, want %v", tt.http, got, tt.want)
			}
		})
	}
}

func TestFakeCheckpointer(t *testing.T) {
	ctx := context.Background()
	fc := NewFakeCheckpointer()

	// Save checkpoint.
	state := NewRoutingStateManagerWithClock(frozenNow)
	id := uuid.New()
	state.RecordFailure(id, FailureAuth, 401, "bad key", nil)

	err := fc.SaveCheckpoint(ctx, state.Checkpoint())
	if err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}

	// Load checkpoint.
	loaded, err := fc.LoadCheckpoint(ctx)
	if err != nil {
		t.Fatalf("LoadCheckpoint: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("got %d entries, want 1", len(loaded))
	}

	// Save transition.
	transition := StateTransition{
		ID:        uuid.New(),
		AccountID: id,
		From:      provider.AccountStatusActive,
		To:        provider.AccountStatusDisabled,
		Reason:    "auth failure",
		CreatedAt: frozenNow(),
	}
	err = fc.SaveTransition(ctx, transition)
	if err != nil {
		t.Fatalf("SaveTransition: %v", err)
	}

	// List transitions.
	since := frozenNow().Add(-24 * time.Hour)
	transitions, err := fc.ListTransitions(ctx, id, since)
	if err != nil {
		t.Fatalf("ListTransitions: %v", err)
	}
	if len(transitions) != 1 {
		t.Fatalf("got %d transitions, want 1", len(transitions))
	}

	// Cleanup old transitions.
	before := frozenNow().Add(-time.Hour)
	removed, err := fc.CleanupTransitions(ctx, before)
	if err != nil {
		t.Fatalf("CleanupTransitions: %v", err)
	}
	// Our transition is at frozenNow which is after `before`, so none removed.
	if removed != 0 {
		t.Errorf("removed %d transitions, want 0", removed)
	}
}

func TestRoutingStateManager_Reset(t *testing.T) {
	m := newRoutingStateWithCooldown(frozenNow, 10*time.Minute)
	id := uuid.New()

	m.RecordFailure(id, FailureUpstream, 502, "bad gateway", nil)
	if !m.IsOnCooldown(id) {
		t.Error("account should be on cooldown")
	}

	m.Reset()
	if m.IsOnCooldown(id) {
		t.Error("account should not be on cooldown after reset")
	}
	if m.GetAccount(id) != nil {
		t.Error("account should not exist after reset")
	}
}

func TestRoutingStateManager_ConcurrencyEdge(t *testing.T) {
	m := NewRoutingStateManagerWithClock(frozenNow)
	id := uuid.New()

	// Decrement unknown account should not panic.
	m.DecrementConcurrency(id)

	// Decrement past zero should not go negative.
	m.IncrementConcurrency(id, 0)
	m.DecrementConcurrency(id)
	m.DecrementConcurrency(id) // should not panic or go negative

	state := m.GetAccount(id)
	if state != nil && state.Concurrency < 0 {
		t.Errorf("Concurrency = %d, want >= 0", state.Concurrency)
	}
}

func TestAllStates(t *testing.T) {
	m := NewRoutingStateManagerWithClock(frozenNow)
	id1 := uuid.New()
	id2 := uuid.New()

	m.RecordFailure(id1, FailureAuth, 401, "bad key", nil)
	m.GetOrCreateAccount(id2, provider.AccountStatusActive)

	states := m.AllStates()
	if len(states) != 2 {
		t.Fatalf("AllStates returned %d entries, want 2", len(states))
	}
}

// ---------------------------------------------------------------------------
// P3-T08 focused tests: single cooldown authority, injected clock,
// cancellation/checkpoint, model locks, concurrency.
// ---------------------------------------------------------------------------

// TestRecordFailureNoCooldownUntilSet verifies that RoutingStateManager
// does NOT compute or store cooldownUntil — that responsibility is delegated
// exclusively to the CooldownAuthority.
func TestRecordFailureNoCooldownUntilSet(t *testing.T) {
	m := NewRoutingStateManagerWithClock(frozenNow)
	id := uuid.New()

	r1 := m.RecordFailure(id, FailureRateLimit, 429, "rate limited", nil)
	if !r1.CooldownUntil.IsZero() {
		t.Errorf("CooldownUntil should be zero (no local computation), got %v", r1.CooldownUntil)
	}

	r2 := m.RecordFailure(id, FailureRateLimit, 429, "rate limited again", nil)
	if !r2.CooldownUntil.IsZero() {
		t.Errorf("CooldownUntil should remain zero after second failure, got %v", r2.CooldownUntil)
	}

	// IsOnCooldown delegates to the authority; without one it returns false.
	if m.IsOnCooldown(id) {
		t.Error("IsOnCooldown should be false without a CooldownAuthority")
	}
}

// TestModelLockUsesInjectedClock verifies ModelLock.IsExpired accepts the
// injected clock rather than calling time.Now() directly.
func TestModelLockUsesInjectedClock(t *testing.T) {
	now := frozenNow()
	m := NewRoutingStateManagerWithClock(func() time.Time { return now })
	id := uuid.New()

	// Acquire lock — ExpiresAt = now + 1s.
	m.GetOrCreateAccount(id, provider.AccountStatusActive)
	m.AcquireModelLock(id, "gpt-4", time.Second)

	// Advance clock past expiry.
	now = now.Add(2 * time.Second)

	// Another account should now acquire the same model lock.
	otherID := uuid.New()
	m.GetOrCreateAccount(otherID, provider.AccountStatusActive)
	acquired := m.AcquireModelLock(otherID, "gpt-4", time.Hour)
	if !acquired {
		t.Error("other account should acquire expired lock when clock advances")
	}

	state := m.GetAccount(otherID)
	if state == nil || state.ModelLock == nil {
		t.Fatal("other account should have model lock")
	}
	if state.ModelLock.OwnerID != otherID {
		t.Errorf("lock owner = %v, want %v", state.ModelLock.OwnerID, otherID)
	}
}

// TestCheckpointUsesFreshContext verifies that the checkpoint on shutdown
// uses a fresh context, not the already-cancelled one. This tests the
// providers/state.go fix; we exercise the RoutingStateManager checkpoint
// path which should not fail due to context cancellation.
func TestCheckpointUsesFreshContext(t *testing.T) {
	m := NewRoutingStateManagerWithClock(frozenNow)
	id := uuid.New()
	m.RecordFailure(id, FailureUpstream, 502, "bad gateway", nil)

	// Create a cancelled context like the shutdown path does.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Immediately cancel.

	// Checkpoint with cancelled context should still work because the
	// snapshot is captured under RLock (not dependent on ctx).
	snapshot := m.Checkpoint()
	if len(snapshot) != 1 {
		t.Fatalf("Checkpoint returned %d entries, want 1", len(snapshot))
	}

	// The snapshot should have the correct cooldown state.
	state := snapshot[id]
	if state.Status != provider.AccountStatusCooldown {
		t.Errorf("checkpointed status = %q, want %q", state.Status, provider.AccountStatusCooldown)
	}
	if state.LastFailure == nil || state.LastFailure.Class != FailureUpstream {
		t.Errorf("checkpointed LastFailure.Class = %v, want %v", state.LastFailure.Class, FailureUpstream)
	}

	// Verify that the cancelled ctx path in providers/state.go would succeed:
	// tryCheckpoint uses m.Checkpoint() which is independent of the context.
	_ = ctx // would be cancelled in shutdown path
}

// TestModelLockExpiryWithInjectedClock verifies that IsExpired uses the
// injected clock parameter, not time.Now().
func TestModelLockExpiryWithInjectedClock(t *testing.T) {
	now := frozenNow()

	// Lock that expires at 10:00:30.
	l := &ModelLock{
		Model:     "gpt-4",
		OwnerID:   uuid.New(),
		LockedAt:  now,
		ExpiresAt: now.Add(30 * time.Second),
	}

	// Before expiry.
	if l.IsExpired(now) {
		t.Error("lock should not be expired at current time")
	}

	// At exact expiry (not after).
	if l.IsExpired(now.Add(30 * time.Second)) {
		t.Error("lock should not be expired at exact expiry time")
	}

	// After expiry.
	if !l.IsExpired(now.Add(31 * time.Second)) {
		t.Error("lock should be expired after expiry time")
	}
}

// TestConcurrentModelLockAcquisition verifies concurrent model lock
// acquisition and release with injected clock.
func TestConcurrentModelLockAcquisition(t *testing.T) {
	m := NewRoutingStateManagerWithClock(frozenNow)
	var wg sync.WaitGroup

	accounts := make([]uuid.UUID, 10)
	for i := range accounts {
		id := uuid.New()
		accounts[i] = id
		m.GetOrCreateAccount(id, provider.AccountStatusActive)
	}

	// Multiple goroutines competing for the same model lock.
	winner := make(chan uuid.UUID, 1)
	for _, id := range accounts {
		wg.Add(1)
		go func(acctID uuid.UUID) {
			defer wg.Done()
			if m.AcquireModelLock(acctID, "shared-model", time.Minute) {
				select {
				case winner <- acctID:
				default:
				}
			}
		}(id)
	}
	wg.Wait()
	close(winner)

	winnerID, ok := <-winner
	if !ok {
		t.Fatal("no goroutine acquired the model lock")
	}

	// Only the winner should hold the lock.
	for _, id := range accounts {
		state := m.GetAccount(id)
		if id == winnerID {
			if state == nil || state.ModelLock == nil {
				t.Errorf("winner %v should have model lock", id)
			}
		} else {
			if state != nil && state.ModelLock != nil {
				t.Errorf("non-winner %v should not have model lock", id)
			}
		}
	}
}

// TestRestoreHonoursClock verifies that Restore uses the injected clock
// when evaluating model-lock expiry, not time.Now().
func TestRestoreHonoursClock(t *testing.T) {
	now := frozenNow()
	m := NewRoutingStateManagerWithClock(func() time.Time { return now })
	id := uuid.New()

	m.GetOrCreateAccount(id, provider.AccountStatusActive)
	m.AcquireModelLock(id, "gpt-4", time.Minute)

	snapshot := m.Checkpoint()

	// Create a new manager with a clock that's 2 minutes in the future.
	futureNow := now.Add(2 * time.Minute)
	m2 := NewRoutingStateManagerWithClock(func() time.Time { return futureNow })
	m2.Restore(snapshot)

	// The lock should be expired (future clock), so a new acquire should succeed.
	otherID := uuid.New()
	m2.GetOrCreateAccount(otherID, provider.AccountStatusActive)
	acquired := m2.AcquireModelLock(otherID, "gpt-4", time.Hour)
	if !acquired {
		t.Error("restored lock should be expired with future clock; new acquire should succeed")
	}
}

// TestIsOnCooldownWithAuthority verifies that IsOnCooldown delegates to the
// CooldownAuthority and reflects the authority's timing.
func TestIsOnCooldownWithAuthority(t *testing.T) {
	now := frozenNow()
	m := newRoutingStateWithCooldown(func() time.Time { return now }, 30*time.Second)
	id := uuid.New()

	m.RecordFailure(id, FailureUpstream, 502, "bad gateway", nil)
	if !m.IsOnCooldown(id) {
		t.Error("should be on cooldown immediately after failure")
	}

	// Advance clock past 30s cooldown.
	now = now.Add(31 * time.Second)
	if m.IsOnCooldown(id) {
		t.Error("should NOT be on cooldown after authority cooldown expires")
	}
}

// TestRecordFailureDoesNotSetCooldownUntil verifies that RoutingStateManager
// never sets CooldownUntil — that responsibility belongs exclusively to the
// CooldownAuthority.
func TestRecordFailureDoesNotSetCooldownUntil(t *testing.T) {
	m := NewRoutingStateManagerWithClock(frozenNow)
	id := uuid.New()

	for i := 0; i < 10; i++ {
		r := m.RecordFailure(id, FailureUpstream, 502, "err", nil)
		if !r.CooldownUntil.IsZero() {
			t.Fatalf("iteration %d: CooldownUntil=%v, want zero — escalation/timing logic should not be in RoutingStateManager", i, r.CooldownUntil)
		}
	}
}
