package retry

import (
	"context"
	"fmt"
	"time"

	"gorouter/internal/domain/engine"
	"gorouter/internal/domain/provider"
)

// Executor wraps an engine.Executor with retry and account fallback logic.
type Executor struct {
	inner    engine.Executor
	policy   *Policy
	fallback *FallbackSelector
}

// NewExecutor creates a retry wrapper around an engine.Executor.
func NewExecutor(inner engine.Executor, policy *Policy, fallback *FallbackSelector) *Executor {
	return &Executor{
		inner:    inner,
		policy:   policy,
		fallback: fallback,
	}
}

// Execute sends a request with retry and account fallback.
//
// On a retryable failure the wrapper waits for the computed backoff delay
// and retries with the same account. When the current account is exhausted
// the fallback selector picks the next available account.
func (e *Executor) Execute(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	current := account
	attempt := 1

	for {
		resp, err := e.inner.Execute(ctx, req, current)

		if err == nil {
			if e.fallback != nil && e.fallback.cooldown != nil {
				e.fallback.cooldown.RecordSuccess(ctx, current.ID)
			}
			return resp, nil
		}

		decision := e.policy.Evaluate(attempt, err)

		if e.fallback != nil && e.fallback.cooldown != nil {
			e.fallback.cooldown.RecordFailure(ctx, current.ID, err)
		}

		if !decision.ShouldRetry {
			return nil, fmt.Errorf("request failed after attempt %d: %w", attempt, err)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(decision.Delay):
		}

		attempt++

		if e.fallback != nil && attempt > e.policy.config.MaxAttempts/2 {
			next := e.fallback.NextAccount(ctx, []provider.Account{*current}, current.ID)
			if next != nil {
				current = next
			}
		}
	}
}

// ExecuteStream sends a streaming request with limited retry.
//
// Stream retries are limited to a single retry since streams carry
// stateful server-side context.
func (e *Executor) ExecuteStream(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	resp, err := e.inner.ExecuteStream(ctx, req, account)
	if err == nil {
		if e.fallback != nil && e.fallback.cooldown != nil {
			e.fallback.cooldown.RecordSuccess(ctx, account.ID)
		}
		return resp, nil
	}

	if e.fallback != nil && e.fallback.cooldown != nil {
		e.fallback.cooldown.RecordFailure(ctx, account.ID, err)
	}

	decision := e.policy.Evaluate(1, err)
	if !decision.ShouldRetry {
		return nil, fmt.Errorf("stream request failed: %w", err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(decision.Delay):
	}

	resp, err = e.inner.ExecuteStream(ctx, req, account)
	if err != nil {
		if e.fallback != nil && e.fallback.cooldown != nil {
			e.fallback.cooldown.RecordFailure(ctx, account.ID, err)
		}
		return nil, fmt.Errorf("stream retry failed: %w", err)
	}

	if e.fallback != nil && e.fallback.cooldown != nil {
		e.fallback.cooldown.RecordSuccess(ctx, account.ID)
	}
	return resp, nil
}