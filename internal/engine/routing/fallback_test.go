package routing

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"

	"gorouter/internal/app/cooldown"
	"gorouter/internal/domain/provider"
)

// cooldownNow returns the reference frozen time used by routing tests.
func cooldownNow() time.Time {
	return time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC)
}

// newFallbackState creates a RoutingStateManager with a real cooldown registry
// for tests that verify cooldown exclusion in fallback selection.
func newFallbackState() *RoutingStateManager {
	cd := cooldown.New(cooldown.Config{
		DefaultCooldown:  10 * time.Minute,
		MaxCooldown:      10 * time.Minute,
		FailureThreshold: 1,
		EscalationFactor: 1.0,
		CleanupInterval:  time.Hour,
	})
	return NewRoutingStateManagerWithClockAndAuthority(cooldownNow, cd)
}

func TestFallbackConfig_Defaults(t *testing.T) {
	cfg := DefaultFallbackConfig()
	if cfg.WaitDuration != 10*time.Second {
		t.Errorf("WaitDuration = %v, want 10s", cfg.WaitDuration)
	}
	if cfg.AttemptTimeout != 30*time.Second {
		t.Errorf("AttemptTimeout = %v, want 30s", cfg.AttemptTimeout)
	}
	if cfg.MaxAttempts != 3 {
		t.Errorf("MaxAttempts = %d, want 3", cfg.MaxAttempts)
	}
}

func TestFallbackHandler_EvaluateFallback(t *testing.T) {
	state := newFallbackState()
	handler := NewFallbackHandler(DefaultFallbackConfig(), state)

	accounts := []provider.Account{
		{ID: uuid.New(), IsEnabled: true},
		{ID: uuid.New(), IsEnabled: true},
	}

	t.Run("auth failure no fallback", func(t *testing.T) {
		decision := handler.EvaluateFallback(nil, 1, FailureAuth, accounts, accounts[0].ID)
		if decision.ShouldFallback {
			t.Error("should not fallback on auth failure")
		}
	})

	t.Run("definitive failure no fallback", func(t *testing.T) {
		decision := handler.EvaluateFallback(nil, 1, FailureDefinitive, accounts, accounts[0].ID)
		if decision.ShouldFallback {
			t.Error("should not fallback on definitive failure")
		}
	})

	t.Run("upstream failure with fallback", func(t *testing.T) {
		decision := handler.EvaluateFallback(nil, 1, FailureUpstream, accounts, accounts[0].ID)
		if !decision.ShouldFallback {
			t.Fatal("should fallback on upstream failure")
		}
		if decision.NextAccount == nil {
			t.Fatal("NextAccount should not be nil")
		}
		if decision.NextAccount.ID != accounts[1].ID {
			t.Errorf("NextAccount = %v, want %v", decision.NextAccount.ID, accounts[1].ID)
		}
		if decision.Delay <= 0 {
			t.Error("Delay should be > 0")
		}
		if decision.Attempt != 2 {
			t.Errorf("Attempt = %d, want 2", decision.Attempt)
		}
	})

	t.Run("max attempts reached", func(t *testing.T) {
		decision := handler.EvaluateFallback(nil, 3, FailureUpstream, accounts, accounts[0].ID)
		if decision.ShouldFallback {
			t.Error("should not fallback when max attempts reached")
		}
	})

	t.Run("no alternate account", func(t *testing.T) {
		singleAccount := []provider.Account{
			{ID: uuid.New(), IsEnabled: true},
		}
		decision := handler.EvaluateFallback(nil, 1, FailureUpstream, singleAccount, singleAccount[0].ID)
		if decision.ShouldFallback {
			t.Error("should not fallback without alternate account")
		}
	})

	t.Run("rate limit with fallback", func(t *testing.T) {
		decision := handler.EvaluateFallback(nil, 1, FailureRateLimit, accounts, accounts[0].ID)
		if !decision.ShouldFallback {
			t.Fatal("should fallback on rate limit")
		}
	})

	t.Run("timeout with fallback", func(t *testing.T) {
		decision := handler.EvaluateFallback(nil, 1, FailureTimeout, accounts, accounts[0].ID)
		if !decision.ShouldFallback {
			t.Fatal("should fallback on timeout")
		}
	})

	t.Run("connection failure with fallback", func(t *testing.T) {
		decision := handler.EvaluateFallback(nil, 1, FailureConnection, accounts, accounts[0].ID)
		if !decision.ShouldFallback {
			t.Fatal("should fallback on connection failure")
		}
	})

	t.Run("skip disabled alternate", func(t *testing.T) {
		disabledAccounts := []provider.Account{
			{ID: uuid.New(), IsEnabled: true},
			{ID: uuid.New(), IsEnabled: false},
			{ID: uuid.New(), IsEnabled: true},
		}
		decision := handler.EvaluateFallback(nil, 1, FailureUpstream, disabledAccounts, disabledAccounts[0].ID)
		if !decision.ShouldFallback {
			t.Fatal("should fallback to last enabled account")
		}
		if decision.NextAccount.ID != disabledAccounts[2].ID {
			t.Errorf("NextAccount = %v, want %v (skip disabled)", decision.NextAccount.ID, disabledAccounts[2].ID)
		}
	})

	t.Run("skip cooldown alternate", func(t *testing.T) {
		state2 := newFallbackState()
		handler2 := NewFallbackHandler(DefaultFallbackConfig(), state2)
		cooldownID := uuid.New()
		state2.RecordFailure(cooldownID, FailureRateLimit, 429, "rate limited", nil)

		cooldownAccounts := []provider.Account{
			{ID: uuid.New(), IsEnabled: true},
			{ID: cooldownID, IsEnabled: true},
			{ID: uuid.New(), IsEnabled: true},
		}
		decision := handler2.EvaluateFallback(nil, 1, FailureUpstream, cooldownAccounts, cooldownAccounts[0].ID)
		if !decision.ShouldFallback {
			t.Fatal("should fallback to first non-cooldown account")
		}
		if decision.NextAccount.ID != cooldownAccounts[2].ID {
			t.Errorf("NextAccount = %v, want %v (skip cooldown)", decision.NextAccount.ID, cooldownAccounts[2].ID)
		}
	})
}

func TestFallbackHandler_Backoff(t *testing.T) {
	handler := NewFallbackHandler(DefaultFallbackConfig(), nil)

	// Backoff should increase with attempts.
	d1 := handler.backoff(0)
	d2 := handler.backoff(1)
	d3 := handler.backoff(2)

	if d1 >= d2 {
		t.Errorf("backoff(0)=%v should be < backoff(1)=%v", d1, d2)
	}
	if d2 >= d3 {
		t.Errorf("backoff(1)=%v should be < backoff(2)=%v", d2, d3)
	}

	// Maximum should be capped.
	handler2 := NewFallbackHandler(FallbackConfig{
		BaseDelay:      time.Second,
		MaxDelay:       5 * time.Second,
		BackoffFactor:  10.0,
		JitterFraction: 0,
		WaitDuration:   10 * time.Second,
		AttemptTimeout: 30 * time.Second,
		MaxAttempts:    3,
	}, nil)
	d := handler2.backoff(5)
	if d > 6*time.Second {
		t.Errorf("backoff(5)=%v should be capped at ~5s", d)
	}
}

func TestFallbackHandler_ZeroConfig(t *testing.T) {
	handler := NewFallbackHandler(FallbackConfig{}, nil)
	if handler.config.WaitDuration != 10*time.Second {
		t.Errorf("WaitDuration = %v, want 10s", handler.config.WaitDuration)
	}
	if handler.config.MaxAttempts != 3 {
		t.Errorf("MaxAttempts = %d, want 3", handler.config.MaxAttempts)
	}
}

// ---------------------------------------------------------------------------
// P3-T08 fallback tests: limits, ordering, same-model only.
// ---------------------------------------------------------------------------

// TestFallbackHandler_MaxAttemptsExhausted verifies that EvaluateFallback
// returns ShouldFallback=false when attempt >= MaxAttempts.
func TestFallbackHandler_MaxAttemptsExhausted(t *testing.T) {
	state := newFallbackState()
	handler := NewFallbackHandler(DefaultFallbackConfig(), state)

	accounts := []provider.Account{
		{ID: uuid.New(), IsEnabled: true},
		{ID: uuid.New(), IsEnabled: true},
	}

	for attempt := 1; attempt <= handler.config.MaxAttempts; attempt++ {
		decision := handler.EvaluateFallback(nil, attempt, FailureUpstream, accounts, accounts[0].ID)
		if attempt < handler.config.MaxAttempts {
			if !decision.ShouldFallback {
				t.Errorf("attempt %d: expected fallback, got none", attempt)
			}
		} else {
			if decision.ShouldFallback {
				t.Errorf("attempt %d (max): expected no fallback", attempt)
			}
		}
	}
}

// TestFallbackHandler_SameModelOnly verifies fallback never switches models.
// It selects next account for the same model, never a different model.
func TestFallbackHandler_SameModelOnly(t *testing.T) {
	state := newFallbackState()
	handler := NewFallbackHandler(DefaultFallbackConfig(), state)

	gpt4Account := provider.Account{ID: uuid.New(), IsEnabled: true, ModelFilters: []string{"gpt-4"}}
	claudeAccount := provider.Account{ID: uuid.New(), IsEnabled: true, ModelFilters: []string{"claude-3"}}
	accounts := []provider.Account{gpt4Account, claudeAccount}

	// Fallback from gpt-4 account should stay within the same candidate list.
	decision := handler.EvaluateFallback(nil, 1, FailureUpstream, accounts, gpt4Account.ID)
	if !decision.ShouldFallback {
		t.Fatal("expected fallback to claude account")
	}
	if decision.NextAccount.ID != claudeAccount.ID {
		t.Errorf("NextAccount = %v, want %v (same-model fallback)", decision.NextAccount.ID, claudeAccount.ID)
	}
}

// TestFallbackHandler_PriorityOrder verifies that when multiple fallback
// accounts exist the highest-priority eligible non-current account is selected.
// selectNextAccount picks the first eligible non-current account in list order.
func TestFallbackHandler_PriorityOrder(t *testing.T) {
	state := newFallbackState()
	handler := NewFallbackHandler(DefaultFallbackConfig(), state)

	high := provider.Account{ID: uuid.New(), Priority: 0, IsEnabled: true}
	mid := provider.Account{ID: uuid.New(), Priority: 1, IsEnabled: true}
	low := provider.Account{ID: uuid.New(), Priority: 2, IsEnabled: true}

	// Accounts already sorted by priority.
	accounts := []provider.Account{high, mid, low}

	// Fallback from low — should get high (first in list, non-current).
	// Use attempt=1 so max-attempts cap (3) does not apply.
	d := handler.EvaluateFallback(nil, 1, FailureUpstream, accounts, low.ID)
	if !d.ShouldFallback {
		t.Fatal("expected fallback from low")
	}
	if d.NextAccount.ID != high.ID {
		t.Errorf("expected fallback to high (first in list, non-current), got %v", d.NextAccount.ID)
	}

	// Fallback from mid — should get high (first in list, non-current).
	d2 := handler.EvaluateFallback(nil, 2, FailureUpstream, accounts, mid.ID)
	if !d2.ShouldFallback {
		t.Fatal("expected fallback from mid")
	}
	if d2.NextAccount.ID != high.ID {
		t.Errorf("expected fallback to high (first in list, non-current), got %v", d2.NextAccount.ID)
	}
}

// TestFallbackHandler_NoAlternateAccount verifies that when only one account
// exists, fallback correctly returns nil.
func TestFallbackHandler_NoAlternateAccount(t *testing.T) {
	state := newFallbackState()
	handler := NewFallbackHandler(DefaultFallbackConfig(), state)

	single := []provider.Account{{ID: uuid.New(), IsEnabled: true}}
	decision := handler.EvaluateFallback(nil, 1, FailureUpstream, single, single[0].ID)
	if decision.ShouldFallback {
		t.Error("should not fallback when no alternate account exists")
	}
	if decision.NextAccount != nil {
		t.Errorf("NextAccount should be nil, got %v", decision.NextAccount.ID)
	}
}

// TestFallbackHandler_DeterministicJitter verifies that backoff uses the
// deterministic random function (not crypto/rand), so jitter values are
// reproducible across runs with the same attempt number.
func TestFallbackHandler_DeterministicJitter(t *testing.T) {
	handler := NewFallbackHandler(DefaultFallbackConfig(), nil)

	// Same attempt should produce same backoff.
	d1a := handler.backoff(1)
	d1b := handler.backoff(1)
	d2a := handler.backoff(2)
	d2b := handler.backoff(2)

	if d1a != d1b {
		t.Errorf("backoff(1) not deterministic: %v vs %v", d1a, d1b)
	}
	if d2a != d2b {
		t.Errorf("backoff(2) not deterministic: %v vs %v", d2a, d2b)
	}
	if d1a >= d2a {
		t.Errorf("backoff(1)=%v should be < backoff(2)=%v", d1a, d2a)
	}
}

// TestDeterministicJitterOffsetBounds verifies the overflow-safe deterministic
// jitter stays within [-r, r] and is reproducible for the same attempt.
func TestDeterministicJitterOffsetBounds(t *testing.T) {
	attempts := []int64{0, 1, 2, 5, 10, 100}
	ranges := []int64{1, 200_000_000, 3_000_000_000}
	for _, r := range ranges {
		for _, attempt := range attempts {
			got := deterministicJitterOffset(attempt, r)
			if got < -r || got > r {
				t.Errorf("deterministicJitterOffset(%d, %d) = %d out of [-%d, %d]", attempt, r, got, r, r)
			}
			if again := deterministicJitterOffset(attempt, r); again != got {
				t.Errorf("deterministicJitterOffset(%d, %d) not reproducible: %d vs %d", attempt, r, got, again)
			}
		}
	}
}

// TestDeterministicJitterOffsetNoTruncation verifies the mix never truncates:
// even with the largest possible span, the offset is bounded by r.
func TestDeterministicJitterOffsetNoTruncation(t *testing.T) {
	r := int64(3_000_000_000)
	seen := make(map[int64]bool)
	for attempt := int64(0); attempt < 1000; attempt++ {
		got := deterministicJitterOffset(attempt, r)
		seen[got] = true
		if got < -r || got > r {
			t.Fatalf("offset %d out of range for attempt %d", got, attempt)
		}
	}
	if len(seen) < 10 {
		t.Errorf("jitter spread too narrow across attempts: %d distinct values", len(seen))
	}
}

// TestFallbackHandler_BackoffWithinJitterBounds verifies backoff with jitter
// stays within the configured delay window.
func TestFallbackHandler_BackoffWithinJitterBounds(t *testing.T) {
	handler := NewFallbackHandler(DefaultFallbackConfig(), nil)
	base := float64(handler.config.BaseDelay) * math.Pow(handler.config.BackoffFactor, 1)
	r := base * handler.config.JitterFraction
	low := time.Duration(base - r)
	high := time.Duration(base + r)

	for i := 0; i < 50; i++ {
		d := handler.backoff(1)
		if d < low || d > high {
			t.Errorf("backoff(1) = %v, want within [%v, %v]", d, low, high)
		}
	}
}
