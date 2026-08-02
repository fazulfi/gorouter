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
// internal surrogate. The console service assigns Seq from the persisted
// watermark (MaxSeq + 1) so sequences stay strictly increasing across
// process restarts. Only redacted text is ever stored: Message (when
// present) and RedactedMessage both carry the redacted form, never raw
// input. RetentionUntil is service-stamped (RetentionDays); the projection
// itself is append-only and retention is applied by the service via
// PurgeBefore.
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
	// RetentionDays is the console-log retention window: rows older than
	// this are purged by RetentionService.PurgeBefore.
	RetentionDays = 90
)

// ConsoleLogRepository defines persistence operations for the console-log
// projection. Entries are stored faithfully: no filtering, redaction or
// retention is performed here — those are service-layer concerns.
type ConsoleLogRepository interface {
	// Append stores a console-log entry. The database assigns the row ID;
	// the entry is stored as given, including Seq, which the console
	// service derives from MaxSeq so it stays strictly increasing across
	// restarts and ordered reads and retention behave deterministically.
	Append(ctx context.Context, entry *ConsoleLog) error
	// ListAfter returns entries whose Seq is strictly greater than the
	// given seq, ordered by (Seq, ID), capped at 50 rows regardless of the
	// requested limit. A zero limit yields an empty result; a negative
	// limit fails closed with a PostgreSQL error.
	ListAfter(ctx context.Context, seq int64, limit int) ([]ConsoleLog, error)
	// MaxSeq returns the highest persisted Seq, or 0 when the table is
	// empty. It is the durable sequence watermark the console service
	// derives the next Seq from: because the newest row carries the
	// newest retention boundary, ordinary retention purges never regress
	// it, and it resets to 0 only when every row has been removed.
	MaxSeq(ctx context.Context) (int64, error)
	// DeleteBefore removes entries whose Seq is strictly less than the
	// given seq and reports how many rows were deleted.
	DeleteBefore(ctx context.Context, seq int64) (int64, error)
	// PurgeBefore removes entries whose RetentionUntil is strictly before
	// the given cutoff and reports how many rows were deleted. Rows at or
	// after the cutoff survive.
	PurgeBefore(ctx context.Context, cutoff time.Time) (int64, error)
}
