// Package cooldown provides an in-memory cooldown registry that tracks provider
// account failures and prevents selection of recently-failed accounts.
//
// The registry is thread-safe via sync.RWMutex and uses a background goroutine
// to periodically purge stale entries. Cooldown durations escalate with repeated
// failures to provide exponential backoff.
//
// This is an in-memory implementation only (decision D114 — no persistence).
package cooldown

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"gorouter/internal/domain/provider"

	"github.com/google/uuid"
)

// Config controls cooldown behavior for the Registry.
type Config struct {
	// DefaultCooldown is the initial cooldown duration after the failure
	// threshold is reached. Default: 30s.
	DefaultCooldown time.Duration

	// MaxCooldown is the absolute upper bound for any cooldown period.
	// Default: 300s (5 minutes).
	MaxCooldown time.Duration

	// FailureThreshold is the number of consecutive failures required to
	// trigger a cooldown. Must be >= 1. Default: 3.
	FailureThreshold int

	// EscalationFactor is the multiplier applied per additional failure
	// beyond the threshold: cooldown = DefaultCooldown * EscalationFactor^(failures - threshold).
	// Must be >= 1.0. Default: 1.5.
	EscalationFactor float64

	// CleanupInterval controls how often the background goroutine purges
	// expired entries. Default: 5 minutes.
	CleanupInterval time.Duration
}

// DefaultConfig returns a Config populated with sensible defaults.
func DefaultConfig() Config {
	return Config{
		DefaultCooldown:  30 * time.Second,
		MaxCooldown:      300 * time.Second, // 5 minutes
		FailureThreshold: 3,
		EscalationFactor: 1.5,
		CleanupInterval:  5 * time.Minute,
	}
}

// entry holds the runtime state for a single account in the cooldown registry.
type entry struct {
	accountID     uuid.UUID
	status        provider.AccountStatus
	failures      int
	lastErr       string
	cooldownUntil time.Time
	createdAt     time.Time
	updatedAt     time.Time
}

// Registry is an in-memory cooldown tracker that maps account IDs to their
// failure state and cooldown period. All exported methods are thread-safe.
type Registry struct {
	mu       sync.RWMutex
	entries  map[uuid.UUID]*entry
	defaults Config

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// New creates a new Registry with the given Config. If config is zero-valued,
// DefaultConfig is used.
func New(config Config) *Registry {
	if config == (Config{}) {
		config = DefaultConfig()
	}
	return &Registry{
		entries:  make(map[uuid.UUID]*entry),
		defaults: config,
		stopCh:   make(chan struct{}),
	}
}

// IsOnCooldown checks whether the specified account is currently cooled down.
// Returns true when the account has exceeded the failure threshold and its
// cooldown period has not yet expired. If the cooldown has expired, the entry
// is eagerly removed.
func (r *Registry) IsOnCooldown(_ context.Context, accountID uuid.UUID) bool {
	r.mu.RLock()
	e, ok := r.entries[accountID]
	if !ok {
		r.mu.RUnlock()
		return false
	}
	if time.Now().Before(e.cooldownUntil) {
		r.mu.RUnlock()
		return true
	}
	r.mu.RUnlock()

	// Cooldown expired — eagerly clean up under write lock.
	r.mu.Lock()
	e2, ok2 := r.entries[accountID]
	if ok2 && !e2.cooldownUntil.IsZero() && !time.Now().Before(e2.cooldownUntil) {
		delete(r.entries, accountID)
	}
	r.mu.Unlock()
	return false
}

// RecordFailure records a failure for the given account. If the consecutive
// failure count reaches the threshold, a cooldown is activated with a duration
// that escalates based on the escalation factor. The cooldown is capped at
// MaxCooldown.
func (r *Registry) RecordFailure(_ context.Context, accountID uuid.UUID, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	e, ok := r.entries[accountID]
	if !ok {
		e = &entry{
			accountID: accountID,
			status:    provider.AccountStatusActive,
			createdAt: now,
		}
		r.entries[accountID] = e
	}

	e.failures++
	e.updatedAt = now
	if err != nil {
		e.lastErr = err.Error()
	}

	if e.failures >= r.defaults.FailureThreshold {
		excessFailures := e.failures - r.defaults.FailureThreshold
		multiplier := math.Pow(r.defaults.EscalationFactor, float64(excessFailures))
		d := time.Duration(float64(r.defaults.DefaultCooldown) * multiplier)
		if d > r.defaults.MaxCooldown {
			d = r.defaults.MaxCooldown
		}
		e.cooldownUntil = now.Add(d)
		e.status = provider.AccountStatusCooldown
	}
}

// RecordSuccess records a success for the given account, resetting its failure
// count and clearing any active cooldown. No-op if the account is not tracked.
func (r *Registry) RecordSuccess(_ context.Context, accountID uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()

	e, ok := r.entries[accountID]
	if !ok {
		return
	}

	e.failures = 0
	e.lastErr = ""
	e.cooldownUntil = time.Time{}
	e.status = provider.AccountStatusActive
	e.updatedAt = time.Now()
}

// Status returns the current cooldown state for the specified account, or nil
// if the account has no entry in the registry.
func (r *Registry) Status(_ context.Context, accountID uuid.UUID) *provider.AccountCooldown {
	r.mu.RLock()
	defer r.mu.RUnlock()

	e, ok := r.entries[accountID]
	if !ok {
		return nil
	}

	return &provider.AccountCooldown{
		AccountID:  e.accountID,
		Reason:     e.lastErr,
		StartedAt:  e.updatedAt,
		ExpiresAt:  e.cooldownUntil,
		RetryCount: e.failures,
	}
}

// Reset clears all cooldown entries from the registry.
func (r *Registry) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = make(map[uuid.UUID]*entry)
}

// Healthy returns nil if the registry is operational. Always returns nil for
// this simple in-memory implementation.
func (r *Registry) Healthy(_ context.Context) error {
	return nil
}

// Start launches a background goroutine that periodically purges expired
// cooldown entries. The goroutine exits when ctx is cancelled or Stop is
// called. Safe to call multiple times — subsequent calls are no-ops after
// the first Start.
func (r *Registry) Start(ctx context.Context) {
	r.mu.Lock()
	select {
	case <-r.stopCh:
		// Already stopped; re-create the channel.
		r.stopCh = make(chan struct{})
	default:
		// Channel is open; proceed.
	}
	r.mu.Unlock()

	r.wg.Add(1)
	go r.cleanupLoop(ctx)
}

// Stop signals the background cleanup goroutine to exit and waits for it to
// finish. After Stop, the registry remains usable but cleanup must be
// re-started via Start.
func (r *Registry) Stop() {
	r.mu.Lock()
	select {
	case <-r.stopCh:
		// Already closed.
	default:
		close(r.stopCh)
	}
	r.mu.Unlock()

	r.wg.Wait()
}

// cleanupLoop runs until ctx is done or stopCh is closed, purging expired
// entries at each CleanupInterval tick.
func (r *Registry) cleanupLoop(ctx context.Context) {
	defer r.wg.Done()

	ticker := time.NewTicker(r.defaults.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			r.purgeExpired()
			return
		case <-r.stopCh:
			r.purgeExpired()
			return
		case <-ticker.C:
			r.purgeExpired()
		}
	}
}

// purgeExpired removes all entries whose cooldown has expired.
func (r *Registry) purgeExpired() {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	for id, e := range r.entries {
		if !e.cooldownUntil.IsZero() && !now.Before(e.cooldownUntil) {
			delete(r.entries, id)
		}
	}
}

// String returns a human-readable description of the Config.
func (c Config) String() string {
	return fmt.Sprintf(
		"DefaultCooldown=%s MaxCooldown=%s FailureThreshold=%d EscalationFactor=%.1f CleanupInterval=%s",
		c.DefaultCooldown, c.MaxCooldown, c.FailureThreshold, c.EscalationFactor, c.CleanupInterval,
	)
}
