// Package backup defines pure domain contracts for the validated
// disaster-recovery backup registry.
package backup

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrBackupNotFound reports that no backup exists with the id.
var ErrBackupNotFound = errors.New("backup: not found")

// Backup is a single validated backup registry entry. Path is the storage
// location, SHA256 the content hash and Bytes the size; GeneratedBy records
// who produced the backup. VerifiedAt and RestoreVerifiedAt are the
// validation markers, and CreatedAt the registry time. The registry is
// append-only: the identity fields are immutable after Create, and the only
// allowed mutation is setting the verification markers.
type Backup struct {
	ID                uuid.UUID
	Path              string
	SHA256            string
	Bytes             int64
	GeneratedBy       *string
	VerifiedAt        *time.Time
	RestoreVerifiedAt *time.Time
	CreatedAt         *time.Time
}

// BackupRepository defines persistence operations for the backup registry.
// Create stores the entry exactly as given; List returns the registry
// newest-first; FindByID returns ErrBackupNotFound when no backup exists;
// UpdateVerification records the dump-validation timestamp and
// UpdateRestoreVerification the shadow-restore timestamp on an existing
// backup, both failing with ErrBackupNotFound when the backup is missing.
// There is no update path for the identity fields and no delete path at all.
type BackupRepository interface {
	// Create persists a backup registry entry exactly as given.
	Create(ctx context.Context, b *Backup) error
	// List returns all registry entries, newest first.
	List(ctx context.Context) ([]Backup, error)
	// FindByID returns the entry with the given id, or ErrBackupNotFound
	// when no such entry exists.
	FindByID(ctx context.Context, id uuid.UUID) (*Backup, error)
	// UpdateVerification records verifiedAt on the entry with the given
	// id, leaving every other field untouched. Fails with
	// ErrBackupNotFound when no such entry exists.
	UpdateVerification(ctx context.Context, id uuid.UUID, verifiedAt time.Time) error
	// UpdateRestoreVerification records restoreVerifiedAt on the entry with
	// the given id, leaving every other field untouched. Fails with
	// ErrBackupNotFound when no such entry exists.
	UpdateRestoreVerification(ctx context.Context, id uuid.UUID, restoreVerifiedAt time.Time) error
}
