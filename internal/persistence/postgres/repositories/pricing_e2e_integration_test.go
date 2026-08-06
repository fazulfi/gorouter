package repositories

import (
	"context"
	"encoding/json"
	"testing"

	"gorouter/internal/app/combos"
	"gorouter/internal/domain/auth"
	"gorouter/internal/domain/pricing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPricingServiceEndToEnd_Integration drives PricingService.ApplyOverride
// through the real wired chain — TxScope factory -> pricingRepo
// (gorouter_settings KV) + auditLogRepo (gorouter_audit_log), one transaction
// per mutation — and proves same-transaction rollback on audit failure plus
// immutable two-event history.
func TestPricingServiceEndToEnd_Integration(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	userID := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO gorouter_users (id, email, password_hash, is_admin, is_active)
		 VALUES ($1, $2, $3, true, true)`,
		userID, "pricing-e2e-"+uuid.NewString()[:8]+"@gorouter.local", "test-hash"); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	beginner := combos.PricingScopeBeginnerFunc(func(ctx context.Context) (combos.PricingScope, error) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return nil, err
		}
		return NewTxScope(tx), nil
	})
	svc := combos.NewPricingService(beginner)
	actor := &auth.Actor{UserID: userID, IsAdmin: true}

	first, err := svc.ApplyOverride(ctx, actor,
		pricing.PriceOverride{ModelID: "gpt-4o", ProviderID: "openai", InputPrice: 1.0, OutputPrice: 2.0})
	if err != nil {
		t.Fatalf("ApplyOverride (first): %v", err)
	}
	if first.UpdatedBy != userID || first.UpdatedAt.IsZero() {
		t.Errorf("applied override not stamped: %+v", first)
	}

	if _, err := svc.ApplyOverride(ctx, actor,
		pricing.PriceOverride{ModelID: "gpt-4o", ProviderID: "openai", InputPrice: 3.5, OutputPrice: 7.0}); err != nil {
		t.Fatalf("ApplyOverride (replace): %v", err)
	}

	list, err := svc.ListOverrides(ctx)
	if err != nil {
		t.Fatalf("ListOverrides: %v", err)
	}
	if len(list) != 1 || list[0].Key() != "openai/gpt-4o" || list[0].InputPrice != 3.5 {
		t.Errorf("materialized overrides = %+v", list)
	}

	rows := pricingAuditRows(t, ctx, pool)
	if len(rows) != 2 {
		t.Fatalf("audit rows = %d, want 2", len(rows))
	}
	firstEventDetails := rowWithAfter(t, rows, "openai", "gpt-4o", 1.0, 2.0)
	if firstEventDetails == "" {
		t.Fatal("first event row missing")
	}
	firstDetails := decodeAuditDetails(t, []byte(firstEventDetails))
	if _, ok := firstDetails["before"]; ok {
		t.Errorf("first event must have no before diff: %v", firstDetails)
	}
	secondEventDetails := rowWithAfter(t, rows, "openai", "gpt-4o", 3.5, 7.0)
	if secondEventDetails == "" {
		t.Fatal("second event row missing")
	}
	secondDetails := decodeAuditDetails(t, []byte(secondEventDetails))
	before, ok := secondDetails["before"].(map[string]any)
	if !ok || before["input_price"] != 1.0 || before["output_price"] != 2.0 {
		t.Errorf("second event before diff = %v", secondDetails["before"])
	}
	after, ok := secondDetails["after"].(map[string]any)
	if !ok || after["input_price"] != 3.5 || after["output_price"] != 7.0 {
		t.Errorf("second event after diff = %v", secondDetails["after"])
	}
	if secondDetails["result"] != "applied" {
		t.Errorf("second event result = %v", secondDetails["result"])
	}

	// A third apply must not mutate the earlier rows: the first event's
	// details stay byte-identical (immutable two-event history).
	if _, err := svc.ApplyOverride(ctx, actor,
		pricing.PriceOverride{ModelID: "gpt-4o-mini", ProviderID: "openai", InputPrice: 0.15, OutputPrice: 0.6}); err != nil {
		t.Fatalf("ApplyOverride (third): %v", err)
	}
	rows = pricingAuditRows(t, ctx, pool)
	if len(rows) != 3 {
		t.Fatalf("audit rows = %d, want 3", len(rows))
	}
	if rowWithAfter(t, rows, "openai", "gpt-4o", 1.0, 2.0) != firstEventDetails {
		t.Errorf("first audit row mutated by later applies")
	}

	// Same-transaction rollback: an actor that does not exist fails the audit
	// INSERT FK, which aborts the transaction; the settings write must not
	// survive.
	beforeValue := pricingSettingsValue(t, ctx, pool)
	badActor := &auth.Actor{UserID: uuid.New()}
	if _, err := svc.ApplyOverride(ctx, badActor,
		pricing.PriceOverride{ModelID: "x", ProviderID: "y", InputPrice: 9.0, OutputPrice: 9.0}); err == nil {
		t.Fatal("ApplyOverride with nonexistent actor must fail")
	}
	afterValue := pricingSettingsValue(t, ctx, pool)
	if afterValue != beforeValue {
		t.Errorf("settings row changed despite failed apply:\n%s\n%s", beforeValue, afterValue)
	}
	if rows := pricingAuditRows(t, ctx, pool); len(rows) != 3 {
		t.Errorf("audit rows = %d after failed apply, want 3", len(rows))
	}
}

type pricingAuditRow struct {
	details []byte
}

func pricingAuditRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool) []pricingAuditRow {
	t.Helper()
	rows, err := pool.Query(ctx,
		`SELECT details FROM gorouter_audit_log WHERE action = 'pricing.override.apply' ORDER BY occurred_at ASC`)
	if err != nil {
		t.Fatalf("query audit rows: %v", err)
	}
	defer rows.Close()
	var out []pricingAuditRow
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			t.Fatalf("scan audit row: %v", err)
		}
		out = append(out, pricingAuditRow{details: raw})
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate audit rows: %v", err)
	}
	return out
}

func decodeAuditDetails(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode audit details: %v", err)
	}
	return m
}

// rowWithAfter returns the details of the audit row whose after diff matches
// the given target and prices, or "" when no row matches.
func rowWithAfter(t *testing.T, rows []pricingAuditRow, providerID, modelID string, inputPrice, outputPrice float64) string {
	t.Helper()
	for _, r := range rows {
		det := decodeAuditDetails(t, r.details)
		after, ok := det["after"].(map[string]any)
		if !ok {
			continue
		}
		if after["provider_id"] == providerID && after["model_id"] == modelID &&
			after["input_price"] == inputPrice && after["output_price"] == outputPrice {
			return string(r.details)
		}
	}
	return ""
}

func pricingSettingsValue(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var raw string
	if err := pool.QueryRow(ctx,
		`SELECT value::text FROM gorouter_settings WHERE key = $1`, pricingSettingsKey).Scan(&raw); err != nil {
		t.Fatalf("read settings value: %v", err)
	}
	return raw
}
