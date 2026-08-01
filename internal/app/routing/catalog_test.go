package routing

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	enginerouting "gorouter/internal/engine/routing"
)

// newTestCatalogService wires a CatalogService over a fake store.
func newTestCatalogService(models ...enginerouting.CatalogModel) *CatalogService {
	return NewCatalogService(&appFakeCatalogStore{models: models})
}

// TestCatalogService_List verifies the app catalog listing returns all models
// in stable order.
func TestCatalogService_List(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	svc := newTestCatalogService(
		appCatalogModel(pid, "gpt-4o", true, true),
		appCatalogModel(pid, "gpt-4o-mini", true, false),
	)
	got, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].ModelName != "gpt-4o" || got[1].ModelName != "gpt-4o-mini" {
		t.Errorf("order = [%s %s]", got[0].ModelName, got[1].ModelName)
	}
}

// TestCatalogService_ReconcileNeverDeletesAdminState verifies discovery
// reconciliation through the app service honors decision #192: custom and
// admin-disabled models survive.
func TestCatalogService_ReconcileNeverDeletesAdminState(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	svc := newTestCatalogService(
		appCatalogModel(pid, "custom-model", false, true),
		appCatalogModel(pid, "disabled-model", true, false),
		appCatalogModel(pid, "stale-builtin", true, true),
	)
	res, err := svc.Reconcile(context.Background(), pid, []enginerouting.DiscoveredModel{
		{ModelName: "gpt-4o", Capabilities: []string{"chat"}},
		{ModelName: "custom-model", Capabilities: []string{"chat"}},
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if res.Deleted != 1 {
		t.Errorf("Deleted = %d, want 1 (stale-builtin only)", res.Deleted)
	}
	all, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	names := map[string]bool{}
	for _, m := range all {
		names[m.ModelName] = true
	}
	if !names["custom-model"] || !names["disabled-model"] {
		t.Errorf("admin state lost after reconcile: %v", names)
	}
	if !names["gpt-4o"] {
		t.Errorf("discovered gpt-4o missing after reconcile: %v", names)
	}
}

// TestCatalogService_DisableAndEnable verifies admin disable/enable toggles.
func TestCatalogService_DisableAndEnable(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	m := appCatalogModel(pid, "gpt-4o", true, true)
	svc := newTestCatalogService(m)
	if err := svc.SetModelEnabled(context.Background(), m.ID, false); err != nil {
		t.Fatalf("SetModelEnabled(false): %v", err)
	}
	got, err := svc.Get(context.Background(), m.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.IsEnabled {
		t.Error("IsEnabled = true after disable")
	}
	if err := svc.SetModelEnabled(context.Background(), m.ID, true); err != nil {
		t.Fatalf("SetModelEnabled(true): %v", err)
	}
	got, _ = svc.Get(context.Background(), m.ID)
	if !got.IsEnabled {
		t.Error("IsEnabled = false after enable")
	}
}

// TestCatalogService_CreateCustomModel verifies admin-created custom models
// are stored as custom (is_builtin=false) and survive later reconciliation.
func TestCatalogService_CreateCustomModel(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	svc := newTestCatalogService()
	m, err := svc.CreateCustomModel(context.Background(), pid, "my-internal-model", []string{"chat"}, 64000)
	if err != nil {
		t.Fatalf("CreateCustomModel: %v", err)
	}
	if m.IsBuiltin {
		t.Error("custom model must have IsBuiltin=false")
	}
	if !m.IsEnabled {
		t.Error("custom model must default to enabled")
	}
	// Reconciliation must preserve it even though the provider never reports it.
	res, err := svc.Reconcile(context.Background(), pid, []enginerouting.DiscoveredModel{
		{ModelName: "gpt-4o"},
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if res.Deleted != 0 {
		t.Errorf("Deleted = %d, custom model must survive", res.Deleted)
	}
	got, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(List) = %d, want 2 (custom preserved + discovered gpt-4o)", len(got))
	}
	foundCustom := false
	for _, m := range got {
		if m.ModelName == "my-internal-model" {
			foundCustom = true
		}
	}
	if !foundCustom {
		t.Errorf("custom model lost after reconcile: %+v", got)
	}
}

// TestCatalogService_DeleteCustomModel verifies explicit admin deletion works
// (decision #192: deleted only by the admin).
func TestCatalogService_DeleteCustomModel(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	m := appCatalogModel(pid, "my-internal-model", false, true)
	svc := newTestCatalogService(m)
	if err := svc.DeleteModel(context.Background(), m.ID); err != nil {
		t.Fatalf("DeleteModel: %v", err)
	}
	if _, err := svc.Get(context.Background(), m.ID); !errors.Is(err, enginerouting.ErrModelNotFound) {
		t.Errorf("Get after delete = %v, want ErrModelNotFound", err)
	}
}
