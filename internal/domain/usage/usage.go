// Package usage defines pure domain contracts for usage aggregates,
// metadata-only request details, and the durable request history that
// backs the Recent Requests surface.
package usage

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strconv"
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
	// DetailsBetween returns the metadata-only request details whose
	// occurred_at is at or after since, ordered by occurred_at ascending
	// (NULLs last) and id ascending. It powers idempotent daily aggregation.
	DetailsBetween(ctx context.Context, since time.Time) ([]RequestDetail, error)
	// PurgeBefore deletes usage rows strictly older than the cutoff: request
	// details and history entries with occurred_at < cutoff and daily
	// aggregates with day < cutoff. The audit log is never touched.
	PurgeBefore(ctx context.Context, cutoff time.Time) (PurgeStats, error)
}

// Frozen usage-pipeline constants (BE-12; audit 13).
const (
	// DefaultRingCapacity is the exact capacity of the process-local Recent
	// Requests ring and the default durable-history fetch limit.
	DefaultRingCapacity = 50
	// DisplayLimit is the maximum number of rows the display pipeline emits.
	DisplayLimit = 20
	// RetentionDays is the usage retention window: daily aggregates older
	// than this are immutable and are never recomputed; rows older than this
	// are purged by RetentionService.
	RetentionDays = 90
)

// UsageRecord is the metadata-only per-request payload recorded by
// UsagePipeline.Record. Request payloads are never persisted; all fields are
// nullable except DebugOptIn, mirroring RequestDetail. OccurredAt is the
// request timestamp; when nil the pipeline stamps the record at write time.
type UsageRecord struct {
	RequestID        *uuid.UUID
	ProviderID       *uuid.UUID
	Provider         *string
	Model            *string
	PromptTokens     *int
	CompletionTokens *int
	Cost             *float64
	Status           *string
	ErrorKind        *string
	OccurredAt       *time.Time
	DebugOptIn       bool
}

// RingDisplayEntry is one row of the Recent Requests display pipeline output:
// the immutable history entry mapped to its display projection (prompt and
// completion token counts) plus its UTC minute bucket.
type RingDisplayEntry struct {
	Model            *string
	Provider         *string
	PromptTokens     int
	CompletionTokens int
	Status           *string
	OccurredAt       time.Time // original timestamp in UTC; zero when absent
	Minute           time.Time // UTC minute bucket; zero when absent
}

// PurgeStats reports the number of rows removed by a retention purge.
type PurgeStats struct {
	DetailsPurged int64
	HistoryPurged int64
	DailyPurged   int64
}

// Ring is the pure, process-local Recent Requests buffer (audit 13). It is
// deliberately NOT persisted: durable request history is the authoritative
// source and hydrates the ring at startup and on refresh.
//
// Storage order is chronological (oldest first, append order); the display
// pipeline re-sorts newest-first. Capacity eviction drops the oldest pushed
// entries (FIFO trim).
type Ring struct {
	capacity    int
	entries     []RequestHistoryEntry
	initialized bool
}

// NewRing creates a ring with the given capacity. The capacity must be at
// least 1.
func NewRing(capacity int) (*Ring, error) {
	if capacity < 1 {
		return nil, fmt.Errorf("usage: ring capacity must be >= 1, got %d", capacity)
	}
	return &Ring{capacity: capacity}, nil
}

// Capacity returns the ring capacity.
func (r *Ring) Capacity() int { return r.capacity }

// Len returns the number of entries currently held.
func (r *Ring) Len() int { return len(r.entries) }

// Initialized reports whether the ring has been hydrated from durable
// history at least once (Hydrate / Refresh).
func (r *Ring) Initialized() bool { return r.initialized }

// Push appends one entry at the end (newest position) and trims the oldest
// entries beyond capacity.
func (r *Ring) Push(entry RequestHistoryEntry) {
	r.entries = append(r.entries, entry)
	if len(r.entries) > r.capacity {
		r.entries = r.entries[len(r.entries)-r.capacity:]
	}
}

// Hydrate replaces the ring contents from durable history. The input is
// newest-first (the repository contract); the ring stores the newest
// `capacity` entries in chronological order (oldest first) so subsequent
// pushes append at the newest position and FIFO trim drops the oldest.
func (r *Ring) Hydrate(entries []RequestHistoryEntry) {
	end := len(entries)
	if end > r.capacity {
		end = r.capacity
	}
	chrono := make([]RequestHistoryEntry, 0, end)
	for i := end - 1; i >= 0; i-- {
		chrono = append(chrono, entries[i])
	}
	r.entries = chrono
	r.initialized = true
}

// Display runs the exact audit-13 display pipeline over the current ring
// contents:
//
//  1. newest-first ordering (occurred_at DESC, NULLs last; id DESC tie-break);
//  2. token mapping: prompt and completion counts (nil maps to 0);
//  3. discard entries where both token counts are zero;
//  4. dedup on the exact key model|provider|promptTokens|completionTokens|minute
//     with the minute bucket in UTC (newest occurrence wins);
//  5. slice at most DisplayLimit rows.
//
// The returned slice is a copy; the ring is never mutated by Display.
func (r *Ring) Display() []RingDisplayEntry {
	entries := make([]RequestHistoryEntry, len(r.entries))
	copy(entries, r.entries)
	sort.Slice(entries, func(i, j int) bool {
		return historyNewer(entries[i], entries[j])
	})

	seen := make(map[string]struct{}, len(entries))
	out := make([]RingDisplayEntry, 0, min(len(entries), DisplayLimit))
	for _, e := range entries {
		prompt := 0
		if e.PromptTokens != nil {
			prompt = *e.PromptTokens
		}
		completion := 0
		if e.CompletionTokens != nil {
			completion = *e.CompletionTokens
		}
		if prompt == 0 && completion == 0 {
			continue
		}
		occurred, minute := time.Time{}, time.Time{}
		key := displayKey(e, &prompt, &completion, &occurred, &minute)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, RingDisplayEntry{
			Model:            e.Model,
			Provider:         e.Provider,
			PromptTokens:     prompt,
			CompletionTokens: completion,
			Status:           e.Status,
			OccurredAt:       occurred,
			Minute:           minute,
		})
		if len(out) == DisplayLimit {
			break
		}
	}
	return out
}

// historyNewer reports whether a sorts strictly before b in newest-first
// order: occurred_at DESC with NULLs last, then id DESC.
func historyNewer(a, b RequestHistoryEntry) bool {
	if a.OccurredAt == nil || b.OccurredAt == nil {
		if a.OccurredAt == nil && b.OccurredAt != nil {
			return false
		}
		if a.OccurredAt != nil && b.OccurredAt == nil {
			return true
		}
	} else if !a.OccurredAt.Equal(*b.OccurredAt) {
		return a.OccurredAt.After(*b.OccurredAt)
	}
	return bytes.Compare(a.ID[:], b.ID[:]) > 0
}

// displayKey builds the exact dedup key
// model|provider|promptTokens|completionTokens|minute with the minute bucket
// in UTC ("" when the timestamp is absent). The key-free pointers are filled
// for the display row in the same pass.
func displayKey(e RequestHistoryEntry, prompt, completion *int, occurred, minute *time.Time) string {
	model := ""
	if e.Model != nil {
		model = *e.Model
	}
	provider := ""
	if e.Provider != nil {
		provider = *e.Provider
	}
	if e.OccurredAt != nil {
		*occurred = e.OccurredAt.UTC()
		*minute = (*occurred).Truncate(time.Minute)
	}
	mk := ""
	if !minute.IsZero() {
		mk = minute.Format("2006-01-02T15:04")
	}
	return model + "|" + provider + "|" + strconv.Itoa(*prompt) + "|" + strconv.Itoa(*completion) + "|" + mk
}
