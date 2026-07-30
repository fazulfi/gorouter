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

func TestAccountRepo_FindByID(t *testing.T) {
	t.Parallel()

	t.Run("found", func(t *testing.T) {
		id := uuid.New()
		providerID := uuid.New()
		now := time.Now().UTC()
		mf := []string{"gpt-4", "gpt-4-turbo"}

		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, args ...interface{}) pgx.Row {
				if len(args) != 1 || args[0] != id {
					t.Error("unexpected args")
				}
				return &mockRow{
					vals: []interface{}{
						id, providerID, "My Account", "api_key", "sk-cred",
						0, true, 5, mf, now, now,
					},
				}
			},
		}
		repo := &accountRepo{tx: tx}
		a, err := repo.FindByID(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if a == nil {
			t.Fatal("expected non-nil")
		}
		if a.ID != id {
			t.Errorf("ID = %v", a.ID)
		}
		if a.ProviderID != providerID {
			t.Errorf("ProviderID = %v", a.ProviderID)
		}
		if a.Label != "My Account" {
			t.Errorf("Label = %q", a.Label)
		}
		if a.AuthType != "api_key" {
			t.Errorf("AuthType = %q", a.AuthType)
		}
		if a.CredentialRef != "sk-cred" {
			t.Errorf("CredentialRef = %q", a.CredentialRef)
		}
		if a.Priority != 0 {
			t.Errorf("Priority = %d", a.Priority)
		}
		if !a.IsEnabled {
			t.Error("expected IsEnabled = true")
		}
		if a.MaxConcurrent != 5 {
			t.Errorf("MaxConcurrent = %d", a.MaxConcurrent)
		}
		if len(a.ModelFilters) != 2 || a.ModelFilters[0] != "gpt-4" {
			t.Error("unexpected ModelFilters")
		}
	})

	t.Run("not found", func(t *testing.T) {
		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				return &mockRow{err: pgx.ErrNoRows}
			},
		}
		repo := &accountRepo{tx: tx}
		a, err := repo.FindByID(context.Background(), uuid.New())
		if err != nil {
			t.Fatal(err)
		}
		if a != nil {
			t.Fatal("expected nil")
		}
	})

	t.Run("query error", func(t *testing.T) {
		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				return &mockRow{err: errors.New("db error")}
			},
		}
		repo := &accountRepo{tx: tx}
		_, err := repo.FindByID(context.Background(), uuid.New())
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestAccountRepo_FindByProviderID(t *testing.T) {
	t.Parallel()

	t.Run("results", func(t *testing.T) {
		pid := uuid.New()
		now := time.Now().UTC()

		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, args ...interface{}) (pgx.Rows, error) {
				if len(args) != 1 || args[0] != pid {
					t.Error("unexpected args")
				}
				return &mockRows{
					rows: [][]interface{}{
						{uuid.New(), pid, "A1", "api_key", "cred1", 0, true, 0, []string{"gpt-4"}, now, now},
						{uuid.New(), pid, "A2", "oauth", "cred2", 1, true, 3, []string{}, now, now},
					},
				}, nil
			},
		}
		repo := &accountRepo{tx: tx}
		accounts, err := repo.FindByProviderID(context.Background(), pid)
		if err != nil {
			t.Fatal(err)
		}
		if len(accounts) != 2 {
			t.Fatalf("expected 2, got %d", len(accounts))
		}
		if accounts[0].Label != "A1" || accounts[1].Label != "A2" {
			t.Error("unexpected order")
		}
		if accounts[0].Priority != 0 || accounts[1].Priority != 1 {
			t.Error("unexpected priorities")
		}
	})

	t.Run("empty", func(t *testing.T) {
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return &mockRows{}, nil
			},
		}
		repo := &accountRepo{tx: tx}
		accounts, err := repo.FindByProviderID(context.Background(), uuid.New())
		if err != nil {
			t.Fatal(err)
		}
		if len(accounts) != 0 {
			t.Fatalf("expected 0, got %d", len(accounts))
		}
	})

	t.Run("query error", func(t *testing.T) {
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return nil, errors.New("query failed")
			},
		}
		repo := &accountRepo{tx: tx}
		_, err := repo.FindByProviderID(context.Background(), uuid.New())
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestAccountRepo_Create(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &accountRepo{tx: tx}
		a := &provider.Account{
			ID: uuid.New(), ProviderID: uuid.New(), Label: "New", AuthType: "api_key",
			CredentialRef: "sk-key", Priority: 0, IsEnabled: true,
			MaxConcurrent: 5, ModelFilters: []string{"gpt-4"},
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		}
		if err := repo.Create(context.Background(), a); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("nil model filters", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &accountRepo{tx: tx}
		a := &provider.Account{
			ID: uuid.New(), ProviderID: uuid.New(), Label: "NilFilters",
			AuthType: "free", CredentialRef: "", Priority: 2, IsEnabled: false,
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		}
		if err := repo.Create(context.Background(), a); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("exec error", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("insert failed")
			},
		}
		repo := &accountRepo{tx: tx}
		err := repo.Create(context.Background(), &provider.Account{
			ID: uuid.New(), ProviderID: uuid.New(), Label: "Fail", AuthType: "api_key",
		})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestAccountRepo_Update(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &accountRepo{tx: tx}
		a := &provider.Account{
			ID: uuid.New(), ProviderID: uuid.New(), Label: "Updated",
			AuthType: "oauth", CredentialRef: "new-cred", Priority: 1,
			IsEnabled: false, MaxConcurrent: 10, ModelFilters: []string{},
			UpdatedAt: time.Now().UTC(),
		}
		if err := repo.Update(context.Background(), a); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("nil model filters", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &accountRepo{tx: tx}
		a := &provider.Account{
			ID: uuid.New(), ProviderID: uuid.New(), Label: "NilFilters",
			AuthType: "free", UpdatedAt: time.Now().UTC(),
		}
		if err := repo.Update(context.Background(), a); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("exec error", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("update failed")
			},
		}
		repo := &accountRepo{tx: tx}
		err := repo.Update(context.Background(), &provider.Account{
			ID: uuid.New(), ProviderID: uuid.New(), Label: "Fail",
			AuthType: "api_key", UpdatedAt: time.Now().UTC(),
		})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestAccountRepo_Delete(t *testing.T) {
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
		repo := &accountRepo{tx: tx}
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
		repo := &accountRepo{tx: tx}
		err := repo.Delete(context.Background(), uuid.New())
		if err == nil {
			t.Fatal("expected error")
		}
	})
}
