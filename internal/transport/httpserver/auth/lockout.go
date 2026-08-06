package auth

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

const (
	maxFailsBeforeLock = 5
	failWindow         = time.Hour
)

// lockSteps are the progressive lock durations matching the pinned upstream
// LOCK_STEPS_MS = [30_000, 120_000, 600_000, 1_800_000]. The final step is
// reused once the band is exhausted.
var lockSteps = []time.Duration{
	30 * time.Second,
	2 * time.Minute,
	10 * time.Minute,
	30 * time.Minute,
}

// LockoutState is the persisted per-IP progressive login-lockout state.
type LockoutState struct {
	FailCount   int       `json:"fail_count"`
	Band        int       `json:"band"`
	LockedUntil time.Time `json:"locked_until"`
	LastFail    time.Time `json:"last_fail"`
}

// LockoutStore persists lockout state per key (login:lockout:<normalized-ip>).
// The production hardening store is backed by gorouter_runtime_state; see
// newRuntimeStateLockoutStore for the labeled deviation from upstream.
type LockoutStore interface {
	Load(ctx context.Context, key string) (*LockoutState, error)
	Save(ctx context.Context, key string, state *LockoutState) error
}

// NewMemoryLockoutStore returns an in-process lockout store. It is the
// single-runtime default and the test seam for the lockout bands.
func NewMemoryLockoutStore() LockoutStore {
	return &memoryLockoutStore{m: map[string]*LockoutState{}}
}

type memoryLockoutStore struct {
	mu sync.Mutex
	m  map[string]*LockoutState
}

func (s *memoryLockoutStore) Load(ctx context.Context, key string) (*LockoutState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.m[key]
	if !ok {
		return nil, nil
	}
	cp := *st
	return &cp, nil
}

func (s *memoryLockoutStore) Save(ctx context.Context, key string, state *LockoutState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *state
	s.m[key] = &cp
	return nil
}

// kvStore is the minimal durable key/value seam the runtime-state backed
// lockout store needs. It is satisfied structurally by the gorouter_runtime_state
// repository (combo.RuntimeState / NewRuntimeStateRepo).
type kvStore interface {
	Get(ctx context.Context, key string) (json.RawMessage, error)
	Set(ctx context.Context, key string, value json.RawMessage, ttl time.Duration) error
}

// runtimeStateLockoutStore persists the active lockout through
// gorouter_runtime_state so a lock survives a process restart. This is a
// deliberate hardening deviation from the pinned upstream behavior, which
// resets lockout state on restart (design D17, decision #22); it is a labeled
// local hardening delta and is never claimed as upstream parity.
type runtimeStateLockoutStore struct {
	kv kvStore
}

func newRuntimeStateLockoutStore(kv kvStore) *runtimeStateLockoutStore {
	return &runtimeStateLockoutStore{kv: kv}
}

func (s *runtimeStateLockoutStore) Load(ctx context.Context, key string) (*LockoutState, error) {
	raw, err := s.kv.Get(ctx, key)
	if err != nil || raw == nil {
		return nil, err
	}
	var st LockoutState
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func (s *runtimeStateLockoutStore) Save(ctx context.Context, key string, state *LockoutState) error {
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return s.kv.Set(ctx, key, raw, 0)
}

// recordLoginFailure applies one failed login to st under the pinned-upstream
// mechanism: a lock is raised after every block of five failures, durations
// advance 30s/2m/10m/30m with the last reused, the failure count resets at
// each lock, and a one-hour window with no failure resets the escalation.
// It reports denied=true with the remaining lock duration when the attempt
// arrives while a lock is active (the failure is not counted then).
func recordLoginFailure(st *LockoutState, now time.Time) (denied bool, retryAfter time.Duration) {
	if !st.LastFail.IsZero() && now.Sub(st.LastFail) > failWindow {
		st.Band = 0
		st.FailCount = 0
	}
	if now.Before(st.LockedUntil) {
		return true, st.LockedUntil.Sub(now)
	}
	st.FailCount++
	st.LastFail = now
	if st.FailCount >= maxFailsBeforeLock {
		st.LockedUntil = now.Add(lockSteps[st.Band])
		if st.Band < len(lockSteps)-1 {
			st.Band++
		}
		st.FailCount = 0
	}
	return false, 0
}
