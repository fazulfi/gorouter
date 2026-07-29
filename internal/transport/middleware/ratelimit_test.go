package middleware

import (
	"fmt"
	"testing"
	"time"
)

type fakeClock struct {
	now time.Time
}

func (f *fakeClock) Now() time.Time { return f.now }

func (f *fakeClock) advance(d time.Duration) { f.now = f.now.Add(d) }

func newFakeClock(t time.Time) *fakeClock { return &fakeClock{now: t} }

func TestLoginRateLimiter_AllowsWithinThreshold(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC))
	rl := NewLoginRateLimiter(LoginRateLimiterConfig{
		MaxAttempts:     5,
		LockoutDuration: 15 * time.Minute,
		Clock:           clock,
	})

	ip := "192.168.1.100"
	for i := 0; i < 4; i++ {
		rl.RecordFailure(ip)
		if !rl.Allow(ip) {
			t.Errorf("attempt %d: expected allowed (under threshold), got blocked", i+1)
		}
	}
}

func TestLoginRateLimiter_LockoutAfterThreshold(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC))
	rl := NewLoginRateLimiter(LoginRateLimiterConfig{
		MaxAttempts:     3,
		LockoutDuration: 15 * time.Minute,
		Clock:           clock,
	})

	ip := "192.168.1.100"
	for i := 0; i < 3; i++ {
		rl.RecordFailure(ip)
	}
	if rl.Allow(ip) {
		t.Error("expected blocked after 3 failed attempts, got allowed")
	}
}

func TestLoginRateLimiter_ExpiryAfterLockoutDuration(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC))
	rl := NewLoginRateLimiter(LoginRateLimiterConfig{
		MaxAttempts:     2,
		LockoutDuration: 15 * time.Minute,
		Clock:           clock,
	})

	ip := "10.0.0.1"
	rl.RecordFailure(ip)
	rl.RecordFailure(ip)

	if rl.Allow(ip) {
		t.Error("expected blocked immediately after threshold")
	}

	clock.advance(16 * time.Minute)

	if !rl.Allow(ip) {
		t.Error("expected allowed after lockout duration expired")
	}
}

func TestLoginRateLimiter_ResetOnSuccess(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC))
	rl := NewLoginRateLimiter(LoginRateLimiterConfig{
		MaxAttempts:     2,
		LockoutDuration: 15 * time.Minute,
		Clock:           clock,
	})

	ip := "10.0.0.1"
	rl.RecordFailure(ip)
	rl.RecordFailure(ip)

	if rl.Allow(ip) {
		t.Error("expected blocked after threshold")
	}

	rl.Reset(ip)

	if !rl.Allow(ip) {
		t.Error("expected allowed after reset")
	}
}

func TestLoginRateLimiter_IsolatedPerIP(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC))
	rl := NewLoginRateLimiter(LoginRateLimiterConfig{
		MaxAttempts:     2,
		LockoutDuration: 15 * time.Minute,
		Clock:           clock,
	})

	attacker := "10.0.0.1"
	legit := "10.0.0.2"

	rl.RecordFailure(attacker)
	rl.RecordFailure(attacker)

	if rl.Allow(attacker) {
		t.Error("expected attacker blocked")
	}
	if !rl.Allow(legit) {
		t.Error("expected legit user allowed")
	}
}

func TestLoginRateLimiter_DefaultConfig(t *testing.T) {
	rl := NewLoginRateLimiter(LoginRateLimiterConfig{})
	if rl == nil {
		t.Fatal("expected non-nil rate limiter")
	}
	if !rl.Allow("10.0.0.1") {
		t.Error("default config should allow initially")
	}
}

func TestLoginRateLimiter_ConcurrentAccess(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC))
	rl := NewLoginRateLimiter(LoginRateLimiterConfig{
		MaxAttempts:     100,
		LockoutDuration: 15 * time.Minute,
		Clock:           clock,
	})

	done := make(chan struct{})
	for i := 0; i < 20; i++ {
		go func() {
			rl.RecordFailure("10.0.0.1")
			rl.Allow("10.0.0.1")
			rl.Reset("10.0.0.2")
			done <- struct{}{}
		}()
	}
	for i := 0; i < 20; i++ {
		<-done
	}
}

func TestLoginRateLimiter_ExpiredEntryCleanup(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC))
	rl := NewLoginRateLimiter(LoginRateLimiterConfig{
		MaxAttempts:     2,
		LockoutDuration: 15 * time.Minute,
		Clock:           clock,
	})

	ip := "10.0.0.1"
	rl.RecordFailure(ip)
	rl.RecordFailure(ip)

	if rl.Allow(ip) {
		t.Error("expected blocked")
	}

	clock.advance(30 * time.Minute)

	if !rl.Allow(ip) {
		t.Error("expected allowed after lockout expired")
	}

	rl.RecordFailure(ip)
	if !rl.Allow(ip) {
		t.Error("expected allowed after fresh failure following expiry")
	}
}

func TestLoginRateLimiter_MultipleFailureRecords(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC))
	rl := NewLoginRateLimiter(LoginRateLimiterConfig{
		MaxAttempts:     3,
		LockoutDuration: 15 * time.Minute,
		Clock:           clock,
	})

	ip := "10.0.0.1"
	if rl.RemainingAttempts(ip) != 3 {
		t.Errorf("expected 3 remaining, got %d", rl.RemainingAttempts(ip))
	}

	rl.RecordFailure(ip)
	if rl.RemainingAttempts(ip) != 2 {
		t.Errorf("expected 2 remaining, got %d", rl.RemainingAttempts(ip))
	}

	rl.RecordFailure(ip)
	rl.RecordFailure(ip)
	if rl.RemainingAttempts(ip) != 0 {
		t.Errorf("expected 0 remaining, got %d", rl.RemainingAttempts(ip))
	}

	rl.Reset(ip)
	if rl.RemainingAttempts(ip) != 3 {
		t.Errorf("expected 3 remaining after reset, got %d", rl.RemainingAttempts(ip))
	}
}

func TestLoginRateLimiter_SingleAttemptLockout(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC))
	rl := NewLoginRateLimiter(LoginRateLimiterConfig{
		MaxAttempts:     1,
		LockoutDuration: 15 * time.Minute,
		Clock:           clock,
	})

	ip := "10.0.0.1"
	rl.RecordFailure(ip)
	if rl.Allow(ip) {
		t.Error("expected blocked after 1 attempt with MaxAttempts=1")
	}
}

func TestLoginRateLimiter_CleanupRemovesExpiredLockouts(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC))
	rl := NewLoginRateLimiter(LoginRateLimiterConfig{
		MaxAttempts:     2,
		LockoutDuration: 15 * time.Minute,
		Clock:           clock,
	})

	ip1 := "10.0.0.1"
	ip2 := "10.0.0.2"

	// Lock out ip1 with 2 failures, lock out ip2 with 2 failures
	rl.RecordFailure(ip1)
	rl.RecordFailure(ip1)
	rl.RecordFailure(ip2)
	rl.RecordFailure(ip2)

	if rl.Allow(ip1) || rl.Allow(ip2) {
		t.Error("expected both locked out")
	}

	// Advance past both lockouts
	clock.advance(20 * time.Minute)

	// Cleanup should remove both (lockout expired)
	rl.Cleanup()

	// Both should now be treated as unknown (Allow returns true for unknown keys)
	if !rl.Allow(ip1) {
		t.Error("expected ip1 allowed after cleanup of expired lockout")
	}
	if !rl.Allow(ip2) {
		t.Error("expected ip2 allowed after cleanup of expired lockout")
	}
}

func TestLoginRateLimiter_CleanupKeepsActiveLockouts(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC))
	rl := NewLoginRateLimiter(LoginRateLimiterConfig{
		MaxAttempts:     2,
		LockoutDuration: 15 * time.Minute,
		Clock:           clock,
	})

	ip := "10.0.0.1"
	rl.RecordFailure(ip)
	rl.RecordFailure(ip)

	// Still locked out
	rl.Cleanup()

	if rl.Allow(ip) {
		t.Error("expected ip still blocked after cleanup while lockout active")
	}
}

func TestLoginRateLimiter_RecordFailureTriggersCleanupAtCap(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC))
	rl := NewLoginRateLimiter(LoginRateLimiterConfig{
		MaxAttempts:     5,
		LockoutDuration: 15 * time.Minute,
		MaxEntries:      10,
		Clock:           clock,
	})

	// Fill with 10 expired entries (single failure each, lockout never triggered)
	for i := 0; i < 10; i++ {
		ip := fmt.Sprintf("10.0.0.%d", i+1)
		rl.RecordFailure(ip)
	}

	// Advance so all entries' lockout window has passed (they never entered lockout)
	clock.advance(30 * time.Minute)

	// RecordFailure on a new key should trigger cleanup, removing the 10 stale entries
	rl.RecordFailure("10.0.0.99")

	// After cleanup, the new key should be tracked and old keys should be gone
	if rl.RemainingAttempts("10.0.0.99") >= 5 {
		t.Error("expected new key tracked after cleanup-triggering RecordFailure")
	}
}
