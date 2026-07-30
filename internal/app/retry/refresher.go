// Package retry provides configurable retry logic with exponential backoff,
// jitter, account fallback, and provider-credential refresh for LLM inference
// requests dispatched through the gorouter pipeline.
package retry

import (
	"context"
	"errors"

	"gorouter/internal/domain/provider"
)

// ErrRefreshUnsupported is returned by TokenRefresher implementations when
// the provider account does not support credential refresh. This is a
// definitive (non-retryable) signal: the caller MUST NOT retry the refresh
// and MUST proceed directly to account fallback.
//
// Production default (NoopRefresher) returns this error so that unsupported
// refresh never silently reports success — doing so would cause requests to
// be retried with stale credentials guaranteed to fail again.
var ErrRefreshUnsupported = errors.New("token refresh not supported for this provider account")

// ErrRefreshFailed is returned when a credential refresh attempt definitively
// failed (e.g. the upstream authorization server rejected the refresh token).
// Callers should fall back to the next candidate account rather than retrying
// the refresh.
var ErrRefreshFailed = errors.New("token refresh definitively failed")

// TokenRefresher knows how to refresh credentials for a provider account
// that has returned a 401 or 403 status code. Provider-specific OAuth or
// API-key refresh implementations are expected in Phase 3; until then the
// default NoopRefresher provides a safe no-op fallback.
//
// Contract:
//   - Success: returns the updated account with a new CredentialRef.
//   - ErrRefreshUnsupported: refresh not available for this account type;
//     caller must fall back immediately without retrying the refresh.
//   - Transient error: caller should retry the refresh with bounded attempts
//     and backoff.
//   - ErrRefreshFailed or other definitive error: caller should not retry
//     the refresh and should proceed to account fallback.
type TokenRefresher interface {
	// Refresh attempts to refresh the credential for the given account.
	// The returned *provider.Account carries the updated CredentialRef
	// on success; on error the original account is not mutated.
	Refresh(ctx context.Context, account *provider.Account) (*provider.Account, error)
}

// NoopRefresher is the safe production default TokenRefresher. It always
// returns ErrRefreshUnsupported, ensuring that callers never silently
// proceed with stale credentials when refresh is not wired.
//
// This is deliberate: silently returning the original account unchanged
// would cause the caller to retry the request with credentials that have
// already been rejected, wasting time and rate-limit capacity. The no-op
// forces an immediate fallback to the next candidate account instead.
type NoopRefresher struct{}

// Refresh always returns ErrRefreshUnsupported.
func (n *NoopRefresher) Refresh(_ context.Context, _ *provider.Account) (*provider.Account, error) {
	return nil, ErrRefreshUnsupported
}

// MaxRefreshAttempts is the default upper bound on consecutive refresh
// retries when the refresh itself returns a transient error.
const MaxRefreshAttempts = 2
