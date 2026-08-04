package realtime

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestJobsStream asserts the jobs stream registry contract: an initial
// `jobs` snapshot with the scheduler current and last run status per job
// type, a quiet `jobStatus` event per job transition, keepalive comments,
// and cleanup on disconnect.
func TestJobsStream(t *testing.T) {
	at := mustTime(t, "2026-08-05T12:00:00Z")
	last := mustTime(t, "2026-08-05T11:55:00Z")
	src := newFakeJobsSource()
	src.snap = JobsSnapshot{
		Current: []JobRunPayload{{Type: "sync_provider", Status: "pending"}},
		Last:    []JobRunPayload{{Type: "sync_provider", Status: "completed", At: &last}},
	}

	h := NewJobsStream(src, 5*time.Millisecond)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/v1/jobs/stream", nil)

	frames, _ := framesOf(t, h, req, func() {
		src.mu.Lock()
		src.snap.Current = []JobRunPayload{{Type: "sync_provider", Status: "running", At: &at}}
		src.mu.Unlock()
		src.notify()
	}, 40*time.Millisecond)

	events := eventSequence(t, frames)
	assertEventAt(t, events, 0, JobsInitEvent)

	var init JobsSnapshot
	mustUnmarshal(t, []byte(frameDataFor(t, frames, JobsInitEvent, 0)), &init)
	if len(init.Current) != 1 || init.Current[0].Type != "sync_provider" || init.Current[0].Status != "pending" {
		t.Errorf("init current = %+v, want sync_provider/pending", init.Current)
	}
	if len(init.Last) != 1 || init.Last[0].Status != "completed" {
		t.Errorf("init last = %+v, want sync_provider/completed", init.Last)
	}

	if count := countEvents(t, frames, JobStatusEvent); count != 1 {
		t.Fatalf("jobStatus events = %d, want 1 (quiet, only changed jobs)", count)
	}
	var status JobRunPayload
	mustUnmarshal(t, []byte(frameDataFor(t, frames, JobStatusEvent, 0)), &status)
	if status.Type != "sync_provider" || status.Status != "running" {
		t.Errorf("jobStatus = %+v, want sync_provider/running", status)
	}
	if status.At == nil || !status.At.Equal(at) {
		t.Errorf("jobStatus At = %v, want %v", status.At, at)
	}

	if !hasKeepalive(frames) {
		t.Error("no keepalive comment frame emitted")
	}
	if !src.cancelled {
		t.Error("source Cancel was not called on disconnect")
	}
}

// TestJobsStreamQuietOnUnchanged asserts unchanged job types produce no
// jobStatus events after an update.
func TestJobsStreamQuietOnUnchanged(t *testing.T) {
	src := newFakeJobsSource()
	src.snap = JobsSnapshot{
		Current: []JobRunPayload{{Type: "sync_provider", Status: "pending"}},
		Last:    []JobRunPayload{{Type: "sync_provider", Status: "pending"}},
	}

	h := NewJobsStream(src, 5*time.Millisecond)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/v1/jobs/stream", nil)

	frames, _ := framesOf(t, h, req, func() {
		src.notify()
	}, 30*time.Millisecond)

	if count := countEvents(t, frames, JobStatusEvent); count != 0 {
		t.Errorf("jobStatus events for unchanged snapshot = %d, want 0", count)
	}
}
