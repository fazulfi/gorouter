package repositories

import (
	"context"
	"encoding/json"
	"fmt"

	enginerouting "gorouter/internal/engine/routing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var nodeStoreNamespace = uuid.Must(uuid.Parse("6ba7b810-9dad-11d1-80b4-00c04fd430c8"))

func deriveUUID(id string) uuid.UUID {
	if u, err := uuid.Parse(id); err == nil {
		return u
	}
	return uuid.NewSHA1(nodeStoreNamespace, []byte(id))
}

const nodeExternalIDKey = "__gorouter_node_id"

type nodeStore struct {
	tx pgx.Tx
}

// Compile-time interface assertion.
var _ enginerouting.NodeStore = (*nodeStore)(nil)

func (s *nodeStore) List(ctx context.Context) ([]enginerouting.ProviderNode, error) {
	rows, err := s.tx.Query(ctx,
		`SELECT id, provider_id, name, base_url, region, priority, is_active, metadata
		 FROM gorouter_provider_nodes ORDER BY priority, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []enginerouting.ProviderNode
	for rows.Next() {
		var (
			id         uuid.UUID
			providerID uuid.UUID
			name       *string
			baseURL    *string
			region     *string
			priority   int
			isActive   bool
			metaBytes  []byte
		)
		if err := rows.Scan(&id, &providerID, &name, &baseURL, &region,
			&priority, &isActive, &metaBytes); err != nil {
			return nil, err
		}
		meta := make(map[string]string)
		if len(metaBytes) > 0 {
			_ = json.Unmarshal(metaBytes, &meta)
		}
		nodeID := id.String()
		if extID, ok := meta[nodeExternalIDKey]; ok && extID != "" {
			if deriveUUID(extID) != id {
				continue // corrupt row: reserved key does not derive to stored UUID
			}
			nodeID = extID
			delete(meta, nodeExternalIDKey)
		}
		n := enginerouting.ProviderNode{
			ID:         nodeID,
			ProviderID: providerID,
			Priority:   priority,
			IsActive:   isActive,
			Metadata:   meta,
		}
		if name != nil {
			n.Name = *name
		}
		if baseURL != nil {
			n.BaseURL = *baseURL
		}
		if region != nil {
			n.Region = *region
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *nodeStore) Save(ctx context.Context, node enginerouting.ProviderNode) error {
	nodeID := deriveUUID(node.ID)
	meta := node.Metadata
	if meta == nil {
		meta = make(map[string]string)
	}
	// Always strip the reserved key from caller metadata to prevent identity spoof.
	// For UUID-native IDs the stripped key stays absent. For non-UUID IDs the
	// store then re-injects the original string so List can restore it.
	meta = copyMetaWithoutReserved(meta)
	if _, err := uuid.Parse(node.ID); err != nil {
		meta[nodeExternalIDKey] = node.ID
	}
	metaBytes, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	_, err = s.tx.Exec(ctx,
		`INSERT INTO gorouter_provider_nodes (id, provider_id, name, base_url, region, priority, is_active, metadata, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
		 ON CONFLICT (id) DO UPDATE SET
		   provider_id = EXCLUDED.provider_id,
		   name        = EXCLUDED.name,
		   base_url    = EXCLUDED.base_url,
		   region      = EXCLUDED.region,
		   priority    = EXCLUDED.priority,
		   is_active   = EXCLUDED.is_active,
		   metadata    = EXCLUDED.metadata,
		   updated_at  = NOW()`,
		nodeID, node.ProviderID, node.Name, node.BaseURL, node.Region,
		node.Priority, node.IsActive, metaBytes)
	return err
}

func copyMetaWithoutReserved(src map[string]string) map[string]string {
	dst := make(map[string]string, len(src))
	for k, v := range src {
		if k != nodeExternalIDKey {
			dst[k] = v
		}
	}
	return dst
}

func (s *nodeStore) Delete(ctx context.Context, id string) error {
	nodeID := deriveUUID(id)
	_, err := s.tx.Exec(ctx, `DELETE FROM gorouter_provider_nodes WHERE id = $1`, nodeID)
	return err
}
