package console

import (
	"context"
	"fmt"
	"time"
)

// RetentionService enforces the console-log retention window (90 days).
// The formula is pinned by the service pair: Append stamps every row with
// RetentionUntil = append time + RetentionDays, and PurgeExpired(now)
// deletes rows whose RetentionUntil is strictly before now. The retention
// job therefore invokes PurgeExpired(ctx, time.Now()) — the current time,
// never now − RetentionDays, which would subtract the window a second time
// and let rows live ~180 days instead of 90. The job records its own job
// provenance; the service itself performs no audit writes because
// retention never touches the audit log (#147).
type RetentionService struct {
	beginner ConsoleScopeBeginner
}

// NewRetentionService creates a console retention service over the given
// scope beginner.
func NewRetentionService(beginner ConsoleScopeBeginner) *RetentionService {
	return &RetentionService{beginner: beginner}
}

// PurgeExpired deletes console-log rows whose 90-day retention window has
// elapsed: rows with RetentionUntil strictly before now are removed; rows
// at or after now survive (strict < cutoff, so a row expiring exactly at
// now is retained until the next run). The purge is transactional and
// idempotent — repeating it with the same time removes nothing.
func (s *RetentionService) PurgeExpired(ctx context.Context, now time.Time) (int64, error) {
	scope, err := s.beginner.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("console: begin tx: %w", err)
	}
	defer func() { _ = scope.Rollback(ctx) }()

	deleted, err := scope.ConsoleLogs().PurgeBefore(ctx, now)
	if err != nil {
		return 0, fmt.Errorf("console: purge before: %w", err)
	}
	if err := scope.Commit(ctx); err != nil {
		return 0, fmt.Errorf("console: commit tx: %w", err)
	}
	return deleted, nil
}
