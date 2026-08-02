package usage

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"gorouter/internal/domain/auth"
	"gorouter/internal/domain/usage"

	"github.com/google/uuid"
)

// UsagePipeline records metadata-only per-request usage and maintains the
// idempotent daily aggregates. Recording is runtime telemetry, not a
// configuration mutation: the actor is accepted for contract symmetry with
// audited mutations but is deliberately not audited, and no sensitive
// prompt/body/credential fields are ever stored.
type UsagePipeline struct {
	beginner      UsageScopeBeginner
	ring          *RecentRequestsRing
	retentionDays int
	now           func() time.Time
}

// NewUsagePipeline creates a usage pipeline with the 90-day retention window
// and the wall clock.
func NewUsagePipeline(beginner UsageScopeBeginner) *UsagePipeline {
	return &UsagePipeline{
		beginner:      beginner,
		retentionDays: usage.RetentionDays,
		now:           time.Now,
	}
}

// WithRing returns a copy of the pipeline that pushes each recorded history
// entry into the given process-local ring after the durable commit. The
// returned copy shares no mutable state with the receiver.
func (p *UsagePipeline) WithRing(r *RecentRequestsRing) *UsagePipeline {
	cp := *p
	cp.ring = r
	return &cp
}

// Record persists one immutable metadata-only request: the request detail
// row and the durable history entry are inserted in a single transaction;
// there is no update or delete path. The record is never mutated. A nil
// OccurredAt is stamped with the pipeline clock (UTC). When a ring is wired,
// the history entry is pushed after the commit.
func (p *UsagePipeline) Record(ctx context.Context, _ *auth.Actor, record usage.UsageRecord) error {
	occurredAt := p.now().UTC()
	if record.OccurredAt != nil {
		occurredAt = record.OccurredAt.UTC()
	}
	detail := usage.RequestDetail{
		ID:               uuid.New(),
		RequestID:        record.RequestID,
		ProviderID:       record.ProviderID,
		Model:            record.Model,
		PromptTokens:     record.PromptTokens,
		CompletionTokens: record.CompletionTokens,
		Cost:             record.Cost,
		Status:           record.Status,
		ErrorKind:        record.ErrorKind,
		OccurredAt:       &occurredAt,
		DebugOptIn:       record.DebugOptIn,
	}
	history := usage.RequestHistoryEntry{
		ID:               uuid.New(),
		Model:            record.Model,
		Provider:         record.Provider,
		PromptTokens:     record.PromptTokens,
		CompletionTokens: record.CompletionTokens,
		Status:           record.Status,
		OccurredAt:       &occurredAt,
	}

	scope, err := p.beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("usage: begin tx: %w", err)
	}
	defer func() { _ = scope.Rollback(ctx) }()

	if err := scope.Usage().WriteRequestDetail(ctx, &detail); err != nil {
		return fmt.Errorf("usage: write request detail: %w", err)
	}
	if err := scope.Usage().AppendHistory(ctx, &history); err != nil {
		return fmt.Errorf("usage: append history: %w", err)
	}
	if err := scope.Commit(ctx); err != nil {
		return fmt.Errorf("usage: commit tx: %w", err)
	}
	if p.ring != nil {
		p.ring.Push(history)
	}
	return nil
}

// AggregateDaily recomputes, idempotently, the daily usage aggregate for
// every UTC day represented in the immutable request details within the
// retention window (now - retentionDays). Days whose UTC start predates the
// cutoff are immutable: they are never read for recomputation and never
// rewritten. One aggregate row is upserted per day (day primary key); the
// aggregate is a whole-day summary (provider/model nil) with the request
// count, summed tokens and summed cost rounded to 6 decimal places. Details
// with a nil occurred_at have no day bucket and are excluded.
func (p *UsagePipeline) AggregateDaily(ctx context.Context) error {
	now := p.now().UTC()
	cutoff := now.AddDate(0, 0, -p.retentionDays)

	scope, err := p.beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("usage: begin tx: %w", err)
	}
	defer func() { _ = scope.Rollback(ctx) }()

	details, err := scope.Usage().DetailsBetween(ctx, cutoff)
	if err != nil {
		return fmt.Errorf("usage: load request details: %w", err)
	}
	aggs := computeDailyAggregates(details, cutoff)
	for i := range aggs {
		if err := scope.Usage().AggregateDaily(ctx, &aggs[i]); err != nil {
			return fmt.Errorf("usage: aggregate daily: %w", err)
		}
	}
	if err := scope.Commit(ctx); err != nil {
		return fmt.Errorf("usage: commit tx: %w", err)
	}
	return nil
}

// computeDailyAggregates buckets request details by UTC day and computes one
// whole-day aggregate per day. Days with a UTC start before the cutoff are
// immutable and are skipped. The result is ordered by day ascending.
func computeDailyAggregates(details []usage.RequestDetail, cutoff time.Time) []usage.DailyAggregate {
	buckets := make(map[time.Time][]usage.RequestDetail)
	for _, d := range details {
		if d.OccurredAt == nil {
			continue
		}
		day := d.OccurredAt.UTC().Truncate(24 * time.Hour)
		buckets[day] = append(buckets[day], d)
	}
	out := make([]usage.DailyAggregate, 0, len(buckets))
	for day, ds := range buckets {
		if day.Before(cutoff) {
			continue
		}
		agg := usage.DailyAggregate{Day: day}
		var requests int
		var prompt, completion int64
		var cost float64
		for _, d := range ds {
			requests++
			if d.PromptTokens != nil {
				prompt += int64(*d.PromptTokens)
			}
			if d.CompletionTokens != nil {
				completion += int64(*d.CompletionTokens)
			}
			if d.Cost != nil {
				cost += *d.Cost
			}
		}
		agg.Requests = &requests
		agg.PromptTokens = &prompt
		agg.CompletionTokens = &completion
		rounded := math.Round(cost*1e6) / 1e6
		agg.Cost = &rounded
		out = append(out, agg)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Day.Before(out[j].Day) })
	return out
}
