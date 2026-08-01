// Package routing provides authority-correct routing resolver, account
// selection/state, same-model fallback, failure classification, cooldown
// checkpoint/restore, 7-day transitions, and deterministic concurrency.
//
// Decision D109: hot state in memory; important cooldown/model-lock/health
// checkpoints in PG; deterministic restore.
// Decision D115: retain transitions 7 days.
package routing

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"gorouter/internal/app/cooldown"
	"gorouter/internal/domain/provider"
)

// FailureClass categorises an upstream error for cooldown and routing decisions.
type FailureClass int

const (
	// FailureUnknown is the default when the error cannot be classified.
	FailureUnknown FailureClass = iota
	// FailureAuth indicates a definitive authentication/authorization failure (401/403).
	// Disables routing but preserves the account.
	FailureAuth
	// FailureRateLimit indicates rate limiting (429). Triggers cooldown.
	FailureRateLimit
	// FailureUpstream indicates a server-side error (5xx). Retryable, may trigger cooldown.
	FailureUpstream
	// FailureTimeout indicates a request timeout. Retryable.
	FailureTimeout
	// FailureConnection indicates a connection-level error. Retryable.
	FailureConnection
	// FailureDefinitive indicates a non-retryable terminal error (e.g. 400 bad request).
	FailureDefinitive
)

// String returns a human-readable name for the failure class.
func (c FailureClass) String() string {
	switch c {
	case FailureAuth:
		return "auth"
	case FailureRateLimit:
		return "rate_limit"
	case FailureUpstream:
		return "upstream"
	case FailureTimeout:
		return "timeout"
	case FailureConnection:
		return "connection"
	case FailureDefinitive:
		return "definitive"
	default:
		return "unknown"
	}
}

// AccountState represents the runtime state of a single provider account.
type AccountState struct {
	AccountID     uuid.UUID
	Status        provider.AccountStatus
	CooldownUntil time.Time
	ModelLock     *ModelLock
	Concurrency   int32
	LastFailure   *FailureInfo
	LastSuccessAt time.Time
	UpdatedAt     time.Time
}

// FailureInfo captures details about the most recent failure.
type FailureInfo struct {
	Class  FailureClass
	Reason string
	HTTP   int
	Err    string
	T      time.Time
}

// ModelLock represents an exclusive lock on a model for an account.
type ModelLock struct {
	Model     string
	OwnerID   uuid.UUID
	LockedAt  time.Time
	ExpiresAt time.Time
}

// IsExpired returns true if the model lock has expired.
// The now parameter MUST come from the injectable clock (m.now()) to ensure
// deterministic test behaviour and avoid wall-clock dependency.
func (l *ModelLock) IsExpired(now time.Time) bool {
	if l == nil {
		return true
	}
	return now.After(l.ExpiresAt)
}

// StateTransition records a state change for auditing and 7-day retention.
type StateTransition struct {
	ID        uuid.UUID
	AccountID uuid.UUID
	From      provider.AccountStatus
	To        provider.AccountStatus
	Reason    string
	CreatedAt time.Time
}

// RoutingStateManager manages the in-memory hot state for all provider accounts,
// including cooldowns, model locks, and concurrency tracking. It also provides
// checkpoint/restore hooks for PG persistence (delegated to repository interface).
//
// All exported methods are goroutine-safe.
type RoutingStateManager struct {
	mu                sync.RWMutex
	accounts          map[uuid.UUID]*AccountState
	locks             map[string]*ModelLock // model -> lock (one account per model)
	now               func() time.Time
	cooldownAuthority cooldown.CooldownAuthority
}

// NewRoutingStateManager creates a new RoutingStateManager.
func NewRoutingStateManager() *RoutingStateManager {
	return &RoutingStateManager{
		accounts: make(map[uuid.UUID]*AccountState),
		locks:    make(map[string]*ModelLock),
		now:      time.Now,
	}
}

// NewRoutingStateManagerWithClock creates a RoutingStateManager with an injectable clock.
func NewRoutingStateManagerWithClock(now func() time.Time) *RoutingStateManager {
	return &RoutingStateManager{
		accounts: make(map[uuid.UUID]*AccountState),
		locks:    make(map[string]*ModelLock),
		now:      now,
	}
}

// NewRoutingStateManagerWithClockAndAuthority creates a RoutingStateManager with
// an injectable clock and a CooldownAuthority. The authority is the sole source
// of cooldown timing and eligibility decisions — the state manager never
// independently computes or stores cooldownUntil.
func NewRoutingStateManagerWithClockAndAuthority(now func() time.Time, authority cooldown.CooldownAuthority) *RoutingStateManager {
	return &RoutingStateManager{
		accounts:          make(map[uuid.UUID]*AccountState),
		locks:             make(map[string]*ModelLock),
		now:               now,
		cooldownAuthority: authority,
	}
}

// GetAccount returns the current state for an account, or nil if not tracked.
func (m *RoutingStateManager) GetAccount(id uuid.UUID) *AccountState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.accounts[id]
	if !ok {
		return nil
	}
	cp := *s
	return &cp
}

// GetOrCreateAccount returns the state for an account, creating a default active state if missing.
func (m *RoutingStateManager) GetOrCreateAccount(id uuid.UUID, status provider.AccountStatus) *AccountState {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.accounts[id]
	if !ok {
		s = &AccountState{
			AccountID: id,
			Status:    status,
			UpdatedAt: m.now(),
		}
		m.accounts[id] = s
	}
	cp := *s
	return &cp
}

// RecordFailure records failure/status/audit metadata only. Cooldown timing and
// eligibility decisions are delegated exclusively to the CooldownAuthority (set
// via NewRoutingStateManagerWithClockAndAuthority). The state manager never
// independently computes or stores cooldownUntil.
func (m *RoutingStateManager) RecordFailure(id uuid.UUID, class FailureClass, httpStatus int, reason string, err error) *AccountState {
	m.mu.Lock()

	now := m.now()
	s, ok := m.accounts[id]
	if !ok {
		s = &AccountState{
			AccountID: id,
			Status:    provider.AccountStatusActive,
			UpdatedAt: now,
		}
		m.accounts[id] = s
	}

	s.LastFailure = &FailureInfo{
		Class:  class,
		Reason: reason,
		HTTP:   httpStatus,
		Err: func() string {
			if err != nil {
				return err.Error()
			}
			return ""
		}(),
		T: now,
	}
	s.UpdatedAt = now

	switch class {
	case FailureAuth:
		s.Status = provider.AccountStatusDisabled
	case FailureRateLimit, FailureUpstream, FailureTimeout, FailureConnection:
		s.Status = provider.AccountStatusCooldown
		// Cooldown timing is delegated to the CooldownAuthority.
		// No local cooldownUntil is computed or stored here.
	case FailureDefinitive:
		s.Status = provider.AccountStatusDegraded
	}

	cp := *s
	m.mu.Unlock()

	// Delegate cooldown tracking to the authority outside the lock.
	if m.cooldownAuthority != nil {
		m.cooldownAuthority.RecordFailure(context.Background(), id, err)
	}

	return &cp
}

// RecordSuccess records a success for an account, resetting its failure state
// and delegating cooldown reset to the CooldownAuthority.
func (m *RoutingStateManager) RecordSuccess(id uuid.UUID) {
	m.mu.Lock()

	now := m.now()
	s, ok := m.accounts[id]
	if !ok {
		m.mu.Unlock()
		return
	}

	s.Status = provider.AccountStatusActive
	s.LastFailure = nil
	s.CooldownUntil = time.Time{}
	s.LastSuccessAt = now
	s.UpdatedAt = now
	m.mu.Unlock()

	if m.cooldownAuthority != nil {
		m.cooldownAuthority.RecordSuccess(context.Background(), id)
	}
}

// IsOnCooldown checks whether the account is currently cooled down by
// delegating to the CooldownAuthority. When no authority is set, returns false
// (no cooldown without an authority).
func (m *RoutingStateManager) IsOnCooldown(id uuid.UUID) bool {
	if m.cooldownAuthority != nil {
		return m.cooldownAuthority.IsOnCooldown(context.Background(), id)
	}
	return false
}

// IsDisabled checks whether the account is disabled for routing.
func (m *RoutingStateManager) IsDisabled(id uuid.UUID) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	s, ok := m.accounts[id]
	if !ok {
		return false
	}
	return s.Status == provider.AccountStatusDisabled
}

// AcquireModelLock attempts to acquire an exclusive model lock for an account.
// Returns true if the lock was acquired.
func (m *RoutingStateManager) AcquireModelLock(accountID uuid.UUID, model string, ttl time.Duration) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()

	existing, hasLock := m.locks[model]
	if hasLock && now.Before(existing.ExpiresAt) {
		if existing.OwnerID == accountID {
			existing.ExpiresAt = now.Add(ttl)
			return true
		}
		return false
	}

	m.locks[model] = &ModelLock{
		Model:     model,
		OwnerID:   accountID,
		LockedAt:  now,
		ExpiresAt: now.Add(ttl),
	}

	if s, ok := m.accounts[accountID]; ok {
		s.ModelLock = m.locks[model]
	}

	return true
}

// ReleaseModelLock releases the model lock held by an account.
func (m *RoutingStateManager) ReleaseModelLock(accountID uuid.UUID, model string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, hasLock := m.locks[model]
	if hasLock {
		delete(m.locks, model)
	}

	if s, ok := m.accounts[accountID]; ok {
		s.ModelLock = nil
	}
}

// IncrementConcurrency increments the concurrency count for an account.
// Returns false if the account has a max concurrency limit and it would be exceeded.
func (m *RoutingStateManager) IncrementConcurrency(id uuid.UUID, max int32) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.accounts[id]
	if !ok {
		s = &AccountState{
			AccountID: id,
			Status:    provider.AccountStatusActive,
			UpdatedAt: m.now(),
		}
		m.accounts[id] = s
	}

	if max > 0 && s.Concurrency >= max {
		return false
	}
	s.Concurrency++
	return true
}

// DecrementConcurrency decrements the concurrency count for an account.
func (m *RoutingStateManager) DecrementConcurrency(id uuid.UUID) {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.accounts[id]
	if !ok {
		return
	}
	if s.Concurrency > 0 {
		s.Concurrency--
	}
}

// Checkpoint returns a snapshot of all tracked state. It copies the data to
// avoid holding locks during PG write.
func (m *RoutingStateManager) Checkpoint() map[uuid.UUID]*AccountState {
	m.mu.RLock()
	defer m.mu.RUnlock()

	snapshot := make(map[uuid.UUID]*AccountState, len(m.accounts))
	for id, s := range m.accounts {
		cp := *s
		if s.ModelLock != nil {
			mlCp := *s.ModelLock
			cp.ModelLock = &mlCp
		}
		if s.LastFailure != nil {
			fCp := *s.LastFailure
			cp.LastFailure = &fCp
		}
		snapshot[id] = &cp
	}
	return snapshot
}

// Restore loads state from a checkpoint snapshot.
func (m *RoutingStateManager) Restore(snapshot map[uuid.UUID]*AccountState) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.accounts = make(map[uuid.UUID]*AccountState, len(snapshot))
	m.locks = make(map[string]*ModelLock)

	for id, s := range snapshot {
		cp := *s
		if s.ModelLock != nil {
			mlCp := *s.ModelLock
			cp.ModelLock = &mlCp
		}
		if s.LastFailure != nil {
			fCp := *s.LastFailure
			cp.LastFailure = &fCp
		}
		m.accounts[id] = &cp
		if s.ModelLock != nil && !s.ModelLock.IsExpired(m.now()) {
			m.locks[s.ModelLock.Model] = m.accounts[id].ModelLock
		}
	}
}

// AllStates returns all account states (used for checkpoint).
func (m *RoutingStateManager) AllStates() map[uuid.UUID]*AccountState {
	return m.Checkpoint()
}

// Reset clears all state and resets the cooldown authority if it supports Reset.
func (m *RoutingStateManager) Reset() {
	m.mu.Lock()
	m.accounts = make(map[uuid.UUID]*AccountState)
	m.locks = make(map[string]*ModelLock)
	m.mu.Unlock()

	if m.cooldownAuthority != nil {
		if reseter, ok := m.cooldownAuthority.(interface{ Reset() }); ok {
			reseter.Reset()
		}
	}
}

// IsEligible checks whether an account is eligible for routing. Disabled accounts
// are ineligible. Cooldown eligibility is delegated to the CooldownAuthority.
// When no authority is set, cooldown status alone does not affect eligibility.
func (m *RoutingStateManager) IsEligible(id uuid.UUID) bool {
	m.mu.RLock()
	s, ok := m.accounts[id]
	if !ok {
		m.mu.RUnlock()
		return true // unknown accounts are eligible (will be tracked on first use)
	}
	disabled := s.Status == provider.AccountStatusDisabled
	m.mu.RUnlock()

	if disabled {
		return false
	}

	// Delegate cooldown eligibility to the authority.
	if m.cooldownAuthority != nil {
		return !m.cooldownAuthority.IsOnCooldown(context.Background(), id)
	}
	return true
}

// ClassifyFailure determines the FailureClass from an HTTP status code and error.
// This is a provider-aware classification.
func ClassifyFailure(httpStatus int, err error, isAuthErr, isTimeout, isConnErr bool) FailureClass {
	if isAuthErr || httpStatus == 401 || httpStatus == 403 {
		return FailureAuth
	}
	if httpStatus == 429 {
		return FailureRateLimit
	}
	if isTimeout || httpStatus == 408 || httpStatus == 504 {
		return FailureTimeout
	}
	if isConnErr {
		return FailureConnection
	}
	if httpStatus >= 500 && httpStatus < 600 {
		return FailureUpstream
	}
	if httpStatus >= 400 && httpStatus < 500 {
		return FailureDefinitive
	}
	if err != nil {
		return FailureConnection
	}
	return FailureUnknown
}

// Checkpointer defines the PG checkpoint persistence interface.
// This is owned by T06 contract; we define the interface here and provide
// a fake for tests. PG integration is deferred until T06 merge synchronization.
type Checkpointer interface {
	// SaveCheckpoint persists the current routing state snapshot.
	SaveCheckpoint(ctx context.Context, snapshot map[uuid.UUID]*AccountState) error

	// LoadCheckpoint loads the most recent routing state snapshot.
	LoadCheckpoint(ctx context.Context) (map[uuid.UUID]*AccountState, error)

	// SaveTransition records a state transition.
	SaveTransition(ctx context.Context, t StateTransition) error

	// ListTransitions returns transitions within the retention period (7 days).
	ListTransitions(ctx context.Context, accountID uuid.UUID, since time.Time) ([]StateTransition, error)

	// CleanupTransitions removes transitions older than the retention period.
	CleanupTransitions(ctx context.Context, before time.Time) (int, error)
}

// FakeCheckpointer is an in-memory implementation of Checkpointer for tests.
type FakeCheckpointer struct {
	checkpoint  map[uuid.UUID]*AccountState
	transitions []StateTransition
}

// NewFakeCheckpointer creates a new FakeCheckpointer.
func NewFakeCheckpointer() *FakeCheckpointer {
	return &FakeCheckpointer{
		checkpoint:  make(map[uuid.UUID]*AccountState),
		transitions: make([]StateTransition, 0),
	}
}

// SaveCheckpoint saves the checkpoint in memory.
func (f *FakeCheckpointer) SaveCheckpoint(_ context.Context, snapshot map[uuid.UUID]*AccountState) error {
	f.checkpoint = make(map[uuid.UUID]*AccountState, len(snapshot))
	for id, s := range snapshot {
		cp := *s
		if s.ModelLock != nil {
			mlCp := *s.ModelLock
			cp.ModelLock = &mlCp
		}
		if s.LastFailure != nil {
			fCp := *s.LastFailure
			cp.LastFailure = &fCp
		}
		f.checkpoint[id] = &cp
	}
	return nil
}

// LoadCheckpoint loads the in-memory checkpoint.
func (f *FakeCheckpointer) LoadCheckpoint(_ context.Context) (map[uuid.UUID]*AccountState, error) {
	result := make(map[uuid.UUID]*AccountState, len(f.checkpoint))
	for id, s := range f.checkpoint {
		cp := *s
		if s.ModelLock != nil {
			mlCp := *s.ModelLock
			cp.ModelLock = &mlCp
		}
		if s.LastFailure != nil {
			fCp := *s.LastFailure
			cp.LastFailure = &fCp
		}
		result[id] = &cp
	}
	return result, nil
}

// SaveTransition appends a transition record.
func (f *FakeCheckpointer) SaveTransition(_ context.Context, t StateTransition) error {
	f.transitions = append(f.transitions, t)
	return nil
}

// ListTransitions returns transitions matching the account filter.
func (f *FakeCheckpointer) ListTransitions(_ context.Context, accountID uuid.UUID, since time.Time) ([]StateTransition, error) {
	var result []StateTransition
	for _, t := range f.transitions {
		if t.AccountID == accountID && !t.CreatedAt.Before(since) {
			result = append(result, t)
		}
	}
	return result, nil
}

// CleanupTransitions removes transitions older than the given time.
func (f *FakeCheckpointer) CleanupTransitions(_ context.Context, before time.Time) (int, error) {
	var kept []StateTransition
	var removed int
	for _, t := range f.transitions {
		if t.CreatedAt.Before(before) {
			removed++
		} else {
			kept = append(kept, t)
		}
	}
	f.transitions = kept
	return removed, nil
}

// Transitions returns all stored transitions (for test inspection).
func (f *FakeCheckpointer) Transitions() []StateTransition {
	return f.transitions
}

// Checkpoint returns the stored checkpoint (for test inspection).
func (f *FakeCheckpointer) CheckpointData() map[uuid.UUID]*AccountState {
	return f.checkpoint
}
