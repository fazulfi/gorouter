package repositories

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorouter/internal/domain/backup"

	"github.com/google/uuid"
)

func TestBackupRepo_CRUD_Integration(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)

	repo := NewBackupRepo(tx)

	t.Run("create preserves caller fields and NULL verification times", func(t *testing.T) {
		generatedBy := "scheduler"
		createdAt := time.Now().UTC().Truncate(time.Microsecond)
		b := &backup.Backup{
			ID: uuid.New(), Path: "/var/lib/gorouter/backups/be09-1.sql",
			SHA256: "aaaa", Bytes: 1234, GeneratedBy: &generatedBy, CreatedAt: &createdAt,
		}
		if err := repo.Create(ctx, b); err != nil {
			t.Fatalf("create: %v", err)
		}

		got, err := repo.FindByID(ctx, b.ID)
		if err != nil {
			t.Fatalf("find: %v", err)
		}
		if got.ID != b.ID || got.Path != b.Path || got.SHA256 != b.SHA256 || got.Bytes != b.Bytes {
			t.Error("identity fields mismatch")
		}
		if got.GeneratedBy == nil || *got.GeneratedBy != generatedBy {
			t.Error("generated_by mismatch")
		}
		if !got.CreatedAt.Equal(createdAt) {
			t.Errorf("created_at = %v, want %v", got.CreatedAt, createdAt)
		}
		if got.VerifiedAt != nil || got.RestoreVerifiedAt != nil {
			t.Error("new backup must have NULL verification times")
		}
	})

	t.Run("list returns backups newest-first", func(t *testing.T) {
		older := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Microsecond)
		newer := time.Now().UTC().Truncate(time.Microsecond)
		b1 := &backup.Backup{ID: uuid.New(), Path: "/older", SHA256: "h1", Bytes: 1, CreatedAt: &older}
		b2 := &backup.Backup{ID: uuid.New(), Path: "/newer", SHA256: "h2", Bytes: 2, CreatedAt: &newer}
		if err := repo.Create(ctx, b1); err != nil {
			t.Fatalf("create older: %v", err)
		}
		if err := repo.Create(ctx, b2); err != nil {
			t.Fatalf("create newer: %v", err)
		}

		got, err := repo.List(ctx)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		posOlder, posNewer := -1, -1
		for i, bb := range got {
			if bb.ID == b1.ID {
				posOlder = i
			}
			if bb.ID == b2.ID {
				posNewer = i
			}
		}
		if posOlder == -1 || posNewer == -1 {
			t.Fatalf("created backups missing from list (older %d, newer %d)", posOlder, posNewer)
		}
		if posNewer >= posOlder {
			t.Errorf("list is not newest-first: newer at %d, older at %d", posNewer, posOlder)
		}
	})

	t.Run("missing FindByID returns ErrBackupNotFound", func(t *testing.T) {
		_, err := repo.FindByID(ctx, uuid.New())
		if !errors.Is(err, backup.ErrBackupNotFound) {
			t.Fatalf("expected ErrBackupNotFound, got %v", err)
		}
	})

	t.Run("update verification preserves all other fields", func(t *testing.T) {
		generatedBy := "scheduler"
		createdAt := generatedTime()
		b := &backup.Backup{
			ID: uuid.New(), Path: "/var/lib/gorouter/backups/verify-me.sql",
			SHA256: "bbbb", Bytes: 99, GeneratedBy: &generatedBy, CreatedAt: &createdAt,
		}
		if err := repo.Create(ctx, b); err != nil {
			t.Fatalf("create: %v", err)
		}

		verifiedAt := time.Now().UTC().Truncate(time.Microsecond)
		if err := repo.UpdateVerification(ctx, b.ID, verifiedAt); err != nil {
			t.Fatalf("update verification: %v", err)
		}

		got, err := repo.FindByID(ctx, b.ID)
		if err != nil {
			t.Fatalf("find: %v", err)
		}
		if got.VerifiedAt == nil || !got.VerifiedAt.Equal(verifiedAt) {
			t.Errorf("verified_at = %v, want %v", got.VerifiedAt, verifiedAt)
		}
		if got.Path != b.Path || got.SHA256 != b.SHA256 || got.Bytes != b.Bytes {
			t.Error("update verification mutated identity fields")
		}
		if got.RestoreVerifiedAt != nil {
			t.Error("update verification must not touch restore_verified_at")
		}
		if got.GeneratedBy == nil || *got.GeneratedBy != generatedBy {
			t.Error("update verification mutated generated_by")
		}
	})

	t.Run("missing UpdateVerification returns ErrBackupNotFound", func(t *testing.T) {
		err := repo.UpdateVerification(ctx, uuid.New(), time.Now().UTC())
		if !errors.Is(err, backup.ErrBackupNotFound) {
			t.Fatalf("expected ErrBackupNotFound, got %v", err)
		}
	})

	t.Run("create does not mutate caller input", func(t *testing.T) {
		before := backup.Backup{ID: uuid.New(), Path: "/p", SHA256: "h", Bytes: 1}
		b := before
		if err := repo.Create(ctx, &b); err != nil {
			t.Fatalf("create: %v", err)
		}
		if b != before {
			t.Error("create mutated caller input")
		}
	})
}

func TestBackupRepo_Constraints_Integration(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)

	repo := NewBackupRepo(tx)

	t.Run("NULL path fails the NOT NULL constraint", func(t *testing.T) {
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatalf("savepoint: %v", err)
		}
		_, err = sp.Exec(ctx,
			`INSERT INTO gorouter_backups (id, path, sha256, bytes) VALUES ($1, NULL, 'h', 1)`,
			uuid.New())
		sp.Rollback(ctx)
		if err == nil {
			t.Fatal("expected NOT NULL violation for path, got nil")
		}
	})

	t.Run("NULL sha256 fails the NOT NULL constraint", func(t *testing.T) {
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatalf("savepoint: %v", err)
		}
		_, err = sp.Exec(ctx,
			`INSERT INTO gorouter_backups (id, path, sha256, bytes) VALUES ($1, '/p', NULL, 1)`,
			uuid.New())
		sp.Rollback(ctx)
		if err == nil {
			t.Fatal("expected NOT NULL violation for sha256, got nil")
		}
	})

	t.Run("NULL bytes fails the NOT NULL constraint", func(t *testing.T) {
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatalf("savepoint: %v", err)
		}
		_, err = sp.Exec(ctx,
			`INSERT INTO gorouter_backups (id, path, sha256, bytes) VALUES ($1, '/p', 'h', NULL)`,
			uuid.New())
		sp.Rollback(ctx)
		if err == nil {
			t.Fatal("expected NOT NULL violation for bytes, got nil")
		}
	})

	t.Run("duplicate id fails the primary key", func(t *testing.T) {
		id := uuid.New()
		if err := repo.Create(ctx, &backup.Backup{ID: id, Path: "/p1", SHA256: "h1", Bytes: 1}); err != nil {
			t.Fatalf("create first: %v", err)
		}
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatalf("savepoint: %v", err)
		}
		spRepo := NewBackupRepo(sp)
		err = spRepo.Create(ctx, &backup.Backup{ID: id, Path: "/p2", SHA256: "h2", Bytes: 2})
		sp.Rollback(ctx)
		if err == nil {
			t.Fatal("expected duplicate key violation, got nil")
		}
	})
}

func generatedTime() time.Time {
	return time.Now().UTC().Truncate(time.Microsecond)
}
