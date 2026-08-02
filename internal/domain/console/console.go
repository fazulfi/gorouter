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
// internal surrogate. Raw message text is stored for server-side diagnostics
// and RedactedMessage is the authoritative payload shown in the console; the
// service layer must always populate RedactedMessage. Retention (for example
// 90 days) is applied by the service via RetentionUntil and DeleteBefore;
// the projection itself is append-only.
type ConsoleLog struct {
	ID              int64
	Seq             int64
	Level           *string
	Message         *string
	RedactedMessage string
	OccurredAt      *time.Time
	RetentionUntil  *time.Time
}

// ConsoleLogRepository defines persistence operations for the console-log
// projection. Entries are stored faithfully: no filtering, redaction or
// retention is performed here — those are service-layer concerns.
type ConsoleLogRepository interface {
	// Append stores a console-log entry. The database assigns the row ID;
	// the entry is stored as given, including Seq, which callers must keep
	// strictly increasing so ordered reads and retention behave
	// deterministically.
	Append(ctx context.Context, entry *ConsoleLog) error
	// ListAfter returns entries whose Seq is strictly greater than the
	// given seq, ordered by (Seq, ID), capped at 50 rows regardless of the
	// requested limit. A zero limit yields an empty result; a negative
	// limit fails closed with a PostgreSQL error.
	ListAfter(ctx context.Context, seq int64, limit int) ([]ConsoleLog, error)
	// DeleteBefore removes entries whose Seq is strictly less than the
	// given seq and reports how many rows were deleted.
	DeleteBefore(ctx context.Context, seq int64) (int64, error)
}
