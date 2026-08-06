package repositories

import (
	"context"
	"time"

	"gorouter/internal/domain/passwordreset"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Compile-time interface assertion.
var _ passwordreset.PasswordResetRepository = (*passwordResetRepo)(nil)

// NewPasswordResetRepo creates a password reset repository bound to the
// given transaction.
func NewPasswordResetRepo(tx pgx.Tx) passwordreset.PasswordResetRepository {
	return &passwordResetRepo{tx: tx}
}

type passwordResetRepo struct {
	tx pgx.Tx
}

func (r *passwordResetRepo) Create(ctx context.Context, reset *passwordreset.PasswordReset) error {
	_, err := r.tx.Exec(ctx,
		`INSERT INTO gorouter_password_resets (id, user_id, token_hash, requested_by, requested_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		reset.ID, reset.UserID, reset.TokenHash, reset.RequestedBy, reset.RequestedAt)
	return err
}

func (r *passwordResetRepo) Complete(ctx context.Context, id uuid.UUID, completedAt time.Time) error {
	return r.setTerminal(ctx, id, "completed_at", completedAt)
}

func (r *passwordResetRepo) Revoke(ctx context.Context, id uuid.UUID, revokedAt time.Time) error {
	return r.setTerminal(ctx, id, "revoked_at", revokedAt)
}

// setTerminal transitions a pending request to a terminal lifecycle state.
// column is a compile-time constant ("completed_at" or "revoked_at"), never
// caller input. Only pending requests (both markers NULL) can transition;
// missing requests and already-terminal requests fail closed with distinct
// sentinel errors.
func (r *passwordResetRepo) setTerminal(ctx context.Context, id uuid.UUID, column string, at time.Time) error {
	tag, err := r.tx.Exec(ctx,
		`UPDATE gorouter_password_resets
		 SET `+column+` = $2
		 WHERE id = $1 AND completed_at IS NULL AND revoked_at IS NULL`, id, at)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 0 {
		return nil
	}
	var exists bool
	if err := r.tx.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM gorouter_password_resets WHERE id = $1)`, id).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return passwordreset.ErrPasswordResetNotFound
	}
	return passwordreset.ErrPasswordResetNotActionable
}
