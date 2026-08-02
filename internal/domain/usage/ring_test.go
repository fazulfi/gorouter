package usage

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }

func entry(id uuid.UUID, model, provider string, prompt, completion int, at *time.Time) RequestHistoryEntry {
	e := RequestHistoryEntry{
		ID:               id,
		Model:            strPtr(model),
		Provider:         strPtr(provider),
		PromptTokens:     intPtr(prompt),
		CompletionTokens: intPtr(completion),
		OccurredAt:       at,
	}
	return e
}

func TestNewRing(t *testing.T) {
	if _, err := NewRing(0); err == nil {
		t.Error("capacity 0 must error")
	}
	if _, err := NewRing(-1); err == nil {
		t.Error("negative capacity must error")
	}
	r, err := NewRing(3)
	if err != nil {
		t.Fatalf("NewRing(3): %v", err)
	}
	if r.Capacity() != 3 || r.Len() != 0 || r.Initialized() {
		t.Errorf("fresh ring = cap %d len %d initialized %v", r.Capacity(), r.Len(), r.Initialized())
	}
}

func TestRingCapacityEviction(t *testing.T) {
	r, _ := NewRing(3)
	for i := 0; i < 5; i++ {
		r.Push(entry(uuid.New(), "m", "p", 1+i, 2+i, nil))
	}
	if r.Len() != 3 {
		t.Fatalf("len = %d, want 3", r.Len())
	}
	// The oldest pushed entries are evicted (FIFO trim): entries 0 and 1.
	if r.entries[0].PromptTokens == nil || *r.entries[0].PromptTokens != 3 {
		t.Errorf("oldest survivor prompt = %v, want 3", r.entries[0].PromptTokens)
	}
	if r.entries[2].PromptTokens == nil || *r.entries[2].PromptTokens != 5 {
		t.Errorf("newest survivor prompt = %v, want 5", r.entries[2].PromptTokens)
	}
}

func TestRingHydrateNewestFirst(t *testing.T) {
	r, _ := NewRing(50)
	var newestFirst []RequestHistoryEntry
	for i := 0; i < 60; i++ {
		at := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC).Add(-time.Duration(i) * time.Minute)
		newestFirst = append(newestFirst, entry(uuid.New(), "m", "p", i, i, &at))
	}
	r.Hydrate(newestFirst)
	if !r.Initialized() {
		t.Error("Hydrate must mark the ring initialized")
	}
	if r.Len() != 50 {
		t.Fatalf("len = %d, want 50", r.Len())
	}
	// Newest 50 held, stored chronologically: entry 49 (oldest kept) first,
	// entry 0 (newest) last.
	if got := *r.entries[0].PromptTokens; got != 49 {
		t.Errorf("oldest kept prompt = %d, want 49", got)
	}
	if got := *r.entries[49].PromptTokens; got != 0 {
		t.Errorf("newest kept prompt = %d, want 0", got)
	}
}

func TestRingHydrateReplaces(t *testing.T) {
	r, _ := NewRing(50)
	r.Hydrate([]RequestHistoryEntry{entry(uuid.New(), "m", "p", 1, 1, nil)})
	r.Hydrate([]RequestHistoryEntry{entry(uuid.New(), "m2", "p2", 2, 2, nil)})
	if r.Len() != 1 || *r.entries[0].PromptTokens != 2 {
		t.Errorf("hydrate must replace contents, got %d entries", r.Len())
	}
}

func TestRingMinuteBoundaries(t *testing.T) {
	r, _ := NewRing(50)
	// Same UTC minute -> same dedup key -> newest wins (12:00:59).
	old := time.Date(2026, 8, 3, 12, 0, 30, 0, time.UTC)
	newer := time.Date(2026, 8, 3, 12, 0, 59, 0, time.UTC)
	// Next minute -> distinct key -> both survive.
	next := time.Date(2026, 8, 3, 12, 1, 0, 0, time.UTC)
	e1 := entry(uuid.New(), "m", "p", 7, 8, &old)
	e2 := entry(uuid.New(), "m", "p", 7, 8, &newer)
	e3 := entry(uuid.New(), "m", "p", 7, 8, &next)
	r.Push(e1)
	r.Push(e2)
	r.Push(e3)
	out := r.Display()
	if len(out) != 2 {
		t.Fatalf("display len = %d, want 2 (minute dedup)", len(out))
	}
	if !out[0].OccurredAt.Equal(next) {
		t.Errorf("newest first = %v, want %v", out[0].OccurredAt, next)
	}
	// The same-minute duplicate collapsed with the newest occurrence winning.
	if !out[1].OccurredAt.Equal(newer) {
		t.Errorf("same-minute survivor = %v, want newest %v", out[1].OccurredAt, newer)
	}
	if !out[1].Minute.Equal(old.Truncate(time.Minute)) {
		t.Errorf("minute bucket = %v, want %v", out[1].Minute, old.Truncate(time.Minute))
	}
}

func TestRingNewestWinsTieBreakByID(t *testing.T) {
	r, _ := NewRing(50)
	at := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	lowID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	highID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	r.Push(entry(lowID, "m", "p", 7, 8, &at))
	r.Push(entry(highID, "m", "p", 7, 8, &at))
	out := r.Display()
	if len(out) != 1 {
		t.Fatalf("display len = %d, want 1", len(out))
	}
	if out[0].Minute.IsZero() || out[0].PromptTokens != 7 || out[0].CompletionTokens != 8 {
		t.Errorf("survivor = %+v", out[0])
	}
	// Same minute/tokens/model/provider -> identical display row either way;
	// the ring itself must have kept both entries (dedup is display-only).
	if r.Len() != 2 {
		t.Errorf("ring len = %d, want 2 (display dedup must not mutate the ring)", r.Len())
	}
}

func TestRingZeroAndOneTokenRules(t *testing.T) {
	r, _ := NewRing(50)
	at := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	plus1 := at.Add(time.Minute)
	plus2 := at.Add(2 * time.Minute)
	r.Push(entry(uuid.New(), "m0", "p", 0, 0, &at))
	r.Push(entry(uuid.New(), "m1", "p", 0, 5, &plus1))
	r.Push(entry(uuid.New(), "m2", "p", 5, 0, &plus2))
	out := r.Display()
	if len(out) != 2 {
		t.Fatalf("display len = %d, want 2 (only both-zero discarded)", len(out))
	}
	if out[0].PromptTokens != 5 || out[0].CompletionTokens != 0 {
		t.Errorf("one-zero row must be kept: %+v", out[0])
	}
	if out[1].PromptTokens != 0 || out[1].CompletionTokens != 5 {
		t.Errorf("zero-one row must be kept: %+v", out[1])
	}
}

func TestRingNilTokenMapping(t *testing.T) {
	r, _ := NewRing(50)
	at := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	nilTokens := RequestHistoryEntry{ID: uuid.New(), Model: strPtr("m"), Provider: strPtr("p"), OccurredAt: &at}
	r.Push(nilTokens)
	if got := r.Display(); len(got) != 0 {
		t.Errorf("nil tokens map to 0/0 and must be discarded, got %d rows", len(got))
	}
}

func TestRingNilTimestampSortsLast(t *testing.T) {
	r, _ := NewRing(50)
	at := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	timed := entry(uuid.New(), "m", "p", 7, 8, &at)
	untimed := entry(uuid.New(), "m2", "p", 7, 8, nil)
	r.Push(untimed)
	r.Push(timed)
	out := r.Display()
	if len(out) != 2 {
		t.Fatalf("display len = %d, want 2", len(out))
	}
	if out[0].OccurredAt.IsZero() {
		t.Error("timed entry must sort first")
	}
	if !out[1].OccurredAt.IsZero() || !out[1].Minute.IsZero() {
		t.Errorf("nil-timestamp entry must sort last with zero minute, got %+v", out[1])
	}
}

func TestRingDisplayCapExact(t *testing.T) {
	r, _ := NewRing(50)
	base := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 60; i++ {
		at := base.Add(-time.Duration(i) * time.Minute)
		r.Push(entry(uuid.New(), "m", "p", 10+i, 20+i, &at))
	}
	if r.Len() != 50 {
		t.Fatalf("ring len = %d, want 50", r.Len())
	}
	out := r.Display()
	if len(out) != DisplayLimit {
		t.Fatalf("display len = %d, want %d", len(out), DisplayLimit)
	}
	for i := 1; i < len(out); i++ {
		if out[i-1].OccurredAt.Before(out[i].OccurredAt) {
			t.Fatalf("display not newest-first at %d", i)
		}
	}
}

func TestRingDisplayDoesNotMutate(t *testing.T) {
	r, _ := NewRing(50)
	at := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	r.Push(entry(uuid.New(), "m", "p", 7, 8, &at))
	before := r.Len()
	first := r.Display()
	second := r.Display()
	if r.Len() != before {
		t.Errorf("Display mutated the ring: len %d -> %d", before, r.Len())
	}
	if len(first) != len(second) {
		t.Error("Display must be deterministic")
	}
}
