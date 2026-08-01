package repositories

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"gorouter/internal/domain/combo"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// noRowsRow is a pgx.Row whose Scan reports no rows, mirroring pgx's
// behavior for absent rows.
type noRowsRow struct{}

func (noRowsRow) Scan(dest ...interface{}) error { return pgx.ErrNoRows }

func TestComboRepo_FindByID_NoRowsNil(t *testing.T) {
	tx := &mockTx{
		queryRowFn: func(ctx context.Context, sql string, args ...interface{}) pgx.Row {
			return noRowsRow{}
		},
	}
	repo := NewComboRepo(tx)
	def, err := repo.FindByID(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if def != nil {
		t.Errorf("expected nil definition, got %+v", def)
	}
}

func TestRuntimeStateRepo_GetAbsentNil(t *testing.T) {
	tx := &mockTx{
		queryRowFn: func(ctx context.Context, sql string, args ...interface{}) pgx.Row {
			return noRowsRow{}
		},
	}
	repo := NewRuntimeStateRepo(tx)
	raw, err := repo.Get(context.Background(), "missing-key")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if raw != nil {
		t.Errorf("expected nil for absent key, got %s", raw)
	}
}

func TestComboRepo_Integration(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t, ctx)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)

	repo := NewComboRepo(tx)
	now := time.Now().UTC()

	providerID := uuid.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO gorouter_providers (id, name, type, base_url, config, is_enabled, created_at, updated_at)
		VALUES ($1, 'combo-test-provider', 'custom', 'http://localhost', '{}', true, $2, $2)`,
		providerID, now); err != nil {
		t.Fatalf("insert provider: %v", err)
	}

	def := &combo.Definition{
		ID:        uuid.New(),
		Name:      "int-combo",
		Strategy:  combo.StrategyFusion,
		Config:    json.RawMessage(`{"quorum":2,"grace_period":"8s","hard_timeout":"90s","judge_model_id":"custom/judge"}`),
		IsActive:  true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := repo.Create(ctx, def); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.FindByID(ctx, def.ID)
	if err != nil || got == nil {
		t.Fatalf("FindByID: %+v, %v", got, err)
	}
	if got.Name != def.Name || got.Strategy != combo.StrategyFusion {
		t.Errorf("definition roundtrip mismatch: %+v", got)
	}
	byName, err := repo.FindByName(ctx, def.Name)
	if err != nil || byName == nil || byName.ID != def.ID {
		t.Errorf("FindByName: %+v, %v", byName, err)
	}

	m1 := combo.Member{ID: uuid.New(), ComboID: def.ID, ProviderID: providerID, ModelRef: "custom/panel-a", Priority: 0, Weight: 1, IsActive: true, CreatedAt: now, UpdatedAt: now}
	m2 := combo.Member{ID: uuid.New(), ComboID: def.ID, ProviderID: providerID, ModelRef: "custom/panel-b", Priority: 1, Weight: 1, IsActive: true, CreatedAt: now, UpdatedAt: now}
	if err := repo.UpsertMembers(ctx, def.ID, []combo.Member{m1, m2}); err != nil {
		t.Fatalf("UpsertMembers: %v", err)
	}
	members, err := repo.ListMembers(ctx, def.ID)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(members) != 2 || members[0].ModelRef != "custom/panel-a" {
		t.Errorf("members = %+v", members)
	}

	// Replace set: drop m1, keep m2, add m3.
	m3 := combo.Member{ID: uuid.New(), ComboID: def.ID, ProviderID: providerID, ModelRef: "custom/panel-c", Priority: 2, Weight: 1, IsActive: true, CreatedAt: now, UpdatedAt: now}
	if err := repo.UpsertMembers(ctx, def.ID, []combo.Member{m2, m3}); err != nil {
		t.Fatalf("UpsertMembers replace: %v", err)
	}
	members, err = repo.ListMembers(ctx, def.ID)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("after replace members = %d, want 2", len(members))
	}
	for _, m := range members {
		if m.ID == m1.ID {
			t.Error("dropped member m1 must be deleted")
		}
	}

	if err := repo.DeleteMember(ctx, m2.ID); err != nil {
		t.Fatalf("DeleteMember: %v", err)
	}
	members, _ = repo.ListMembers(ctx, def.ID)
	if len(members) != 1 || members[0].ID != m3.ID {
		t.Errorf("after DeleteMember members = %+v", members)
	}

	if err := repo.SetActive(ctx, def.ID, false); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	got, _ = repo.FindByID(ctx, def.ID)
	if got.IsActive {
		t.Error("expected inactive")
	}

	def.IsActive = true
	def.UpdatedAt = now.Add(time.Minute)
	if err := repo.Update(ctx, def); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, _ = repo.FindByID(ctx, def.ID)
	if !got.IsActive {
		t.Error("expected active after Update")
	}

	list, err := repo.List(ctx)
	if err != nil || len(list) == 0 {
		t.Errorf("List: %d, %v", len(list), err)
	}

	if err := repo.Delete(ctx, def.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got, _ = repo.FindByID(ctx, def.ID)
	if got != nil {
		t.Error("definition must be gone after Delete")
	}
	members, _ = repo.ListMembers(ctx, def.ID)
	if len(members) != 0 {
		t.Errorf("members must cascade-delete, got %d", len(members))
	}
}

func TestRuntimeStateRepo_Integration(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t, ctx)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)

	repo := NewRuntimeStateRepo(tx)
	key := "combo:rotation:" + uuid.New().String()

	raw, err := repo.Get(ctx, key)
	if err != nil || raw != nil {
		t.Fatalf("Get absent = %s, %v", raw, err)
	}

	value := json.RawMessage(`{"index":2,"stickiness":0}`)
	if err := repo.Set(ctx, key, value, time.Hour); err != nil {
		t.Fatalf("Set: %v", err)
	}
	raw, err = repo.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !jsonEqual(t, raw, value) {
		t.Errorf("roundtrip = %s, want %s", raw, value)
	}

	// Overwrite existing key.
	value2 := json.RawMessage(`{"index":0,"stickiness":1}`)
	if err := repo.Set(ctx, key, value2, time.Hour); err != nil {
		t.Fatalf("Set overwrite: %v", err)
	}
	raw, _ = repo.Get(ctx, key)
	if !jsonEqual(t, raw, value2) {
		t.Errorf("overwrite = %s, want %s", raw, value2)
	}

	// Short TTL expires the checkpoint.
	if err := repo.Set(ctx, key, value2, 100*time.Millisecond); err != nil {
		t.Fatalf("Set ttl: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	raw, err = repo.Get(ctx, key)
	if err != nil || raw != nil {
		t.Errorf("Get after TTL expiry = %s, %v; want nil", raw, err)
	}
}

func jsonEqual(t *testing.T, got, want json.RawMessage) bool {
	t.Helper()
	var g, w interface{}
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("unmarshal got: %v", err)
	}
	if err := json.Unmarshal(want, &w); err != nil {
		t.Fatalf("unmarshal want: %v", err)
	}
	gm, gok := g.(map[string]interface{})
	wm, wok := w.(map[string]interface{})
	if gok && wok {
		if len(gm) != len(wm) {
			return false
		}
		for k, v := range wm {
			if gm[k] != v {
				return false
			}
		}
		return true
	}
	return string(got) == string(want)
}
