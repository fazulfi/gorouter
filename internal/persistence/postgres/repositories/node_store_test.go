package repositories

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	enginerouting "gorouter/internal/engine/routing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestNodeStore_List(t *testing.T) {
	t.Parallel()

	t.Run("results", func(t *testing.T) {
		provID := uuid.New()
		nodeID := uuid.New()
		metaBytes, _ := json.Marshal(map[string]string{"source": "test"})
		name := "node-a"
		baseURL := "https://api.example.com"
		region := "us-east"

		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return &mockRows{
					rows: [][]interface{}{
						{nodeID, provID, &name, &baseURL, &region, 5, true, metaBytes},
					},
				}, nil
			},
		}
		store := &nodeStore{tx: tx}
		nodes, err := store.List(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(nodes) != 1 {
			t.Fatalf("expected 1, got %d", len(nodes))
		}
		n := nodes[0]
		if n.ID != nodeID.String() {
			t.Errorf("ID = %q, want %q", n.ID, nodeID.String())
		}
		if n.ProviderID != provID {
			t.Error("ProviderID mismatch")
		}
		if n.Name != "node-a" || n.BaseURL != "https://api.example.com" || n.Region != "us-east" {
			t.Error("field mismatch")
		}
		if n.Priority != 5 || !n.IsActive {
			t.Error("priority/active mismatch")
		}
		if n.Metadata["source"] != "test" {
			t.Error("metadata mismatch")
		}
	})

	t.Run("empty", func(t *testing.T) {
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return &mockRows{}, nil
			},
		}
		store := &nodeStore{tx: tx}
		nodes, err := store.List(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(nodes) != 0 {
			t.Fatalf("expected 0, got %d", len(nodes))
		}
	})

	t.Run("query error", func(t *testing.T) {
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return nil, errors.New("query failed")
			},
		}
		store := &nodeStore{tx: tx}
		_, err := store.List(context.Background())
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestNodeStore_Save(t *testing.T) {
	t.Parallel()

	t.Run("success with UUID id", func(t *testing.T) {
		nodeID := uuid.New()
		provID := uuid.New()
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, nil
			},
		}
		store := &nodeStore{tx: tx}
		node := enginerouting.ProviderNode{
			ID:         nodeID.String(),
			ProviderID: provID,
			Name:       "test-node",
			BaseURL:    "https://test.example.com",
			Region:     "eu-west",
			Priority:   10,
			IsActive:   true,
			Metadata:   map[string]string{"type": "primary"},
		}
		if err := store.Save(context.Background(), node); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("success with non-UUID id derives deterministic UUID", func(t *testing.T) {
		provID := uuid.New()
		expectedID := deriveUUID("default:openai")
		var capturedID interface{}
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, args ...interface{}) (pgconn.CommandTag, error) {
				capturedID = args[0]
				return pgconn.CommandTag{}, nil
			},
		}
		store := &nodeStore{tx: tx}
		node := enginerouting.ProviderNode{
			ID:         "default:openai",
			ProviderID: provID,
			Name:       "openai-default",
			IsActive:   true,
			Metadata:   map[string]string{},
		}
		if err := store.Save(context.Background(), node); err != nil {
			t.Fatal(err)
		}
		if capturedID == nil {
			t.Fatal("exec was not called")
		}
		if fmt.Sprint(capturedID) != expectedID.String() {
			t.Errorf("expected deterministic UUID %v, got %v", expectedID, capturedID)
		}
	})

	t.Run("exec error", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("insert failed")
			},
		}
		store := &nodeStore{tx: tx}
		err := store.Save(context.Background(), enginerouting.ProviderNode{
			ID:         uuid.New().String(),
			ProviderID: uuid.New(),
		})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestNodeStore_Delete(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, nil
			},
		}
		store := &nodeStore{tx: tx}
		if err := store.Delete(context.Background(), uuid.New().String()); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("non-uuid id uses deterministic derivation", func(t *testing.T) {
		expectedID := deriveUUID("not-a-uuid")
		var capturedID interface{}
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, args ...interface{}) (pgconn.CommandTag, error) {
				capturedID = args[0]
				return pgconn.CommandTag{}, nil
			},
		}
		store := &nodeStore{tx: tx}
		if err := store.Delete(context.Background(), "not-a-uuid"); err != nil {
			t.Fatal(err)
		}
		if fmt.Sprint(capturedID) != expectedID.String() {
			t.Errorf("expected deterministic UUID %v, got %v", expectedID, capturedID)
		}
	})

	t.Run("exec error", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("delete failed")
			},
		}
		store := &nodeStore{tx: tx}
		err := store.Delete(context.Background(), uuid.New().String())
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestNodeStore_StableIDRoundtrip(t *testing.T) {
	t.Parallel()

	provID := uuid.New()

	t.Run("List restores non-UUID id and strips reserved key", func(t *testing.T) {
		dbUUID := deriveUUID("default:openai")
		name := "openai-default"
		baseURL := "https://api.openai.com"
		region := "us-east"
		metaWithReserved := map[string]string{
			"source":          "provider-test",
			nodeExternalIDKey: "default:openai",
		}
		metaBytes, _ := json.Marshal(metaWithReserved)

		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return &mockRows{
					rows: [][]interface{}{
						{dbUUID, provID, &name, &baseURL, &region, 0, true, metaBytes},
					},
				}, nil
			},
		}
		store := &nodeStore{tx: tx}
		nodes, err := store.List(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(nodes) != 1 {
			t.Fatalf("expected 1, got %d", len(nodes))
		}
		n := nodes[0]
		if n.ID != "default:openai" {
			t.Errorf("expected restored ID %q, got %q", "default:openai", n.ID)
		}
		if n.Metadata["source"] != "provider-test" {
			t.Error("user metadata lost")
		}
		if _, exists := n.Metadata[nodeExternalIDKey]; exists {
			t.Error("reserved key was not stripped from List result")
		}
	})

	t.Run("UUID-only node passes through unchanged", func(t *testing.T) {
		dbUUID := uuid.New()
		name := "uuid-node"
		metaBytes, _ := json.Marshal(map[string]string{"key": "val"})

		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return &mockRows{
					rows: [][]interface{}{
						{dbUUID, provID, &name, nil, nil, 0, true, metaBytes},
					},
				}, nil
			},
		}
		store := &nodeStore{tx: tx}
		nodes, err := store.List(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(nodes) != 1 {
			t.Fatalf("expected 1, got %d", len(nodes))
		}
		if nodes[0].ID != dbUUID.String() {
			t.Errorf("expected UUID %q, got %q", dbUUID.String(), nodes[0].ID)
		}
		if nodes[0].Metadata["key"] != "val" {
			t.Error("metadata lost for UUID node")
		}
	})

	t.Run("repeat Save with same non-UUID produces same derived UUID", func(t *testing.T) {
		expectedID := deriveUUID("repeat:id")
		capturedIDs := make([]interface{}, 0, 2)
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, args ...interface{}) (pgconn.CommandTag, error) {
				capturedIDs = append(capturedIDs, args[0])
				return pgconn.CommandTag{}, nil
			},
		}
		store := &nodeStore{tx: tx}
		node := enginerouting.ProviderNode{
			ID:         "repeat:id",
			ProviderID: provID,
			Name:       "repeat-node",
			IsActive:   true,
		}
		if err := store.Save(context.Background(), node); err != nil {
			t.Fatal(err)
		}
		node.Name = "repeat-node-v2"
		if err := store.Save(context.Background(), node); err != nil {
			t.Fatal(err)
		}
		if len(capturedIDs) != 2 {
			t.Fatalf("expected 2 exec calls, got %d", len(capturedIDs))
		}
		if fmt.Sprint(capturedIDs[0]) != expectedID.String() {
			t.Errorf("first save: expected %v, got %v", expectedID, capturedIDs[0])
		}
		if fmt.Sprint(capturedIDs[1]) != expectedID.String() {
			t.Errorf("second save: expected same UUID %v, got %v", expectedID, capturedIDs[1])
		}
	})

	t.Run("Delete by non-UUID derives same UUID as Save", func(t *testing.T) {
		expectedID := deriveUUID("save-delete:id")
		savedIDs := []interface{}{}
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, args ...interface{}) (pgconn.CommandTag, error) {
				savedIDs = append(savedIDs, args[0])
				return pgconn.CommandTag{}, nil
			},
		}
		store := &nodeStore{tx: tx}
		if err := store.Save(context.Background(), enginerouting.ProviderNode{
			ID: "save-delete:id", ProviderID: provID, Name: "sd", IsActive: true,
		}); err != nil {
			t.Fatal(err)
		}
		if err := store.Delete(context.Background(), "save-delete:id"); err != nil {
			t.Fatal(err)
		}
		if len(savedIDs) != 2 {
			t.Fatalf("expected 2 exec calls, got %d", len(savedIDs))
		}
		if fmt.Sprint(savedIDs[0]) != expectedID.String() {
			t.Errorf("Save used %v, Delete used %v", savedIDs[0], savedIDs[1])
		}
		if fmt.Sprint(savedIDs[0]) != fmt.Sprint(savedIDs[1]) {
			t.Error("Save and Delete used different UUIDs for same string ID")
		}
	})

	t.Run("metadata collision: user key same as reserved is preserved in DB but stripped on List", func(t *testing.T) {
		dbUUID := deriveUUID("collision-test")
		name := "collision-node"
		metaWithCollision := map[string]string{
			nodeExternalIDKey: "collision-test",
			"source":          "user",
		}
		metaBytes, _ := json.Marshal(metaWithCollision)

		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return &mockRows{
					rows: [][]interface{}{
						{dbUUID, provID, &name, nil, nil, 0, true, metaBytes},
					},
				}, nil
			},
		}
		store := &nodeStore{tx: tx}
		nodes, err := store.List(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(nodes) != 1 {
			t.Fatalf("expected 1, got %d", len(nodes))
		}
		if nodes[0].ID != "collision-test" {
			t.Errorf("expected restored ID %q, got %q", "collision-test", nodes[0].ID)
		}
		if _, exists := nodes[0].Metadata[nodeExternalIDKey]; exists {
			t.Error("reserved key was not stripped, even with user collision")
		}
		if nodes[0].Metadata["source"] != "user" {
			t.Error("user metadata key lost alongside reserved key")
		}
	})
}
