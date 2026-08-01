package routing

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"

	"github.com/google/uuid"

	"gorouter/internal/domain/provider"
)

// ErrNoProviderNode is returned when a provider has no routable node.
var ErrNoProviderNode = errors.New("routing: no provider node available")

// ProviderNode is a routable provider endpoint. The schema defines no
// provider-node table (000004_engine_matrix), so nodes are runtime/config
// backed: MemoryNodeStore for process-local state and provider config for
// defaults. No migration table is invented.
type ProviderNode struct {
	ID         string
	ProviderID uuid.UUID
	Name       string
	BaseURL    string
	Region     string
	Priority   int
	IsActive   bool
	Metadata   map[string]string
}

// NodeStore is the persistence boundary for provider nodes.
type NodeStore interface {
	List(ctx context.Context) ([]ProviderNode, error)
	Save(ctx context.Context, node ProviderNode) error
	Delete(ctx context.Context, id string) error
}

// MemoryNodeStore is a process-local, mutex-guarded node store.
type MemoryNodeStore struct {
	mu    sync.RWMutex
	nodes map[string]ProviderNode
}

// NewMemoryNodeStore creates an empty MemoryNodeStore.
func NewMemoryNodeStore() *MemoryNodeStore {
	return &MemoryNodeStore{nodes: make(map[string]ProviderNode)}
}

// List returns all nodes in stable ID order.
func (s *MemoryNodeStore) List(_ context.Context) ([]ProviderNode, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ProviderNode, 0, len(s.nodes))
	for _, n := range s.nodes {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Save upserts a node by ID.
func (s *MemoryNodeStore) Save(_ context.Context, node ProviderNode) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nodes[node.ID] = node
	return nil
}

// Delete removes a node by ID.
func (s *MemoryNodeStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.nodes, id)
	return nil
}

// NodeManager selects routable nodes for a provider.
type NodeManager struct {
	store NodeStore
}

// NewNodeManager creates a NodeManager over the given store.
func NewNodeManager(store NodeStore) *NodeManager {
	return &NodeManager{store: store}
}

// List returns all managed nodes.
func (m *NodeManager) List(ctx context.Context) ([]ProviderNode, error) {
	if m.store == nil {
		return nil, nil
	}
	return m.store.List(ctx)
}

// Save upserts a node.
func (m *NodeManager) Save(ctx context.Context, node ProviderNode) error {
	if m.store == nil {
		return ErrNoProviderNode
	}
	return m.store.Save(ctx, node)
}

// Delete removes a node.
func (m *NodeManager) Delete(ctx context.Context, id string) error {
	if m.store == nil {
		return ErrNoProviderNode
	}
	return m.store.Delete(ctx, id)
}

// SelectNode picks the best active node for a provider: a node in the
// preferred region wins; otherwise any active node. Ties break by lowest
// priority then name (deterministic).
func (m *NodeManager) SelectNode(ctx context.Context, providerID uuid.UUID, preferRegion string) (*ProviderNode, error) {
	if m.store == nil {
		return nil, ErrNoProviderNode
	}
	nodes, err := m.store.List(ctx)
	if err != nil {
		return nil, err
	}
	var inRegion, any []ProviderNode
	for _, n := range nodes {
		if n.ProviderID != providerID || !n.IsActive {
			continue
		}
		any = append(any, n)
		if n.Region == preferRegion {
			inRegion = append(inRegion, n)
		}
	}
	best := any
	if len(inRegion) > 0 {
		best = inRegion
	}
	if len(best) == 0 {
		return nil, ErrNoProviderNode
	}
	sort.Slice(best, func(i, j int) bool {
		if best[i].Priority != best[j].Priority {
			return best[i].Priority < best[j].Priority
		}
		return best[i].Name < best[j].Name
	})
	n := best[0]
	return &n, nil
}

// DefaultNodesFromProviders derives one default node per enabled provider,
// carrying the provider BaseURL and an optional "region" key from the
// provider config JSON (config-backed defaults).
func DefaultNodesFromProviders(providers []provider.Provider) []ProviderNode {
	var nodes []ProviderNode
	for i := range providers {
		p := providers[i]
		if !p.IsEnabled {
			continue
		}
		nodes = append(nodes, ProviderNode{
			ID:         "default:" + p.Name,
			ProviderID: p.ID,
			Name:       p.Name,
			BaseURL:    p.BaseURL,
			Region:     regionFromConfig(p.Config),
			Priority:   0,
			IsActive:   true,
			Metadata:   map[string]string{"source": "provider"},
		})
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	return nodes
}

// MergeDefaultAndRuntime overlays runtime nodes over config-backed defaults;
// a runtime node with the same ID wins.
func MergeDefaultAndRuntime(defaults []ProviderNode, store NodeStore) []ProviderNode {
	merged := make([]ProviderNode, len(defaults))
	copy(merged, defaults)
	if store == nil {
		return merged
	}
	runtime, err := store.List(context.Background())
	if err != nil {
		return merged
	}
	byID := make(map[string]int, len(merged))
	for i := range merged {
		byID[merged[i].ID] = i
	}
	for _, n := range runtime {
		if idx, ok := byID[n.ID]; ok {
			merged[idx] = n
		} else {
			merged = append(merged, n)
		}
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].ID < merged[j].ID })
	return merged
}

// regionFromConfig extracts the "region" key from a provider config JSONB.
// Invalid or absent config yields an empty region.
func regionFromConfig(config json.RawMessage) string {
	if len(config) == 0 {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(config, &m); err != nil {
		return ""
	}
	if v, ok := m["region"].(string); ok {
		return v
	}
	return ""
}
