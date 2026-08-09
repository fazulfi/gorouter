package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pashagolub/pgxmock/v2"
)

func TestValidateBootstrapBackup(t *testing.T) {
	t.Parallel()

	t.Run("nil db passes", func(t *testing.T) {
		if err := ValidateBootstrapBackup(context.Background(), nil); err != nil {
			t.Errorf("err = %v, want nil", err)
		}
	})

	t.Run("fresh database passes", func(t *testing.T) {
		db, _ := pgxmock.NewPool()
		db.ExpectQuery(`SELECT count\(\*\) FROM gorouter_migrations`).
			WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(0))
		if err := validateBootstrapBackup(context.Background(), db, func(string) (string, error) {
			return "", errors.New("must not be called")
		}); err != nil {
			t.Errorf("err = %v, want nil", err)
		}
	})

	t.Run("missing tracking table is fresh and passes", func(t *testing.T) {
		db, _ := pgxmock.NewPool()
		db.ExpectQuery(`SELECT count\(\*\) FROM gorouter_migrations`).
			WillReturnError(&pgconn.PgError{Code: "42P01"})
		if err := validateBootstrapBackup(context.Background(), db, func(string) (string, error) {
			return "", errors.New("must not be called")
		}); err != nil {
			t.Errorf("err = %v, want nil", err)
		}
	})

	t.Run("unknown db error fails closed", func(t *testing.T) {
		db, _ := pgxmock.NewPool()
		db.ExpectQuery(`SELECT count\(\*\) FROM gorouter_migrations`).
			WillReturnError(errors.New("connection lost"))
		err := validateBootstrapBackup(context.Background(), db, func(string) (string, error) {
			return "", errors.New("must not be called")
		})
		if err == nil || !strings.Contains(err.Error(), "cannot determine applied migrations") {
			t.Errorf("err = %v, want fail-closed error", err)
		}
	})

	t.Run("no pending migrations passes without any backup", func(t *testing.T) {
		db, _ := pgxmock.NewPool()
		db.ExpectQuery(`SELECT count\(\*\) FROM gorouter_migrations`).
			WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(9))
		rows := pgxmock.NewRows([]string{"version", "name"})
		for i := 1; i <= 9; i++ {
			rows.AddRow("00000"+string(rune('0'+i)), "migration")
		}
		db.ExpectQuery("SELECT version, name FROM gorouter_migrations").
			WillReturnRows(rows)
		if err := validateBootstrapBackup(context.Background(), db, func(string) (string, error) {
			return "", errors.New("must not be called")
		}); err != nil {
			t.Errorf("err = %v, want nil", err)
		}
	})

	t.Run("pending migrations without validated backup fail closed", func(t *testing.T) {
		db, _ := pgxmock.NewPool()
		db.ExpectQuery(`SELECT count\(\*\) FROM gorouter_migrations`).
			WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(8))
		rows := pgxmock.NewRows([]string{"version", "name"})
		for i := 1; i <= 8; i++ {
			rows.AddRow("00000"+string(rune('0'+i)), "migration")
		}
		db.ExpectQuery("SELECT version, name FROM gorouter_migrations").
			WillReturnRows(rows)
		db.ExpectQuery("SELECT path, sha256, bytes, verified_at, restore_verified_at").
			WillReturnRows(pgxmock.NewRows([]string{"path", "sha256", "bytes", "verified_at", "restore_verified_at"}))
		err := validateBootstrapBackup(context.Background(), db, func(string) (string, error) {
			return "", errors.New("must not be called")
		})
		if err == nil || !strings.Contains(err.Error(), "none available") {
			t.Errorf("err = %v, want validated-backup error", err)
		}
	})

	t.Run("pending migrations with only one marker fail closed", func(t *testing.T) {
		db, _ := pgxmock.NewPool()
		db.ExpectQuery(`SELECT count\(\*\) FROM gorouter_migrations`).
			WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(8))
		rows := pgxmock.NewRows([]string{"version", "name"})
		for i := 1; i <= 8; i++ {
			rows.AddRow("00000"+string(rune('0'+i)), "migration")
		}
		db.ExpectQuery("SELECT version, name FROM gorouter_migrations").
			WillReturnRows(rows)
		db.ExpectQuery("SELECT path, sha256, bytes, verified_at, restore_verified_at").
			WillReturnRows(pgxmock.NewRows([]string{"path", "sha256", "bytes", "verified_at", "restore_verified_at"}))
		err := validateBootstrapBackup(context.Background(), db, func(string) (string, error) {
			return "", errors.New("must not be called")
		})
		if err == nil || !strings.Contains(err.Error(), "none available") {
			t.Errorf("err = %v, want validated-backup error", err)
		}
	})

	t.Run("validated backup with matching file passes", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "valid.dump")
		body := []byte("content")
		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatal(err)
		}
		sha := strings.Repeat("ab", 32)
		now := time.Now().UTC()
		db, _ := pgxmock.NewPool()
		db.ExpectQuery(`SELECT count\(\*\) FROM gorouter_migrations`).
			WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(8))
		rows := pgxmock.NewRows([]string{"version", "name"})
		for i := 1; i <= 8; i++ {
			rows.AddRow("00000"+string(rune('0'+i)), "migration")
		}
		db.ExpectQuery("SELECT version, name FROM gorouter_migrations").
			WillReturnRows(rows)
		db.ExpectQuery("SELECT path, sha256, bytes, verified_at, restore_verified_at").
			WillReturnRows(pgxmock.NewRows([]string{"path", "sha256", "bytes", "verified_at", "restore_verified_at"}).
				AddRow(path, sha, int64(len(body)), &now, &now))
		if err := validateBootstrapBackup(context.Background(), db, func(string) (string, error) {
			return sha, nil
		}); err != nil {
			t.Errorf("err = %v, want nil", err)
		}
	})

	t.Run("validated backup with missing file fails closed", func(t *testing.T) {
		now := time.Now().UTC()
		db, _ := pgxmock.NewPool()
		db.ExpectQuery(`SELECT count\(\*\) FROM gorouter_migrations`).
			WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(8))
		rows := pgxmock.NewRows([]string{"version", "name"})
		for i := 1; i <= 8; i++ {
			rows.AddRow("00000"+string(rune('0'+i)), "migration")
		}
		db.ExpectQuery("SELECT version, name FROM gorouter_migrations").
			WillReturnRows(rows)
		db.ExpectQuery("SELECT path, sha256, bytes, verified_at, restore_verified_at").
			WillReturnRows(pgxmock.NewRows([]string{"path", "sha256", "bytes", "verified_at", "restore_verified_at"}).
				AddRow("/nonexistent/x.dump", "aa", int64(5), &now, &now))
		err := validateBootstrapBackup(context.Background(), db, func(string) (string, error) {
			return "", os.ErrNotExist
		})
		if err == nil || !strings.Contains(err.Error(), "unavailable") {
			t.Errorf("err = %v, want unavailable error", err)
		}
	})

	t.Run("validated backup with size mismatch fails closed", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "size.dump")
		if err := os.WriteFile(path, []byte("0123456789"), 0o600); err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC()
		db, _ := pgxmock.NewPool()
		db.ExpectQuery(`SELECT count\(\*\) FROM gorouter_migrations`).
			WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(8))
		rows := pgxmock.NewRows([]string{"version", "name"})
		for i := 1; i <= 8; i++ {
			rows.AddRow("00000"+string(rune('0'+i)), "migration")
		}
		db.ExpectQuery("SELECT version, name FROM gorouter_migrations").
			WillReturnRows(rows)
		db.ExpectQuery("SELECT path, sha256, bytes, verified_at, restore_verified_at").
			WillReturnRows(pgxmock.NewRows([]string{"path", "sha256", "bytes", "verified_at", "restore_verified_at"}).
				AddRow(path, "aa", int64(999), &now, &now))
		err := validateBootstrapBackup(context.Background(), db, func(string) (string, error) {
			return "", errors.New("must not be called")
		})
		if err == nil || !strings.Contains(err.Error(), "size mismatch") {
			t.Errorf("err = %v, want size mismatch error", err)
		}
	})

	t.Run("validated backup with hash mismatch fails closed", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "hash.dump")
		if err := os.WriteFile(path, []byte("content"), 0o600); err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC()
		db, _ := pgxmock.NewPool()
		db.ExpectQuery(`SELECT count\(\*\) FROM gorouter_migrations`).
			WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(8))
		rows := pgxmock.NewRows([]string{"version", "name"})
		for i := 1; i <= 8; i++ {
			rows.AddRow("00000"+string(rune('0'+i)), "migration")
		}
		db.ExpectQuery("SELECT version, name FROM gorouter_migrations").
			WillReturnRows(rows)
		db.ExpectQuery("SELECT path, sha256, bytes, verified_at, restore_verified_at").
			WillReturnRows(pgxmock.NewRows([]string{"path", "sha256", "bytes", "verified_at", "restore_verified_at"}).
				AddRow(path, "expected-sha", int64(7), &now, &now))
		err := validateBootstrapBackup(context.Background(), db, func(string) (string, error) {
			return "different-sha", nil
		})
		if err == nil || !strings.Contains(err.Error(), "mismatch") {
			t.Errorf("err = %v, want mismatch error", err)
		}
	})

	t.Run("validated backup query error fails closed", func(t *testing.T) {
		db, _ := pgxmock.NewPool()
		db.ExpectQuery(`SELECT count\(\*\) FROM gorouter_migrations`).
			WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(8))
		rows := pgxmock.NewRows([]string{"version", "name"})
		for i := 1; i <= 8; i++ {
			rows.AddRow("00000"+string(rune('0'+i)), "migration")
		}
		db.ExpectQuery("SELECT version, name FROM gorouter_migrations").
			WillReturnRows(rows)
		db.ExpectQuery("SELECT path, sha256, bytes, verified_at, restore_verified_at").
			WillReturnError(errors.New("query failed"))
		if err := validateBootstrapBackup(context.Background(), db, func(string) (string, error) {
			return "", errors.New("must not be called")
		}); err == nil {
			t.Error("err = nil, want fail-closed error")
		}
	})

	t.Run("HashFileSHA256 matches real content", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "h.dump")
		if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := HashFileSHA256(path)
		if err != nil {
			t.Fatal(err)
		}
		want := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
		if got != want {
			t.Errorf("sha256 = %s, want %s", got, want)
		}
	})
}
