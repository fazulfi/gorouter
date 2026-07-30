package repositories

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorouter/internal/domain/provider"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestProxyRepo_FindByAccountID(t *testing.T) {
	t.Parallel()

	t.Run("results", func(t *testing.T) {
		accountID := uuid.New()
		now := time.Now().UTC()

		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, args ...interface{}) (pgx.Rows, error) {
				if len(args) != 1 || args[0] != accountID {
					t.Error("unexpected args")
				}
				return &mockRows{
					rows: [][]interface{}{
						{uuid.New(), accountID, "https://proxy1.com", "user1", "pass1", true, 0, now, now},
						{uuid.New(), accountID, "https://proxy2.com", "user2", "pass2", false, 1, now, now},
					},
				}, nil
			},
		}
		repo := &proxyRepo{tx: tx}
		proxies, err := repo.FindByAccountID(context.Background(), accountID)
		if err != nil {
			t.Fatal(err)
		}
		if len(proxies) != 2 {
			t.Fatalf("expected 2, got %d", len(proxies))
		}
		if proxies[0].URL != "https://proxy1.com" {
			t.Errorf("URL[0] = %q", proxies[0].URL)
		}
		if proxies[0].Username != "user1" || proxies[0].Password != "pass1" {
			t.Error("unexpected credentials on proxy[0]")
		}
		if !proxies[0].IsEnabled {
			t.Error("expected proxy[0] IsEnabled = true")
		}
		if proxies[0].Priority != 0 {
			t.Errorf("proxy[0] Priority = %d", proxies[0].Priority)
		}
		if proxies[1].URL != "https://proxy2.com" {
			t.Errorf("URL[1] = %q", proxies[1].URL)
		}
		if proxies[1].IsEnabled {
			t.Error("expected proxy[1] IsEnabled = false")
		}
	})

	t.Run("empty", func(t *testing.T) {
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return &mockRows{}, nil
			},
		}
		repo := &proxyRepo{tx: tx}
		proxies, err := repo.FindByAccountID(context.Background(), uuid.New())
		if err != nil {
			t.Fatal(err)
		}
		if len(proxies) != 0 {
			t.Fatalf("expected 0, got %d", len(proxies))
		}
	})

	t.Run("query error", func(t *testing.T) {
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return nil, errors.New("query failed")
			},
		}
		repo := &proxyRepo{tx: tx}
		_, err := repo.FindByAccountID(context.Background(), uuid.New())
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestProxyRepo_Create(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &proxyRepo{tx: tx}
		p := &provider.ProxyConfig{
			ID: uuid.New(), AccountID: uuid.New(), URL: "https://proxy.example.com",
			Username: "user", Password: "pass", IsEnabled: true, Priority: 0,
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
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
		repo := &proxyRepo{tx: tx}
		err := repo.Create(context.Background(), &provider.ProxyConfig{
			ID: uuid.New(), AccountID: uuid.New(), URL: "https://fail.com",
		})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestProxyRepo_Update(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &proxyRepo{tx: tx}
		p := &provider.ProxyConfig{
			ID: uuid.New(), AccountID: uuid.New(), URL: "https://updated.example.com",
			Username: "newuser", Password: "newpass", IsEnabled: false, Priority: 2,
			UpdatedAt: time.Now().UTC(),
		}
		if err := repo.Update(context.Background(), p); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("exec error", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("update failed")
			},
		}
		repo := &proxyRepo{tx: tx}
		err := repo.Update(context.Background(), &provider.ProxyConfig{
			ID: uuid.New(), AccountID: uuid.New(), URL: "https://fail.com",
			UpdatedAt: time.Now().UTC(),
		})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestProxyRepo_Delete(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		id := uuid.New()
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, args ...interface{}) (pgconn.CommandTag, error) {
				if len(args) != 1 || args[0] != id {
					t.Error("unexpected args")
				}
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &proxyRepo{tx: tx}
		if err := repo.Delete(context.Background(), id); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("exec error", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("delete failed")
			},
		}
		repo := &proxyRepo{tx: tx}
		err := repo.Delete(context.Background(), uuid.New())
		if err == nil {
			t.Fatal("expected error")
		}
	})
}
