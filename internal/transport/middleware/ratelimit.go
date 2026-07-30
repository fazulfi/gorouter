package middleware

import (
	"sync"
	"time"
)

// Clock provides injectable time for deterministic testing.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

type attemptEntry struct {
	count   int
	lockout time.Time
}

// LoginRateLimiterConfig configures the per-IP login rate limiter.
type LoginRateLimiterConfig struct {
	MaxAttempts     int
	LockoutDuration time.Duration
	MaxEntries      int
	Clock           Clock
}

// LoginRateLimiter tracks failed login attempts per key (typically source IP)
// and enforces lockout thresholds with deterministic clock injection.
type LoginRateLimiter struct {
	mu              sync.RWMutex
	clock           Clock
	entries         map[string]*attemptEntry
	maxAttempts     int
	lockoutDuration time.Duration
	maxEntries      int
}

// NewLoginRateLimiter creates a LoginRateLimiter. If MaxAttempts <= 0, defaults
// to 5. If LockoutDuration <= 0, defaults to 15 minutes. If MaxEntries <= 0,
// defaults to 10000. If Clock is nil, uses real time.
func NewLoginRateLimiter(cfg LoginRateLimiterConfig) *LoginRateLimiter {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 5
	}
	if cfg.LockoutDuration <= 0 {
		cfg.LockoutDuration = 15 * time.Minute
	}
	if cfg.MaxEntries <= 0 {
		cfg.MaxEntries = 10000
	}
	if cfg.Clock == nil {
		cfg.Clock = realClock{}
	}
	return &LoginRateLimiter{
		clock:           cfg.Clock,
		entries:         make(map[string]*attemptEntry),
		maxAttempts:     cfg.MaxAttempts,
		lockoutDuration: cfg.LockoutDuration,
		maxEntries:      cfg.MaxEntries,
	}
}

// Allow reports whether a request from the given key should be allowed.
func (rl *LoginRateLimiter) Allow(key string) bool {
	rl.mu.RLock()
	entry, ok := rl.entries[key]
	if !ok {
		rl.mu.RUnlock()
		return true
	}
	now := rl.clock.Now()
	if entry.lockout.After(now) {
		rl.mu.RUnlock()
		return false
	}
	rl.mu.RUnlock()
	return true
}

// RecordFailure records a failed attempt for the given key.
func (rl *LoginRateLimiter) RecordFailure(key string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// If the map has grown beyond the cap, shed stale entries.
	if len(rl.entries) >= rl.maxEntries {
		rl.cleanupExpiredLocked()
	}

	now := rl.clock.Now()
	entry, ok := rl.entries[key]
	if !ok {
		entry = &attemptEntry{count: 1}
		if entry.count >= rl.maxAttempts {
			entry.lockout = now.Add(rl.lockoutDuration)
		}
		rl.entries[key] = entry
		return
	}

	if wasLocked := entry.lockout.After(now); wasLocked {
		entry.count++
		return
	}

	if !entry.lockout.IsZero() {
		entry.count = 1
		entry.lockout = time.Time{}
		return
	}

	entry.count++
	if entry.count >= rl.maxAttempts {
		entry.lockout = now.Add(rl.lockoutDuration)
	}
}

// Cleanup removes entries that are no longer in an active lockout state.
// This prevents unbounded map growth from stale tracking data.
func (rl *LoginRateLimiter) Cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.cleanupExpiredLocked()
}

// cleanupExpiredLocked removes entries whose lockout has expired and that are
// not currently tracking active lockouts. Must be called with rl.mu held.
func (rl *LoginRateLimiter) cleanupExpiredLocked() {
	now := rl.clock.Now()
	for key, entry := range rl.entries {
		if entry.lockout.IsZero() || !entry.lockout.After(now) {
			delete(rl.entries, key)
		}
	}
}

// Reset clears the attempt record for the given key.
func (rl *LoginRateLimiter) Reset(key string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.entries, key)
}

// RemainingAttempts returns how many more failures are allowed before lockout
// for the given key. Returns 0 if already locked out.
func (rl *LoginRateLimiter) RemainingAttempts(key string) int {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	entry, ok := rl.entries[key]
	if !ok {
		return rl.maxAttempts
	}

	now := rl.clock.Now()
	if entry.lockout.After(now) {
		return 0
	}

	remaining := rl.maxAttempts - entry.count
	if remaining < 0 {
		return 0
	}
	return remaining
}
