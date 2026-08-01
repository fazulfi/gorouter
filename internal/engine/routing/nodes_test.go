package routing

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"gorouter/internal/domain/provider"
)

// ---------------------------------------------------------------------------
// Provider nodes — runtime/config-backed (no node table exists in schema)
// ---------------------------------------------------------------------------

// TestMemoryNodeStore_RoundTrip verifies the runtime node store persists
// nodes for the process lifetime.
func TestMemoryNodeStore_RoundTrip(t *testing.T) {
	t.Parallel()
	store := NewMemoryNodeStore()
	pid := uuid.New()
	n := ProviderNode{
		ID:         "eu-1",
		ProviderID: pid,
		Name:       "EU primary",
		BaseURL:    "https://eu.example.com/v1",
		Region:     "eu",
		Priority:   0,
		IsActive:   true,
	}
	if err := store.Save(context.Background(), n); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].ID != "eu-1" || got[0].BaseURL != "https://eu.example.com/v1" {
		t.Errorf("List = %+v", got)
	}
	if err := store.Delete(context.Background(), "eu-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got, _ = store.List(context.Background())
	if len(got) != 0 {
		t.Errorf("List after delete = %+v, want empty", got)
	}
}

// TestNodeManager_SelectByRegion verifies selection prefers an active node in
// the requested region, then lowest priority.
func TestNodeManager_SelectByRegion(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	store := NewMemoryNodeStore()
	_ = store.Save(context.Background(), ProviderNode{ID: "us", ProviderID: pid, Region: "us", Priority: 1, IsActive: true})
	_ = store.Save(context.Background(), ProviderNode{ID: "eu", ProviderID: pid, Region: "eu", Priority: 0, IsActive: true})
	_ = store.Save(context.Background(), ProviderNode{ID: "off", ProviderID: pid, Region: "eu", Priority: 0, IsActive: false})
	m := NewNodeManager(store)
	n, err := m.SelectNode(context.Background(), pid, "eu")
	if err != nil {
		t.Fatalf("SelectNode: %v", err)
	}
	if n.ID != "eu" {
		t.Errorf("selected %q, want eu (region match + priority)", n.ID)
	}
}

// TestNodeManager_SelectFallbackNoRegion verifies selection falls back to any
// active node when no region matches.
func TestNodeManager_SelectFallbackNoRegion(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	store := NewMemoryNodeStore()
	_ = store.Save(context.Background(), ProviderNode{ID: "us", ProviderID: pid, Region: "us", Priority: 2, IsActive: true})
	_ = store.Save(context.Background(), ProviderNode{ID: "apac", ProviderID: pid, Region: "apac", Priority: 1, IsActive: true})
	m := NewNodeManager(store)
	n, err := m.SelectNode(context.Background(), pid, "eu")
	if err != nil {
		t.Fatalf("SelectNode: %v", err)
	}
	if n.ID != "apac" {
		t.Errorf("selected %q, want apac (lowest priority active)", n.ID)
	}
}

// TestNodeManager_NoNodes verifies selection fails cleanly when the provider
// has no active nodes.
func TestNodeManager_NoNodes(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	m := NewNodeManager(NewMemoryNodeStore())
	_, err := m.SelectNode(context.Background(), pid, "")
	if !errors.Is(err, ErrNoProviderNode) {
		t.Errorf("SelectNode = %v, want ErrNoProviderNode", err)
	}
}

// TestNodeManager_StableOrder verifies equal-priority nodes resolve
// deterministically by name.
func TestNodeManager_StableOrder(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	store := NewMemoryNodeStore()
	_ = store.Save(context.Background(), ProviderNode{ID: "b", ProviderID: pid, Priority: 0, IsActive: true})
	_ = store.Save(context.Background(), ProviderNode{ID: "a", ProviderID: pid, Priority: 0, IsActive: true})
	m := NewNodeManager(store)
	n1, _ := m.SelectNode(context.Background(), pid, "")
	n2, _ := m.SelectNode(context.Background(), pid, "")
	if n1.ID != "a" || n2.ID != "a" {
		t.Errorf("stable order violated: %q then %q", n1.ID, n2.ID)
	}
}

// TestDefaultNodesFromProviders verifies each configured provider produces one
// default node carrying its base URL (config-backed default nodes).
func TestDefaultNodesFromProviders(t *testing.T) {
	t.Parallel()
	p := provider.Provider{
		ID:        uuid.New(),
		Name:      "openai",
		Type:      provider.ProviderOpenAI,
		BaseURL:   "https://api.openai.com/v1",
		Config:    []byte(`{"region":"us"}`),
		IsEnabled: true,
	}
	nodes := DefaultNodesFromProviders([]provider.Provider{p})
	if len(nodes) != 1 {
		t.Fatalf("len = %d, want 1", len(nodes))
	}
	n := nodes[0]
	if n.ProviderID != p.ID {
		t.Errorf("ProviderID = %v", n.ProviderID)
	}
	if n.BaseURL != "https://api.openai.com/v1" {
		t.Errorf("BaseURL = %q", n.BaseURL)
	}
	if n.Region != "us" {
		t.Errorf("Region = %q, want us (config-backed)", n.Region)
	}
	if !n.IsActive {
		t.Error("default node must be active")
	}
	if n.Priority != 0 {
		t.Errorf("Priority = %d, want 0", n.Priority)
	}
}

// TestDefaultNodesFromProviders_DisabledProvider verifies disabled providers
// produce no default nodes.
func TestDefaultNodesFromProviders_DisabledProvider(t *testing.T) {
	t.Parallel()
	p := provider.Provider{ID: uuid.New(), Name: "off", IsEnabled: false}
	nodes := DefaultNodesFromProviders([]provider.Provider{p})
	if len(nodes) != 0 {
		t.Errorf("len = %d, want 0 for disabled provider", len(nodes))
	}
}

// TestNodeManager_MergeDefaultAndRuntime verifies runtime overrides merge over
// config-backed defaults (runtime nodes win on ID collision).
func TestNodeManager_MergeDefaultAndRuntime(t *testing.T) {
	t.Parallel()
	p := provider.Provider{ID: uuid.New(), Name: "openai", BaseURL: "https://default.example.com/v1", IsEnabled: true}
	store := NewMemoryNodeStore()
	_ = store.Save(context.Background(), ProviderNode{
		ID: "default:openai", ProviderID: p.ID, BaseURL: "https://runtime.example.com/v1", IsActive: true,
	})
	merged := MergeDefaultAndRuntime(DefaultNodesFromProviders([]provider.Provider{p}), store)
	if len(merged) != 1 {
		t.Fatalf("len = %d, want 1", len(merged))
	}
	if merged[0].BaseURL != "https://runtime.example.com/v1" {
		t.Errorf("BaseURL = %q, want runtime override", merged[0].BaseURL)
	}
}
