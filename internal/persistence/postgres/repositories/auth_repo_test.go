package repositories

import (
	"context"
	"os"
	"testing"
	"time"

	"gorouter/internal/domain/auth"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func skipIfShort(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
}

// FIX_A: aligned with getTestPool topology (isolation + complete fixture grants) to restore
// least-privilege CI correctness after Wave-3 exposed true role boundaries. The historical
// shared DB approach using POSTGRES_TEST_DSN was broken under correct topology: migrations
// applied as postgres admin → gorouter_migrations owned by postgres; gorouter_ddl cannot SELECT.
func testPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}

	bootstrapRolesForRepoTest(t, ctx, dsn)
	dbName := setupRepoTestDB(t, ctx)
	dsn = isolatedTestDSN(t, dbName)

	grantSchemaCreateToDDLRepoTest(t, ctx, dsn)
	pool := migrateAsDDLRepoTest(t, ctx, dsn)
	grantRuntimeFixturePrivilegesRepoTest(t, ctx, dsn)
	return pool
}

func TestUserRepo_Integration(t *testing.T) {
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

	repo := &userRepo{tx: tx}
	now := time.Now().UTC()
	uid := uuid.New()

	user := &auth.User{
		ID:           uid,
		Email:        "test-int@example.com",
		PasswordHash: "$2a$12$hashvalue",
		DisplayName:  strPtr("Integration"),
		IsAdmin:      false,
		IsActive:     true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// FindByID
	found, err := repo.FindByID(ctx, uid)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if found == nil {
		t.Fatal("FindByID returned nil")
	}
	if found.Email != user.Email {
		t.Errorf("expected email %q, got %q", user.Email, found.Email)
	}

	// FindByEmail
	foundByEmail, err := repo.FindByEmail(ctx, user.Email)
	if err != nil {
		t.Fatalf("FindByEmail: %v", err)
	}
	if foundByEmail == nil {
		t.Fatal("FindByEmail returned nil")
	}
	if foundByEmail.ID != uid {
		t.Errorf("expected ID %v, got %v", uid, foundByEmail.ID)
	}

	// UpdateLastLogin
	if err := repo.UpdateLastLogin(ctx, uid); err != nil {
		t.Fatalf("UpdateLastLogin: %v", err)
	}
	updated, err := repo.FindByID(ctx, uid)
	if err != nil {
		t.Fatalf("FindByID after UpdateLastLogin: %v", err)
	}
	if updated.LastLoginAt == nil {
		t.Fatal("expected LastLoginAt to be set after UpdateLastLogin")
	}

	// Update
	updatedUser := *user
	updatedUser.DisplayName = strPtr("Updated Integration")
	if err := repo.Update(ctx, &updatedUser); err != nil {
		t.Fatalf("Update: %v", err)
	}
	afterUpdate, err := repo.FindByID(ctx, uid)
	if err != nil {
		t.Fatalf("FindByID after Update: %v", err)
	}
	if afterUpdate.DisplayName == nil || *afterUpdate.DisplayName != "Updated Integration" {
		t.Errorf("expected display name 'Updated Integration', got %v", afterUpdate.DisplayName)
	}

	// FindByID not found
	notFound, err := repo.FindByID(ctx, uuid.New())
	if err != nil {
		t.Fatalf("FindByID not found: %v", err)
	}
	if notFound != nil {
		t.Fatal("expected nil for non-existent ID")
	}

	// FindByEmail not found
	notFoundEmail, err := repo.FindByEmail(ctx, "nonexistent@example.com")
	if err != nil {
		t.Fatalf("FindByEmail not found: %v", err)
	}
	if notFoundEmail != nil {
		t.Fatal("expected nil for non-existent email")
	}
}

func TestSessionRepo_Integration(t *testing.T) {
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
	sessionRepo := &sessionRepo{tx: tx}

	// Create a user first
	now := time.Now().UTC()
	uid := uuid.New()
	user := &auth.User{
		ID:           uid,
		Email:        "session-test@example.com",
		PasswordHash: "hash",
		IsActive:     true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := userRepo.Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Create session
	sid := uuid.New()
	expiresAt := time.Now().Add(1 * time.Hour)
	session := &auth.Session{
		ID:        sid,
		UserID:    uid,
		TokenHash: "testhash123",
		UserAgent: "test-agent",
		ExpiresAt: expiresAt,
		CreatedAt: now,
	}
	if err := sessionRepo.Create(ctx, session); err != nil {
		t.Fatalf("Create session: %v", err)
	}

	// FindByID
	found, err := sessionRepo.FindByID(ctx, sid)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if found == nil {
		t.Fatal("FindByID returned nil")
	}
	if found.UserID != uid {
		t.Errorf("expected UserID %v, got %v", uid, found.UserID)
	}

	// FindByTokenHash
	foundByHash, err := sessionRepo.FindByTokenHash(ctx, "testhash123")
	if err != nil {
		t.Fatalf("FindByTokenHash: %v", err)
	}
	if foundByHash == nil {
		t.Fatal("FindByTokenHash returned nil")
	}

	// Revoke
	if err := sessionRepo.Revoke(ctx, sid); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	revoked, err := sessionRepo.FindByID(ctx, sid)
	if err != nil {
		t.Fatalf("FindByID after Revoke: %v", err)
	}
	if revoked.RevokedAt == nil {
		t.Fatal("expected RevokedAt to be set")
	}

	// DeleteExpired
	if err := sessionRepo.DeleteExpired(ctx); err != nil {
		t.Fatalf("DeleteExpired: %v", err)
	}

	// FindByID not found
	notFound, err := sessionRepo.FindByID(ctx, uuid.New())
	if err != nil {
		t.Fatalf("FindByID not found: %v", err)
	}
	if notFound != nil {
		t.Fatal("expected nil for non-existent session")
	}
}

func strPtr(s string) *string {
	return &s
}
