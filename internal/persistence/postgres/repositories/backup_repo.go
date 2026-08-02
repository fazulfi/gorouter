package repositories

import (
	"context"
	"errors"
	"time"

	"gorouter/internal/domain/backup"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Compile-time interface assertion.
var _ backup.BackupRepository = (*backupRepo)(nil)

// NewBackupRepo creates a backup registry repository bound to the given
// transaction.
func NewBackupRepo(tx pgx.Tx) backup.BackupRepository {
	return &backupRepo{tx: tx}
}

type backupRepo struct {
	tx pgx.Tx
}

const backupSelectColumns = "id, path, sha256, bytes, generated_by, verified_at, restore_verified_at, created_at"

func (r *backupRepo) Create(ctx context.Context, b *backup.Backup) error {
	_, err := r.tx.Exec(ctx,
		`INSERT INTO gorouter_backups (id, path, sha256, bytes, generated_by, verified_at, restore_verified_at, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		b.ID, b.Path, b.SHA256, b.Bytes, b.GeneratedBy, b.VerifiedAt, b.RestoreVerifiedAt, b.CreatedAt)
	return err
}

func (r *backupRepo) List(ctx context.Context) ([]backup.Backup, error) {
	rows, err := r.tx.Query(ctx,
		`SELECT `+backupSelectColumns+`
		 FROM gorouter_backups
		 ORDER BY created_at DESC, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []backup.Backup
	for rows.Next() {
		var b backup.Backup
		if err := rows.Scan(&b.ID, &b.Path, &b.SHA256, &b.Bytes,
			&b.GeneratedBy, &b.VerifiedAt, &b.RestoreVerifiedAt, &b.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (r *backupRepo) FindByID(ctx context.Context, id uuid.UUID) (*backup.Backup, error) {
	b := &backup.Backup{}
	err := r.tx.QueryRow(ctx,
		`SELECT `+backupSelectColumns+`
		 FROM gorouter_backups WHERE id = $1`, id).
		Scan(&b.ID, &b.Path, &b.SHA256, &b.Bytes,
			&b.GeneratedBy, &b.VerifiedAt, &b.RestoreVerifiedAt, &b.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, backup.ErrBackupNotFound
		}
		return nil, err
	}
	return b, nil
}

func (r *backupRepo) UpdateVerification(ctx context.Context, id uuid.UUID, verifiedAt time.Time) error {
	tag, err := r.tx.Exec(ctx,
		`UPDATE gorouter_backups SET verified_at = $2 WHERE id = $1`, id, verifiedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return backup.ErrBackupNotFound
	}
	return nil
}
