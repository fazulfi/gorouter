package providers

import (
	"context"
	"testing"

	"gorouter/internal/domain/auth"
	enginerouting "gorouter/internal/engine/routing"

	"github.com/google/uuid"
)

func TestNodeService(t *testing.T) {
	ctx := context.Background()
	actor := &auth.Actor{UserID: uuid.New()}
	node := enginerouting.ProviderNode{
		ID:         uuid.New().String(),
		ProviderID: uuid.New(),
		Name:       "node-a",
		BaseURL:    "https://api.example.com",
		Region:     "us-east",
		Priority:   3,
		IsActive:   true,
	}

	t.Run("list delegates to node store", func(t *testing.T) {
		b := newFakeBeginner()
		svc := NewNodeService(b, testLogger())
		mustBegin(t, b)
		b.nodes.Save(ctx, node)

		nodes, err := svc.List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(nodes) != 1 || nodes[0].ID != node.ID {
			t.Fatalf("unexpected nodes: %+v", nodes)
		}
		if b.rollbacks.Load() != 1 {
			t.Errorf("read path must roll back, rollbacks=%d", b.rollbacks.Load())
		}
	})

	t.Run("save delegates and audits", func(t *testing.T) {
		b := newFakeBeginner()
		svc := NewNodeService(b, testLogger())
		mustBegin(t, b)

		if err := svc.Save(ctx, actor, node); err != nil {
			t.Fatal(err)
		}
		stored := b.nodes.nodes[node.ID]
		if stored.Name != "node-a" || stored.BaseURL != "https://api.example.com" {
			t.Errorf("node not stored: %+v", stored)
		}
		entries := b.audit.all()
		if len(entries) != 1 {
			t.Fatalf("expected 1 audit entry, got %d", len(entries))
		}
		nodeID := uuid.MustParse(node.ID)
		assertAuditEntry(t, entries[0], actor, "node.save", "provider_node", &nodeID)
		details := decodeDetails(t, entries[0])
		if details["name"] != "node-a" || details["base_url"] != "https://api.example.com" {
			t.Errorf("details missing node fields: %v", details)
		}
	})

	t.Run("delete delegates and audits", func(t *testing.T) {
		b := newFakeBeginner()
		svc := NewNodeService(b, testLogger())
		mustBegin(t, b)
		b.nodes.Save(ctx, node)

		if err := svc.Delete(ctx, actor, node.ID); err != nil {
			t.Fatal(err)
		}
		if _, ok := b.nodes.nodes[node.ID]; ok {
			t.Error("node still present after delete")
		}
		entries := b.audit.all()
		if len(entries) != 1 {
			t.Fatalf("expected 1 audit entry, got %d", len(entries))
		}
		nodeID := uuid.MustParse(node.ID)
		assertAuditEntry(t, entries[0], actor, "node.delete", "provider_node", &nodeID)
	})

	t.Run("mutation without actor fails", func(t *testing.T) {
		b := newFakeBeginner()
		svc := NewNodeService(b, testLogger())
		if err := svc.Save(ctx, nil, node); err == nil {
			t.Error("nil actor must be rejected")
		}
		if err := svc.Delete(ctx, nil, node.ID); err == nil {
			t.Error("nil actor must be rejected")
		}
	})

	t.Run("store error propagates", func(t *testing.T) {
		b := newFakeBeginner()
		svc := NewNodeService(b, testLogger())
		mustBegin(t, b)
		b.nodes.err = errRepo
		if _, err := svc.List(ctx); err == nil {
			t.Error("store error must propagate")
		}
	})
}
