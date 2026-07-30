package repositories

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"gorouter/internal/domain/provider"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestProviderRepo_FindByID(t *testing.T) {
	t.Parallel()

	t.Run("found", func(t *testing.T) {
		id := uuid.New()
		now := time.Now().UTC()
		apiKey := strPtr("sk-encrypted")
		cfg := []byte(`{"model":"gpt-4"}`)

		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, args ...interface{}) pgx.Row {
				if len(args) != 1 || args[0] != id {
					t.Error("unexpected args")
				}
				return &mockRow{
					vals: []interface{}{
						id, "My Provider", provider.ProviderOpenAI, "https://api.openai.com",
						apiKey, cfg, true, now, now,
					},
				}
			},
		}
		repo := &providerRepo{tx: tx}
		p, err := repo.FindByID(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if p == nil {
			t.Fatal("expected non-nil")
		}
		if p.ID != id {
			t.Errorf("ID = %v", p.ID)
		}
		if p.Name != "My Provider" {
			t.Errorf("Name = %q", p.Name)
		}
		if p.Type != provider.ProviderOpenAI {
			t.Errorf("Type = %q", p.Type)
		}
		if p.APIKeyEncrypted == nil || *p.APIKeyEncrypted != "sk-encrypted" {
			t.Error("unexpected APIKeyEncrypted")
		}
		if string(p.Config) != `{"model":"gpt-4"}` {
			t.Errorf("Config = %s", string(p.Config))
		}
		if !p.IsEnabled {
			t.Error("expected IsEnabled = true")
		}
	})

	t.Run("not found", func(t *testing.T) {
		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				return &mockRow{err: pgx.ErrNoRows}
			},
		}
		repo := &providerRepo{tx: tx}
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
		repo := &providerRepo{tx: tx}
		_, err := repo.FindByID(context.Background(), uuid.New())
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestProviderRepo_FindByType(t *testing.T) {
	t.Parallel()

	t.Run("results", func(t *testing.T) {
		now := time.Now().UTC()
		cfg := []byte(`{}`)

		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, args ...interface{}) (pgx.Rows, error) {
				if len(args) != 1 || args[0] != string(provider.ProviderOpenAI) {
					t.Error("unexpected args")
				}
				return &mockRows{
					rows: [][]interface{}{
						{uuid.New(), "P1", provider.ProviderOpenAI, "https://oai.com", (*string)(nil), cfg, true, now, now},
						{uuid.New(), "P2", provider.ProviderOpenAI, "https://oai2.com", (*string)(nil), cfg, false, now, now},
					},
				}, nil
			},
		}
		repo := &providerRepo{tx: tx}
		providers, err := repo.FindByType(context.Background(), provider.ProviderOpenAI)
		if err != nil {
			t.Fatal(err)
		}
		if len(providers) != 2 {
			t.Fatalf("expected 2, got %d", len(providers))
		}
		if providers[0].Name != "P1" || providers[1].Name != "P2" {
			t.Error("unexpected order")
		}
	})

	t.Run("empty", func(t *testing.T) {
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return &mockRows{}, nil
			},
		}
		repo := &providerRepo{tx: tx}
		providers, err := repo.FindByType(context.Background(), provider.ProviderAnthropic)
		if err != nil {
			t.Fatal(err)
		}
		if len(providers) != 0 {
			t.Fatalf("expected 0, got %d", len(providers))
		}
	})

	t.Run("query error", func(t *testing.T) {
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return nil, errors.New("query failed")
			},
		}
		repo := &providerRepo{tx: tx}
		_, err := repo.FindByType(context.Background(), provider.ProviderOpenAI)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("scan error", func(t *testing.T) {
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return &mockRows{rows: [][]interface{}{{}}}, nil
			},
		}
		repo := &providerRepo{tx: tx}
		_, err := repo.FindByType(context.Background(), provider.ProviderOpenAI)
		if err != nil {
			t.Fatal("expected no error with partial row data")
		}
	})
}

func TestProviderRepo_List(t *testing.T) {
	t.Parallel()

	t.Run("results", func(t *testing.T) {
		now := time.Now().UTC()
		cfg := []byte(`{}`)

		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return &mockRows{
					rows: [][]interface{}{
						{uuid.New(), "A", provider.ProviderCustom, "https://a.com", (*string)(nil), cfg, true, now, now},
					},
				}, nil
			},
		}
		repo := &providerRepo{tx: tx}
		providers, err := repo.List(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(providers) != 1 {
			t.Fatalf("expected 1, got %d", len(providers))
		}
	})

	t.Run("empty", func(t *testing.T) {
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return &mockRows{}, nil
			},
		}
		repo := &providerRepo{tx: tx}
		providers, err := repo.List(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(providers) != 0 {
			t.Fatalf("expected 0, got %d", len(providers))
		}
	})

	t.Run("query error", func(t *testing.T) {
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return nil, errors.New("list failed")
			},
		}
		repo := &providerRepo{tx: tx}
		_, err := repo.List(context.Background())
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestProviderRepo_Create(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &providerRepo{tx: tx}
		p := &provider.Provider{
			ID: uuid.New(), Name: "New", Type: provider.ProviderCustom,
			BaseURL: "https://custom.com", Config: json.RawMessage(`{}`),
			IsEnabled: true, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		}
		if err := repo.Create(context.Background(), p); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("nil config", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &providerRepo{tx: tx}
		p := &provider.Provider{
			ID: uuid.New(), Name: "NilCfg", Type: provider.ProviderCustom,
			BaseURL: "https://nil.com", IsEnabled: true,
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
		repo := &providerRepo{tx: tx}
		err := repo.Create(context.Background(), &provider.Provider{
			ID: uuid.New(), Name: "Fail", Type: provider.ProviderCustom,
		})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestProviderRepo_Update(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &providerRepo{tx: tx}
		p := &provider.Provider{
			ID: uuid.New(), Name: "Updated", Type: provider.ProviderOpenAI,
			Config: json.RawMessage(`{"key":"val"}`),
		}
		if err := repo.Update(context.Background(), p); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("nil config", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &providerRepo{tx: tx}
		p := &provider.Provider{
			ID: uuid.New(), Name: "NilCfg", Type: provider.ProviderAzure,
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
		repo := &providerRepo{tx: tx}
		err := repo.Update(context.Background(), &provider.Provider{
			ID: uuid.New(), Name: "Fail", Type: provider.ProviderCustom,
		})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestProviderRepo_Delete(t *testing.T) {
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
		repo := &providerRepo{tx: tx}
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
		repo := &providerRepo{tx: tx}
		err := repo.Delete(context.Background(), uuid.New())
		if err == nil {
			t.Fatal("expected error")
		}
	})
}
