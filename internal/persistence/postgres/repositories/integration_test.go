package repositories

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"gorouter/internal/domain/provider"
	enginerouting "gorouter/internal/engine/routing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// repoTestDBPrefix is the dedicated test database used by the repositories
// integration tests. A dedicated DB keeps the migrations package test
// binary's shared public schema fully isolated from the repositories
// binary's schema lifecycle, eliminating the deterministic cross-binary
// race that a same-DB / same-schema setup inevitably incurs.
const repoTestDBPrefix = "gorouter_repo_iso_"

func setupRepoTestDB(t *testing.T, ctx context.Context) string {
	t.Helper()
	adminDSN := os.Getenv("DATABASE_URL")
	if adminDSN == "" {
		t.Skip("DATABASE_URL not set")
	}
	conn, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		t.Fatalf("admin connect: %v", err)
	}
	defer conn.Close(ctx)
	dbName := repoTestDBPrefix + uuid.NewString()[:8]
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+dbName); err != nil {
		t.Fatalf("create test db %s: %v", dbName, err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cleanupConn, err := pgx.Connect(cleanupCtx, adminDSN)
		if err != nil {
			return
		}
		_, _ = cleanupConn.Exec(cleanupCtx,
			"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()",
			dbName)
		_, _ = cleanupConn.Exec(cleanupCtx, "DROP DATABASE IF EXISTS "+dbName)
		cleanupConn.Close(cleanupCtx)
	})
	return dbName
}

func isolatedTestDSN(t *testing.T, dbName string) string {
	t.Helper()
	base := os.Getenv("DATABASE_URL")
	slash := strings.LastIndex(base, "/")
	if slash == -1 {
		t.Fatalf("DATABASE_URL missing /dbname: %q", base)
	}
	return base[:slash+1] + dbName + "?sslmode=disable"
}

func getTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL not set")
	}

	ctx := context.Background()
	adminDSN := os.Getenv("DATABASE_URL")

	bootstrapRolesForRepoTest(t, ctx, adminDSN)

	dbName := setupRepoTestDB(t, ctx)
	dsn := isolatedTestDSN(t, dbName)

	grantSchemaCreateToDDLRepoTest(t, ctx, dsn)
	return migrateAsDDLRepoTest(t, ctx, dsn)
}

// ─────────────────────────────────────────────────────────────
// ProxyPoolRepo real-PG integration tests
// ─────────────────────────────────────────────────────────────

func TestProxyPoolRepo_CRUD_Integration(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	// Ensure tables exist (migrations should have been applied)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)

	repo := &proxyPoolRepo{tx: tx}

	pool1 := &provider.ProxyPool{
		ID:          uuid.New(),
		Name:        "test-pool-crud",
		Description: "CRUD test pool",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}

	t.Run("Create", func(t *testing.T) {
		if err := repo.Create(ctx, pool1); err != nil {
			t.Fatalf("create: %v", err)
		}
	})

	t.Run("List after Create", func(t *testing.T) {
		pools, err := repo.List(ctx)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		found := false
		for _, p := range pools {
			if p.ID == pool1.ID {
				found = true
				if p.Name != pool1.Name {
					t.Errorf("name = %q, want %q", p.Name, pool1.Name)
				}
			}
		}
		if !found {
			t.Error("created pool not found in list")
		}
	})

	t.Run("Update", func(t *testing.T) {
		pool1.Name = "test-pool-crud-updated"
		pool1.Description = "Updated description"
		pool1.UpdatedAt = time.Now().UTC()
		if err := repo.Update(ctx, pool1); err != nil {
			t.Fatalf("update: %v", err)
		}
	})

	t.Run("Delete", func(t *testing.T) {
		if err := repo.Delete(ctx, pool1.ID); err != nil {
			t.Fatalf("delete: %v", err)
		}
		pools, err := repo.List(ctx)
		if err != nil {
			t.Fatalf("list after delete: %v", err)
		}
		for _, p := range pools {
			if p.ID == pool1.ID {
				t.Error("deleted pool still found in list")
			}
		}
	})
}

func TestProxyPoolRepo_SetMembers_Integration(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)

	// Create a pool first.
	poolID := uuid.New()
	_, err = tx.Exec(ctx,
		`INSERT INTO gorouter_proxy_pools (id, name) VALUES ($1, $2)`, poolID, "set-members-test")
	if err != nil {
		t.Fatalf("insert pool: %v", err)
	}

	// Create two proxy configs.
	pc1 := uuid.New()
	pc2 := uuid.New()
	// Insert dummy provider + account first to satisfy FKs.
	provID := uuid.New()
	_, err = tx.Exec(ctx,
		`INSERT INTO gorouter_providers (id, name, type, base_url, is_enabled) VALUES ($1, $2, 'openai', 'https://sm.example.com', true)`,
		provID, "sm-prov")
	if err != nil {
		t.Fatalf("insert provider: %v", err)
	}
	acctID := uuid.New()
	_, err = tx.Exec(ctx,
		`INSERT INTO gorouter_provider_accounts (id, provider_id, label, auth_type, credential_ref) VALUES ($1, $2, $3, 'api_key', 'sm-cred')`,
		acctID, provID, "sm-acct")
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	for _, pcID := range []uuid.UUID{pc1, pc2} {
		_, err = tx.Exec(ctx,
			`INSERT INTO gorouter_proxy_configs (id, account_id, url) VALUES ($1, $2, $3)`,
			pcID, acctID, "https://example.com")
		if err != nil {
			t.Fatalf("insert proxy_config %s: %v", pcID, err)
		}
	}

	repo := &proxyPoolRepo{tx: tx}

	t.Run("SetMembers with two members preserves order", func(t *testing.T) {
		members := []provider.PoolMember{
			{PoolID: poolID, ProxyConfigID: pc1, Position: 0},
			{PoolID: poolID, ProxyConfigID: pc2, Position: 1},
		}
		if err := repo.SetMembers(ctx, poolID, members); err != nil {
			t.Fatalf("SetMembers: %v", err)
		}
		got, err := repo.Members(ctx, poolID)
		if err != nil {
			t.Fatalf("Members: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("expected 2 members, got %d", len(got))
		}
		if got[0].ProxyConfigID != pc1 || got[0].Position != 0 {
			t.Error("member 0 mismatch")
		}
		if got[1].ProxyConfigID != pc2 || got[1].Position != 1 {
			t.Error("member 1 mismatch")
		}
	})

	t.Run("SetMembers replacement replaces existing members", func(t *testing.T) {
		pc3 := uuid.New()
		_, err := tx.Exec(ctx,
			`INSERT INTO gorouter_proxy_configs (id, account_id, url) VALUES ($1, $2, $3)`,
			pc3, acctID, "https://example3.com")
		if err != nil {
			t.Fatalf("insert pc3: %v", err)
		}
		members := []provider.PoolMember{
			{PoolID: poolID, ProxyConfigID: pc3, Position: 0},
		}
		if err := repo.SetMembers(ctx, poolID, members); err != nil {
			t.Fatalf("SetMembers replacement: %v", err)
		}
		got, err := repo.Members(ctx, poolID)
		if err != nil {
			t.Fatalf("Members after replacement: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("expected 1 member after replacement, got %d", len(got))
		}
		if got[0].ProxyConfigID != pc3 {
			t.Error("replacement member mismatch")
		}
		// Clean up: set back to pc1,pc2 for next subtest
		_ = repo.SetMembers(ctx, poolID, []provider.PoolMember{
			{PoolID: poolID, ProxyConfigID: pc1, Position: 0},
			{PoolID: poolID, ProxyConfigID: pc2, Position: 1},
		})
	})

	t.Run("SetMembers with empty input clears members", func(t *testing.T) {
		if err := repo.SetMembers(ctx, poolID, nil); err != nil {
			t.Fatalf("SetMembers nil: %v", err)
		}
		got, err := repo.Members(ctx, poolID)
		if err != nil {
			t.Fatalf("Members after clear: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("expected 0 members after clear, got %d", len(got))
		}
		// Restore members for remaining subtests.
		_ = repo.SetMembers(ctx, poolID, []provider.PoolMember{
			{PoolID: poolID, ProxyConfigID: pc1, Position: 0},
			{PoolID: poolID, ProxyConfigID: pc2, Position: 1},
		})
	})

	t.Run("SetMembers FK violation preserves old members (atomic CTE)", func(t *testing.T) {
		badPC := uuid.New()
		members := []provider.PoolMember{
			{PoolID: poolID, ProxyConfigID: badPC, Position: 0},
		}
		// Use savepoint: the FK-violating SetMembers will abort the subtransaction,
		// but the outer transaction must stay intact so we can verify old members.
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatalf("savepoint: %v", err)
		}
		spRepo := &proxyPoolRepo{tx: sp}
		err = spRepo.SetMembers(ctx, poolID, members)
		if err == nil {
			sp.Commit(ctx)
			t.Fatal("expected FK violation error, got nil")
		}
		sp.Rollback(ctx)
		t.Logf("FK violation correctly returned error: %v", err)

		// Old members should still be present in the outer transaction.
		got, err := repo.Members(ctx, poolID)
		if err != nil {
			t.Fatalf("Members after failed SetMembers: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("expected old members preserved, got %d", len(got))
		}
	})

	t.Run("SetMembers does not mutate caller input", func(t *testing.T) {
		if err := repo.SetMembers(ctx, poolID, nil); err != nil {
			t.Fatalf("clear members: %v", err)
		}
		pcID := pc1
		members := []provider.PoolMember{
			{ProxyConfigID: pcID, Position: 0},
		}
		if err := repo.SetMembers(ctx, poolID, members); err != nil {
			t.Fatalf("SetMembers: %v", err)
		}
		if members[0].PoolID != uuid.Nil {
			t.Error("SetMembers mutated caller's PoolMember.PoolID")
		}
	})
}

// ─────────────────────────────────────────────────────────────
// NodeStore real-PG integration tests
// ─────────────────────────────────────────────────────────────

func TestNodeStore_SaveListDelete_Integration(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)

	provID := uuid.New()
	_, err = tx.Exec(ctx,
		`INSERT INTO gorouter_providers (id, name, type, base_url, is_enabled) VALUES ($1, $2, 'openai', 'https://ns.example.com', true)`, provID, "ns-prov")
	if err != nil {
		t.Fatalf("insert provider: %v", err)
	}

	store := &nodeStore{tx: tx}

	t.Run("Save and List UUID node", func(t *testing.T) {
		nodeID := uuid.New()
		node := enginerouting.ProviderNode{
			ID:         nodeID.String(),
			ProviderID: provID,
			Name:       "uuid-node",
			BaseURL:    "https://uuid.example.com",
			Region:     "us-west",
			Priority:   5,
			IsActive:   true,
			Metadata:   map[string]string{"source": "integration-test"},
		}
		if err := store.Save(ctx, node); err != nil {
			t.Fatalf("Save: %v", err)
		}

		nodes, err := store.List(ctx)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		found := false
		for _, n := range nodes {
			if n.ID == nodeID.String() {
				found = true
				if n.Name != "uuid-node" {
					t.Errorf("name = %q, want uuid-node", n.Name)
				}
				if n.Metadata["source"] != "integration-test" {
					t.Errorf("metadata missing: %v", n.Metadata)
				}
				if _, exists := n.Metadata[nodeExternalIDKey]; exists {
					t.Error("reserved key leaked in List for UUID node")
				}
			}
		}
		if !found {
			t.Error("saved UUID node not found in List")
		}
	})

	t.Run("Save and List non-UUID node restores original ID", func(t *testing.T) {
		node := enginerouting.ProviderNode{
			ID:         "default:openai",
			ProviderID: provID,
			Name:       "openai-default-node",
			BaseURL:    "https://api.openai.com",
			Region:     "us-east",
			Priority:   10,
			IsActive:   true,
			Metadata:   map[string]string{"type": "primary"},
		}
		if err := store.Save(ctx, node); err != nil {
			t.Fatalf("Save non-UUID: %v", err)
		}

		nodes, err := store.List(ctx)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		found := false
		for _, n := range nodes {
			if n.ID == "default:openai" {
				found = true
				if n.Metadata["type"] != "primary" {
					t.Error("user metadata lost for non-UUID node")
				}
				if _, exists := n.Metadata[nodeExternalIDKey]; exists {
					t.Error("reserved key not stripped from non-UUID node")
				}
			}
		}
		if !found {
			t.Errorf("non-UUID node not found in List; all IDs: %v", nodeIDs(nodes))
		}
	})

	t.Run("Repeat Save with same non-UUID is idempotent", func(t *testing.T) {
		node := enginerouting.ProviderNode{
			ID:         "repeat:save",
			ProviderID: provID,
			Name:       "repeat-v1",
			IsActive:   true,
		}
		if err := store.Save(ctx, node); err != nil {
			t.Fatalf("first Save: %v", err)
		}
		node.Name = "repeat-v2"
		if err := store.Save(ctx, node); err != nil {
			t.Fatalf("second Save: %v", err)
		}
		nodes, err := store.List(ctx)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		count := 0
		for _, n := range nodes {
			if n.ID == "repeat:save" {
				count++
				if n.Name != "repeat-v2" {
					t.Errorf("expected updated name, got %q", n.Name)
				}
			}
		}
		if count != 1 {
			t.Errorf("expected 1 node with ID repeat:save, found %d", count)
		}
	})

	t.Run("Delete by non-UUID removes the node", func(t *testing.T) {
		if err := store.Delete(ctx, "default:openai"); err != nil {
			t.Fatalf("Delete non-UUID: %v", err)
		}
		nodes, err := store.List(ctx)
		if err != nil {
			t.Fatalf("List after Delete: %v", err)
		}
		for _, n := range nodes {
			if n.ID == "default:openai" {
				t.Error("deleted node still present in List")
			}
		}
	})

	t.Run("Delete by UUID removes the node", func(t *testing.T) {
		// First save a new UUID node to delete
		nodeID := uuid.New()
		node := enginerouting.ProviderNode{
			ID: nodeID.String(), ProviderID: provID, Name: "to-delete-uuid", IsActive: true,
		}
		if err := store.Save(ctx, node); err != nil {
			t.Fatalf("Save: %v", err)
		}
		if err := store.Delete(ctx, nodeID.String()); err != nil {
			t.Fatalf("Delete UUID: %v", err)
		}
		nodes, err := store.List(ctx)
		if err != nil {
			t.Fatalf("List after UUID delete: %v", err)
		}
		for _, n := range nodes {
			if n.ID == nodeID.String() {
				t.Error("deleted UUID node still present in List")
			}
		}
	})
}

func TestNodeStore_IdentitySpoofPrevention_Integration(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)

	provID := uuid.New()
	_, err = tx.Exec(ctx,
		`INSERT INTO gorouter_providers (id, name, type, base_url, is_enabled) VALUES ($1, $2, 'openai', 'https://spoof.example.com', true)`, provID, "spoof-prov")
	if err != nil {
		t.Fatalf("insert provider: %v", err)
	}

	store := &nodeStore{tx: tx}

	t.Run("UUID node cannot spoof identity via reserved key", func(t *testing.T) {
		// Attacker tries to set __gorouter_node_id in metadata for a UUID node
		realID := uuid.New()
		node := enginerouting.ProviderNode{
			ID:         realID.String(),
			ProviderID: provID,
			Name:       "spoof-uuid",
			IsActive:   true,
			Metadata: map[string]string{
				"source":          "attacker",
				nodeExternalIDKey: "spoofed-other-node-id",
			},
		}
		if err := store.Save(ctx, node); err != nil {
			t.Fatalf("Save: %v", err)
		}

		nodes, err := store.List(ctx)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		found := false
		for _, n := range nodes {
			if n.ID == "spoofed-other-node-id" {
				t.Error("identity spoof succeeded: node restored with spoofed ID")
			}
			if n.ID == realID.String() {
				found = true
				if _, exists := n.Metadata[nodeExternalIDKey]; exists {
					t.Error("reserved key leaked in List after spoof attempt")
				}
			}
		}
		if !found {
			t.Error("original UUID node not found in List after spoof attempt")
		}
	})

	t.Run("Non-UUID node metadata collision gets corrected to true ID", func(t *testing.T) {
		// Attacker saves a non-UUID node with a misleading __gorouter_node_id
		node := enginerouting.ProviderNode{
			ID:         "my-real-node",
			ProviderID: provID,
			Name:       "collision-node",
			IsActive:   true,
			Metadata: map[string]string{
				"source":          "attacker",
				nodeExternalIDKey: "fake-identity",
			},
		}
		if err := store.Save(ctx, node); err != nil {
			t.Fatalf("Save: %v", err)
		}

		nodes, err := store.List(ctx)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		found := false
		for _, n := range nodes {
			if n.ID == "my-real-node" {
				found = true
				if _, exists := n.Metadata[nodeExternalIDKey]; exists {
					t.Error("reserved key leaked in List")
				}
			}
			if n.ID == "fake-identity" {
				t.Error("spoofed identity appeared in List")
			}
		}
		if !found {
			t.Error("original non-UUID node not found in List after metadata collision")
		}
	})
}

func TestNodeStore_CorruptReservedKeyExcluded_Integration(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)

	provID := uuid.New()
	_, err = tx.Exec(ctx,
		`INSERT INTO gorouter_providers (id, name, type, base_url, is_enabled) VALUES ($1, $2, 'openai', 'https://corrupt-test.example.com', true)`, provID, "corrupt-prov")
	if err != nil {
		t.Fatalf("insert provider: %v", err)
	}

	store := &nodeStore{tx: tx}

	// Save a legitimate non-UUID node.
	node := enginerouting.ProviderNode{
		ID: "legit:node", ProviderID: provID, Name: "legit-node", IsActive: true,
		Metadata: map[string]string{"source": "test"},
	}
	if err := store.Save(ctx, node); err != nil {
		t.Fatalf("Save legit node: %v", err)
	}

	// Save a UUID node as baseline.
	uuidID := uuid.New()
	if err := store.Save(ctx, enginerouting.ProviderNode{
		ID: uuidID.String(), ProviderID: provID, Name: "uuid-baseline", IsActive: true,
	}); err != nil {
		t.Fatalf("Save baseline UUID node: %v", err)
	}

	// Direct SQL: insert a corrupt row where __gorouter_node_id does not derive to the row UUID.
	corruptUUID := uuid.New() // will not match deriveUUID("stolen-identity")
	corruptMeta, _ := json.Marshal(map[string]string{
		"source":          "attacker-direct-sql",
		nodeExternalIDKey: "stolen-identity",
	})
	_, err = tx.Exec(ctx,
		`INSERT INTO gorouter_provider_nodes (id, provider_id, name, is_active, metadata, created_at, updated_at)
		 VALUES ($1, $2, $3, true, $4, NOW(), NOW())`,
		corruptUUID, provID, "corrupt-row", corruptMeta)
	if err != nil {
		t.Fatalf("insert corrupt row via SQL: %v", err)
	}

	nodes, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	foundLegit := false
	foundUUID := false
	for _, n := range nodes {
		if n.ID == "legit:node" {
			foundLegit = true
		}
		if n.ID == uuidID.String() {
			foundUUID = true
		}
		if n.ID == "stolen-identity" {
			t.Error("RESERVED-KEY SPOOF: List returned node with stolen identity from corrupt DB row")
		}
	}
	if !foundLegit {
		t.Error("legitimate non-UUID node excluded alongside corrupt row (false positive)")
	}
	if !foundUUID {
		t.Error("legitimate UUID node excluded alongside corrupt row (false positive)")
	}
}

func nodeIDs(nodes []enginerouting.ProviderNode) []string {
	ids := make([]string, len(nodes))
	for i, n := range nodes {
		ids[i] = n.ID
	}
	return ids
}
