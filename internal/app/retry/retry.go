package retry

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"gorouter/internal/domain/engine"
	"gorouter/internal/domain/provider"
)

type Executor struct {
	inner     engine.Executor
	policy    *Policy
	fallback  *FallbackSelector
	refresher TokenRefresher
}

func NewExecutor(inner engine.Executor, policy *Policy, fallback *FallbackSelector, refresher TokenRefresher) *Executor {
	if refresher == nil {
		refresher = &NoopRefresher{}
	}
	return &Executor{
		inner:     inner,
		policy:    policy,
		fallback:  fallback,
		refresher: refresher,
	}
}

type executeState struct {
	req             *engine.Request
	account         *provider.Account
	candidates      []provider.Account
	attempt         int
	refreshAttempts int
	triedAccounts   []uuid.UUID
}

func (e *Executor) Execute(ctx context.Context, req *engine.Request, account *provider.Account, candidates []provider.Account) (*engine.Response, error) {
	state := &executeState{
		req:           req,
		account:       account,
		candidates:    candidates,
		attempt:       1,
		triedAccounts: []uuid.UUID{account.ID},
	}
	for {
		resp, err := e.inner.Execute(ctx, state.req, state.account)
		if err == nil {
			e.recordSuccess(ctx, state.account.ID)
			return resp, nil
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		errorClass := ClassifyError(err)
		if errorClass == ErrorClassAuth {
			result, refreshErr := e.tryRefresh(ctx, state)
			if refreshErr == nil {
				state.account = result
				state.refreshAttempts = 0
				continue
			}
			if errors.Is(refreshErr, ErrRefreshUnsupported) || errors.Is(refreshErr, ErrRefreshFailed) {
				e.recordFailure(ctx, state.account.ID, err)
				next := e.pickFallback(ctx, state)
				if next == nil {
					return nil, fmt.Errorf("request failed after attempt %d, refresh unsupported, no fallback available: %w", state.attempt, err)
				}
				state.account = next
				state.triedAccounts = append(state.triedAccounts, next.ID)
				state.attempt = 1
				state.refreshAttempts = 0
				continue
			}
			if state.refreshAttempts >= MaxRefreshAttempts {
				e.recordFailure(ctx, state.account.ID, err)
				next := e.pickFallback(ctx, state)
				if next == nil {
					return nil, fmt.Errorf("request failed after attempt %d, refresh retries exhausted, no fallback available: %w", state.attempt, err)
				}
				state.account = next
				state.triedAccounts = append(state.triedAccounts, next.ID)
				state.attempt = 1
				state.refreshAttempts = 0
				continue
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(e.policy.backoff(state.refreshAttempts + 1)):
			}
			state.refreshAttempts++
			continue
		}
		decision := e.policy.Evaluate(state.attempt, err)
		e.recordFailure(ctx, state.account.ID, err)
		if !decision.ShouldRetry {
			next := e.pickFallback(ctx, state)
			if next != nil {
				state.account = next
				state.triedAccounts = append(state.triedAccounts, next.ID)
				state.attempt = 1
				state.refreshAttempts = 0
				continue
			}
			return nil, fmt.Errorf("request failed after attempt %d: %w", state.attempt, err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(decision.Delay):
		}
		state.attempt++
	}
}

func (e *Executor) ExecuteStream(ctx context.Context, req *engine.Request, account *provider.Account, candidates []provider.Account) (*engine.Response, error) {
	state := &executeState{
		req:           req,
		account:       account,
		candidates:    candidates,
		attempt:       1,
		triedAccounts: []uuid.UUID{account.ID},
	}
	for {
		resp, err := e.inner.ExecuteStream(ctx, state.req, state.account)
		if err == nil {
			e.recordSuccess(ctx, state.account.ID)
			return resp, nil
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		errorClass := ClassifyError(err)
		if errorClass == ErrorClassAuth {
			result, refreshErr := e.tryRefresh(ctx, state)
			if refreshErr == nil {
				state.account = result
				state.refreshAttempts = 0
				continue
			}
			e.recordFailure(ctx, state.account.ID, err)
			if errors.Is(refreshErr, ErrRefreshUnsupported) || errors.Is(refreshErr, ErrRefreshFailed) {
				next := e.pickFallback(ctx, state)
				if next == nil {
					return nil, fmt.Errorf("stream request failed after refresh, no fallback available: %w", err)
				}
				state.account = next
				state.triedAccounts = append(state.triedAccounts, next.ID)
				state.attempt = 1
				state.refreshAttempts = 0
				continue
			}
			if state.refreshAttempts >= MaxRefreshAttempts {
				next := e.pickFallback(ctx, state)
				if next == nil {
					return nil, fmt.Errorf("stream request failed after refresh retry exhaustion, no fallback available: %w", err)
				}
				state.account = next
				state.triedAccounts = append(state.triedAccounts, next.ID)
				state.attempt = 1
				state.refreshAttempts = 0
				continue
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(e.policy.backoff(state.refreshAttempts + 1)):
			}
			state.refreshAttempts++
			continue
		}
		decision := e.policy.Evaluate(state.attempt, err)
		e.recordFailure(ctx, state.account.ID, err)
		if !decision.ShouldRetry {
			next := e.pickFallback(ctx, state)
			if next != nil {
				state.account = next
				state.triedAccounts = append(state.triedAccounts, next.ID)
				state.attempt = 1
				continue
			}
			return nil, fmt.Errorf("stream request failed: %w", err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(decision.Delay):
		}
		state.attempt++
	}
}

func (e *Executor) tryRefresh(ctx context.Context, state *executeState) (*provider.Account, error) {
	if e.refresher == nil {
		return nil, ErrRefreshUnsupported
	}
	return e.refresher.Refresh(ctx, state.account)
}

func (e *Executor) pickFallback(ctx context.Context, state *executeState) *provider.Account {
	if e.fallback == nil {
		return nil
	}
	var remaining []provider.Account
	for _, a := range state.candidates {
		skip := false
		for _, tried := range state.triedAccounts {
			if a.ID == tried {
				skip = true
				break
			}
		}
		if !skip {
			remaining = append(remaining, a)
		}
	}
	return e.fallback.NextAccount(ctx, remaining, state.account.ID)
}

func (e *Executor) recordFailure(ctx context.Context, accountID [16]byte, err error) {
	if e.fallback != nil && e.fallback.cooldown != nil {
		e.fallback.cooldown.RecordFailure(ctx, accountID, err)
	}
}

func (e *Executor) recordSuccess(ctx context.Context, accountID [16]byte) {
	if e.fallback != nil && e.fallback.cooldown != nil {
		e.fallback.cooldown.RecordSuccess(ctx, accountID)
	}
}
