package cooldown

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNewAccountNotOnCooldown(t *testing.T) {
	r := New(DefaultConfig())
	ctx := context.Background()
	acctID := uuid.New()

	if r.IsOnCooldown(ctx, acctID) {
		t.Error("expected new account not to be on cooldown")
	}
}

func TestFailuresBelowThreshold(t *testing.T) {
	r := New(Config{
		DefaultCooldown:  time.Minute,
		MaxCooldown:      time.Minute,
		FailureThreshold: 3,
		EscalationFactor: 1.0,
		CleanupInterval:  time.Hour,
	})
	ctx := context.Background()
	acctID := uuid.New()

	// Two failures, threshold is 3 → not on cooldown.
	r.RecordFailure(ctx, acctID, errors.New("bad response"))
	r.RecordFailure(ctx, acctID, errors.New("timeout"))

	if r.IsOnCooldown(ctx, acctID) {
		t.Error("expected account not on cooldown before reaching threshold")
	}
}

func TestFailuresAtThresholdActivatesCooldown(t *testing.T) {
	r := New(Config{
		DefaultCooldown:  time.Minute,
		MaxCooldown:      time.Minute,
		FailureThreshold: 3,
		EscalationFactor: 1.0,
		CleanupInterval:  time.Hour,
	})
	ctx := context.Background()
	acctID := uuid.New()

	r.RecordFailure(ctx, acctID, errors.New("bad response"))
	r.RecordFailure(ctx, acctID, errors.New("timeout"))
	r.RecordFailure(ctx, acctID, errors.New("server error"))

	if !r.IsOnCooldown(ctx, acctID) {
		t.Error("expected account on cooldown after reaching threshold")
	}
}

func TestCooldownExpiresAndAutoClears(t *testing.T) {
	r := New(Config{
		DefaultCooldown:  50 * time.Millisecond,
		MaxCooldown:      50 * time.Millisecond,
		FailureThreshold: 1,
		EscalationFactor: 1.0,
		CleanupInterval:  time.Hour,
	})
	ctx := context.Background()
	acctID := uuid.New()

	r.RecordFailure(ctx, acctID, errors.New("err"))

	if !r.IsOnCooldown(ctx, acctID) {
		t.Fatal("expected on cooldown immediately after failure")
	}

	// Wait for cooldown to expire.
	time.Sleep(60 * time.Millisecond)

	if r.IsOnCooldown(ctx, acctID) {
		t.Error("expected cooldown to expire and auto-clear")
	}
}

func TestRecordSuccessResetsFailures(t *testing.T) {
	r := New(Config{
		DefaultCooldown:  time.Minute,
		MaxCooldown:      time.Minute,
		FailureThreshold: 3,
		EscalationFactor: 1.0,
		CleanupInterval:  time.Hour,
	})
	ctx := context.Background()
	acctID := uuid.New()

	r.RecordFailure(ctx, acctID, errors.New("err"))
	r.RecordFailure(ctx, acctID, errors.New("err"))
	r.RecordSuccess(ctx, acctID)

	// After success, failures are reset; another failure should not trigger
	// cooldown yet.
	r.RecordFailure(ctx, acctID, errors.New("err"))

	if r.IsOnCooldown(ctx, acctID) {
		t.Error("expected RecordSuccess to reset failure counter")
	}
}

func TestEscalationLongerWithMoreFailures(t *testing.T) {
	r := New(Config{
		DefaultCooldown:  100 * time.Millisecond,
		MaxCooldown:      10 * time.Second,
		FailureThreshold: 2,
		EscalationFactor: 2.0,
		CleanupInterval:  time.Hour,
	})
	ctx := context.Background()
	acctID := uuid.New()

	// First cooldown: 2 failures → DefaultCooldown * 2^(2-2) = 100ms * 1 = 100ms.
	r.RecordFailure(ctx, acctID, errors.New("err"))
	r.RecordFailure(ctx, acctID, errors.New("err"))

	if !r.IsOnCooldown(ctx, acctID) {
		t.Fatal("expected on cooldown after threshold")
	}

	time.Sleep(150 * time.Millisecond)

	if r.IsOnCooldown(ctx, acctID) {
		t.Fatal("expected first cooldown to expire")
	}

	// Second cycle: 3 failures → DefaultCooldown * 2^(3-2) = 100ms * 2 = 200ms.
	r.RecordFailure(ctx, acctID, errors.New("err"))
	r.RecordFailure(ctx, acctID, errors.New("err"))
	r.RecordFailure(ctx, acctID, errors.New("err"))

	// 150ms in — should still be on cooldown (200ms total).
	time.Sleep(150 * time.Millisecond)

	if !r.IsOnCooldown(ctx, acctID) {
		t.Error("expected escalated cooldown to still be active at 150ms")
	}

	// Wait for it to expire.
	time.Sleep(100 * time.Millisecond)

	if r.IsOnCooldown(ctx, acctID) {
		t.Error("expected escalated cooldown to have expired")
	}
}

func TestMaxCooldownCap(t *testing.T) {
	r := New(Config{
		DefaultCooldown:  10 * time.Second,
		MaxCooldown:      50 * time.Millisecond, // very low cap
		FailureThreshold: 1,
		EscalationFactor: 100.0, // huge escalation, but should be capped
		CleanupInterval:  time.Hour,
	})
	ctx := context.Background()
	acctID := uuid.New()

	r.RecordFailure(ctx, acctID, errors.New("err"))

	// Cooldown should be capped at 50ms.
	time.Sleep(60 * time.Millisecond)

	if r.IsOnCooldown(ctx, acctID) {
		t.Error("expected cooldown to be capped at MaxCooldown and expire quickly")
	}
}

func TestStatusReturnsCorrectInfo(t *testing.T) {
	r := New(DefaultConfig())
	ctx := context.Background()
	acctID := uuid.New()

	// Unknown account → nil.
	if s := r.Status(ctx, acctID); s != nil {
		t.Fatal("expected nil status for unknown account")
	}

	cause := errors.New("rate limited")
	r.RecordFailure(ctx, acctID, cause)
	r.RecordFailure(ctx, acctID, cause)
	r.RecordFailure(ctx, acctID, cause)

	s := r.Status(ctx, acctID)
	if s == nil {
		t.Fatal("expected non-nil status")
	}
	if s.AccountID != acctID {
		t.Errorf("expected AccountID %v, got %v", acctID, s.AccountID)
	}
	if s.Reason != cause.Error() {
		t.Errorf("expected reason %q, got %q", cause.Error(), s.Reason)
	}
	if s.RetryCount != 3 {
		t.Errorf("expected RetryCount 3, got %d", s.RetryCount)
	}
	if s.ExpiresAt.IsZero() {
		t.Error("expected non-zero ExpiresAt")
	}
	if !s.IsOnCooldown() {
		t.Error("expected IsOnCooldown() to be true")
	}
}

func TestResetClearsAllEntries(t *testing.T) {
	r := New(Config{
		DefaultCooldown:  time.Minute,
		MaxCooldown:      time.Minute,
		FailureThreshold: 1,
		EscalationFactor: 1.0,
		CleanupInterval:  time.Hour,
	})
	ctx := context.Background()
	acctID := uuid.New()

	r.RecordFailure(ctx, acctID, errors.New("err"))
	r.Reset()

	if r.IsOnCooldown(ctx, acctID) {
		t.Error("expected Reset to clear all cooldown entries")
	}
	if s := r.Status(ctx, acctID); s != nil {
		t.Error("expected nil status after Reset")
	}
}

func TestRecordSuccessNoEntryIsNoOp(t *testing.T) {
	r := New(DefaultConfig())
	ctx := context.Background()

	// Should not panic.
	r.RecordSuccess(ctx, uuid.New())
}

func TestIsOnCooldownNoEntryReturnsFalse(t *testing.T) {
	r := New(DefaultConfig())
	ctx := context.Background()

	if r.IsOnCooldown(ctx, uuid.New()) {
		t.Error("expected false for unknown account")
	}
}

func TestHealthyAlwaysReturnsNil(t *testing.T) {
	r := New(DefaultConfig())
	ctx := context.Background()

	if err := r.Healthy(ctx); err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestZeroConfigUsesDefaults(t *testing.T) {
	r := New(Config{})
	if r.defaults.DefaultCooldown != 30*time.Second {
		t.Error("expected DefaultCooldown to be 30s")
	}
	if r.defaults.MaxCooldown != 300*time.Second {
		t.Error("expected MaxCooldown to be 300s")
	}
	if r.defaults.FailureThreshold != 3 {
		t.Error("expected FailureThreshold to be 3")
	}
	if r.defaults.EscalationFactor != 1.5 {
		t.Error("expected EscalationFactor to be 1.5")
	}
}

func TestThreadSafety(t *testing.T) {
	r := New(Config{
		DefaultCooldown:  time.Minute,
		MaxCooldown:      time.Minute,
		FailureThreshold: 2,
		EscalationFactor: 1.0,
		CleanupInterval:  time.Hour,
	})
	ctx := context.Background()

	var wg sync.WaitGroup
	const goroutines = 20

	// Concurrent writers.
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			acctID := uuid.New()
			r.RecordFailure(ctx, acctID, errors.New("concurrent failure"))
			_ = r.IsOnCooldown(ctx, acctID)
			r.RecordSuccess(ctx, acctID)
		}()
	}

	// Concurrent readers.
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			acctID := uuid.New()
			for j := 0; j < 5; j++ {
				_ = r.IsOnCooldown(ctx, acctID)
				_ = r.Status(ctx, acctID)
			}
		}()
	}

	wg.Wait()
	// If we get here without a data race, the test passes.
}

func TestStartStopLifecycle(t *testing.T) {
	r := New(Config{
		DefaultCooldown:  50 * time.Millisecond,
		MaxCooldown:      50 * time.Millisecond,
		FailureThreshold: 1,
		EscalationFactor: 1.0,
		CleanupInterval:  20 * time.Millisecond, // frequently cleanup
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	acctID := uuid.New()

	r.Start(ctx)
	defer r.Stop()

	r.RecordFailure(ctx, acctID, errors.New("boom"))

	if !r.IsOnCooldown(ctx, acctID) {
		t.Fatal("expected on cooldown")
	}

	// Wait for cleanup goroutine to purge expired entry.
	time.Sleep(100 * time.Millisecond)

	if r.IsOnCooldown(ctx, acctID) {
		t.Error("expected cleanup goroutine to remove expired entry")
	}
}

func TestStartStopMultipleCalls(t *testing.T) {
	r := New(DefaultConfig())
	ctx := context.Background()

	// Multiple Stop calls should not panic.
	r.Stop()
	r.Stop()

	// Multiple Start calls — second should be no-op.
	r.Start(ctx)
	r.Start(ctx)
	r.Stop()
}

func TestRecordFailureWithNilError(t *testing.T) {
	r := New(Config{
		DefaultCooldown:  time.Minute,
		MaxCooldown:      time.Minute,
		FailureThreshold: 1,
		EscalationFactor: 1.0,
		CleanupInterval:  time.Hour,
	})
	ctx := context.Background()
	acctID := uuid.New()

	r.RecordFailure(ctx, acctID, nil)

	if !r.IsOnCooldown(ctx, acctID) {
		t.Error("expected on cooldown even with nil error")
	}
	s := r.Status(ctx, acctID)
	if s.Reason != "" {
		t.Errorf("expected empty reason for nil error, got %q", s.Reason)
	}
}

func TestBackgroundCleanupViaStop(t *testing.T) {
	r := New(Config{
		DefaultCooldown:  20 * time.Millisecond,
		MaxCooldown:      20 * time.Millisecond,
		FailureThreshold: 1,
		EscalationFactor: 1.0,
		CleanupInterval:  time.Hour, // long interval, but Stop purges on exit
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	acctID := uuid.New()

	r.RecordFailure(ctx, acctID, errors.New("err"))

	// Wait for cooldown to expire.
	time.Sleep(30 * time.Millisecond)

	// Start with cancelled context so cleanupLoop exits immediately.
	ctxCancelled, cancelImmediate := context.WithCancel(context.Background())
	cancelImmediate()
	r.Start(ctxCancelled)
	r.Stop() // Stop should call purgeExpired and clean up expired entries

	if r.IsOnCooldown(ctx, acctID) {
		t.Error("expected Stop to purge expired entries")
	}
}

func TestErrorStoredAsReason(t *testing.T) {
	r := New(Config{
		DefaultCooldown:  time.Minute,
		MaxCooldown:      time.Minute,
		FailureThreshold: 3,
		EscalationFactor: 1.0,
		CleanupInterval:  time.Hour,
	})
	ctx := context.Background()
	acctID := uuid.New()

	r.RecordFailure(ctx, acctID, errors.New("first error"))
	r.RecordFailure(ctx, acctID, errors.New("second error"))
	r.RecordFailure(ctx, acctID, errors.New("third error"))

	s := r.Status(ctx, acctID)
	if s.Reason != "third error" {
		t.Errorf("expected last error as reason, got %q", s.Reason)
	}
}

func TestConsecutiveCallsDontResetFailures(t *testing.T) {
	r := New(Config{
		DefaultCooldown:  time.Minute,
		MaxCooldown:      time.Minute,
		FailureThreshold: 3,
		EscalationFactor: 1.0,
		CleanupInterval:  time.Hour,
	})
	ctx := context.Background()
	acctID := uuid.New()

	// Only consecutive failures should count. A success resets.
	r.RecordFailure(ctx, acctID, errors.New("err1"))
	r.RecordFailure(ctx, acctID, errors.New("err2"))
	r.RecordSuccess(ctx, acctID)

	if s := r.Status(ctx, acctID); s != nil && s.RetryCount != 0 {
		t.Errorf("expected RetryCount 0 after success, got %d", s.RetryCount)
	}

	// Now two failures should bring us to 2, not trigger cooldown.
	r.RecordFailure(ctx, acctID, errors.New("err3"))
	r.RecordFailure(ctx, acctID, errors.New("err4"))

	if r.IsOnCooldown(ctx, acctID) {
		t.Error("expected not on cooldown at 2 failures after reset")
	}
}
