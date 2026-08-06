package realtime

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestProvidersStream asserts the providers stream registry contract: an
// initial full-array `providers` push, then a quiet `providerStatus` event
// only for providers whose status or cooldown changed, keepalive comments,
// and cleanup on disconnect.
func TestProvidersStream(t *testing.T) {
	cd := time.Now().Add(15 * time.Minute).UTC()
	src := newFakeProvidersSource()
	src.statuses = []ProviderStatusPayload{
		{Provider: "openai", Status: "healthy"},
		{Provider: "claude", Status: "healthy"},
	}

	h := NewProvidersStream(src, 5*time.Millisecond)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/v1/providers/stream", nil)

	frames, _ := framesOf(t, h, req, func() {
		src.mu.Lock()
		src.statuses = []ProviderStatusPayload{
			{Provider: "openai", Status: "healthy"},
			{Provider: "claude", Status: "cooldown", CooldownUntil: &cd},
		}
		src.mu.Unlock()
		src.notify()
	}, 40*time.Millisecond)

	events := eventSequence(t, frames)
	assertEventAt(t, events, 0, ProvidersInitEvent)

	var init ProvidersStatusPayload
	mustUnmarshal(t, []byte(frameDataFor(t, frames, ProvidersInitEvent, 0)), &init)
	if len(init.Providers) != 2 {
		t.Fatalf("init providers = %d, want 2", len(init.Providers))
	}

	// Only claude changed, so exactly one providerStatus event is emitted
	// (quiet refresh) and its payload carries the new cooldown.
	if count := countEvents(t, frames, ProviderStatusEvent); count != 1 {
		t.Fatalf("providerStatus events = %d, want 1 (quiet, only changed providers)", count)
	}
	var status ProviderStatusPayload
	mustUnmarshal(t, []byte(frameDataFor(t, frames, ProviderStatusEvent, 0)), &status)
	if status.Provider != "claude" || status.Status != "cooldown" {
		t.Errorf("providerStatus = %+v, want claude/cooldown", status)
	}
	if status.CooldownUntil == nil || !status.CooldownUntil.Equal(cd) {
		t.Errorf("providerStatus CooldownUntil = %v, want %v", status.CooldownUntil, cd)
	}

	if !hasKeepalive(frames) {
		t.Error("no keepalive comment frame emitted")
	}
	if !src.cancelled {
		t.Error("source Cancel was not called on disconnect")
	}
}

// TestProvidersStreamCooldownChangeOnly asserts the quiet refresh emits an
// event for a cooldown-only change even when status is unchanged.
func TestProvidersStreamCooldownChangeOnly(t *testing.T) {
	before := mustTime(t, "2026-08-05T12:00:00Z")
	after := mustTime(t, "2026-08-05T12:30:00Z")
	src := newFakeProvidersSource()
	src.statuses = []ProviderStatusPayload{
		{Provider: "claude", Status: "cooldown", CooldownUntil: &before},
	}

	h := NewProvidersStream(src, 5*time.Millisecond)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/v1/providers/stream", nil)

	frames, _ := framesOf(t, h, req, func() {
		src.mu.Lock()
		src.statuses = []ProviderStatusPayload{
			{Provider: "claude", Status: "cooldown", CooldownUntil: &after},
		}
		src.mu.Unlock()
		src.notify()
	}, 30*time.Millisecond)

	if count := countEvents(t, frames, ProviderStatusEvent); count != 1 {
		t.Errorf("providerStatus events for cooldown-only change = %d, want 1", count)
	}
}

func countEvents(t *testing.T, frames []sseFrame, want string) int {
	t.Helper()
	n := 0
	for _, f := range frames {
		if !f.comment && f.event == want {
			n++
		}
	}
	return n
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse time: %v", err)
	}
	return tm
}
