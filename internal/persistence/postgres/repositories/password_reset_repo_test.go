package repositories

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"gorouter/internal/domain/passwordreset"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestPasswordResetRepo_Create(t *testing.T) {
	t.Parallel()

	t.Run("success passes fields in column order", func(t *testing.T) {
		var gotSQL string
		var gotArgs []interface{}
		tx := &mockTx{
			execFn: func(_ context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
				gotSQL = sql
				gotArgs = args
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &passwordResetRepo{tx: tx}
		id := uuid.New()
		userID := uuid.New()
		requestedBy := "cli"
		requestedAt := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
		reset := &passwordreset.PasswordReset{
			ID:          id,
			UserID:      userID,
			TokenHash:   "opaque-hash",
			RequestedBy: &requestedBy,
			RequestedAt: &requestedAt,
		}
		if err := repo.Create(context.Background(), reset); err != nil {
			t.Fatal(err)
		}
		if len(gotArgs) != 5 {
			t.Fatalf("expected 5 args, got %d", len(gotArgs))
		}
		if gotArgs[0] != id || gotArgs[1] != userID || gotArgs[2] != "opaque-hash" {
			t.Error("id/user_id/token_hash arg mismatch")
		}
		if gotArgs[3] != &requestedBy || gotArgs[4] != &requestedAt {
			t.Error("requested_by/requested_at arg mismatch")
		}
		if !strings.Contains(gotSQL, "gorouter_password_resets") {
			t.Errorf("SQL does not reference gorouter_password_resets:\n%s", gotSQL)
		}
		if strings.Contains(gotSQL, "token_hash,") == false {
			t.Errorf("Create SQL must write token_hash only (no plaintext column):\n%s", gotSQL)
		}
	})

	t.Run("never mutates caller input", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &passwordResetRepo{tx: tx}
		before := passwordreset.PasswordReset{ID: uuid.New(), UserID: uuid.New(), TokenHash: "opaque-hash"}
		reset := before
		if err := repo.Create(context.Background(), &reset); err != nil {
			t.Fatal(err)
		}
		if reset != before {
			t.Error("Create mutated caller input")
		}
	})

	t.Run("exec error propagates", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("insert failed")
			},
		}
		repo := &passwordResetRepo{tx: tx}
		err := repo.Create(context.Background(), &passwordreset.PasswordReset{ID: uuid.New(), UserID: uuid.New(), TokenHash: "h"})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestPasswordResetRepo_Complete(t *testing.T) {
	t.Parallel()
	completedAt := time.Date(2026, 8, 2, 13, 0, 0, 0, time.UTC)

	t.Run("success updates completed_at when pending", func(t *testing.T) {
		var gotSQL string
		var gotArgs []interface{}
		tx := &mockTx{
			execFn: func(_ context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
				gotSQL = sql
				gotArgs = args
				return pgconn.NewCommandTag("UPDATE 1"), nil
			},
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				t.Error("discriminator query must not run on successful update")
				return &mockRow{}
			},
		}
		repo := &passwordResetRepo{tx: tx}
		id := uuid.New()
		if err := repo.Complete(context.Background(), id, completedAt); err != nil {
			t.Fatal(err)
		}
		if len(gotArgs) != 2 || gotArgs[0] != id || gotArgs[1] != completedAt {
			t.Error("id/completed_at args mismatch")
		}
		if !strings.Contains(gotSQL, "completed_at") || !strings.Contains(gotSQL, "revoked_at IS NULL") || !strings.Contains(gotSQL, "completed_at IS NULL") {
			t.Errorf("Complete SQL must guard pending state only:\n%s", gotSQL)
		}
	})

	t.Run("not found returns ErrPasswordResetNotFound", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 0"), nil
			},
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				return &mockRow{vals: []interface{}{false}}
			},
		}
		repo := &passwordResetRepo{tx: tx}
		err := repo.Complete(context.Background(), uuid.New(), completedAt)
		if !errors.Is(err, passwordreset.ErrPasswordResetNotFound) {
			t.Fatalf("error = %v, want ErrPasswordResetNotFound", err)
		}
	})

	t.Run("already completed or revoked fails closed", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 0"), nil
			},
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				return &mockRow{vals: []interface{}{true}}
			},
		}
		repo := &passwordResetRepo{tx: tx}
		err := repo.Complete(context.Background(), uuid.New(), completedAt)
		if !errors.Is(err, passwordreset.ErrPasswordResetNotActionable) {
			t.Fatalf("error = %v, want ErrPasswordResetNotActionable", err)
		}
	})

	t.Run("update error propagates", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("update failed")
			},
		}
		repo := &passwordResetRepo{tx: tx}
		err := repo.Complete(context.Background(), uuid.New(), completedAt)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("discriminator query error propagates", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 0"), nil
			},
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				return &mockRow{err: errors.New("query failed")}
			},
		}
		repo := &passwordResetRepo{tx: tx}
		err := repo.Complete(context.Background(), uuid.New(), completedAt)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestPasswordResetRepo_Revoke(t *testing.T) {
	t.Parallel()
	revokedAt := time.Date(2026, 8, 2, 14, 0, 0, 0, time.UTC)

	t.Run("success updates revoked_at when pending", func(t *testing.T) {
		var gotSQL string
		var gotArgs []interface{}
		tx := &mockTx{
			execFn: func(_ context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
				gotSQL = sql
				gotArgs = args
				return pgconn.NewCommandTag("UPDATE 1"), nil
			},
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				t.Error("discriminator query must not run on successful update")
				return &mockRow{}
			},
		}
		repo := &passwordResetRepo{tx: tx}
		id := uuid.New()
		if err := repo.Revoke(context.Background(), id, revokedAt); err != nil {
			t.Fatal(err)
		}
		if len(gotArgs) != 2 || gotArgs[0] != id || gotArgs[1] != revokedAt {
			t.Error("id/revoked_at args mismatch")
		}
		if !strings.Contains(gotSQL, "revoked_at") || !strings.Contains(gotSQL, "revoked_at IS NULL") || !strings.Contains(gotSQL, "completed_at IS NULL") {
			t.Errorf("Revoke SQL must guard pending state only:\n%s", gotSQL)
		}
	})

	t.Run("not found returns ErrPasswordResetNotFound", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 0"), nil
			},
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				return &mockRow{vals: []interface{}{false}}
			},
		}
		repo := &passwordResetRepo{tx: tx}
		err := repo.Revoke(context.Background(), uuid.New(), revokedAt)
		if !errors.Is(err, passwordreset.ErrPasswordResetNotFound) {
			t.Fatalf("error = %v, want ErrPasswordResetNotFound", err)
		}
	})

	t.Run("already completed or revoked fails closed", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 0"), nil
			},
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				return &mockRow{vals: []interface{}{true}}
			},
		}
		repo := &passwordResetRepo{tx: tx}
		err := repo.Revoke(context.Background(), uuid.New(), revokedAt)
		if !errors.Is(err, passwordreset.ErrPasswordResetNotActionable) {
			t.Fatalf("error = %v, want ErrPasswordResetNotActionable", err)
		}
	})

	t.Run("update error propagates", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("update failed")
			},
		}
		repo := &passwordResetRepo{tx: tx}
		err := repo.Revoke(context.Background(), uuid.New(), revokedAt)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("discriminator query error propagates", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 0"), nil
			},
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				return &mockRow{err: errors.New("query failed")}
			},
		}
		repo := &passwordResetRepo{tx: tx}
		err := repo.Revoke(context.Background(), uuid.New(), revokedAt)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}
