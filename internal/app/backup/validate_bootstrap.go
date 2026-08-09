package backup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"gorouter/internal/persistence/postgres/migrations"

	"github.com/jackc/pgx/v5/pgconn"
)

// ValidateBootstrapBackup is the fail-closed precondition gate that must pass
// before any DDL migration batch runs (design §8, BE-04 seam). It passes when
// no database is configured, when the database is fresh (nothing applied, so
// nothing can be lost), or when no migrations are pending. Otherwise the
// newest registered backup must carry BOTH verification markers (verified_at
// and restore_verified_at) and its file must exist with matching size and
// sha256; any unavailability or mismatch fails closed.
func ValidateBootstrapBackup(ctx context.Context, db migrations.Pool) error {
	return validateBootstrapBackup(ctx, db, HashFileSHA256)
}

func validateBootstrapBackup(ctx context.Context, db migrations.Pool, hashFile func(string) (string, error)) error {
	if db == nil {
		return nil
	}
	var applied int
	if err := scanCount(ctx, db, "SELECT count(*) FROM gorouter_migrations", &applied); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42P01" {
			return nil
		}
		return fmt.Errorf("backup: cannot determine applied migrations: %w", err)
	}
	if applied == 0 {
		return nil
	}
	pending, err := migrations.NewRunner(db).ListPending(ctx, migrations.DirectionUp)
	if err != nil {
		return fmt.Errorf("backup: cannot determine pending migrations: %w", err)
	}
	if len(pending) == 0 {
		return nil
	}

	var path, sha string
	var bytes int64
	var verifiedAt, restoreVerifiedAt *time.Time
	rows, err := db.Query(ctx, `SELECT path, sha256, bytes, verified_at, restore_verified_at
		FROM gorouter_backups
		WHERE verified_at IS NOT NULL AND restore_verified_at IS NOT NULL
		ORDER BY created_at DESC, id LIMIT 1`)
	if err != nil {
		return fmt.Errorf("backup: cannot query validated backups: %w", err)
	}
	found := rows.Next()
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("backup: cannot query validated backups: %w", err)
	}
	if found {
		err = rows.Scan(&path, &sha, &bytes, &verifiedAt, &restoreVerifiedAt)
	}
	rows.Close()
	if err != nil {
		return fmt.Errorf("backup: cannot query validated backups: %w", err)
	}
	if !found {
		return fmt.Errorf("backup: pending schema migrations require a validated backup; none available")
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("backup: validated backup file unavailable: %w", err)
	}
	if info.Size() != bytes {
		return fmt.Errorf("backup: validated backup file size mismatch")
	}
	got, err := hashFile(path)
	if err != nil {
		return fmt.Errorf("backup: validated backup file unavailable: %w", err)
	}
	if !strings.EqualFold(got, sha) {
		return fmt.Errorf("backup: validated backup file hash mismatch")
	}
	return nil
}

func scanCount(ctx context.Context, db migrations.Pool, query string, dest *int) error {
	rows, err := db.Query(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		if err := rows.Scan(dest); err != nil {
			return err
		}
	}
	return rows.Err()
}
