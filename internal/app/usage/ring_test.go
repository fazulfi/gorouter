package usage

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"gorouter/internal/domain/usage"

	"github.com/google/uuid"
)

var ringBase = time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)

func ringEntry(model, provider string, prompt, completion int, at time.Time) usage.RequestHistoryEntry {
	return usage.RequestHistoryEntry{
		ID:               uuid.New(),
		Model:            &model,
		Provider:         &provider,
		PromptTokens:     &prompt,
		CompletionTokens: &completion,
		OccurredAt:       &at,
	}
}

func ringKey(model, provider string, prompt, completion int, minute time.Time) string {
	return fmt.Sprintf("%s|%s|%d|%d|%s", model, provider, prompt, completion, minute.UTC().Format("2006-01-02T15:04"))
}

// TestRingDisplayPipeline is the plan's one-behavior test (BE-12, P4-T07): it
// feeds the ring exactly 60 entries and asserts the exact audit-13 display
// pipeline — newest-first ordering, token mapping (prompt + completion),
// discard entries where both token counts are zero, dedup on the exact key
// model|provider|promptTokens|completionTokens|minute (UTC, newest wins),
// and a display cap of 20.
func TestRingDisplayPipeline(t *testing.T) {
	ring, err := usage.NewRing(usage.DefaultRingCapacity)
	if err != nil {
		t.Fatalf("NewRing: %v", err)
	}

	uniq := func(i int) (model, provider string, prompt, completion int, at time.Time) {
		model = fmt.Sprintf("m%02d", i)
		provider = fmt.Sprintf("p%02d", i)
		prompt = 10 + i
		completion = 20 + i
		at = ringBase.Add(-time.Duration(i) * time.Minute)
		if i == 6 {
			prompt = 0
			completion = 50
		}
		if i == 8 {
			model = ""
		}
		if i == 15 {
			provider = ""
		}
		return
	}

	pushed := 0
	// Duplicates of the newest entries pushed first (evicted by capacity).
	for i := 0; i < 10; i++ {
		m, p, pt, ct, at := uniq(i)
		ring.Push(ringEntry(m, p, pt, ct, at))
		pushed++
	}
	// Duplicates of entries 10..14 (held, then deduped on display).
	for i := 10; i < 15; i++ {
		m, p, pt, ct, at := uniq(i)
		ring.Push(ringEntry(m, p, pt, ct, at))
		pushed++
	}
	// Five zero-token entries with the newest timestamps: both token counts
	// are zero so they must be discarded even though they sort first.
	for i := 0; i < 5; i++ {
		ring.Push(ringEntry("z-model", "z-prov", 0, 0, ringBase.Add(time.Duration(i)*time.Minute)))
		pushed++
	}
	// Forty unique entries.
	for i := 0; i < 40; i++ {
		m, p, pt, ct, at := uniq(i)
		ring.Push(ringEntry(m, p, pt, ct, at))
		pushed++
	}
	if pushed != 60 {
		t.Fatalf("fed %d entries, want 60", pushed)
	}
	if ring.Len() != usage.DefaultRingCapacity {
		t.Fatalf("ring len = %d, want capacity %d", ring.Len(), usage.DefaultRingCapacity)
	}

	got := ring.Display()
	if len(got) != usage.DisplayLimit {
		t.Fatalf("display len = %d, want %d", len(got), usage.DisplayLimit)
	}

	seen := make(map[string]int)
	for k, row := range got {
		key := ringKey(derefStr(row.Model), derefStr(row.Provider), row.PromptTokens, row.CompletionTokens, row.Minute)
		seen[key]++
		if row.PromptTokens == 0 && row.CompletionTokens == 0 {
			t.Fatalf("row %d: both-token-zero entry survived: %+v", k, row)
		}
		if k > 0 && !row.OccurredAt.Before(got[k-1].OccurredAt) {
			t.Fatalf("row %d: not newest-first (%v after %v)", k, row.OccurredAt, got[k-1].OccurredAt)
		}
	}

	// Expected top-20 after dedup and zero-token drops: entries 0..19.
	for k := 0; k < usage.DisplayLimit; k++ {
		_, _, pt, ct, at := uniq(k)
		wantKey := ringKey(fmt.Sprintf("m%02d", k), fmt.Sprintf("p%02d", k), pt, ct, at)
		if k == 8 {
			wantKey = ringKey("", fmt.Sprintf("p%02d", k), pt, ct, at)
		}
		if k == 15 {
			wantKey = ringKey(fmt.Sprintf("m%02d", k), "", pt, ct, at)
		}
		if got[k].PromptTokens != pt || got[k].CompletionTokens != ct {
			t.Fatalf("row %d tokens = %d/%d, want %d/%d", k, got[k].PromptTokens, got[k].CompletionTokens, pt, ct)
		}
		if !got[k].OccurredAt.Equal(at) || !got[k].Minute.Equal(at.Truncate(time.Minute)) {
			t.Fatalf("row %d time = %v (min %v), want %v", k, got[k].OccurredAt, got[k].Minute, at)
		}
		if seen[wantKey] != 1 {
			t.Fatalf("row %d key %q seen %d times, want exactly 1 (dedup)", k, wantKey, seen[wantKey])
		}
	}

	// Duplicates of entries 10..14 collapsed: exactly one survivor per key.
	for i := 10; i < 15; i++ {
		_, _, pt, ct, at := uniq(i)
		key := ringKey(fmt.Sprintf("m%02d", i), fmt.Sprintf("p%02d", i), pt, ct, at)
		if seen[key] != 1 {
			t.Errorf("dup key %q seen %d times, want 1", key, seen[key])
		}
	}
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// fakeRingSource serves canned newest-first entries to the ring.
type fakeRingSource struct {
	entries []usage.RequestHistoryEntry
	err     error
	limit   int
}

func (f *fakeRingSource) Recent(_ context.Context, limit int) ([]usage.RequestHistoryEntry, error) {
	f.limit = limit
	if f.err != nil {
		return nil, f.err
	}
	return f.entries, nil
}

func TestRecentRequestsRingCapacityExact(t *testing.T) {
	ring := NewRecentRequestsRing(&fakeRingSource{})
	if ring.Capacity() != usage.DefaultRingCapacity {
		t.Errorf("capacity = %d, want exactly %d", ring.Capacity(), usage.DefaultRingCapacity)
	}
	if ring.Len() != 0 || ring.Initialized() {
		t.Error("fresh ring must be empty and uninitialized")
	}
}

func TestRecentRequestsRingRefresh(t *testing.T) {
	at := ringBase
	entries := []usage.RequestHistoryEntry{
		ringEntry("m0", "p0", 1, 2, at),
		ringEntry("m1", "p1", 3, 4, at.Add(-time.Minute)),
		ringEntry("m2", "p2", 5, 6, at.Add(-2*time.Minute)),
	}
	src := &fakeRingSource{entries: entries}
	ring := NewRecentRequestsRing(src)
	if err := ring.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if src.limit != usage.DefaultRingCapacity {
		t.Errorf("Refresh fetched limit %d, want %d", src.limit, usage.DefaultRingCapacity)
	}
	if !ring.Initialized() || ring.Len() != 3 {
		t.Errorf("after Refresh: initialized %v len %d", ring.Initialized(), ring.Len())
	}
	got := ring.Display()
	if len(got) != 3 || !got[0].OccurredAt.Equal(at) {
		t.Errorf("display not newest-first after refresh: %+v", got)
	}
}

func TestRecentRequestsRingRefreshLarge(t *testing.T) {
	var entries []usage.RequestHistoryEntry
	for i := 0; i < 60; i++ {
		entries = append(entries, ringEntry("m", "p", i, i, ringBase.Add(-time.Duration(i)*time.Minute)))
	}
	ring := NewRecentRequestsRing(&fakeRingSource{entries: entries})
	if err := ring.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if ring.Len() != usage.DefaultRingCapacity {
		t.Errorf("len = %d, want %d (newest 50 kept)", ring.Len(), usage.DefaultRingCapacity)
	}
}

func TestRecentRequestsRingRefreshErrorPreservesState(t *testing.T) {
	src := &fakeRingSource{err: errors.New("db down")}
	ring := NewRecentRequestsRing(src)
	if err := ring.Refresh(context.Background()); err == nil {
		t.Fatal("Refresh must propagate source errors")
	}
	if ring.Len() != 0 || ring.Initialized() {
		t.Error("failed refresh must not mutate the ring")
	}
	ring.Push(ringEntry("m", "p", 1, 2, ringBase))
	if err := ring.Refresh(context.Background()); err == nil {
		t.Fatal("Refresh must propagate source errors even when populated")
	}
	if ring.Len() != 1 {
		t.Errorf("failed refresh must preserve prior contents, len = %d", ring.Len())
	}
}

func TestRecentRequestsRingHydrateReplaces(t *testing.T) {
	ring := NewRecentRequestsRing(&fakeRingSource{})
	ring.Hydrate([]usage.RequestHistoryEntry{ringEntry("m", "p", 1, 2, ringBase)})
	if !ring.Initialized() || ring.Len() != 1 {
		t.Fatalf("hydrate: initialized %v len %d", ring.Initialized(), ring.Len())
	}
	ring.Hydrate([]usage.RequestHistoryEntry{ringEntry("m2", "p2", 3, 4, ringBase.Add(-time.Minute))})
	if ring.Len() != 1 || *ring.Display()[0].Model != "m2" {
		t.Errorf("second hydrate must replace, len %d", ring.Len())
	}
}

func TestRecentRequestsRingConcurrent(t *testing.T) {
	ring := NewRecentRequestsRing(&fakeRingSource{})
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				at := ringBase.Add(-time.Duration(seed*1000+i) * time.Minute)
				ring.Push(ringEntry("m", "p", i, i+seed, at))
				_ = ring.Display()
				_ = ring.Len()
				_ = ring.Capacity()
				_ = ring.Initialized()
			}
		}(g)
	}
	wg.Wait()
	if ring.Len() != usage.DefaultRingCapacity {
		t.Errorf("concurrent pushes: len = %d, want %d", ring.Len(), usage.DefaultRingCapacity)
	}
	got := ring.Display()
	if len(got) != usage.DisplayLimit {
		t.Errorf("concurrent display: len = %d, want %d", len(got), usage.DisplayLimit)
	}
}
