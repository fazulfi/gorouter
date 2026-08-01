package repositories

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	enginerouting "gorouter/internal/engine/routing"
)

// aliasScanRow builds a mockRow in exact gorouter_model_aliases column order:
// id, alias, target, provider_id, is_active, created_at, updated_at.
func aliasScanRow(id uuid.UUID, alias, target string, pid *uuid.UUID, active bool, now time.Time) *mockRow {
	return &mockRow{vals: []interface{}{id, alias, target, pid, active, now, now}}
}

// TestAliasRepo_CreateAndFind verifies Create inserts all seven schema columns
// and FindByAlias scans them back in the same order.
func TestAliasRepo_CreateAndFind(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	pid := uuid.New()
	now := time.Now().UTC()

	var capturedSQL string
	var capturedArgs []interface{}
	tx := &mockTx{
		execFn: func(_ context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
			capturedSQL = sql
			capturedArgs = args
			return pgconn.CommandTag{}, nil
		},
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
			return aliasScanRow(id, "my-model", "openai/gpt-4o", &pid, true, now)
		},
	}

	repo := NewAliasRepo(tx)
	a := &enginerouting.Alias{
		ID:         id,
		Alias:      "my-model",
		Target:     "openai/gpt-4o",
		ProviderID: &pid,
		IsActive:   true,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := repo.Create(context.Background(), a); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// The INSERT must reference exactly the gorouter_model_aliases columns.
	if !strings.Contains(capturedSQL, "INSERT INTO gorouter_model_aliases") {
		t.Errorf("Create SQL does not target gorouter_model_aliases: %s", capturedSQL)
	}
	for _, col := range []string{"id", "alias", "target", "provider_id", "is_active", "created_at", "updated_at"} {
		if !strings.Contains(capturedSQL, col) {
			t.Errorf("Create SQL missing column %q: %s", col, capturedSQL)
		}
	}
	if len(capturedArgs) != 7 {
		t.Fatalf("Create args = %d, want 7", len(capturedArgs))
	}
	if capturedArgs[0] != id || capturedArgs[1] != "my-model" || capturedArgs[2] != "openai/gpt-4o" {
		t.Errorf("Create args[0:3] = %v, %v, %v", capturedArgs[0], capturedArgs[1], capturedArgs[2])
	}
	if capturedArgs[3] != &pid || capturedArgs[4] != true {
		t.Errorf("Create args[3:5] = %v, %v (provider_id, is_active)", capturedArgs[3], capturedArgs[4])
	}

	got, err := repo.FindByAlias(context.Background(), "my-model")
	if err != nil {
		t.Fatalf("FindByAlias: %v", err)
	}
	if got == nil {
		t.Fatal("FindByAlias returned nil")
	}
	if got.ID != id || got.Alias != "my-model" || got.Target != "openai/gpt-4o" {
		t.Errorf("FindByAlias = %+v", got)
	}
	if got.ProviderID == nil || *got.ProviderID != pid {
		t.Errorf("ProviderID = %v, want %v", got.ProviderID, pid)
	}
	if !got.IsActive {
		t.Error("IsActive = false")
	}
}

// TestAliasRepo_FindByAliasNotExist verifies a missing alias returns nil
// without error.
func TestAliasRepo_FindByAliasNotExist(t *testing.T) {
	t.Parallel()
	tx := &mockTx{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
			return &mockRow{err: pgx.ErrNoRows}
		},
	}
	repo := NewAliasRepo(tx)
	got, err := repo.FindByAlias(context.Background(), "missing")
	if err != nil {
		t.Fatalf("FindByAlias: %v", err)
	}
	if got != nil {
		t.Errorf("FindByAlias = %+v, want nil", got)
	}
}

// TestAliasRepo_Update verifies Update rewrites alias/target/provider_id/
// is_active and bumps updated_at.
func TestAliasRepo_Update(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	var sql string
	var args []interface{}
	tx := &mockTx{
		execFn: func(_ context.Context, s string, a ...interface{}) (pgconn.CommandTag, error) {
			sql = s
			args = a
			return pgconn.CommandTag{}, nil
		},
	}
	repo := NewAliasRepo(tx)
	a := &enginerouting.Alias{
		ID: id, Alias: "renamed", Target: "anthropic/claude-opus-4",
		ProviderID: nil, IsActive: false, UpdatedAt: time.Now().UTC(),
	}
	if err := repo.Update(context.Background(), a); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !strings.Contains(sql, "UPDATE gorouter_model_aliases") {
		t.Errorf("Update SQL = %s", sql)
	}
	if len(args) != 6 || args[5] != id {
		t.Errorf("Update args = %v (want 6 args ending with id)", args)
	}
}

// TestAliasRepo_SetActive verifies the toggle statement targets is_active and
// updates updated_at.
func TestAliasRepo_SetActive(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	var sql string
	var args []interface{}
	tx := &mockTx{
		execFn: func(_ context.Context, s string, a ...interface{}) (pgconn.CommandTag, error) {
			sql = s
			args = a
			return pgconn.CommandTag{}, nil
		},
	}
	repo := NewAliasRepo(tx)
	if err := repo.SetActive(context.Background(), id, false); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	if !strings.Contains(sql, "is_active") || !strings.Contains(sql, "UPDATE gorouter_model_aliases") {
		t.Errorf("SetActive SQL = %s", sql)
	}
	if len(args) != 2 || args[0] != false || args[1] != id {
		t.Errorf("SetActive args = %v, want [false, id]", args)
	}
}

// TestAliasRepo_Delete verifies deletion by id.
func TestAliasRepo_Delete(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	var sql string
	tx := &mockTx{
		execFn: func(_ context.Context, s string, _ ...interface{}) (pgconn.CommandTag, error) {
			sql = s
			return pgconn.CommandTag{}, nil
		},
	}
	repo := NewAliasRepo(tx)
	if err := repo.Delete(context.Background(), id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !strings.Contains(sql, "DELETE FROM gorouter_model_aliases") {
		t.Errorf("Delete SQL = %s", sql)
	}
}

// TestAliasRepo_ListStableOrder verifies List orders by original creation
// time with an alias tie-break (stable original order, parity rows 100/110).
func TestAliasRepo_ListStableOrder(t *testing.T) {
	t.Parallel()
	var sql string
	tx := &mockTx{
		queryFn: func(_ context.Context, s string, _ ...interface{}) (pgx.Rows, error) {
			sql = s
			return &mockRows{}, nil
		},
	}
	repo := NewAliasRepo(tx)
	if _, err := repo.List(context.Background()); err != nil {
		t.Fatalf("List: %v", err)
	}
	if !strings.Contains(sql, "ORDER BY created_at ASC") || !strings.Contains(sql, "alias ASC") {
		t.Errorf("List SQL lacks stable ordering: %s", sql)
	}
}

// TestAliasRepo_ListActiveOnly verifies ListActive filters is_active.
func TestAliasRepo_ListActiveOnly(t *testing.T) {
	t.Parallel()
	var sql string
	tx := &mockTx{
		queryFn: func(_ context.Context, s string, _ ...interface{}) (pgx.Rows, error) {
			sql = s
			return &mockRows{}, nil
		},
	}
	repo := NewAliasRepo(tx)
	if _, err := repo.ListActive(context.Background()); err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if !strings.Contains(sql, "is_active = true") {
		t.Errorf("ListActive SQL lacks is_active filter: %s", sql)
	}
}

// ---------------------------------------------------------------------------
// Catalog repository (gorouter_provider_models) — decision #192 SQL guards
// ---------------------------------------------------------------------------

// TestCatalogRepo_UpsertPreservesAdminFlags verifies the upsert never
// overwrites is_enabled/is_builtin on conflict (decision #192).
func TestCatalogRepo_UpsertPreservesAdminFlags(t *testing.T) {
	t.Parallel()
	var sql string
	tx := &mockTx{
		execFn: func(_ context.Context, s string, _ ...interface{}) (pgconn.CommandTag, error) {
			sql = s
			return pgconn.CommandTag{}, nil
		},
	}
	repo := NewCatalogRepo(tx)
	err := repo.UpsertDiscovered(context.Background(), enginerouting.CatalogModel{
		ProviderID: uuid.New(), ModelName: "gpt-4o", Capabilities: []string{"chat"},
	})
	if err != nil {
		t.Fatalf("UpsertDiscovered: %v", err)
	}
	if !strings.Contains(sql, "ON CONFLICT (provider_id, model_id)") {
		t.Errorf("UpsertDiscovered lacks conflict target: %s", sql)
	}
	if strings.Contains(sql, "is_enabled = EXCLUDED") || strings.Contains(sql, "is_builtin = EXCLUDED") {
		t.Errorf("UpsertDiscovered must preserve admin flags on conflict: %s", sql)
	}
	for _, col := range []string{"capabilities", "max_tokens", "display_name"} {
		if !strings.Contains(sql, col) {
			t.Errorf("UpsertDiscovered missing refresh column %q: %s", col, sql)
		}
	}
}

// TestCatalogRepo_DeleteStaleGuarded verifies reconciliation deletes are
// guarded to builtin+enabled rows only (decision #192 enforced in SQL).
func TestCatalogRepo_DeleteStaleGuarded(t *testing.T) {
	t.Parallel()
	var sql string
	var args []interface{}
	tx := &mockTx{
		execFn: func(_ context.Context, s string, a ...interface{}) (pgconn.CommandTag, error) {
			sql = s
			args = a
			return pgconn.CommandTag{}, nil
		},
	}
	repo := NewCatalogRepo(tx)
	id := uuid.New()
	if err := repo.DeleteStale(context.Background(), id); err != nil {
		t.Fatalf("DeleteStale: %v", err)
	}
	if !strings.Contains(sql, "is_builtin = true") || !strings.Contains(sql, "is_enabled = true") {
		t.Errorf("DeleteStale must be guarded to builtin+enabled: %s", sql)
	}
	if len(args) != 1 || args[0] != id {
		t.Errorf("DeleteStale args = %v", args)
	}
}

// TestCatalogRepo_AdminDeleteUnconditional verifies the explicit admin delete
// is not guarded (custom models are removed only by admin action, decision
// #192).
func TestCatalogRepo_AdminDeleteUnconditional(t *testing.T) {
	t.Parallel()
	var sql string
	tx := &mockTx{
		execFn: func(_ context.Context, s string, _ ...interface{}) (pgconn.CommandTag, error) {
			sql = s
			return pgconn.CommandTag{}, nil
		},
	}
	repo := NewCatalogRepo(tx)
	if err := repo.Delete(context.Background(), uuid.New()); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if strings.Contains(sql, "is_builtin") || strings.Contains(sql, "is_enabled") {
		t.Errorf("admin Delete must be unconditional: %s", sql)
	}
}

// TestCatalogRepo_SetEnabled verifies the admin toggle targets is_enabled.
func TestCatalogRepo_SetEnabled(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	var sql string
	var args []interface{}
	tx := &mockTx{
		execFn: func(_ context.Context, s string, a ...interface{}) (pgconn.CommandTag, error) {
			sql = s
			args = a
			return pgconn.CommandTag{}, nil
		},
	}
	repo := NewCatalogRepo(tx)
	if err := repo.SetEnabled(context.Background(), id, false); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	if !strings.Contains(sql, "is_enabled") {
		t.Errorf("SetEnabled SQL = %s", sql)
	}
	if len(args) != 2 || args[0] != false || args[1] != id {
		t.Errorf("SetEnabled args = %v", args)
	}
}

// TestCatalogRepo_InsertCustom verifies custom models are inserted with
// is_builtin=false (admin-owned).
func TestCatalogRepo_InsertCustom(t *testing.T) {
	t.Parallel()
	var sql string
	var args []interface{}
	tx := &mockTx{
		execFn: func(_ context.Context, s string, a ...interface{}) (pgconn.CommandTag, error) {
			sql = s
			args = a
			return pgconn.CommandTag{}, nil
		},
	}
	repo := NewCatalogRepo(tx)
	m := &enginerouting.CatalogModel{
		ID: uuid.New(), ProviderID: uuid.New(), ModelName: "custom", IsBuiltin: false, IsEnabled: true,
	}
	if err := repo.InsertCustom(context.Background(), m); err != nil {
		t.Fatalf("InsertCustom: %v", err)
	}
	if !strings.Contains(sql, "is_builtin") || !strings.Contains(sql, "false") {
		t.Errorf("InsertCustom SQL = %s", sql)
	}
	if len(args) != 7 {
		t.Errorf("InsertCustom args = %v, want 7 (is_builtin is a SQL literal)", args)
	}
}

// TestCatalogRepo_ListAllScan verifies ListAll scans the full column set
// including admin flags.
func TestCatalogRepo_ListAllScan(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	pid := uuid.New()
	now := time.Now().UTC()
	displayName := "GPT-4o"
	maxTokens := int64(128000)
	tx := &mockTx{
		queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
			return &mockRows{rows: [][]interface{}{{
				id, pid, "gpt-4o", &displayName, []string{"chat"}, &maxTokens, true, false, now, now,
			}}}, nil
		},
	}
	repo := NewCatalogRepo(tx)
	got, err := repo.ListAll(context.Background())
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d", len(got))
	}
	m := got[0]
	if m.ID != id || m.ModelName != "gpt-4o" || m.MaxTokens != 128000 {
		t.Errorf("ListAll = %+v", m)
	}
	if m.DisplayName != "GPT-4o" {
		t.Errorf("DisplayName = %q", m.DisplayName)
	}
	if !m.IsBuiltin || m.IsEnabled {
		t.Errorf("admin flags wrong: builtin=%v enabled=%v", m.IsBuiltin, m.IsEnabled)
	}
}
