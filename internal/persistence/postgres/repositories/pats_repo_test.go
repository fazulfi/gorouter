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

func TestPATRepo_FindByID(t *testing.T) {
	t.Parallel()

	t.Run("found", func(t *testing.T) {
		id := uuid.New()
		userID := uuid.New()
		now := time.Now().UTC()
		desc := "my-token"

		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, args ...interface{}) pgx.Row {
				if len(args) != 1 || args[0] != id {
					t.Error("unexpected args")
				}
				return &mockRow{
					vals: []interface{}{
						id, userID, "hashval", &desc,
						(*time.Time)(nil), (*time.Time)(nil), (*time.Time)(nil), now,
					},
				}
			},
		}
		repo := &patRepo{tx: tx}
		p, err := repo.FindByID(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if p == nil {
			t.Fatal("expected non-nil")
		}
		if p.ID != id {
			t.Errorf("ID = %v, want %v", p.ID, id)
		}
		if p.Description == nil || *p.Description != "my-token" {
			t.Errorf("Description = %v", p.Description)
		}
	})

	t.Run("not found", func(t *testing.T) {
		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				return &mockRow{err: pgx.ErrNoRows}
			},
		}
		repo := &patRepo{tx: tx}
		p, err := repo.FindByID(context.Background(), uuid.New())
		if err != nil {
			t.Fatal(err)
		}
		if p != nil {
			t.Fatal("expected nil")
		}
	})

	t.Run("query error", func(t *testing.T) {
		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				return &mockRow{err: errors.New("db error")}
			},
		}
		repo := &patRepo{tx: tx}
		_, err := repo.FindByID(context.Background(), uuid.New())
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestPATRepo_FindByHash(t *testing.T) {
	t.Parallel()

	t.Run("found", func(t *testing.T) {
		id := uuid.New()
		now := time.Now().UTC()
		desc := "pat-token"

		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, args ...interface{}) pgx.Row {
				if len(args) != 1 || args[0] != "pathash" {
					t.Error("unexpected args")
				}
				return &mockRow{
					vals: []interface{}{
						id, uuid.New(), "pathash", &desc,
						(*time.Time)(nil), (*time.Time)(nil), (*time.Time)(nil), now,
					},
				}
			},
		}
		repo := &patRepo{tx: tx}
		p, err := repo.FindByHash(context.Background(), "pathash")
		if err != nil {
			t.Fatal(err)
		}
		if p == nil {
			t.Fatal("expected non-nil")
		}
		if p.TokenHash != "pathash" {
			t.Errorf("TokenHash = %q", p.TokenHash)
		}
	})

	t.Run("not found", func(t *testing.T) {
		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				return &mockRow{err: pgx.ErrNoRows}
			},
		}
		repo := &patRepo{tx: tx}
		p, err := repo.FindByHash(context.Background(), "missing")
		if err != nil {
			t.Fatal(err)
		}
		if p != nil {
			t.Fatal("expected nil")
		}
	})
}

func TestPATRepo_FindByUserID(t *testing.T) {
	t.Parallel()

	t.Run("multiple pats", func(t *testing.T) {
		userID := uuid.New()
		now := time.Now().UTC()
		d1, d2 := "token-a", "token-b"

		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, args ...interface{}) (pgx.Rows, error) {
				if len(args) != 1 || args[0] != userID {
					t.Error("unexpected args")
				}
				return &mockRows{
					rows: [][]interface{}{
						{uuid.New(), userID, "h1", &d1, (*time.Time)(nil), (*time.Time)(nil), (*time.Time)(nil), now},
						{uuid.New(), userID, "h2", &d2, (*time.Time)(nil), (*time.Time)(nil), (*time.Time)(nil), now},
					},
				}, nil
			},
		}
		repo := &patRepo{tx: tx}
		pats, err := repo.FindByUserID(context.Background(), userID)
		if err != nil {
			t.Fatal(err)
		}
		if len(pats) != 2 {
			t.Fatalf("expected 2 PATs, got %d", len(pats))
		}
		if pats[0].TokenHash != "h1" || pats[1].TokenHash != "h2" {
			t.Error("unexpected token order")
		}
	})

	t.Run("empty", func(t *testing.T) {
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return &mockRows{}, nil
			},
		}
		repo := &patRepo{tx: tx}
		pats, err := repo.FindByUserID(context.Background(), uuid.New())
		if err != nil {
			t.Fatal(err)
		}
		if len(pats) != 0 {
			t.Fatalf("expected 0 PATs, got %d", len(pats))
		}
	})

	t.Run("query error", func(t *testing.T) {
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return nil, errors.New("query failed")
			},
		}
		repo := &patRepo{tx: tx}
		_, err := repo.FindByUserID(context.Background(), uuid.New())
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("scan error", func(t *testing.T) {
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return &mockRows{
					rows: [][]interface{}{{}},
				}, nil
			},
		}
		repo := &patRepo{tx: tx}
		_, err := repo.FindByUserID(context.Background(), uuid.New())
		if err != nil {
			t.Fatal("expected no scan error with partial row data")
		}
	})
}

func TestPATRepo_Create(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &patRepo{tx: tx}
		desc := "new-token"
		p := &keys.PAT{
			ID: uuid.New(), UserID: uuid.New(), TokenHash: "h",
			Description: &desc, CreatedAt: time.Now().UTC(),
		}
		if err := repo.Create(context.Background(), p); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("nil description", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &patRepo{tx: tx}
		p := &keys.PAT{
			ID: uuid.New(), UserID: uuid.New(), TokenHash: "h",
			CreatedAt: time.Now().UTC(),
		}
		if err := repo.Create(context.Background(), p); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("exec error", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("insert failed")
			},
		}
		repo := &patRepo{tx: tx}
		err := repo.Create(context.Background(), &keys.PAT{ID: uuid.New(), UserID: uuid.New(), TokenHash: "h"})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestPATRepo_Revoke(t *testing.T) {
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
		repo := &patRepo{tx: tx}
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
		repo := &patRepo{tx: tx}
		err := repo.Revoke(context.Background(), uuid.New())
		if err == nil {
			t.Fatal("expected error")
		}
	})
}
