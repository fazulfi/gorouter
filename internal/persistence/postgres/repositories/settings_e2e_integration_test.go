package repositories

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"gorouter/internal/app/combos"
	"gorouter/internal/app/settings"
	"gorouter/internal/app/tx"
	"gorouter/internal/domain/auth"
	"gorouter/internal/domain/pricing"
	settingsdomain "gorouter/internal/domain/settings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// settingsScopeBeginner wires the settings services to the real TxScope.
func settingsScopeBeginner(pool *pgxpool.Pool) settings.ScopeBeginner {
	return settings.ScopeBeginnerFunc(func(ctx context.Context) (settings.Scope, error) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return nil, err
		}
		return NewTxScope(tx), nil
	})
}

// TestSettingsServiceEndToEnd_Integration drives SettingsService.Set through
// the real wired chain: TxScope -> settingsRepo (gorouter_settings) +
// auditLogRepo (gorouter_audit_log), one transaction per mutation, with the
// sanitized before/after diff and the typed key contract.
func TestSettingsServiceEndToEnd_Integration(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	userID := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO gorouter_users (id, email, password_hash, is_admin, is_active)
		 VALUES ($1, $2, $3, true, true)`,
		userID, "settings-e2e-"+uuid.NewString()[:8]+"@gorouter.local", "test-hash"); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	svc := settings.NewSettingsService(settingsScopeBeginner(pool))
	actor := &auth.Actor{UserID: userID, IsAdmin: true}

	stored, err := svc.Set(ctx, actor, settingsdomain.KeyRTKEnabled, json.RawMessage(`true`))
	if err != nil {
		t.Fatalf("Set rtkEnabled: %v", err)
	}
	if stored.Key != settingsdomain.KeyRTKEnabled || string(stored.Value) != "true" {
		t.Errorf("stored = %+v", stored)
	}

	// Non-boolean typed value must be refused.
	if _, err := svc.Set(ctx, actor, settingsdomain.KeyRTKEnabled, json.RawMessage(`"yes"`)); err == nil {
		t.Error("non-boolean rtkEnabled accepted")
	}

	// A value carrying a credential shape must be stored raw but audited
	// sanitized.
	e2eKey := "sk-proj-" + "e2e123456789"
	secretValue := json.RawMessage(`{"api_key_value":"` + e2eKey + `"}`)
	if _, err := svc.Set(ctx, actor, "provider-secret", secretValue); err != nil {
		t.Fatalf("Set secret: %v", err)
	}
	got, err := svc.Get(ctx, actor, "provider-secret")
	if err != nil {
		t.Fatal(err)
	}
	var gotV, wantV map[string]any
	if err := json.Unmarshal(got.Value, &gotV); err != nil {
		t.Fatalf("parse stored value: %v", err)
	}
	if err := json.Unmarshal(secretValue, &wantV); err != nil {
		t.Fatalf("parse seed value: %v", err)
	}
	if !reflect.DeepEqual(gotV, wantV) {
		t.Errorf("stored value = %s, want %s", got.Value, secretValue)
	}

	rows := auditRowsForAction(t, ctx, pool, "settings.set")
	if len(rows) < 2 {
		t.Fatalf("settings.set audit rows = %d, want >= 2", len(rows))
	}
	joined := strings.Join(rows, "\n")
	if strings.Contains(joined, e2eKey) {
		t.Errorf("raw credential leaked into audit details:\n%s", joined)
	}
	if !strings.Contains(joined, "[REDACTED]") {
		t.Errorf("audit details not redacted:\n%s", joined)
	}

	// Reserved key refused end-to-end.
	if _, err := svc.Set(ctx, actor, "pricing:overrides", json.RawMessage(`[]`)); !errors.Is(err, settingsdomain.ErrReservedKey) {
		t.Errorf("err = %v, want ErrReservedKey", err)
	}

	// List returns the stored non-reserved keys and excludes the reserved
	// pricing:overrides key even when the pricing repository owns a row.
	if _, err := pool.Exec(ctx,
		`INSERT INTO gorouter_settings (key, value) VALUES ('pricing:overrides', '[]')`); err != nil {
		t.Fatalf("insert reserved key: %v", err)
	}
	listed, err := svc.List(ctx, actor)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := map[string]bool{}
	for _, s := range listed {
		found[s.Key] = true
	}
	if !found[settingsdomain.KeyRTKEnabled] || !found["provider-secret"] {
		t.Errorf("listed keys = %v", found)
	}
	if found["pricing:overrides"] {
		t.Error("List must not return the reserved pricing:overrides key")
	}
}

// TestAuditQueryServiceEndToEnd_Integration drives the SELECT-only audit
// query through the real wired chain and proves deterministic filtering,
// pagination, order, and redacted export.
func TestAuditQueryServiceEndToEnd_Integration(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	userID := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO gorouter_users (id, email, password_hash, is_admin, is_active)
		 VALUES ($1, $2, $3, true, true)`,
		userID, "auditq-e2e-"+uuid.NewString()[:8]+"@gorouter.local", "test-hash"); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	actor := &auth.Actor{UserID: userID, IsAdmin: true}

	// Seed two settings events through the real chain, one carrying a
	// credential shape in its stored value.
	svc := settings.NewSettingsService(settingsScopeBeginner(pool))
	if _, err := svc.Set(ctx, actor, "k1", json.RawMessage(`1`)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Set(ctx, actor, "k2", json.RawMessage(`{"api_key_value":"sk-proj-e2e-query-123456"}`)); err != nil {
		t.Fatal(err)
	}

	querySvc := settings.NewAuditQueryService(settings.AuditQueryScopeBeginnerFunc(func(ctx context.Context) (settings.AuditQueryScope, error) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return nil, err
		}
		return NewTxScope(tx), nil
	}))

	if _, err := querySvc.List(ctx, nil, tx.AuditFilters{}, tx.AuditPage{}); !errors.Is(err, settings.ErrAuditActorRequired) {
		t.Errorf("err = %v, want ErrAuditActorRequired", err)
	}

	action := "settings.set"
	entries, err := querySvc.List(ctx, actor, tx.AuditFilters{Action: &action}, tx.AuditPage{Number: 1, Size: 1})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("page entries = %d, want 1", len(entries))
	}
	page2, err := querySvc.List(ctx, actor, tx.AuditFilters{Action: &action}, tx.AuditPage{Number: 2, Size: 1})
	if err != nil {
		t.Fatalf("List page 2: %v", err)
	}
	if len(page2) != 1 || page2[0].ID == entries[0].ID {
		t.Errorf("pagination overlap: %+v vs %+v", entries[0], page2)
	}

	exported, err := querySvc.Export(ctx, actor, tx.AuditFilters{Action: &action})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if len(exported) != 2 {
		t.Fatalf("export entries = %d, want 2", len(exported))
	}
	joined := ""
	for _, e := range exported {
		if len(e.Details) > 0 {
			var probe interface{}
			if err := json.Unmarshal(e.Details, &probe); err != nil {
				t.Errorf("exported details invalid JSON: %s (%v)", e.Details, err)
			}
		}
		joined += string(e.Details) + "\n"
	}
	if strings.Contains(joined, "sk-proj-e2e-query-123456") {
		t.Errorf("raw credential leaked into export:\n%s", joined)
	}
	if !strings.Contains(joined, "[REDACTED]") {
		t.Errorf("export not redacted:\n%s", joined)
	}
}

// TestConfigTransferEndToEnd_Integration drives the manual configuration
// transfer through the real wired chain: export carries no excluded domains,
// import applies missing-key wipe transactionally, unsupported domains are
// rejected, and the confirmation gates the destructive write.
func TestConfigTransferEndToEnd_Integration(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	userID := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO gorouter_users (id, email, password_hash, is_admin, is_active)
		 VALUES ($1, $2, $3, true, true)`,
		userID, "transfer-e2e-"+uuid.NewString()[:8]+"@gorouter.local", "test-hash"); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	actor := &auth.Actor{UserID: userID, IsAdmin: true}

	beginner := settings.ConfigTransferScopeBeginnerFunc(func(ctx context.Context) (settings.ConfigTransferScope, error) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return nil, err
		}
		return NewTxScope(tx), nil
	})
	svc := settings.NewConfigTransferService(beginner)

	// Seed a provider, an alias, a setting, and a pricing override.
	providerID := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO gorouter_providers (id, name, type, base_url, is_enabled)
		 VALUES ($1, $2, $3, $4, true)`,
		providerID, "openai", "openai", "https://api.openai.com/v1"); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	aliasID := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO gorouter_model_aliases (id, alias, target, provider_id)
		 VALUES ($1, $2, $3, $4)`,
		aliasID, "fast", "gpt-4o", providerID); err != nil {
		t.Fatalf("seed alias: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO gorouter_settings (key, value) VALUES ('rtkEnabled', 'true')`); err != nil {
		t.Fatalf("seed setting: %v", err)
	}
	pricingBeginner := combos.PricingScopeBeginnerFunc(func(ctx context.Context) (combos.PricingScope, error) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return nil, err
		}
		return NewTxScope(tx), nil
	})
	pricingSvc := combos.NewPricingService(pricingBeginner)
	if _, err := pricingSvc.ApplyOverride(ctx, actor,
		pricing.PriceOverride{ModelID: "gpt-4o", ProviderID: "openai", InputPrice: 1, OutputPrice: 2}); err != nil {
		t.Fatalf("seed pricing: %v", err)
	}

	exported, err := svc.Export(ctx, actor)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	raw, _ := json.Marshal(exported)
	for _, banned := range []string{"gorouter_usage_daily", "gorouter_request_history", "gorouter_console_logs",
		"gorouter_sessions", "gorouter_audit_log", "usageHistory", "requestDetails"} {
		if strings.Contains(string(raw), banned) {
			t.Errorf("export leaks excluded domain %q", banned)
		}
	}
	if len(exported.ProviderConnections) != 1 || exported.ProviderConnections[0].ID != providerID {
		t.Errorf("exported providers = %+v", exported.ProviderConnections)
	}
	if len(exported.ModelAliases) != 1 || len(exported.Pricing) != 1 {
		t.Errorf("exported domains = %+v", exported)
	}

	// Wrong confirmation: nothing changes.
	if _, err := svc.Import(ctx, actor, settings.ConfigPayload{}, "nope"); !errors.Is(err, settings.ErrImportDeclined) {
		t.Fatalf("err = %v, want ErrImportDeclined", err)
	}
	var probe int
	if err := pool.QueryRow(ctx, "SELECT 1 FROM gorouter_providers WHERE id = $1", providerID).Scan(&probe); err != nil {
		t.Fatalf("declined import must not touch providers: %v", err)
	}

	// Partial import with correct confirmation: missing-key wipe.
	replacementID := uuid.New()
	now := time.Now().UTC()
	partial := settings.ConfigPayload{
		Settings: map[string]json.RawMessage{"rtkEnabled": json.RawMessage(`false`)},
		ProviderConnections: []settings.ProviderConnectionTransfer{
			{ID: replacementID, Name: "replacement", Type: "openai",
				BaseURL: "https://api.replacement.example/v1", Config: json.RawMessage(`{}`),
				IsEnabled: true, CreatedAt: now, UpdatedAt: now},
		},
	}
	reexported, err := svc.Import(ctx, actor, partial, settings.ConfigImportConfirmation)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	// The old provider and the alias are gone (missing-key wipe); the
	// replacement is present; the audit recorded the accept.
	var providerCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM gorouter_providers").Scan(&providerCount); err != nil {
		t.Fatal(err)
	}
	if providerCount != 1 {
		t.Errorf("providers after wipe = %d, want 1", providerCount)
	}
	var aliasCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM gorouter_model_aliases").Scan(&aliasCount); err != nil {
		t.Fatal(err)
	}
	if aliasCount != 0 {
		t.Errorf("aliases after wipe = %d, want 0", aliasCount)
	}
	if reexported == nil || len(reexported.ProviderConnections) != 1 || reexported.ProviderConnections[0].ID != replacementID {
		t.Errorf("re-exported payload = %+v", reexported)
	}
	if !strings.Contains(strings.Join(auditRowsForAction(t, ctx, pool, "config.transfer.import.accept"), "\n"), "applied") {
		t.Error("accept audit missing")
	}

	// Unsupported domain: the frozen upstream fixture carries apiKeys and
	// combos; a payload with them must be rejected without state change.
	rejected := settings.ConfigPayload{APIKeys: json.RawMessage(`[{"id":"x"}]`)}
	if _, err := svc.Import(ctx, actor, rejected, settings.ConfigImportConfirmation); !errors.Is(err, settings.ErrUnsupportedDomain) {
		t.Fatalf("err = %v, want ErrUnsupportedDomain", err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM gorouter_providers").Scan(&providerCount); err != nil {
		t.Fatal(err)
	}
	if providerCount != 1 {
		t.Errorf("providers after rejected import = %d, want 1", providerCount)
	}

	// Mid-transaction insert failure: the payload passes validation but its
	// node references a provider that is not part of the payload, so the
	// insert violates the foreign key. The whole wipe must roll back.
	bad := settings.ConfigPayload{
		Settings: map[string]json.RawMessage{"rtkEnabled": json.RawMessage(`true`)},
		ProviderNodes: []settings.ProviderNodeTransfer{
			{ID: "orphan-node", ProviderID: uuid.New(), Name: "orphan"},
		},
	}
	if _, err := svc.Import(ctx, actor, bad, settings.ConfigImportConfirmation); err == nil {
		t.Fatal("FK-violating payload accepted")
	}
	var rtk string
	if err := pool.QueryRow(ctx, `SELECT value::text FROM gorouter_settings WHERE key = 'rtkEnabled'`).Scan(&rtk); err != nil {
		t.Fatalf("rollback check: %v", err)
	}
	if rtk != "false" {
		t.Errorf("rtkEnabled after failed import = %s, want false (rollback preserved pre-import state)", rtk)
	}
}

func auditRowsForAction(t *testing.T, ctx context.Context, pool *pgxpool.Pool, action string) []string {
	t.Helper()
	rows, err := pool.Query(ctx,
		`SELECT details::text FROM gorouter_audit_log WHERE action = $1 ORDER BY occurred_at`, action)
	if err != nil {
		t.Fatalf("query audit: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			t.Fatal(err)
		}
		out = append(out, d)
	}
	return out
}
