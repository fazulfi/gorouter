package repositories

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"gorouter/internal/domain/pricing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func repoTestOverride(providerID, modelID string, inputPrice, outputPrice float64) pricing.PriceOverride {
	return pricing.PriceOverride{
		ModelID:     modelID,
		ProviderID:  providerID,
		InputPrice:  inputPrice,
		OutputPrice: outputPrice,
		UpdatedBy:   uuid.New(),
		UpdatedAt:   time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC),
	}
}

func TestPricingRepo_SaveOverrides_UpsertSQLAndDeterministicJSON(t *testing.T) {
	var gotSQL string
	var gotArgs []interface{}
	mock := &mockTx{
		execFn: func(_ context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
			gotSQL = sql
			gotArgs = args
			return pgconn.CommandTag{}, nil
		},
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
			return &mockRow{}
		},
		queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
			return &mockRows{}, nil
		},
	}
	repo := NewPricingRepo(mock)

	set := []pricing.PriceOverride{
		repoTestOverride("openai", "gpt-4o", 2.5, 10.0),
		repoTestOverride("anthropic", "claude-3-5-sonnet", 3.0, 15.0),
		repoTestOverride("openai", "gpt-4o-mini", 0.15, 0.6),
	}
	if err := repo.SaveOverrides(context.Background(), set); err != nil {
		t.Fatalf("SaveOverrides: %v", err)
	}

	if !strings.Contains(gotSQL, "INSERT INTO gorouter_settings") ||
		!strings.Contains(gotSQL, "ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()") {
		t.Errorf("upsert SQL unexpected:\n%s", gotSQL)
	}
	if len(gotArgs) != 2 {
		t.Fatalf("args = %d, want 2 (key, payload)", len(gotArgs))
	}
	if gotArgs[0] != "pricing:overrides" {
		t.Errorf("key = %v, want pricing:overrides", gotArgs[0])
	}
	payload, ok := gotArgs[1].([]byte)
	if !ok {
		t.Fatalf("payload arg type = %T, want []byte", gotArgs[1])
	}
	var decoded []pricing.PriceOverride
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("payload not JSON: %v\n%s", err, payload)
	}
	if len(decoded) != 3 {
		t.Fatalf("decoded = %d entries, want 3", len(decoded))
	}
	wantKeys := []string{"anthropic/claude-3-5-sonnet", "openai/gpt-4o", "openai/gpt-4o-mini"}
	for i, k := range wantKeys {
		if decoded[i].Key() != k {
			t.Errorf("position %d = %s, want %s", i, decoded[i].Key(), k)
		}
	}

	// The encoding is deterministic: a shuffled equivalent set yields the
	// exact same payload bytes, and the caller's slice is never mutated.
	shuffled := []pricing.PriceOverride{set[2], set[0], set[1]}
	orderBefore := shuffled[0].Key()
	if err := repo.SaveOverrides(context.Background(), shuffled); err != nil {
		t.Fatalf("SaveOverrides (shuffled): %v", err)
	}
	payload2 := gotArgs[1].([]byte)
	if string(payload) != string(payload2) {
		t.Errorf("payload depends on insertion order:\n%s\n%s", payload, payload2)
	}
	if shuffled[0].Key() != orderBefore {
		t.Errorf("caller slice was mutated: %s -> %s", orderBefore, shuffled[0].Key())
	}
}

func TestPricingRepo_SaveOverrides_NilAndEmptyBecomeEmptyArray(t *testing.T) {
	var payloads [][]byte
	mock := &mockTx{
		execFn: func(_ context.Context, _ string, args ...interface{}) (pgconn.CommandTag, error) {
			payloads = append(payloads, args[1].([]byte))
			return pgconn.CommandTag{}, nil
		},
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
			return &mockRow{}
		},
		queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
			return &mockRows{}, nil
		},
	}
	repo := NewPricingRepo(mock)
	ctx := context.Background()

	if err := repo.SaveOverrides(ctx, nil); err != nil {
		t.Fatalf("SaveOverrides(nil): %v", err)
	}
	if err := repo.SaveOverrides(ctx, []pricing.PriceOverride{}); err != nil {
		t.Fatalf("SaveOverrides(empty): %v", err)
	}
	for i, p := range payloads {
		if string(p) != "[]" {
			t.Errorf("payload %d = %s, want []", i, p)
		}
	}
}

func TestPricingRepo_LoadOverrides_NoRowYieldsEmptySet(t *testing.T) {
	mock := &mockTx{
		execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
			return &mockRow{err: pgx.ErrNoRows}
		},
		queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
			return &mockRows{}, nil
		},
	}
	repo := NewPricingRepo(mock)

	got, err := repo.LoadOverrides(context.Background())
	if err != nil {
		t.Fatalf("LoadOverrides: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("absent key must yield empty non-nil set, got %+v", got)
	}
}

func TestPricingRepo_LoadOverrides_Decodes(t *testing.T) {
	raw := `[{"model_id":"gpt-4o","provider_id":"openai","input_price":2.5,"output_price":10,"updated_by":"11111111-1111-1111-1111-111111111111","updated_at":"2026-08-03T12:00:00Z"}]`
	mock := &mockTx{
		execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
			return &mockRow{vals: []interface{}{[]byte(raw)}}
		},
		queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
			return &mockRows{}, nil
		},
	}
	repo := NewPricingRepo(mock)

	got, err := repo.LoadOverrides(context.Background())
	if err != nil {
		t.Fatalf("LoadOverrides: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("entries = %d, want 1", len(got))
	}
	o := got[0]
	if o.Key() != "openai/gpt-4o" || o.InputPrice != 2.5 || o.OutputPrice != 10.0 {
		t.Errorf("decoded = %+v", o)
	}
	if o.UpdatedBy != uuid.MustParse("11111111-1111-1111-1111-111111111111") {
		t.Errorf("updated_by = %v", o.UpdatedBy)
	}
	if o.UpdatedAt.IsZero() {
		t.Error("updated_at not decoded")
	}
}

func TestPricingRepo_LoadOverrides_EmptyArrayAndMalformed(t *testing.T) {
	mock := &mockTx{
		execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
			return &mockRow{}
		},
		queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
			return &mockRows{}, nil
		},
	}

	t.Run("empty array", func(t *testing.T) {
		mock.queryRowFn = func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
			return &mockRow{vals: []interface{}{[]byte(`[]`)}}
		}
		got, err := NewPricingRepo(mock).LoadOverrides(context.Background())
		if err != nil {
			t.Fatalf("LoadOverrides: %v", err)
		}
		if got == nil || len(got) != 0 {
			t.Errorf("empty array must yield empty non-nil set, got %+v", got)
		}
	})

	t.Run("malformed JSON", func(t *testing.T) {
		mock.queryRowFn = func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
			return &mockRow{vals: []interface{}{[]byte(`{not json`)}}
		}
		if _, err := NewPricingRepo(mock).LoadOverrides(context.Background()); err == nil {
			t.Fatal("LoadOverrides = nil error, want decode error")
		}
	})
}
