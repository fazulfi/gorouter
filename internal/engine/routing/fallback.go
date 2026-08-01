package routing

import (
	"context"
	"math"
	"time"

	"github.com/google/uuid"

	"gorouter/internal/domain/provider"
)

// FallbackConfig controls same-model fallback behavior.
type FallbackConfig struct {
	// WaitDuration is how long to wait for a concurrent refresh/operation
	// before attempting same-model alternate account. Default: 10s.
	WaitDuration time.Duration

	// AttemptTimeout is the per-attempt timeout. Default: 30s.
	AttemptTimeout time.Duration

	// MaxAttempts is the maximum number of fallback attempts. Default: 3.
	MaxAttempts int

	// BackoffFactor controls exponential backoff between attempts. Default: 2.0.
	BackoffFactor float64

	// JitterFraction controls random jitter as fraction of delay. Default: 0.1.
	JitterFraction float64

	// BaseDelay is the initial backoff delay. Default: 1s.
	BaseDelay time.Duration

	// MaxDelay is the upper bound for backoff. Default: 30s.
	MaxDelay time.Duration
}

// DefaultFallbackConfig returns sensible defaults for fallback behavior.
func DefaultFallbackConfig() FallbackConfig {
	return FallbackConfig{
		WaitDuration:   10 * time.Second,
		AttemptTimeout: 30 * time.Second,
		MaxAttempts:    3,
		BackoffFactor:  2.0,
		JitterFraction: 0.1,
		BaseDelay:      1 * time.Second,
		MaxDelay:       30 * time.Second,
	}
}

// FallbackHandler manages same-model alternate account fallback logic.
// Decision D98: same-model alternate account only; no model switch outside combos.
type FallbackHandler struct {
	config FallbackConfig
	state  *RoutingStateManager
	now    func() time.Time
}

// NewFallbackHandler creates a FallbackHandler.
func NewFallbackHandler(config FallbackConfig, state *RoutingStateManager) *FallbackHandler {
	if config.WaitDuration == 0 {
		config.WaitDuration = 10 * time.Second
	}
	if config.AttemptTimeout == 0 {
		config.AttemptTimeout = 30 * time.Second
	}
	if config.MaxAttempts == 0 {
		config.MaxAttempts = 3
	}
	if config.BackoffFactor == 0 {
		config.BackoffFactor = 2.0
	}
	if config.JitterFraction == 0 {
		config.JitterFraction = 0.1
	}
	if config.BaseDelay == 0 {
		config.BaseDelay = 1 * time.Second
	}
	if config.MaxDelay == 0 {
		config.MaxDelay = 30 * time.Second
	}
	return &FallbackHandler{
		config: config,
		state:  state,
		now:    time.Now,
	}
}

// FallbackDecision represents the result of a fallback evaluation.
type FallbackDecision struct {
	// ShouldFallback indicates whether to proceed with fallback.
	ShouldFallback bool

	// NextAccount is the alternate account to use.
	NextAccount *provider.Account

	// Delay before attempting the next account.
	Delay time.Duration

	// Attempt is the attempt number (1-based).
	Attempt int

	// Reason describes why fallback is/isn't happening.
	Reason string
}

// EvaluateFallback determines whether to fall back to an alternate account
// after a failure. It implements:
//   - 10s wait for concurrent operation then same-model alternate
//   - 30s per-attempt timeout
//   - Max 3 attempts with exponential backoff+jitter
func (h *FallbackHandler) EvaluateFallback(
	ctx context.Context,
	attempt int,
	failureClass FailureClass,
	accounts []provider.Account,
	currentID uuid.UUID,
) *FallbackDecision {
	if attempt >= h.config.MaxAttempts {
		return &FallbackDecision{
			Attempt: attempt,
			Reason:  "max attempts reached",
		}
	}

	// Auth/definitive failures are terminal — do not fall back.
	if failureClass == FailureAuth || failureClass == FailureDefinitive {
		return &FallbackDecision{
			Attempt: attempt,
			Reason:  "terminal failure, no fallback",
		}
	}

	// Find next eligible account (same-model, different account).
	next := h.selectNextAccount(accounts, currentID)
	if next == nil {
		return &FallbackDecision{
			Attempt: attempt,
			Reason:  "no alternate account available",
		}
	}

	delay := h.backoff(attempt)

	return &FallbackDecision{
		ShouldFallback: true,
		NextAccount:    next,
		Delay:          delay,
		Attempt:        attempt + 1,
		Reason:         "same-model alternate account available",
	}
}

// selectNextAccount finds the next eligible account after currentID.
func (h *FallbackHandler) selectNextAccount(accounts []provider.Account, currentID uuid.UUID) *provider.Account {
	for _, a := range accounts {
		if a.ID == currentID {
			continue
		}
		if !a.IsEnabled {
			continue
		}
		if h.state != nil && !h.state.IsEligible(a.ID) {
			continue
		}
		return &a
	}
	return nil
}

// backoff computes the delay before the next attempt with exponential backoff + jitter.
func (h *FallbackHandler) backoff(attempt int) time.Duration {
	exp := math.Pow(h.config.BackoffFactor, float64(attempt))
	delay := float64(h.config.BaseDelay) * exp
	delay = math.Min(delay, float64(h.config.MaxDelay))

	if h.config.JitterFraction > 0 {
		jitterRange := delay * h.config.JitterFraction
		offset := deterministicRandInt64Range(int64(-jitterRange), int64(jitterRange+1))
		delay += float64(offset)
	}

	if delay < 0 {
		delay = 0
	}
	return time.Duration(delay)
}

// deterministicRandInt64Range returns a random int64 in [min, max).
func deterministicRandInt64Range(min, max int64) int64 {
	if max <= min {
		return min
	}
	return deterministicRandInt64(max-min) + min
}

// deterministicRandInt64 returns a non-negative deterministic int64 in [0, max).
// This is NOT cryptographically secure — it uses a hash-based deterministic
// function suitable for jitter in retry/fallback backoff calculations where
// reproducibility across runs is desirable.
func deterministicRandInt64(max int64) int64 {
	if max <= 0 {
		return 0
	}
	return int64(hashInt(int(max))) % max
}

// hashInt provides a simple deterministic hash for seedable random behaviour.
func hashInt(n int) int {
	h := uint64(n) * 0x9e3779b97f4a7c15
	h ^= h >> 37
	return int(h)
}
