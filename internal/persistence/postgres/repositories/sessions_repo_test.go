package repositories

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"gorouter/internal/domain/auth"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestSessionRepo_FindByID(t *testing.T) {
	t.Parallel()

	t.Run("found", func(t *testing.T) {
		id := uuid.New()
		userID := uuid.New()
		now := time.Now().UTC()
		expiresAt := now.Add(24 * time.Hour)
		ipRaw := net.ParseIP("10.0.0.1") // raw bytes

		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, args ...interface{}) pgx.Row {
				if len(args) != 1 || args[0] != id {
					t.Error("unexpected query args")
				}
				return &mockRow{
					vals: []interface{}{
						id, userID, "tokhash", ipRaw, "curl/7.0",
						expiresAt, (*time.Time)(nil), now,
					},
				}
			},
		}
		repo := &sessionRepo{tx: tx}
		s, err := repo.FindByID(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if s == nil {
			t.Fatal("expected non-nil session")
		}
		if s.ID != id {
			t.Errorf("ID = %v, want %v", s.ID, id)
		}
		if s.UserID != userID {
			t.Errorf("UserID = %v, want %v", s.UserID, userID)
		}
		if s.TokenHash != "tokhash" {
			t.Errorf("TokenHash = %q", s.TokenHash)
		}
		if !s.IPAddress.Equal(net.ParseIP("10.0.0.1")) {
			t.Errorf("IPAddress = %v", s.IPAddress)
		}
		if s.UserAgent != "curl/7.0" {
			t.Errorf("UserAgent = %q", s.UserAgent)
		}
		if s.RevokedAt != nil {
			t.Error("expected RevokedAt to be nil")
		}
	})

	t.Run("not found", func(t *testing.T) {
		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				return &mockRow{err: pgx.ErrNoRows}
			},
		}
		repo := &sessionRepo{tx: tx}
		s, err := repo.FindByID(context.Background(), uuid.New())
		if err != nil {
			t.Fatal(err)
		}
		if s != nil {
			t.Fatal("expected nil")
		}
	})

	t.Run("query error", func(t *testing.T) {
		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				return &mockRow{err: errors.New("query failed")}
			},
		}
		repo := &sessionRepo{tx: tx}
		_, err := repo.FindByID(context.Background(), uuid.New())
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestSessionRepo_FindByTokenHash(t *testing.T) {
	t.Parallel()

	t.Run("found", func(t *testing.T) {
		id := uuid.New()
		now := time.Now().UTC()
		ipRaw := net.ParseIP("::1")

		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, args ...interface{}) pgx.Row {
				if len(args) != 1 || args[0] != "the-hash" {
					t.Error("unexpected args")
				}
				return &mockRow{
					vals: []interface{}{
						id, uuid.New(), "the-hash", ipRaw, "Mozilla/5.0",
						now.Add(1 * time.Hour), (*time.Time)(nil), now,
					},
				}
			},
		}
		repo := &sessionRepo{tx: tx}
		s, err := repo.FindByTokenHash(context.Background(), "the-hash")
		if err != nil {
			t.Fatal(err)
		}
		if s == nil {
			t.Fatal("expected non-nil")
		}
		if s.TokenHash != "the-hash" {
			t.Errorf("TokenHash = %q", s.TokenHash)
		}
	})

	t.Run("not found", func(t *testing.T) {
		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				return &mockRow{err: pgx.ErrNoRows}
			},
		}
		repo := &sessionRepo{tx: tx}
		s, err := repo.FindByTokenHash(context.Background(), "missing")
		if err != nil {
			t.Fatal(err)
		}
		if s != nil {
			t.Fatal("expected nil")
		}
	})

	t.Run("query error", func(t *testing.T) {
		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				return &mockRow{err: errors.New("query error")}
			},
		}
		repo := &sessionRepo{tx: tx}
		_, err := repo.FindByTokenHash(context.Background(), "hash")
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestSessionRepo_Create(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &sessionRepo{tx: tx}
		s := &auth.Session{
			ID: uuid.New(), UserID: uuid.New(), TokenHash: "h",
			IPAddress: net.ParseIP("10.0.0.1"),
			ExpiresAt: time.Now().Add(1 * time.Hour),
			CreatedAt: time.Now().UTC(),
		}
		if err := repo.Create(context.Background(), s); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("nil IP", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &sessionRepo{tx: tx}
		s := &auth.Session{
			ID: uuid.New(), UserID: uuid.New(), TokenHash: "h",
			ExpiresAt: time.Now().Add(1 * time.Hour),
			CreatedAt: time.Now().UTC(),
		}
		if err := repo.Create(context.Background(), s); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("exec error", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("insert failed")
			},
		}
		repo := &sessionRepo{tx: tx}
		err := repo.Create(context.Background(), &auth.Session{
			ID: uuid.New(), UserID: uuid.New(), TokenHash: "h",
			ExpiresAt: time.Now(), CreatedAt: time.Now().UTC(),
		})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestSessionRepo_Revoke(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, args ...interface{}) (pgconn.CommandTag, error) {
				if len(args) != 1 {
					t.Error("expected 1 arg for Revoke")
				}
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &sessionRepo{tx: tx}
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
		repo := &sessionRepo{tx: tx}
		err := repo.Revoke(context.Background(), uuid.New())
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestSessionRepo_DeleteExpired(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &sessionRepo{tx: tx}
		if err := repo.DeleteExpired(context.Background()); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("exec error", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("delete failed")
			},
		}
		repo := &sessionRepo{tx: tx}
		err := repo.DeleteExpired(context.Background())
		if err == nil {
			t.Fatal("expected error")
		}
	})
}
