package providers

import (
	"context"
	"encoding/json"
	"testing"

	"gorouter/internal/app/tx"
	"gorouter/internal/domain/auth"
	"gorouter/internal/domain/provider"

	"github.com/google/uuid"
)

func TestPoolService(t *testing.T) {
	ctx := context.Background()
	actor := &auth.Actor{UserID: uuid.New(), IsAdmin: true}
	memberID := uuid.New()

	t.Run("list", func(t *testing.T) {
		b := newFakeBeginner()
		svc := NewPoolService(b, testLogger())
		mustBegin(t, b)
		b.pools.Create(ctx, &provider.ProxyPool{ID: uuid.New(), Name: "pool-a"})

		pools, err := svc.List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(pools) != 1 || pools[0].Name != "pool-a" {
			t.Fatalf("unexpected pools: %+v", pools)
		}
		if b.rollbacks.Load() != 1 {
			t.Errorf("read path must roll back, rollbacks=%d", b.rollbacks.Load())
		}
	})

	t.Run("create audits and assigns identity", func(t *testing.T) {
		b := newFakeBeginner()
		svc := NewPoolService(b, testLogger())

		got, err := svc.Create(ctx, actor, &provider.ProxyPool{Name: "pool-1"})
		if err != nil {
			t.Fatal(err)
		}
		if got.ID == uuid.Nil {
			t.Error("create must assign an ID")
		}
		if got.Name != "pool-1" {
			t.Errorf("name = %q", got.Name)
		}
		if b.commits.Load() != 1 {
			t.Errorf("mutation must commit, commits=%d", b.commits.Load())
		}
		entries := b.audit.all()
		if len(entries) != 1 {
			t.Fatalf("expected 1 audit entry, got %d", len(entries))
		}
		assertAuditEntry(t, entries[0], actor, "pool.create", "proxy_pool", &got.ID)
		details := decodeDetails(t, entries[0])
		if details["name"] != "pool-1" {
			t.Errorf("details name = %v", details["name"])
		}
	})

	t.Run("update audits sanitized before and after", func(t *testing.T) {
		b := newFakeBeginner()
		svc := NewPoolService(b, testLogger())
		mustBegin(t, b)
		b.pools.Create(ctx, &provider.ProxyPool{ID: uuid.MustParse("11111111-1111-1111-1111-111111111111"), Name: "old"})

		got, err := svc.Update(ctx, actor, &provider.ProxyPool{
			ID:   uuid.MustParse("11111111-1111-1111-1111-111111111111"),
			Name: "new",
		})
		if err != nil {
			t.Fatal(err)
		}
		if got.Name != "new" {
			t.Errorf("name = %q", got.Name)
		}
		entries := b.audit.all()
		if len(entries) != 1 {
			t.Fatalf("expected 1 audit entry, got %d", len(entries))
		}
		assertAuditEntry(t, entries[0], actor, "pool.update", "proxy_pool", &got.ID)
		details := decodeDetails(t, entries[0])
		before, ok := details["before"].(map[string]any)
		if !ok {
			t.Fatalf("details.before missing: %v", details)
		}
		after, ok := details["after"].(map[string]any)
		if !ok {
			t.Fatalf("details.after missing: %v", details)
		}
		if before["name"] != "old" || after["name"] != "new" {
			t.Errorf("before/after diff wrong: %v -> %v", before, after)
		}
	})

	t.Run("delete audits", func(t *testing.T) {
		b := newFakeBeginner()
		svc := NewPoolService(b, testLogger())
		mustBegin(t, b)
		id := uuid.New()
		b.pools.Create(ctx, &provider.ProxyPool{ID: id, Name: "pool-x"})

		if err := svc.Delete(ctx, actor, id); err != nil {
			t.Fatal(err)
		}
		entries := b.audit.all()
		if len(entries) != 1 {
			t.Fatalf("expected 1 audit entry, got %d", len(entries))
		}
		assertAuditEntry(t, entries[0], actor, "pool.delete", "proxy_pool", &id)
	})

	t.Run("get members", func(t *testing.T) {
		b := newFakeBeginner()
		svc := NewPoolService(b, testLogger())
		mustBegin(t, b)
		poolID := uuid.New()
		b.pools.SetMembers(ctx, poolID, []provider.PoolMember{{PoolID: poolID, ProxyConfigID: memberID, Position: 0}})

		members, err := svc.GetMembers(ctx, poolID)
		if err != nil {
			t.Fatal(err)
		}
		if len(members) != 1 || members[0].ProxyConfigID != memberID {
			t.Fatalf("unexpected members: %+v", members)
		}
	})

	t.Run("set members audits", func(t *testing.T) {
		b := newFakeBeginner()
		svc := NewPoolService(b, testLogger())
		mustBegin(t, b)
		poolID := uuid.New()
		members := []provider.PoolMember{{PoolID: poolID, ProxyConfigID: memberID, Position: 1}}

		if err := svc.SetMembers(ctx, actor, poolID, members); err != nil {
			t.Fatal(err)
		}
		entries := b.audit.all()
		if len(entries) != 1 {
			t.Fatalf("expected 1 audit entry, got %d", len(entries))
		}
		assertAuditEntry(t, entries[0], actor, "pool.set_members", "proxy_pool", &poolID)
	})

	t.Run("mutation without actor fails", func(t *testing.T) {
		b := newFakeBeginner()
		svc := NewPoolService(b, testLogger())
		if _, err := svc.Create(ctx, nil, &provider.ProxyPool{Name: "p"}); err == nil {
			t.Error("nil actor must be rejected")
		}
		if err := svc.SetMembers(ctx, nil, uuid.New(), nil); err == nil {
			t.Error("nil actor must be rejected")
		}
	})

	t.Run("audit failure fails the mutation", func(t *testing.T) {
		b := newFakeBeginner()
		b.audit.err = errAuditWrite
		svc := NewPoolService(b, testLogger())
		if _, err := svc.Create(ctx, actor, &provider.ProxyPool{Name: "p"}); err == nil {
			t.Error("audit failure must fail the mutation")
		}
	})

	t.Run("caller pool input is preserved", func(t *testing.T) {
		b := newFakeBeginner()
		svc := NewPoolService(b, testLogger())
		pool := &provider.ProxyPool{Name: "kept"}
		before := *pool
		if _, err := svc.Create(ctx, actor, pool); err != nil {
			t.Fatal(err)
		}
		if *pool != before {
			t.Errorf("caller input mutated: %+v", pool)
		}
	})

	t.Run("repo error propagates", func(t *testing.T) {
		b := newFakeBeginner()
		svc := NewPoolService(b, testLogger())
		mustBegin(t, b)
		b.pools.err = errRepo
		if _, err := svc.List(ctx); err == nil {
			t.Error("repo error must propagate")
		}
	})
}

func assertAuditEntry(t *testing.T, entry *tx.AuditLogEntry, actor *auth.Actor, action, resourceType string, resourceID *uuid.UUID) {
	t.Helper()
	if entry.ActorID == nil || *entry.ActorID != actor.UserID {
		t.Errorf("actor id = %v, want %v", entry.ActorID, actor.UserID)
	}
	if entry.Action != action {
		t.Errorf("action = %q, want %q", entry.Action, action)
	}
	if entry.ResourceType != resourceType {
		t.Errorf("resource type = %q, want %q", entry.ResourceType, resourceType)
	}
	if entry.ResourceID == nil || *entry.ResourceID != *resourceID {
		t.Errorf("resource id = %v, want %v", entry.ResourceID, *resourceID)
	}
	if entry.OccurredAt.IsZero() {
		t.Error("occurred at must be set")
	}
}

func decodeDetails(t *testing.T, entry *tx.AuditLogEntry) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(entry.Details, &m); err != nil {
		t.Fatalf("details not JSON: %v", err)
	}
	return m
}

func mustBegin(t *testing.T, b *fakeBeginner) *fakeScope {
	t.Helper()
	scope, err := b.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return scope.(*fakeScope)
}
