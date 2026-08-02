package console

import (
	"context"
	"fmt"
	"time"
)

// RetentionService enforces the console-log retention window (90 days). It
// is the callable contract the scheduler retention job invokes: the job
// passes cutoff = now − console.RetentionDays and records its own job
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

// PurgeBefore deletes console-log rows strictly older than the cutoff:
// rows whose retention_until is strictly before the supplied cutoff (the
// 90-day window, caller-computed as now − console.RetentionDays) are
// removed; rows at or after the cutoff survive. The purge is transactional
// and idempotent — repeating it with the same cutoff removes nothing.
func (s *RetentionService) PurgeBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	scope, err := s.beginner.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("console: begin tx: %w", err)
	}
	defer func() { _ = scope.Rollback(ctx) }()

	deleted, err := scope.ConsoleLogs().PurgeBefore(ctx, cutoff)
	if err != nil {
		return 0, fmt.Errorf("console: purge before: %w", err)
	}
	if err := scope.Commit(ctx); err != nil {
		return 0, fmt.Errorf("console: commit tx: %w", err)
	}
	return deleted, nil
}
