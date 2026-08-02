package usage

import (
	"context"
	"fmt"
	"time"

	"gorouter/internal/domain/usage"
)

// RetentionService enforces the 90-day usage retention window. It is the
// callable contract CH-08 wires to its scheduled usage-retention job: the
// job invokes PurgeBefore and records its own job provenance; the service
// itself performs no audit writes because retention never touches the audit
// log (#147) and recording usage is runtime telemetry.
type RetentionService struct {
	beginner UsageScopeBeginner
}

// NewRetentionService creates a retention service over the given scope
// beginner.
func NewRetentionService(beginner UsageScopeBeginner) *RetentionService {
	return &RetentionService{beginner: beginner}
}

// PurgeBefore deletes usage rows strictly older than the cutoff: request
// details and history entries with occurred_at < cutoff and daily aggregates
// with day < cutoff (all in UTC). Rows at or after the cutoff survive; the
// audit log is never modified. The purge is transactional and idempotent —
// repeating it with the same cutoff removes nothing.
func (s *RetentionService) PurgeBefore(ctx context.Context, cutoff time.Time) (usage.PurgeStats, error) {
	scope, err := s.beginner.Begin(ctx)
	if err != nil {
		return usage.PurgeStats{}, fmt.Errorf("usage: begin tx: %w", err)
	}
	defer func() { _ = scope.Rollback(ctx) }()

	stats, err := scope.Usage().PurgeBefore(ctx, cutoff)
	if err != nil {
		return usage.PurgeStats{}, fmt.Errorf("usage: purge before: %w", err)
	}
	if err := scope.Commit(ctx); err != nil {
		return usage.PurgeStats{}, fmt.Errorf("usage: commit tx: %w", err)
	}
	return stats, nil
}
