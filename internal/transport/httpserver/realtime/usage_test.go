package realtime

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestUsageStream asserts the usage stream registry contract: full stats
// push plus lightweight activeRequests/recentRequests/errorProvider pushes,
// full-array replacement of the Recent Requests array on update, keepalive
// comment format, and cleanup on disconnect.
func TestUsageStream(t *testing.T) {
	src := newFakeUsageSource()
	src.stats = StatsPayload{TotalRequests: 11, TotalTokensIn: 100, TotalTokensOut: 50, ActiveRequests: 2, ErrorRate: 0.1, Period: "24h"}
	src.active = 2
	src.recent = []RecentRequestPayload{
		{Model: strPtr("gpt-4o"), Provider: strPtr("openai"), PromptTokens: 10, CompletionTokens: 5, Minute: "2026-08-05T12:04"},
	}
	src.errs = []string{"claude"}

	h := NewUsageStream(src, 5*time.Millisecond)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/v1/usage/stream", nil)

	frames, rr := framesOf(t, h, req, func() {
		src.mu.Lock()
		src.recent = []RecentRequestPayload{
			{Model: strPtr("gpt-4o"), Provider: strPtr("openai"), PromptTokens: 10, CompletionTokens: 5, Minute: "2026-08-05T12:04"},
			{Model: strPtr("claude-3-5"), Provider: strPtr("anthropic"), PromptTokens: 7, CompletionTokens: 3, Minute: "2026-08-05T12:05"},
		}
		src.errs = []string{"claude", "gemini"}
		src.mu.Unlock()
		src.notify()
	}, 40*time.Millisecond)

	if got := rr.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", got)
	}
	if got := rr.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", got)
	}
	if !src.cancelled {
		t.Error("source Cancel was not called on disconnect (cancel cleanup)")
	}

	events := eventSequence(t, frames)
	assertEventAt(t, events, 0, UsageStatsEvent)
	assertEventAt(t, events, 1, UsageActiveRequestsEvent)
	assertEventAt(t, events, 2, UsageRecentRequestsEvent)
	assertEventAt(t, events, 3, UsageErrorProviderEvent)

	stats := frameDataFor(t, frames, UsageStatsEvent, 0)
	var statsPayload StatsPayload
	mustUnmarshal(t, []byte(stats), &statsPayload)
	if statsPayload.TotalRequests != 11 || statsPayload.ActiveRequests != 2 {
		t.Errorf("stats payload = %+v, want total_requests 11 active_requests 2", statsPayload)
	}

	var active ActiveRequestsPayload
	mustUnmarshal(t, []byte(frameDataFor(t, frames, UsageActiveRequestsEvent, 0)), &active)
	if active.ActiveRequests != 2 {
		t.Errorf("activeRequests = %+v, want 2", active)
	}

	var recent RecentRequestsPayload
	mustUnmarshal(t, []byte(frameDataFor(t, frames, UsageRecentRequestsEvent, 0)), &recent)
	if len(recent.Entries) != 1 {
		t.Fatalf("initial recentRequests entries = %d, want 1", len(recent.Entries))
	}

	var recentUpdated RecentRequestsPayload
	mustUnmarshal(t, []byte(frameDataFor(t, frames, UsageRecentRequestsEvent, 1)), &recentUpdated)
	if len(recentUpdated.Entries) != 2 {
		t.Fatalf("updated recentRequests entries = %d, want full-array replacement with 2", len(recentUpdated.Entries))
	}
	if *recentUpdated.Entries[1].Provider != "anthropic" {
		t.Errorf("second entry provider = %v, want anthropic", recentUpdated.Entries[1].Provider)
	}

	var errs ErrorProviderPayload
	mustUnmarshal(t, []byte(frameDataFor(t, frames, UsageErrorProviderEvent, 0)), &errs)
	if len(errs.Providers) != 1 || errs.Providers[0] != "claude" {
		t.Errorf("errorProvider = %+v, want [claude]", errs)
	}

	var errsUpdated ErrorProviderPayload
	mustUnmarshal(t, []byte(frameDataFor(t, frames, UsageErrorProviderEvent, 1)), &errsUpdated)
	if len(errsUpdated.Providers) != 2 {
		t.Errorf("updated errorProvider = %+v, want 2 providers", errsUpdated.Providers)
	}

	if !hasKeepalive(frames) {
		t.Error("no `: keepalive` comment frame emitted")
	}
}

// eventSequence returns the ordered non-comment event names.
func eventSequence(t *testing.T, frames []sseFrame) []string {
	t.Helper()
	var out []string
	for _, f := range frames {
		if !f.comment {
			out = append(out, f.event)
		}
	}
	return out
}

func assertEventAt(t *testing.T, events []string, i int, want string) {
	t.Helper()
	if i >= len(events) {
		t.Fatalf("event index %d out of range (have %d events: %v)", i, len(events), events)
	}
	if events[i] != want {
		t.Fatalf("event[%d] = %q, want %q (sequence %v)", i, events[i], want, events)
	}
}

func frameDataFor(t *testing.T, frames []sseFrame, event string, occurrence int) string {
	t.Helper()
	n := 0
	for _, f := range frames {
		if f.comment || f.event != event {
			continue
		}
		if n == occurrence {
			return f.data
		}
		n++
	}
	t.Fatalf("frame for event %q occurrence %d not found", event, occurrence)
	return ""
}

func hasKeepalive(frames []sseFrame) bool {
	for _, f := range frames {
		if f.comment {
			return true
		}
	}
	return false
}

func mustUnmarshal(t *testing.T, data []byte, dst any) {
	t.Helper()
	if err := json.Unmarshal(data, dst); err != nil {
		t.Fatalf("unmarshal %s: %v", string(data), err)
	}
}

func strPtr(s string) *string { return &s }
