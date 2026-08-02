package console

import (
	"context"
	"fmt"
	"time"

	"gorouter/internal/domain/console"
	"gorouter/internal/shared/redact"
)

// ConsoleService exposes the redacted console-log projection: log lines are
// written through Append (redacted at the boundary, durably persisted,
// strictly ordered) and replayed through ListAfter for the realtime console
// init. Only redacted text is ever persisted or exposed — raw console
// lines, request bodies, prompts, credentials and secrets never reach
// storage (decision #127 no-secrets contract).
type ConsoleService struct {
	beginner ConsoleScopeBeginner
	now      func() time.Time
}

// NewConsoleService creates a console service over the given scope
// beginner.
func NewConsoleService(beginner ConsoleScopeBeginner) *ConsoleService {
	return &ConsoleService{beginner: beginner, now: time.Now}
}

// WithClock replaces the internal clock (tests only; the default is
// time.Now). All timestamps are UTC.
func (s *ConsoleService) WithClock(now func() time.Time) { s.now = now }

// Append redacts the entry's message text, assigns the next strictly
// increasing sequence number, stamps the 90-day retention boundary and
// persists the entry in one transaction. Only the redacted text is stored:
// RedactedMessage is always populated and Message (when present) is
// overwritten with the same redacted text, so raw input never reaches the
// database.
//
// The sequence number is derived from the persisted watermark (MaxSeq + 1),
// which keeps sequences strictly increasing across process restarts: a
// fresh service instance continues from the durable watermark. A caller
// supplied Seq is ignored; the assigned Seq, RedactedMessage, OccurredAt
// (when nil) and RetentionUntil (when nil) are written back to the entry.
func (s *ConsoleService) Append(ctx context.Context, entry *console.ConsoleLog) error {
	if entry == nil {
		return fmt.Errorf("console: entry is required")
	}
	redacted := ""
	if entry.Message != nil {
		redacted = redact.Redact(*entry.Message)
		entry.Message = &redacted
	}
	entry.RedactedMessage = redacted

	scope, err := s.beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("console: begin tx: %w", err)
	}
	defer func() { _ = scope.Rollback(ctx) }()

	max, err := scope.ConsoleLogs().MaxSeq(ctx)
	if err != nil {
		return fmt.Errorf("console: max seq: %w", err)
	}
	entry.Seq = max + 1
	now := s.now().UTC()
	if entry.OccurredAt == nil {
		entry.OccurredAt = &now
	}
	if entry.RetentionUntil == nil {
		until := now.Add(console.RetentionDays * 24 * time.Hour)
		entry.RetentionUntil = &until
	}
	if err := scope.ConsoleLogs().Append(ctx, entry); err != nil {
		return fmt.Errorf("console: append: %w", err)
	}
	if err := scope.Commit(ctx); err != nil {
		return fmt.Errorf("console: commit tx: %w", err)
	}
	return nil
}

// ListAfter returns the entries whose Seq is strictly greater than the
// given seq, in ascending sequence order — the realtime console init
// replay contract. The limit is normalized to the default cap (50):
// non-positive values default to 50 and values above 50 are capped at 50,
// so the repository's fail-closed negative-limit error is unreachable
// through the service. The returned entries expose only redacted text.
func (s *ConsoleService) ListAfter(ctx context.Context, seq int64, limit int) ([]console.ConsoleLog, error) {
	if limit <= 0 || limit > console.DefaultCap {
		limit = console.DefaultCap
	}
	scope, err := s.beginner.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("console: begin tx: %w", err)
	}
	defer func() { _ = scope.Rollback(ctx) }()

	entries, err := scope.ConsoleLogs().ListAfter(ctx, seq, limit)
	if err != nil {
		return nil, fmt.Errorf("console: list after: %w", err)
	}
	return entries, nil
}
