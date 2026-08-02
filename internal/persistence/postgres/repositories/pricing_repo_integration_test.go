package repositories

import (
	"context"
	"testing"
	"time"

	"gorouter/internal/domain/pricing"

	"github.com/google/uuid"
)

// TestPricingRepo_OverridesRoundTrip_Integration proves the pricing
// repository persists the current override set in the existing
// gorouter_settings KV row "pricing:overrides" (JSONB array) with no new
// table or migration.
func TestPricingRepo_OverridesRoundTrip_Integration(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)

	repo := NewPricingRepo(tx)
	actor := uuid.New()
	now := time.Now().UTC().Truncate(time.Microsecond)

	got, err := repo.LoadOverrides(ctx)
	if err != nil {
		t.Fatalf("LoadOverrides (absent key): %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("absent key must load as empty set, got %+v", got)
	}

	set := []pricing.PriceOverride{
		{ModelID: "gpt-4o-mini", ProviderID: "openai", InputPrice: 0.15, OutputPrice: 0.6, UpdatedBy: actor, UpdatedAt: now},
		{ModelID: "claude-3-5-sonnet", ProviderID: "anthropic", InputPrice: 3.0, OutputPrice: 15.0, UpdatedBy: actor, UpdatedAt: now},
	}
	if err := repo.SaveOverrides(ctx, set); err != nil {
		t.Fatalf("SaveOverrides: %v", err)
	}

	var kind string
	if err := tx.QueryRow(ctx,
		`SELECT jsonb_typeof(value) FROM gorouter_settings WHERE key = $1`, pricingSettingsKey).Scan(&kind); err != nil {
		t.Fatalf("read settings row: %v", err)
	}
	if kind != "array" {
		t.Errorf("settings value kind = %s, want array", kind)
	}

	got, err = repo.LoadOverrides(ctx)
	if err != nil {
		t.Fatalf("LoadOverrides: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("entries = %d, want 2", len(got))
	}
	if got[0].Key() != "anthropic/claude-3-5-sonnet" || got[0].InputPrice != 3.0 || got[0].OutputPrice != 15.0 {
		t.Errorf("got[0] = %+v", got[0])
	}
	if got[1].Key() != "openai/gpt-4o-mini" || got[1].InputPrice != 0.15 || got[1].OutputPrice != 0.6 {
		t.Errorf("got[1] = %+v", got[1])
	}
	if got[0].UpdatedBy != actor || got[0].UpdatedAt.IsZero() {
		t.Errorf("stamps not preserved: %+v", got[0])
	}

	replacement := []pricing.PriceOverride{
		{ModelID: "gpt-4o", ProviderID: "openai", InputPrice: 2.5, OutputPrice: 10.0, UpdatedBy: actor, UpdatedAt: now},
	}
	if err := repo.SaveOverrides(ctx, replacement); err != nil {
		t.Fatalf("SaveOverrides (replacement): %v", err)
	}
	got, err = repo.LoadOverrides(ctx)
	if err != nil {
		t.Fatalf("LoadOverrides: %v", err)
	}
	if len(got) != 1 || got[0].Key() != "openai/gpt-4o" || got[0].InputPrice != 2.5 {
		t.Errorf("replacement set = %+v", got)
	}

	if err := repo.SaveOverrides(ctx, []pricing.PriceOverride{}); err != nil {
		t.Fatalf("SaveOverrides (empty): %v", err)
	}
	got, err = repo.LoadOverrides(ctx)
	if err != nil {
		t.Fatalf("LoadOverrides: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("empty set must round-trip as empty, got %+v", got)
	}
}
