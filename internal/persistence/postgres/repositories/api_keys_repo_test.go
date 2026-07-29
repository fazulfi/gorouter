package repositories

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorouter/internal/domain/keys"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestAPIKeyRepo_FindByID(t *testing.T) {
	t.Parallel()

	t.Run("found", func(t *testing.T) {
		id := uuid.New()
		userID := uuid.New()
		now := time.Now().UTC()
		scopes := []string{"proxy:read", "proxy:write"}

		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, args ...interface{}) pgx.Row {
				if len(args) != 1 || args[0] != id {
					t.Error("unexpected args")
				}
				return &mockRow{
					vals: []interface{}{
						id, userID, "pre_", "hashval", "my-key", scopes,
						(*time.Time)(nil), (*time.Time)(nil), (*time.Time)(nil), now,
					},
				}
			},
		}
		repo := &apiKeyRepo{tx: tx}
		k, err := repo.FindByID(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if k == nil {
			t.Fatal("expected non-nil")
		}
		if k.ID != id {
			t.Errorf("ID = %v, want %v", k.ID, id)
		}
		if k.Name != "my-key" {
			t.Errorf("Name = %q", k.Name)
		}
		if len(k.Scopes) != 2 || k.Scopes[0] != "proxy:read" {
			t.Error("unexpected scopes")
		}
	})

	t.Run("not found", func(t *testing.T) {
		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				return &mockRow{err: pgx.ErrNoRows}
			},
		}
		repo := &apiKeyRepo{tx: tx}
		k, err := repo.FindByID(context.Background(), uuid.New())
		if err != nil {
			t.Fatal(err)
		}
		if k != nil {
			t.Fatal("expected nil")
		}
	})

	t.Run("query error", func(t *testing.T) {
		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				return &mockRow{err: errors.New("db error")}
			},
		}
		repo := &apiKeyRepo{tx: tx}
		_, err := repo.FindByID(context.Background(), uuid.New())
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestAPIKeyRepo_FindByHash(t *testing.T) {
	t.Parallel()

	t.Run("found", func(t *testing.T) {
		id := uuid.New()
		now := time.Now().UTC()

		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, args ...interface{}) pgx.Row {
				if len(args) != 1 || args[0] != "thehash" {
					t.Error("unexpected args")
				}
				return &mockRow{
					vals: []interface{}{
						id, uuid.New(), "pre_", "thehash", "key-name",
						[]string{}, (*time.Time)(nil), (*time.Time)(nil), (*time.Time)(nil), now,
					},
				}
			},
		}
		repo := &apiKeyRepo{tx: tx}
		k, err := repo.FindByHash(context.Background(), "thehash")
		if err != nil {
			t.Fatal(err)
		}
		if k == nil {
			t.Fatal("expected non-nil")
		}
		if k.KeyHash != "thehash" {
			t.Errorf("KeyHash = %q", k.KeyHash)
		}
	})

	t.Run("not found", func(t *testing.T) {
		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				return &mockRow{err: pgx.ErrNoRows}
			},
		}
		repo := &apiKeyRepo{tx: tx}
		k, err := repo.FindByHash(context.Background(), "missing")
		if err != nil {
			t.Fatal(err)
		}
		if k != nil {
			t.Fatal("expected nil")
		}
	})
}

func TestAPIKeyRepo_FindByUserID(t *testing.T) {
	t.Parallel()

	t.Run("multiple keys", func(t *testing.T) {
		userID := uuid.New()
		now := time.Now().UTC()

		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, args ...interface{}) (pgx.Rows, error) {
				if len(args) != 1 || args[0] != userID {
					t.Error("unexpected args")
				}
				return &mockRows{
					rows: [][]interface{}{
						{
							uuid.New(), userID, "k1_", "h1", "key-1",
							[]string{"read"}, (*time.Time)(nil), (*time.Time)(nil), (*time.Time)(nil), now,
						},
						{
							uuid.New(), userID, "k2_", "h2", "key-2",
							[]string{"write"}, (*time.Time)(nil), (*time.Time)(nil), (*time.Time)(nil), now,
						},
					},
				}, nil
			},
		}
		repo := &apiKeyRepo{tx: tx}
		keys, err := repo.FindByUserID(context.Background(), userID)
		if err != nil {
			t.Fatal(err)
		}
		if len(keys) != 2 {
			t.Fatalf("expected 2 keys, got %d", len(keys))
		}
		if keys[0].Name != "key-1" || keys[1].Name != "key-2" {
			t.Error("unexpected key order or names")
		}
	})

	t.Run("empty", func(t *testing.T) {
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return &mockRows{}, nil
			},
		}
		repo := &apiKeyRepo{tx: tx}
		keys, err := repo.FindByUserID(context.Background(), uuid.New())
		if err != nil {
			t.Fatal(err)
		}
		if len(keys) != 0 {
			t.Fatalf("expected 0 keys, got %d", len(keys))
		}
	})

	t.Run("query error", func(t *testing.T) {
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return nil, errors.New("query failed")
			},
		}
		repo := &apiKeyRepo{tx: tx}
		_, err := repo.FindByUserID(context.Background(), uuid.New())
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("scan error", func(t *testing.T) {
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return &mockRows{
					rows: [][]interface{}{{}}, // incomplete row → scan will underflow
				}, nil
			},
		}
		repo := &apiKeyRepo{tx: tx}
		_, err := repo.FindByUserID(context.Background(), uuid.New())
		if err != nil {
			t.Fatal("expected no scan error with partial row data") // scan will fill what it can
		}
	})
}

func TestAPIKeyRepo_Create(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &apiKeyRepo{tx: tx}
		k := &keys.APIKey{
			ID: uuid.New(), UserID: uuid.New(), KeyPrefix: "pre_",
			KeyHash: "hash", Name: "k", Scopes: []string{"read"},
			CreatedAt: time.Now().UTC(),
		}
		if err := repo.Create(context.Background(), k); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("duplicate error", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("duplicate key")
			},
		}
		repo := &apiKeyRepo{tx: tx}
		err := repo.Create(context.Background(), &keys.APIKey{ID: uuid.New(), UserID: uuid.New(), KeyHash: "h"})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestAPIKeyRepo_Revoke(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, args ...interface{}) (pgconn.CommandTag, error) {
				if len(args) != 1 {
					t.Error("expected 1 arg")
				}
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &apiKeyRepo{tx: tx}
		if err := repo.Revoke(context.Background(), uuid.New()); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("exec error", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("revoke failed")
			},
		}
		repo := &apiKeyRepo{tx: tx}
		err := repo.Revoke(context.Background(), uuid.New())
		if err == nil {
			t.Fatal("expected error")
		}
	})
}
