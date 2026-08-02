// Package passwordreset defines pure domain contracts for password reset
// requests. Resets are initiated exclusively by the local operator CLI; no
// HTTP or Admin API surface exists for them, and the repository stores only
// an opaque token hash, never a raw token.
package passwordreset

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Sentinel errors returned by the repository, kept pure so callers can
// distinguish a missing request from one whose lifecycle forbids the
// operation.
var (
	// ErrPasswordResetNotFound reports that no request exists with the id.
	ErrPasswordResetNotFound = errors.New("password reset: request not found")
	// ErrPasswordResetNotActionable reports that the request exists but is
	// no longer pending (already completed or already revoked), so the
	// attempted transition is refused.
	ErrPasswordResetNotActionable = errors.New("password reset: request is not actionable")
)

// PasswordReset is a single password reset request.
//
// TokenHash is the opaque hash of the reset token; it must never hold the
// raw token value. RequestedBy records the local CLI actor (for example
// "cli"), and the lifecycle markers CompletedAt and RevokedAt are mutually
// exclusive terminal states set by the repository; a request with both nil
// is pending.
type PasswordReset struct {
	ID          uuid.UUID
	UserID      uuid.UUID
	TokenHash   string
	RequestedBy *string
	RequestedAt *time.Time
	CompletedAt *time.Time
	RevokedAt   *time.Time
}

// PasswordResetRepository defines persistence operations for password reset
// requests. Lifecycle semantics are fail-closed: Complete and Revoke only
// transition a pending request, preserve all other lifecycle fields, and
// refuse (with ErrPasswordResetNotActionable) any transition of an already
// completed or revoked request. Tokens are never stored; only TokenHash is
// persisted.
type PasswordResetRepository interface {
	// Create persists a reset request exactly as given, storing only the
	// opaque TokenHash. The request starts pending.
	Create(ctx context.Context, reset *PasswordReset) error
	// Complete marks the pending request completed at completedAt, leaving
	// revoked_at NULL. Fails with ErrPasswordResetNotFound when no request
	// exists and ErrPasswordResetNotActionable when the request is already
	// completed or revoked.
	Complete(ctx context.Context, id uuid.UUID, completedAt time.Time) error
	// Revoke marks the pending request revoked at revokedAt, leaving
	// completed_at NULL. Fails with ErrPasswordResetNotFound when no
	// request exists and ErrPasswordResetNotActionable when the request is
	// already completed or revoked.
	Revoke(ctx context.Context, id uuid.UUID, revokedAt time.Time) error
}
