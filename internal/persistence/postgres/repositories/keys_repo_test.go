package repositories

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"gorouter/internal/app/tx"
	"gorouter/internal/domain/auth"
	"gorouter/internal/domain/keys"

	"github.com/google/uuid"
)

func TestAPIKeyRepo_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := context.Background()
	pool := testPool(t, ctx)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(ctx)

	userRepo := &userRepo{tx: tx}
	apiKeyRepo := &apiKeyRepo{tx: tx}

	// Create a user first
	now := time.Now().UTC()
	uid := uuid.New()
	user := &auth.User{
		ID:           uid,
		Email:        "apikey-test@example.com",
		PasswordHash: "hash",
		IsActive:     true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := userRepo.Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Create API key
	kid := uuid.New()
	key := &keys.APIKey{
		ID:        kid,
		UserID:    uid,
		KeyPrefix: "testpref",
		KeyHash:   "hashvalue123",
		Name:      "test-key",
		Scopes:    []string{"proxy:read"},
		CreatedAt: now,
	}
	if err := apiKeyRepo.Create(ctx, key); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// FindByID
	found, err := apiKeyRepo.FindByID(ctx, kid)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if found == nil {
		t.Fatal("FindByID returned nil")
	}
	if found.KeyHash != "hashvalue123" {
		t.Errorf("expected hash 'hashvalue123', got %q", found.KeyHash)
	}

	// FindByHash
	foundByHash, err := apiKeyRepo.FindByHash(ctx, "hashvalue123")
	if err != nil {
		t.Fatalf("FindByHash: %v", err)
	}
	if foundByHash == nil {
		t.Fatal("FindByHash returned nil")
	}
	if foundByHash.ID != kid {
		t.Errorf("expected ID %v, got %v", kid, foundByHash.ID)
	}

	// FindByUserID
	userKeys, err := apiKeyRepo.FindByUserID(ctx, uid)
	if err != nil {
		t.Fatalf("FindByUserID: %v", err)
	}
	if len(userKeys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(userKeys))
	}

	// Revoke
	if err := apiKeyRepo.Revoke(ctx, kid); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	revoked, err := apiKeyRepo.FindByID(ctx, kid)
	if err != nil {
		t.Fatalf("FindByID after Revoke: %v", err)
	}
	if revoked.RevokedAt == nil {
		t.Fatal("expected RevokedAt to be set")
	}

	// Not found cases
	notFound, err := apiKeyRepo.FindByID(ctx, uuid.New())
	if err != nil {
		t.Fatalf("FindByID not found: %v", err)
	}
	if notFound != nil {
		t.Fatal("expected nil for non-existent ID")
	}

	notFoundHash, err := apiKeyRepo.FindByHash(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("FindByHash not found: %v", err)
	}
	if notFoundHash != nil {
		t.Fatal("expected nil for non-existent hash")
	}

	emptyKeys, err := apiKeyRepo.FindByUserID(ctx, uuid.New())
	if err != nil {
		t.Fatalf("FindByUserID empty: %v", err)
	}
	if len(emptyKeys) != 0 {
		t.Fatalf("expected 0 keys for non-existent user, got %d", len(emptyKeys))
	}
}

func TestPATRepo_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := context.Background()
	pool := testPool(t, ctx)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(ctx)

	userRepo := &userRepo{tx: tx}
	patRepo := &patRepo{tx: tx}

	// Create a user first
	now := time.Now().UTC()
	uid := uuid.New()
	user := &auth.User{
		ID:           uid,
		Email:        "pat-test@example.com",
		PasswordHash: "hash",
		IsActive:     true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := userRepo.Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Create PAT
	pid := uuid.New()
	desc := "test-pat"
	pat := &keys.PAT{
		ID:          pid,
		UserID:      uid,
		TokenHash:   "pathash123",
		Description: &desc,
		CreatedAt:   now,
	}
	if err := patRepo.Create(ctx, pat); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// FindByID
	found, err := patRepo.FindByID(ctx, pid)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if found == nil {
		t.Fatal("FindByID returned nil")
	}
	if found.TokenHash != "pathash123" {
		t.Errorf("expected hash 'pathash123', got %q", found.TokenHash)
	}

	// FindByHash
	foundByHash, err := patRepo.FindByHash(ctx, "pathash123")
	if err != nil {
		t.Fatalf("FindByHash: %v", err)
	}
	if foundByHash == nil {
		t.Fatal("FindByHash returned nil")
	}
	if foundByHash.ID != pid {
		t.Errorf("expected ID %v, got %v", pid, foundByHash.ID)
	}

	// FindByUserID
	userPats, err := patRepo.FindByUserID(ctx, uid)
	if err != nil {
		t.Fatalf("FindByUserID: %v", err)
	}
	if len(userPats) != 1 {
		t.Fatalf("expected 1 PAT, got %d", len(userPats))
	}

	// Revoke
	if err := patRepo.Revoke(ctx, pid); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	revoked, err := patRepo.FindByID(ctx, pid)
	if err != nil {
		t.Fatalf("FindByID after Revoke: %v", err)
	}
	if revoked.RevokedAt == nil {
		t.Fatal("expected RevokedAt to be set")
	}

	// Not found cases
	notFound, err := patRepo.FindByID(ctx, uuid.New())
	if err != nil {
		t.Fatalf("FindByID not found: %v", err)
	}
	if notFound != nil {
		t.Fatal("expected nil for non-existent ID")
	}
}

func TestAuditLogRepo_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := context.Background()
	pool := testPool(t, ctx)

	txx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer txx.Rollback(ctx)

	repo := &auditLogRepo{tx: txx}
	now := time.Now().UTC()

	entry := &tx.AuditLogEntry{
		ID:           uuid.New(),
		ActorID:      nil,
		Action:       "user.login",
		ResourceType: "session",
		ResourceID:   nil,
		Details:      json.RawMessage(`{"reason":"test"}`),
		IPAddress:    nil,
		OccurredAt:   now,
	}

	if err := repo.Create(ctx, entry); err != nil {
		t.Fatalf("Create: %v", err)
	}

	var count int
	err = txx.QueryRow(ctx, `SELECT COUNT(*) FROM gorouter_audit_log WHERE id = $1`, entry.ID).Scan(&count)
	if err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 audit log entry, got %d", count)
	}
}
