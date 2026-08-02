package usage

import (
	"context"
	"fmt"

	"gorouter/internal/domain/usage"

	"github.com/google/uuid"
)

// RequestHistoryService reads and appends the durable request history that
// backs the Recent Requests surface. Appending is runtime telemetry and is
// deliberately not audited.
type RequestHistoryService struct {
	beginner UsageScopeBeginner
}

// NewRequestHistoryService creates a request-history service over the given
// scope beginner.
func NewRequestHistoryService(beginner UsageScopeBeginner) *RequestHistoryService {
	return &RequestHistoryService{beginner: beginner}
}

// Append durably appends one history entry (insert-only; there is no update
// or delete path). A zero entry ID is assigned here; all other fields are
// preserved as given.
func (s *RequestHistoryService) Append(ctx context.Context, entry usage.RequestHistoryEntry) error {
	if entry.ID == uuid.Nil {
		entry.ID = uuid.New()
	}
	scope, err := s.beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("usage: begin tx: %w", err)
	}
	defer func() { _ = scope.Rollback(ctx) }()

	if err := scope.Usage().AppendHistory(ctx, &entry); err != nil {
		return fmt.Errorf("usage: append history: %w", err)
	}
	if err := scope.Commit(ctx); err != nil {
		return fmt.Errorf("usage: commit tx: %w", err)
	}
	return nil
}

// Recent returns the newest history entries from the durable history,
// newest first (occurred_at DESC, NULLs last, id DESC). Non-positive limits
// default to 50; the durable source caps the result at 50 rows regardless of
// the requested limit.
func (s *RequestHistoryService) Recent(ctx context.Context, limit int) ([]usage.RequestHistoryEntry, error) {
	if limit <= 0 {
		limit = usage.DefaultRingCapacity
	}
	scope, err := s.beginner.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("usage: begin tx: %w", err)
	}
	defer func() { _ = scope.Rollback(ctx) }()

	entries, err := scope.Usage().RecentHistory(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("usage: recent history: %w", err)
	}
	return entries, nil
}
