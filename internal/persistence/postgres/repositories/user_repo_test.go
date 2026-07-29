package repositories

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorouter/internal/domain/auth"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestUserRepo_FindByID(t *testing.T) {
	t.Parallel()

	t.Run("found", func(t *testing.T) {
		id := uuid.New()
		now := time.Now().UTC()
		display := "Alice"
		lastLogin := now.Add(-1 * time.Hour)

		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, args ...interface{}) pgx.Row {
				if len(args) != 1 || args[0] != id {
					t.Error("unexpected args")
				}
				return &mockRow{
					vals: []interface{}{
						id, "alice@test.com", "$2a$12$hash", &display,
						false, true, &lastLogin, now, now,
					},
				}
			},
		}
		repo := &userRepo{tx: tx}
		u, err := repo.FindByID(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if u == nil {
			t.Fatal("expected non-nil user")
		}
		if u.ID != id {
			t.Errorf("ID = %v, want %v", u.ID, id)
		}
		if u.Email != "alice@test.com" {
			t.Errorf("Email = %q, want %q", u.Email, "alice@test.com")
		}
		if u.DisplayName == nil || *u.DisplayName != "Alice" {
			t.Errorf("DisplayName = %v, want 'Alice'", u.DisplayName)
		}
		if u.LastLoginAt == nil || !u.LastLoginAt.Equal(lastLogin) {
			t.Errorf("LastLoginAt = %v, want %v", u.LastLoginAt, lastLogin)
		}
	})

	t.Run("not found", func(t *testing.T) {
		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				return &mockRow{err: pgx.ErrNoRows}
			},
		}
		repo := &userRepo{tx: tx}
		u, err := repo.FindByID(context.Background(), uuid.New())
		if err != nil {
			t.Fatal(err)
		}
		if u != nil {
			t.Fatal("expected nil for not-found")
		}
	})

	t.Run("query error", func(t *testing.T) {
		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				return &mockRow{err: errors.New("db down")}
			},
		}
		repo := &userRepo{tx: tx}
		_, err := repo.FindByID(context.Background(), uuid.New())
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestUserRepo_FindByEmail(t *testing.T) {
	t.Parallel()

	t.Run("found", func(t *testing.T) {
		id := uuid.New()
		now := time.Now().UTC()

		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, args ...interface{}) pgx.Row {
				if len(args) != 1 || args[0] != "bob@test.com" {
					t.Error("unexpected args")
				}
				return &mockRow{
					vals: []interface{}{
						id, "bob@test.com", "hash", (*string)(nil),
						true, true, (*time.Time)(nil), now, now,
					},
				}
			},
		}
		repo := &userRepo{tx: tx}
		u, err := repo.FindByEmail(context.Background(), "bob@test.com")
		if err != nil {
			t.Fatal(err)
		}
		if u == nil {
			t.Fatal("expected non-nil")
		}
		if u.Email != "bob@test.com" {
			t.Errorf("Email = %q", "bob@test.com")
		}
	})

	t.Run("not found", func(t *testing.T) {
		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				return &mockRow{err: pgx.ErrNoRows}
			},
		}
		repo := &userRepo{tx: tx}
		u, err := repo.FindByEmail(context.Background(), "nope@test.com")
		if err != nil {
			t.Fatal(err)
		}
		if u != nil {
			t.Fatal("expected nil")
		}
	})

	t.Run("query error", func(t *testing.T) {
		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				return &mockRow{err: errors.New("db error")}
			},
		}
		repo := &userRepo{tx: tx}
		_, err := repo.FindByEmail(context.Background(), "x@y.com")
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestUserRepo_Create(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		var capturedArgs []interface{}
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, args ...interface{}) (pgconn.CommandTag, error) {
				capturedArgs = args
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &userRepo{tx: tx}
		now := time.Now().UTC()
		u := &auth.User{
			ID: uuid.New(), Email: "new@test.com", PasswordHash: "hash",
			IsActive: true, CreatedAt: now, UpdatedAt: now,
		}
		if err := repo.Create(context.Background(), u); err != nil {
			t.Fatal(err)
		}
		if len(capturedArgs) < 3 {
			t.Fatal("expected exec args")
		}
	})

	t.Run("duplicate key error", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("duplicate key value violates unique constraint")
			},
		}
		repo := &userRepo{tx: tx}
		err := repo.Create(context.Background(), &auth.User{ID: uuid.New(), Email: "dup@test.com", PasswordHash: "h"})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestUserRepo_Update(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &userRepo{tx: tx}
		err := repo.Update(context.Background(), &auth.User{ID: uuid.New(), Email: "upd@test.com"})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("exec error", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("update failed")
			},
		}
		repo := &userRepo{tx: tx}
		err := repo.Update(context.Background(), &auth.User{ID: uuid.New()})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestUserRepo_UpdateLastLogin(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		var capturedArgs []interface{}
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, args ...interface{}) (pgconn.CommandTag, error) {
				capturedArgs = args
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &userRepo{tx: tx}
		id := uuid.New()
		if err := repo.UpdateLastLogin(context.Background(), id); err != nil {
			t.Fatal(err)
		}
		if len(capturedArgs) != 1 || capturedArgs[0] != id {
			t.Error("unexpected exec args")
		}
	})

	t.Run("error", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("db error")
			},
		}
		repo := &userRepo{tx: tx}
		err := repo.UpdateLastLogin(context.Background(), uuid.New())
		if err == nil {
			t.Fatal("expected error")
		}
	})
}
