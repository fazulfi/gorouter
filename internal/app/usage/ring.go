package usage

import (
	"context"
	"sync"

	"gorouter/internal/domain/usage"
)

// RecentSource supplies the durable history used to hydrate the ring.
// *RequestHistoryService satisfies it.
type RecentSource interface {
	Recent(ctx context.Context, limit int) ([]usage.RequestHistoryEntry, error)
}

// RecentRequestsRing is the process-local Recent Requests buffer (audit 13):
// capacity exactly 50, hydrated from durable history at startup (Hydrate) and
// on refresh (Refresh), maintained for realtime display by Push. The ring is
// deliberately NOT persisted — gorouter_request_history is the authoritative
// source. All accessors are safe for concurrent use.
type RecentRequestsRing struct {
	mu     sync.RWMutex
	ring   *usage.Ring
	source RecentSource
}

// NewRecentRequestsRing creates a ring with the frozen capacity of 50 backed
// by the given durable source.
func NewRecentRequestsRing(source RecentSource) *RecentRequestsRing {
	r, err := usage.NewRing(usage.DefaultRingCapacity)
	if err != nil {
		panic("usage: " + err.Error())
	}
	return &RecentRequestsRing{ring: r, source: source}
}

// Capacity returns the ring capacity (exactly 50).
func (r *RecentRequestsRing) Capacity() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.ring.Capacity()
}

// Len returns the number of entries currently held.
func (r *RecentRequestsRing) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.ring.Len()
}

// Initialized reports whether the ring has been hydrated from durable
// history at least once.
func (r *RecentRequestsRing) Initialized() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.ring.Initialized()
}

// Push appends one entry (FIFO trim beyond capacity). Typically called after
// a durable history append so the realtime display stays current.
func (r *RecentRequestsRing) Push(entry usage.RequestHistoryEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ring.Push(entry)
}

// Hydrate replaces the ring contents from durable history entries given
// newest-first (the repository contract).
func (r *RecentRequestsRing) Hydrate(entries []usage.RequestHistoryEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ring.Hydrate(entries)
}

// Refresh pulls the newest 50 entries from the durable source and hydrates
// the ring. On source failure the current ring state is preserved.
func (r *RecentRequestsRing) Refresh(ctx context.Context) error {
	entries, err := r.source.Recent(ctx, usage.DefaultRingCapacity)
	if err != nil {
		return err
	}
	r.Hydrate(entries)
	return nil
}

// Display runs the exact audit-13 display pipeline over the current ring
// contents: newest-first, token mapping (prompt + completion), discard
// entries where both token counts are zero, dedup on
// model|provider|promptTokens|completionTokens|minute (UTC, newest wins),
// capped at 20 rows.
func (r *RecentRequestsRing) Display() []usage.RingDisplayEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.ring.Display()
}
