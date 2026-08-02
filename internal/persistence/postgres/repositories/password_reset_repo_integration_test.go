package repositories

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorouter/internal/domain/passwordreset"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// adminUserID returns the id of the seeded admin user from migration 000001
// within the caller's own test database (the pool the test already created).
func adminUserID(t *testing.T, ctx context.Context, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := pool.QueryRow(ctx,
		`SELECT id FROM gorouter_users WHERE email = 'admin@gorouter.local'`).Scan(&id)
	if err != nil {
		t.Fatalf("lookup seeded admin user: %v", err)
	}
	return id
}

// TestPasswordResetRepo_Lifecycle_Integration exercises the full reset-request
// lifecycle against real PostgreSQL: create, complete, revoke, and the
// fail-closed boundary semantics (no completion of revoked or completed
// requests, no revocation of completed requests).
func TestPasswordResetRepo_Lifecycle_Integration(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)

	repo := NewPasswordResetRepo(tx)
	userID := adminUserID(t, ctx, pool)

	t.Run("create preserves caller fields and stores only the opaque hash", func(t *testing.T) {
		requestedBy := "cli"
		requestedAt := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
		reset := &passwordreset.PasswordReset{
			ID:          uuid.New(),
			UserID:      userID,
			TokenHash:   "sha256:opaque-hash-value",
			RequestedBy: &requestedBy,
			RequestedAt: &requestedAt,
		}
		before := *reset
		if err := repo.Create(ctx, reset); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if *reset != before {
			t.Error("Create mutated caller input")
		}

		var got passwordreset.PasswordReset
		err := tx.QueryRow(ctx,
			`SELECT id, user_id, token_hash, requested_by, requested_at, completed_at, revoked_at
			 FROM gorouter_password_resets WHERE id = $1`, reset.ID).
			Scan(&got.ID, &got.UserID, &got.TokenHash, &got.RequestedBy,
				&got.RequestedAt, &got.CompletedAt, &got.RevokedAt)
		if err != nil {
			t.Fatalf("read back: %v", err)
		}
		if got.ID != reset.ID || got.UserID != userID {
			t.Error("id/user_id mismatch")
		}
		if got.TokenHash != "sha256:opaque-hash-value" {
			t.Errorf("token_hash = %q, want the exact opaque value passed", got.TokenHash)
		}
		if got.RequestedBy == nil || *got.RequestedBy != "cli" {
			t.Error("requested_by mismatch")
		}
		if got.RequestedAt == nil || !got.RequestedAt.Equal(requestedAt) {
			t.Error("requested_at mismatch")
		}
		if got.CompletedAt != nil || got.RevokedAt != nil {
			t.Error("new request must have NULL lifecycle markers")
		}
	})

	t.Run("nullable columns round-trip as NULL", func(t *testing.T) {
		reset := &passwordreset.PasswordReset{ID: uuid.New(), UserID: userID, TokenHash: "h2"}
		if err := repo.Create(ctx, reset); err != nil {
			t.Fatalf("Create: %v", err)
		}
		var got passwordreset.PasswordReset
		err := tx.QueryRow(ctx,
			`SELECT id, user_id, token_hash, requested_by, requested_at, completed_at, revoked_at
			 FROM gorouter_password_resets WHERE id = $1`, reset.ID).
			Scan(&got.ID, &got.UserID, &got.TokenHash, &got.RequestedBy,
				&got.RequestedAt, &got.CompletedAt, &got.RevokedAt)
		if err != nil {
			t.Fatalf("read back: %v", err)
		}
		if got.RequestedBy != nil || got.RequestedAt != nil {
			t.Error("requested_by/requested_at should be NULL")
		}
	})

	t.Run("complete sets completed_at and preserves other fields", func(t *testing.T) {
		requestedBy := "cli"
		requestedAt := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
		completedAt := time.Date(2026, 8, 2, 13, 0, 0, 0, time.UTC)
		reset := &passwordreset.PasswordReset{
			ID:          uuid.New(),
			UserID:      userID,
			TokenHash:   "h3",
			RequestedBy: &requestedBy,
			RequestedAt: &requestedAt,
		}
		if err := repo.Create(ctx, reset); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := repo.Complete(ctx, reset.ID, completedAt); err != nil {
			t.Fatalf("Complete: %v", err)
		}
		var got passwordreset.PasswordReset
		err := tx.QueryRow(ctx,
			`SELECT id, user_id, token_hash, requested_by, requested_at, completed_at, revoked_at
			 FROM gorouter_password_resets WHERE id = $1`, reset.ID).
			Scan(&got.ID, &got.UserID, &got.TokenHash, &got.RequestedBy,
				&got.RequestedAt, &got.CompletedAt, &got.RevokedAt)
		if err != nil {
			t.Fatalf("read back: %v", err)
		}
		if got.CompletedAt == nil || !got.CompletedAt.Equal(completedAt) {
			t.Error("completed_at mismatch")
		}
		if got.RevokedAt != nil {
			t.Error("revoked_at must stay NULL after complete")
		}
		if got.RequestedBy == nil || *got.RequestedBy != "cli" || got.RequestedAt == nil || !got.RequestedAt.Equal(requestedAt) {
			t.Error("requested_by/requested_at must be preserved by complete")
		}
		if got.TokenHash != "h3" {
			t.Error("token_hash must be preserved by complete")
		}
	})

	t.Run("revoke sets revoked_at and preserves other fields", func(t *testing.T) {
		revokedAt := time.Date(2026, 8, 2, 14, 0, 0, 0, time.UTC)
		reset := &passwordreset.PasswordReset{ID: uuid.New(), UserID: userID, TokenHash: "h4"}
		if err := repo.Create(ctx, reset); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := repo.Revoke(ctx, reset.ID, revokedAt); err != nil {
			t.Fatalf("Revoke: %v", err)
		}
		var got passwordreset.PasswordReset
		err := tx.QueryRow(ctx,
			`SELECT id, user_id, token_hash, requested_by, requested_at, completed_at, revoked_at
			 FROM gorouter_password_resets WHERE id = $1`, reset.ID).
			Scan(&got.ID, &got.UserID, &got.TokenHash, &got.RequestedBy,
				&got.RequestedAt, &got.CompletedAt, &got.RevokedAt)
		if err != nil {
			t.Fatalf("read back: %v", err)
		}
		if got.RevokedAt == nil || !got.RevokedAt.Equal(revokedAt) {
			t.Error("revoked_at mismatch")
		}
		if got.CompletedAt != nil {
			t.Error("completed_at must stay NULL after revoke")
		}
		if got.TokenHash != "h4" {
			t.Error("token_hash must be preserved by revoke")
		}
	})

	t.Run("complete twice fails closed", func(t *testing.T) {
		reset := &passwordreset.PasswordReset{ID: uuid.New(), UserID: userID, TokenHash: "h5"}
		if err := repo.Create(ctx, reset); err != nil {
			t.Fatalf("Create: %v", err)
		}
		completedAt := time.Date(2026, 8, 2, 13, 0, 0, 0, time.UTC)
		if err := repo.Complete(ctx, reset.ID, completedAt); err != nil {
			t.Fatalf("first Complete: %v", err)
		}
		err := repo.Complete(ctx, reset.ID, completedAt.Add(time.Hour))
		if !errors.Is(err, passwordreset.ErrPasswordResetNotActionable) {
			t.Fatalf("second Complete error = %v, want ErrPasswordResetNotActionable", err)
		}
	})

	t.Run("revoke twice fails closed", func(t *testing.T) {
		reset := &passwordreset.PasswordReset{ID: uuid.New(), UserID: userID, TokenHash: "h6"}
		if err := repo.Create(ctx, reset); err != nil {
			t.Fatalf("Create: %v", err)
		}
		revokedAt := time.Date(2026, 8, 2, 14, 0, 0, 0, time.UTC)
		if err := repo.Revoke(ctx, reset.ID, revokedAt); err != nil {
			t.Fatalf("first Revoke: %v", err)
		}
		err := repo.Revoke(ctx, reset.ID, revokedAt.Add(time.Hour))
		if !errors.Is(err, passwordreset.ErrPasswordResetNotActionable) {
			t.Fatalf("second Revoke error = %v, want ErrPasswordResetNotActionable", err)
		}
	})

	t.Run("completing a revoked request fails closed", func(t *testing.T) {
		reset := &passwordreset.PasswordReset{ID: uuid.New(), UserID: userID, TokenHash: "h7"}
		if err := repo.Create(ctx, reset); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := repo.Revoke(ctx, reset.ID, time.Date(2026, 8, 2, 14, 0, 0, 0, time.UTC)); err != nil {
			t.Fatalf("Revoke: %v", err)
		}
		err := repo.Complete(ctx, reset.ID, time.Date(2026, 8, 2, 15, 0, 0, 0, time.UTC))
		if !errors.Is(err, passwordreset.ErrPasswordResetNotActionable) {
			t.Fatalf("Complete on revoked error = %v, want ErrPasswordResetNotActionable", err)
		}
	})

	t.Run("revoking a completed request fails closed", func(t *testing.T) {
		reset := &passwordreset.PasswordReset{ID: uuid.New(), UserID: userID, TokenHash: "h8"}
		if err := repo.Create(ctx, reset); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := repo.Complete(ctx, reset.ID, time.Date(2026, 8, 2, 13, 0, 0, 0, time.UTC)); err != nil {
			t.Fatalf("Complete: %v", err)
		}
		err := repo.Revoke(ctx, reset.ID, time.Date(2026, 8, 2, 14, 0, 0, 0, time.UTC))
		if !errors.Is(err, passwordreset.ErrPasswordResetNotActionable) {
			t.Fatalf("Revoke on completed error = %v, want ErrPasswordResetNotActionable", err)
		}
	})

	t.Run("nonexistent id fails closed for both mutations", func(t *testing.T) {
		err := repo.Complete(ctx, uuid.New(), time.Date(2026, 8, 2, 13, 0, 0, 0, time.UTC))
		if !errors.Is(err, passwordreset.ErrPasswordResetNotFound) {
			t.Fatalf("Complete on missing id error = %v, want ErrPasswordResetNotFound", err)
		}
		err = repo.Revoke(ctx, uuid.New(), time.Date(2026, 8, 2, 14, 0, 0, 0, time.UTC))
		if !errors.Is(err, passwordreset.ErrPasswordResetNotFound) {
			t.Fatalf("Revoke on missing id error = %v, want ErrPasswordResetNotFound", err)
		}
	})
}

// TestPasswordResetRepo_Constraints_Integration verifies the database-level
// constraints are enforced against real PostgreSQL: the user foreign key and
// the NOT NULL token_hash column.
func TestPasswordResetRepo_Constraints_Integration(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)

	repo := NewPasswordResetRepo(tx)

	t.Run("user foreign key is enforced", func(t *testing.T) {
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatalf("savepoint: %v", err)
		}
		spRepo := NewPasswordResetRepo(sp)
		err = spRepo.Create(ctx, &passwordreset.PasswordReset{
			ID: uuid.New(), UserID: uuid.New(), TokenHash: "fk-probe",
		})
		if err == nil {
			sp.Commit(ctx)
			t.Fatal("expected foreign key violation for unknown user, got nil")
		}
		sp.Rollback(ctx)
	})

	t.Run("token_hash NOT NULL is enforced", func(t *testing.T) {
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatalf("savepoint: %v", err)
		}
		_, err = sp.Exec(ctx,
			`INSERT INTO gorouter_password_resets (id, user_id, token_hash)
			 VALUES ($1, $2, NULL)`, uuid.New(), adminUserID(t, ctx, pool))
		if err == nil {
			sp.Commit(ctx)
			t.Fatal("expected NOT NULL violation for NULL token_hash, got nil")
		}
		sp.Rollback(ctx)
	})

	t.Run("empty token_hash is stored as-is by the repository", func(t *testing.T) {
		// The repository stores faithfully; guaranteeing a non-empty opaque
		// hash is the caller's (local CLI) obligation.
		reset := &passwordreset.PasswordReset{ID: uuid.New(), UserID: adminUserID(t, ctx, pool), TokenHash: ""}
		if err := repo.Create(ctx, reset); err != nil {
			t.Fatalf("Create with empty hash should succeed at the repository: %v", err)
		}
	})
}
