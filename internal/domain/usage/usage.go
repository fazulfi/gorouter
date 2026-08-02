// Package usage defines pure domain contracts for usage aggregates,
// metadata-only request details, and the durable request history that
// backs the Recent Requests surface.
package usage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// DailyAggregate is a per-day usage summary. The storage key is
// (day, provider_id, model_id); a day has at most one aggregate row
// (the day column is the table's primary key).
type DailyAggregate struct {
	Day              time.Time
	ProviderID       *uuid.UUID
	ModelID          *string
	Requests         *int
	PromptTokens     *int64
	CompletionTokens *int64
	Cost             *float64
}

// RequestDetail is a metadata-only per-request record. Request payloads are
// never persisted; DebugOptIn marks requests whose caller opted into debug
// metadata collection. All fields except the ID and DebugOptIn are nullable.
type RequestDetail struct {
	ID               uuid.UUID
	RequestID        *uuid.UUID
	ProviderID       *uuid.UUID
	Model            *string
	PromptTokens     *int
	CompletionTokens *int
	Cost             *float64
	Status           *string
	ErrorKind        *string
	OccurredAt       *time.Time
	DebugOptIn       bool
}

// RequestHistoryEntry is a compact durable record powering the Recent
// Requests display. The in-process display ring is NOT persisted; this
// durable history is the authoritative source used to hydrate it.
type RequestHistoryEntry struct {
	ID               uuid.UUID
	Model            *string
	Provider         *string
	PromptTokens     *int
	CompletionTokens *int
	Status           *string
	OccurredAt       *time.Time
}

// UsageRepository defines persistence operations for daily usage aggregates,
// metadata-only request details, and the durable request history.
//
// Token filtering (e.g. discarding entries where both token counts are zero)
// is a service-layer concern and is deliberately NOT applied here: the
// repository stores and returns rows faithfully.
type UsageRepository interface {
	// AggregateDaily writes or updates the daily aggregate row for the given
	// day. A second write for the same day replaces the stored values
	// (upsert semantics driven by the day primary key).
	AggregateDaily(ctx context.Context, agg *DailyAggregate) error
	// Daily returns the aggregate rows for the given day.
	Daily(ctx context.Context, day time.Time) ([]DailyAggregate, error)
	// WriteRequestDetail stores a metadata-only request detail.
	WriteRequestDetail(ctx context.Context, detail *RequestDetail) error
	// RequestDetail returns a single detail by ID, or nil when absent.
	RequestDetail(ctx context.Context, id uuid.UUID) (*RequestDetail, error)
	// AppendHistory appends a durable request history entry.
	AppendHistory(ctx context.Context, entry *RequestHistoryEntry) error
	// RecentHistory returns the newest history entries, newest first,
	// capped at 50 rows regardless of the requested limit.
	RecentHistory(ctx context.Context, limit int) ([]RequestHistoryEntry, error)
}
