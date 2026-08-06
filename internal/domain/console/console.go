// Package console defines pure domain contracts for the console-log
// projection: durable, strictly ordered, redacted log entries rendered by
// the operator console.
package console

import (
	"context"
	"time"
)

// ConsoleLog is a single durable console-log projection entry.
//
// Seq is an application-assigned strictly increasing sequence number; it is
// the ordering and retention key, while the database-assigned ID is an
// internal surrogate. The console service assigns Seq via the repository's
// atomic NextSeq, backed by a purge-proof gorouter_runtime_state watermark,
// so sequences stay strictly increasing across process restarts and full
// retention sweeps. Only redacted text is ever stored: Message (when
// present) and RedactedMessage both carry the redacted form, never raw
// input. RetentionUntil is service-stamped (now + RetentionDays); the
// projection itself is append-only and retention is applied by the service
// via PurgeExpired.
type ConsoleLog struct {
	ID              int64
	Seq             int64
	Level           *string
	Message         *string
	RedactedMessage string
	OccurredAt      *time.Time
	RetentionUntil  *time.Time
}

// Frozen console-log projection constants.
const (
	// DefaultCap is the default ListAfter limit and the hard cap on the
	// number of rows returned, applied by the console service.
	DefaultCap = 50
	// RetentionDays is the console-log retention window: rows whose
	// now + RetentionDays stamp has passed are purged by
	// RetentionService.PurgeExpired.
	RetentionDays = 90
)

// ConsoleLogRepository defines persistence operations for the console-log
// projection. Entries are stored faithfully: no filtering, redaction or
// retention is performed here — those are service-layer concerns.
type ConsoleLogRepository interface {
	// Append stores a console-log entry. The database assigns the row ID;
	// the entry is stored as given, including Seq, which the console
	// service derives from NextSeq so it stays strictly increasing across
	// restarts and ordered reads and retention behave deterministically.
	Append(ctx context.Context, entry *ConsoleLog) error
	// ListAfter returns entries whose Seq is strictly greater than the
	// given seq, ordered by (Seq, ID), capped at 50 rows regardless of the
	// requested limit. A zero limit yields an empty result; a negative
	// limit fails closed with a PostgreSQL error.
	ListAfter(ctx context.Context, seq int64, limit int) ([]ConsoleLog, error)
	// MaxSeq returns the highest persisted Seq among live rows, or 0 when
	// the table is empty. It is informational only: allocation uses
	// NextSeq, whose purge-proof watermark never regresses.
	MaxSeq(ctx context.Context) (int64, error)
	// NextSeq atomically allocates the next strictly increasing sequence
	// number from the purge-proof sequence watermark. The watermark lives
	// in the approved gorouter_runtime_state KV table (key
	// console:seq:watermark), not in the purged console table, so neither
	// a full retention sweep nor a process restart can regress it: the
	// returned Seq is always greater than every previously assigned Seq,
	// even when the console table is empty. On first use after an upgrade
	// the watermark seeds above the highest existing row. Allocation and
	// the console-row insert share one transaction, so a failed append
	// rolls back the watermark increment (a harmless gap, never a
	// duplicate).
	NextSeq(ctx context.Context) (int64, error)
	// DeleteBefore removes entries whose Seq is strictly less than the
	// given seq and reports how many rows were deleted.
	DeleteBefore(ctx context.Context, seq int64) (int64, error)
	// PurgeBefore removes entries whose RetentionUntil is strictly before
	// the given cutoff and reports how many rows were deleted. Rows at or
	// after the cutoff survive.
	PurgeBefore(ctx context.Context, cutoff time.Time) (int64, error)
}
