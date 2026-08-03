package repositories

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"gorouter/internal/domain/backup"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestBackupRepo_Create(t *testing.T) {
	t.Parallel()

	t.Run("success passes fields in column order", func(t *testing.T) {
		var gotSQL string
		var gotArgs []interface{}
		tx := &mockTx{
			execFn: func(_ context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
				gotSQL = sql
				gotArgs = args
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &backupRepo{tx: tx}
		id := uuid.New()
		generatedBy := "scheduler"
		at := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
		b := &backup.Backup{
			ID: id, Path: "/var/lib/gorouter/backups/be09-1.sql",
			SHA256: "abc123", Bytes: 4096, GeneratedBy: &generatedBy,
			VerifiedAt: &at, RestoreVerifiedAt: nil, CreatedAt: &at,
		}
		if err := repo.Create(context.Background(), b); err != nil {
			t.Fatal(err)
		}
		if len(gotArgs) != 8 {
			t.Fatalf("expected 8 args, got %d", len(gotArgs))
		}
		if gotArgs[0] != id || gotArgs[1] != "/var/lib/gorouter/backups/be09-1.sql" ||
			gotArgs[2] != "abc123" || gotArgs[3] != int64(4096) {
			t.Error("id/path/sha256/bytes arg mismatch")
		}
		if gotArgs[4] != &generatedBy || gotArgs[5] != &at || gotArgs[6] != (*time.Time)(nil) || gotArgs[7] != &at {
			t.Error("generated_by/verified_at/restore_verified_at/created_at arg mismatch")
		}
		if !strings.Contains(gotSQL, "gorouter_backups") {
			t.Errorf("SQL does not reference gorouter_backups:\n%s", gotSQL)
		}
	})

	t.Run("never mutates caller input", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &backupRepo{tx: tx}
		before := backup.Backup{ID: uuid.New(), Path: "/p", SHA256: "h", Bytes: 1}
		b := before
		if err := repo.Create(context.Background(), &b); err != nil {
			t.Fatal(err)
		}
		if b != before {
			t.Error("Create mutated caller input")
		}
	})

	t.Run("exec error propagates", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("insert failed")
			},
		}
		repo := &backupRepo{tx: tx}
		err := repo.Create(context.Background(), &backup.Backup{ID: uuid.New(), Path: "/p", SHA256: "h", Bytes: 1})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestBackupRepo_List(t *testing.T) {
	t.Parallel()

	t.Run("returns scanned backups newest-first", func(t *testing.T) {
		var gotSQL string
		tx := &mockTx{
			queryFn: func(_ context.Context, sql string, _ ...interface{}) (pgx.Rows, error) {
				gotSQL = sql
				return &mockRows{rows: [][]interface{}{
					{uuid.New(), "/newest", "h1", int64(2), nil, nil, nil, nil},
					{uuid.New(), "/older", "h2", int64(1), nil, nil, nil, nil},
				}}, nil
			},
		}
		repo := &backupRepo{tx: tx}
		got, err := repo.List(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 {
			t.Fatalf("expected 2 backups, got %d", len(got))
		}
		if got[0].Path != "/newest" || got[1].Path != "/older" {
			t.Error("list order mismatch")
		}
		if got[0].Bytes != 2 || got[1].Bytes != 1 {
			t.Error("bytes scan mismatch")
		}
		if !strings.Contains(gotSQL, "gorouter_backups") || !strings.Contains(gotSQL, "ORDER BY created_at DESC") {
			t.Errorf("List SQL must read gorouter_backups newest-first:\n%s", gotSQL)
		}
	})

	t.Run("returns empty slice when no rows", func(t *testing.T) {
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return &mockRows{}, nil
			},
		}
		repo := &backupRepo{tx: tx}
		got, err := repo.List(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 0 {
			t.Errorf("expected empty list, got %v", got)
		}
	})

	t.Run("query error propagates", func(t *testing.T) {
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return nil, errors.New("select failed")
			},
		}
		repo := &backupRepo{tx: tx}
		if _, err := repo.List(context.Background()); err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestBackupRepo_FindByID(t *testing.T) {
	t.Parallel()

	t.Run("found returns the backup", func(t *testing.T) {
		id := uuid.New()
		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				return &mockRow{vals: []interface{}{id, "/p", "h", int64(7), nil, nil, nil, nil}}
			},
		}
		repo := &backupRepo{tx: tx}
		got, err := repo.FindByID(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != id || got.Path != "/p" || got.SHA256 != "h" || got.Bytes != 7 {
			t.Error("scanned fields mismatch")
		}
		if got.GeneratedBy != nil || got.VerifiedAt != nil || got.RestoreVerifiedAt != nil || got.CreatedAt != nil {
			t.Error("nullable columns must scan as NULL when absent")
		}
	})

	t.Run("missing returns ErrBackupNotFound", func(t *testing.T) {
		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				return &mockRow{err: pgx.ErrNoRows}
			},
		}
		repo := &backupRepo{tx: tx}
		_, err := repo.FindByID(context.Background(), uuid.New())
		if !errors.Is(err, backup.ErrBackupNotFound) {
			t.Fatalf("expected ErrBackupNotFound, got %v", err)
		}
	})

	t.Run("scan error propagates", func(t *testing.T) {
		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				return &mockRow{err: errors.New("scan failed")}
			},
		}
		repo := &backupRepo{tx: tx}
		if _, err := repo.FindByID(context.Background(), uuid.New()); err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestBackupRepo_UpdateVerification(t *testing.T) {
	t.Parallel()

	t.Run("success updates verified_at when backup exists", func(t *testing.T) {
		var gotSQL string
		var gotArgs []interface{}
		tx := &mockTx{
			execFn: func(_ context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
				gotSQL = sql
				gotArgs = args
				return pgconn.NewCommandTag("UPDATE 1"), nil
			},
		}
		repo := &backupRepo{tx: tx}
		id := uuid.New()
		verifiedAt := time.Date(2026, 8, 2, 14, 0, 0, 0, time.UTC)
		if err := repo.UpdateVerification(context.Background(), id, verifiedAt); err != nil {
			t.Fatal(err)
		}
		if len(gotArgs) != 2 || gotArgs[0] != id || gotArgs[1] != verifiedAt {
			t.Error("id/verified_at args mismatch")
		}
		if !strings.Contains(gotSQL, "gorouter_backups") || !strings.Contains(gotSQL, "verified_at") {
			t.Errorf("UpdateVerification SQL must target gorouter_backups.verified_at:\n%s", gotSQL)
		}
		if strings.Contains(gotSQL, "path") || strings.Contains(gotSQL, "sha256") || strings.Contains(gotSQL, "bytes") {
			t.Errorf("UpdateVerification must not touch immutable identity fields:\n%s", gotSQL)
		}
	})

	t.Run("missing backup returns ErrBackupNotFound", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 0"), nil
			},
		}
		repo := &backupRepo{tx: tx}
		err := repo.UpdateVerification(context.Background(), uuid.New(), time.Now().UTC())
		if !errors.Is(err, backup.ErrBackupNotFound) {
			t.Fatalf("expected ErrBackupNotFound, got %v", err)
		}
	})

	t.Run("exec error propagates", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("update failed")
			},
		}
		repo := &backupRepo{tx: tx}
		if err := repo.UpdateVerification(context.Background(), uuid.New(), time.Now().UTC()); err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestBackupRepo_UpdateRestoreVerification(t *testing.T) {
	t.Parallel()

	t.Run("success updates restore_verified_at when backup exists", func(t *testing.T) {
		var gotSQL string
		var gotArgs []interface{}
		tx := &mockTx{
			execFn: func(_ context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
				gotSQL = sql
				gotArgs = args
				return pgconn.NewCommandTag("UPDATE 1"), nil
			},
		}
		repo := &backupRepo{tx: tx}
		id := uuid.New()
		restoreVerifiedAt := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
		if err := repo.UpdateRestoreVerification(context.Background(), id, restoreVerifiedAt); err != nil {
			t.Fatal(err)
		}
		if len(gotArgs) != 2 || gotArgs[0] != id || gotArgs[1] != restoreVerifiedAt {
			t.Error("id/restore_verified_at args mismatch")
		}
		if !strings.Contains(gotSQL, "gorouter_backups") || !strings.Contains(gotSQL, "restore_verified_at = $2") {
			t.Errorf("UpdateRestoreVerification SQL must set gorouter_backups.restore_verified_at:\n%s", gotSQL)
		}
		if strings.Contains(gotSQL, "SET verified_at") {
			t.Errorf("UpdateRestoreVerification SQL must leave verified_at untouched:\n%s", gotSQL)
		}
		if strings.Contains(gotSQL, "path") || strings.Contains(gotSQL, "sha256") || strings.Contains(gotSQL, "bytes") {
			t.Errorf("UpdateRestoreVerification must not touch immutable identity fields:\n%s", gotSQL)
		}
	})

	t.Run("missing backup returns ErrBackupNotFound", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("UPDATE 0"), nil
			},
		}
		repo := &backupRepo{tx: tx}
		err := repo.UpdateRestoreVerification(context.Background(), uuid.New(), time.Now().UTC())
		if !errors.Is(err, backup.ErrBackupNotFound) {
			t.Fatalf("expected ErrBackupNotFound, got %v", err)
		}
	})

	t.Run("exec error propagates", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("update failed")
			},
		}
		repo := &backupRepo{tx: tx}
		if err := repo.UpdateRestoreVerification(context.Background(), uuid.New(), time.Now().UTC()); err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}
