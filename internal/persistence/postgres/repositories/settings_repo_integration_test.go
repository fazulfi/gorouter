package repositories

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"gorouter/internal/domain/settings"
)

func TestSettingsRepo_Integration(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)

	repo := NewSettingsRepo(tx)

	t.Run("set then get round-trips", func(t *testing.T) {
		desc := "rtk toggle"
		if err := repo.Set(ctx, &settings.Setting{Key: settings.KeyRTKEnabled, Value: json.RawMessage(`true`), Description: &desc}); err != nil {
			t.Fatalf("set: %v", err)
		}
		got, err := repo.Get(ctx, settings.KeyRTKEnabled)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.Key != settings.KeyRTKEnabled || string(got.Value) != "true" {
			t.Errorf("round-trip = %+v", got)
		}
		if got.Description == nil || *got.Description != desc {
			t.Errorf("description = %v", got.Description)
		}
		if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
			t.Errorf("timestamps not set: %+v", got)
		}
	})

	t.Run("set upserts in place", func(t *testing.T) {
		if err := repo.Set(ctx, &settings.Setting{Key: settings.KeyRTKEnabled, Value: json.RawMessage(`false`)}); err != nil {
			t.Fatalf("set: %v", err)
		}
		got, err := repo.Get(ctx, settings.KeyRTKEnabled)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if string(got.Value) != "false" {
			t.Errorf("value after upsert = %s", got.Value)
		}
	})

	t.Run("missing get returns ErrSettingNotFound", func(t *testing.T) {
		if _, err := repo.Get(ctx, "no-such-key"); !errors.Is(err, settings.ErrSettingNotFound) {
			t.Fatalf("err = %v, want ErrSettingNotFound", err)
		}
	})

	t.Run("reserved pricing key refused on get and set", func(t *testing.T) {
		if _, err := repo.Get(ctx, "pricing:overrides"); !errors.Is(err, settings.ErrReservedKey) {
			t.Errorf("get err = %v, want ErrReservedKey", err)
		}
		if err := repo.Set(ctx, &settings.Setting{Key: "pricing:overrides", Value: json.RawMessage(`[]`)}); !errors.Is(err, settings.ErrReservedKey) {
			t.Errorf("set err = %v, want ErrReservedKey", err)
		}
	})

	t.Run("list is key-ordered and excludes reserved keys", func(t *testing.T) {
		for _, k := range []string{"z-key", "a-key", "m-key"} {
			if err := repo.Set(ctx, &settings.Setting{Key: k, Value: json.RawMessage(`1`)}); err != nil {
				t.Fatalf("set %s: %v", k, err)
			}
		}
		// The pricing override key is owned by the pricing repository; it
		// must never surface through the settings read path.
		if _, err := tx.Exec(ctx,
			`INSERT INTO gorouter_settings (key, value) VALUES ('pricing:overrides', '[]')`); err != nil {
			t.Fatalf("seed pricing key: %v", err)
		}
		list, err := repo.List(ctx)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		// rtkEnabled persists from the earlier subtests in the same
		// transaction; pricing:overrides (seeded via SQL) is reserved and
		// must never surface.
		want := []string{"a-key", "m-key", "rtkEnabled", "z-key"}
		if len(list) != 4 {
			t.Fatalf("list len = %d, want 4 (%+v)", len(list), list)
		}
		for i, s := range list {
			if s.Key != want[i] {
				t.Errorf("list[%d].Key = %s, want %s", i, s.Key, want[i])
			}
		}
	})

	t.Run("wipe removes non-reserved keys only", func(t *testing.T) {
		if err := repo.Wipe(ctx); err != nil {
			t.Fatalf("wipe: %v", err)
		}
		list, err := repo.List(ctx)
		if err != nil {
			t.Fatalf("list after wipe: %v", err)
		}
		if len(list) != 0 {
			t.Errorf("list after wipe = %+v, want empty", list)
		}
		var reserved json.RawMessage
		if err := tx.QueryRow(ctx, `SELECT value FROM gorouter_settings WHERE key = 'pricing:overrides'`).Scan(&reserved); err != nil {
			t.Fatalf("pricing:overrides must survive the wipe: %v", err)
		}
		if string(reserved) != "[]" {
			t.Errorf("pricing value after wipe = %s", reserved)
		}
	})

	t.Run("nil value is stored as json null", func(t *testing.T) {
		if err := repo.Set(ctx, &settings.Setting{Key: "nil-value-key", Value: nil}); err != nil {
			t.Fatalf("set nil value: %v", err)
		}
		got, err := repo.Get(ctx, "nil-value-key")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if string(got.Value) != "null" {
			t.Errorf("nil value stored as %s, want null", got.Value)
		}
	})
}

// TestSettingsRepo_Transactionality_Integration proves the settings repo
// participates in the enclosing transaction: a rolled-back write leaves no
// row behind.
func TestSettingsRepo_Transactionality_Integration(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	repo := NewSettingsRepo(tx)
	if err := repo.Set(ctx, &settings.Setting{Key: "tx-key", Value: json.RawMessage(`1`)}); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	tx2, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin 2: %v", err)
	}
	defer tx2.Rollback(ctx)
	if _, err := NewSettingsRepo(tx2).Get(ctx, "tx-key"); !errors.Is(err, settings.ErrSettingNotFound) {
		t.Fatalf("rolled-back write visible: %v", err)
	}
}
