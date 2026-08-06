// Package productsurfaces consolidates the cross-surface integration tests
// for the Admin API v1 realtime streams served through the admin router.
//
// The fixtures here are machine-checkable shape specs: each test drives the
// stream with a test-injected source and asserts the exact event names,
// payload shapes, full-array vs incremental rule, keepalive comment format,
// the session-cookie-only auth boundary (P1-5), and cleanup on disconnect.
package productsurfaces

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	domauth "gorouter/internal/domain/auth"
	"gorouter/internal/domain/console"
	server "gorouter/internal/transport/httpserver"
	"gorouter/internal/transport/httpserver/realtime"

	"github.com/google/uuid"
)

// streamAuth satisfies the admin router's auth surface: every presented
// session cookie resolves to a session actor, so the SessionAuth middleware
// admits the stream requests in these tests.
type streamAuth struct{}

func (streamAuth) Login(w http.ResponseWriter, r *http.Request)  {}
func (streamAuth) Logout(w http.ResponseWriter, r *http.Request) {}
func (streamAuth) Me(w http.ResponseWriter, r *http.Request)     {}
func (streamAuth) Status(w http.ResponseWriter, r *http.Request) {}
func (streamAuth) ValidateSession(context.Context, string) (*domauth.Actor, error) {
	return &domauth.Actor{UserID: uuid.New(), Kind: domauth.ActorKindSession, Origin: domauth.ActorOriginRemote}, nil
}
func (streamAuth) SessionCookieName() string { return "gorouter_session" }

// ---- test-injected stream sources ----

type usageSource struct {
	mu        sync.Mutex
	updates   chan struct{}
	stats     realtime.StatsPayload
	active    int
	recent    []realtime.RecentRequestPayload
	errs      []string
	cancelled bool
}

func newUsageSource() *usageSource              { return &usageSource{updates: make(chan struct{}, 16)} }
func (s *usageSource) Updates() <-chan struct{} { return s.updates }
func (s *usageSource) Stats(context.Context) (realtime.StatsPayload, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stats, nil
}
func (s *usageSource) ActiveRequests(context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active, nil
}
func (s *usageSource) RecentRequests(context.Context) ([]realtime.RecentRequestPayload, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.recent, nil
}
func (s *usageSource) ErrorProviders(context.Context) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.errs, nil
}
func (s *usageSource) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelled = true
}
func (s *usageSource) notify() {
	select {
	case s.updates <- struct{}{}:
	default:
	}
}

type consoleSource struct {
	mu        sync.Mutex
	updates   chan struct{}
	rows      []console.ConsoleLog
	cancelled bool
}

func newConsoleSource() *consoleSource            { return &consoleSource{updates: make(chan struct{}, 16)} }
func (s *consoleSource) Updates() <-chan struct{} { return s.updates }
func (s *consoleSource) ListAfter(_ context.Context, seq int64, _ int) ([]console.ConsoleLog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []console.ConsoleLog
	for _, row := range s.rows {
		if row.Seq > seq {
			out = append(out, row)
		}
	}
	return out, nil
}
func (s *consoleSource) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelled = true
}
func (s *consoleSource) notify() {
	select {
	case s.updates <- struct{}{}:
	default:
	}
}

type providersSource struct {
	mu        sync.Mutex
	updates   chan struct{}
	statuses  []realtime.ProviderStatusPayload
	cancelled bool
}

func newProvidersSource() *providersSource          { return &providersSource{updates: make(chan struct{}, 16)} }
func (s *providersSource) Updates() <-chan struct{} { return s.updates }
func (s *providersSource) Status(context.Context) ([]realtime.ProviderStatusPayload, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statuses, nil
}
func (s *providersSource) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelled = true
}
func (s *providersSource) notify() {
	select {
	case s.updates <- struct{}{}:
	default:
	}
}

type jobsSource struct {
	mu        sync.Mutex
	updates   chan struct{}
	snap      realtime.JobsSnapshot
	cancelled bool
}

func newJobsSource() *jobsSource               { return &jobsSource{updates: make(chan struct{}, 16)} }
func (s *jobsSource) Updates() <-chan struct{} { return s.updates }
func (s *jobsSource) Snapshot(context.Context) (realtime.JobsSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snap, nil
}
func (s *jobsSource) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelled = true
}
func (s *jobsSource) notify() {
	select {
	case s.updates <- struct{}{}:
	default:
	}
}

// ---- wire helpers ----

type frame struct {
	event   string
	data    string
	comment bool
}

func parseFrames(t *testing.T, body string) []frame {
	t.Helper()
	var frames []frame
	for _, block := range strings.Split(body, "\n\n") {
		block = strings.TrimRight(block, "\n")
		if block == "" {
			continue
		}
		var f frame
		for _, line := range strings.Split(block, "\n") {
			switch {
			case strings.HasPrefix(line, "event: "):
				f.event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				f.data += strings.TrimPrefix(line, "data: ")
			case strings.HasPrefix(line, ":"):
				f.comment = true
			}
		}
		frames = append(frames, f)
	}
	return frames
}

func buildRouter(rt *realtime.Config) http.Handler {
	if rt.Keepalive == 0 {
		rt.Keepalive = 5 * time.Millisecond
	}
	return server.NewAdminRouter(server.AdminConfig{Auth: streamAuth{}, Realtime: rt})
}

// runSSE serves a stream through the full admin router on a cancellable
// context, runs the optional during callback after the initial push, cancels
// the request, and fails the test unless the handler returns (cleanup on
// disconnect). It returns the parsed frames and the recorder for header
// assertions.
func runSSE(t *testing.T, h http.Handler, path string, session bool, during func(), settle time.Duration) ([]frame, *httptest.ResponseRecorder) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req = req.WithContext(ctx)
	if session {
		req.AddCookie(&http.Cookie{Name: "gorouter_session", Value: "raw-session"})
	} else {
		req.Header.Set("Authorization", "Bearer pat-token")
	}
	rr := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		h.ServeHTTP(rr, req)
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)
	if during != nil {
		during()
	}
	time.Sleep(settle)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stream handler did not return after request cancel (cleanup broken)")
	}
	return parseFrames(t, rr.Body.String()), rr
}

func frameDataFor(t *testing.T, frames []frame, want string, occurrence int) string {
	t.Helper()
	n := 0
	for _, f := range frames {
		if f.comment || f.event != want {
			continue
		}
		if n == occurrence {
			return f.data
		}
		n++
	}
	t.Fatalf("no frame for event %q occurrence %d", want, occurrence)
	return ""
}

func countEvent(t *testing.T, frames []frame, want string) int {
	t.Helper()
	n := 0
	for _, f := range frames {
		if !f.comment && f.event == want {
			n++
		}
	}
	return n
}

func hasKeepalive(frames []frame) bool {
	for _, f := range frames {
		if f.comment {
			return true
		}
	}
	return false
}

func decode(t *testing.T, data string, dst any) {
	t.Helper()
	if err := json.Unmarshal([]byte(data), dst); err != nil {
		t.Fatalf("unmarshal %s: %v", data, err)
	}
}

// TestUsageStream asserts the usage stream through the admin router: full
// stats push + lightweight updates, full-array recent requests replacement,
// session-cookie gate, keepalive comment format, and cleanup on disconnect.
func TestUsageStream(t *testing.T) {
	src := newUsageSource()
	src.stats = realtime.StatsPayload{TotalRequests: 11, TotalTokensIn: 100, TotalTokensOut: 50, ActiveRequests: 2, ErrorRate: 0.1, Period: "24h"}
	src.active = 2
	src.recent = []realtime.RecentRequestPayload{
		{Model: pstr("gpt-4o"), Provider: pstr("openai"), PromptTokens: 10, CompletionTokens: 5, Minute: "2026-08-05T12:04"},
	}
	src.errs = []string{"claude"}

	h := buildRouter(&realtime.Config{UsageSource: src})
	frames, rr := runSSE(t, h, "/api/admin/v1/usage/stream", true, func() {
		src.mu.Lock()
		src.recent = append(src.recent, realtime.RecentRequestPayload{Model: pstr("claude-3-5"), Provider: pstr("anthropic"), PromptTokens: 7, CompletionTokens: 3, Minute: "2026-08-05T12:05"})
		src.mu.Unlock()
		src.notify()
	}, 50*time.Millisecond)

	if got := rr.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", got)
	}
	if got := rr.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", got)
	}

	var lastRecents string
	var recentCount int
	for _, f := range frames {
		if f.event == "recentRequests" {
			recentCount++
			lastRecents = f.data
		}
	}
	if recentCount != 2 {
		t.Fatalf("recentRequests frames = %d, want 2 (initial + update)", recentCount)
	}
	var recent realtime.RecentRequestsPayload
	decode(t, lastRecents, &recent)
	if len(recent.Entries) != 2 {
		t.Fatalf("updated recent requests entries = %d, want full-array replacement with 2", len(recent.Entries))
	}
	if !hasKeepalive(frames) {
		t.Error("no keepalive comment frame emitted")
	}
	if !src.cancelled {
		t.Error("usage source Cancel not called on disconnect")
	}
}

// TestConsoleStream asserts the console stream through the admin router:
// init replays buffered lines, line emits new lines, keepalive, disconnect.
func TestConsoleStream(t *testing.T) {
	msg2 := "listening on :8080"
	src := newConsoleSource()
	src.rows = []console.ConsoleLog{{Seq: 1, RedactedMessage: "started"}, {Seq: 2, RedactedMessage: msg2}}

	h := buildRouter(&realtime.Config{ConsoleSource: src})
	frames, _ := runSSE(t, h, "/api/admin/v1/console/stream", true, func() {
		src.mu.Lock()
		src.rows = append(src.rows, console.ConsoleLog{Seq: 3, RedactedMessage: "new line"})
		src.mu.Unlock()
		src.notify()
	}, 50*time.Millisecond)

	var init realtime.ConsoleInitPayload
	decode(t, frameDataFor(t, frames, "init", 0), &init)
	if len(init.Lines) != 2 {
		t.Fatalf("init lines = %d, want 2", len(init.Lines))
	}
	if init.Lines[1].Message != msg2 {
		t.Errorf("init line 2 message = %q, want %q", init.Lines[1].Message, msg2)
	}
	var line realtime.ConsoleLinePayload
	decode(t, frameDataFor(t, frames, "line", 0), &line)
	if line.Seq != 3 || line.Message != "new line" {
		t.Errorf("line event = %+v, want seq 3 message %q", line, "new line")
	}
	if !hasKeepalive(frames) {
		t.Error("no keepalive comment frame emitted")
	}
	if !src.cancelled {
		t.Error("console source Cancel not called on disconnect")
	}
}

// TestJobsStream asserts the jobs stream through the admin router: an
// initial current/last snapshot and a quiet per-job status transition.
func TestJobsStream(t *testing.T) {
	src := newJobsSource()
	src.snap = realtime.JobsSnapshot{
		Current: []realtime.JobRunPayload{{Type: "sync_provider", Status: "pending"}},
		Last:    []realtime.JobRunPayload{{Type: "sync_provider", Status: "completed"}},
	}

	h := buildRouter(&realtime.Config{JobsSource: src})
	frames, _ := runSSE(t, h, "/api/admin/v1/jobs/stream", true, func() {
		src.mu.Lock()
		src.snap.Current = []realtime.JobRunPayload{{Type: "sync_provider", Status: "running"}}
		src.mu.Unlock()
		src.notify()
	}, 50*time.Millisecond)

	var snap realtime.JobsSnapshot
	decode(t, frameDataFor(t, frames, "jobs", 0), &snap)
	if len(snap.Current) != 1 || snap.Current[0].Status != "pending" || len(snap.Last) != 1 {
		t.Fatalf("init jobs snapshot = %+v, want current pending + last completed", snap)
	}
	if countEvent(t, frames, "jobStatus") != 1 {
		t.Fatalf("jobStatus events = %d, want 1 (quiet transition)", countEvent(t, frames, "jobStatus"))
	}
	var job realtime.JobRunPayload
	decode(t, frameDataFor(t, frames, "jobStatus", 0), &job)
	if job.Status != "running" {
		t.Errorf("jobStatus status = %q, want running", job.Status)
	}
	if !hasKeepalive(frames) {
		t.Error("no keepalive comment frame emitted")
	}
	if !src.cancelled {
		t.Error("jobs source Cancel not called on disconnect")
	}
}

// TestProvidersStream asserts the providers stream through the admin
// router: an initial full-array status push and a quiet per-provider
// cooldown refresh.
func TestProvidersStream(t *testing.T) {
	src := newProvidersSource()
	src.statuses = []realtime.ProviderStatusPayload{{Provider: "openai", Status: "healthy"}}

	h := buildRouter(&realtime.Config{ProvidersSource: src})
	frames, _ := runSSE(t, h, "/api/admin/v1/providers/stream", true, func() {
		src.mu.Lock()
		until := time.Now().UTC().Add(15 * time.Minute)
		src.statuses = append(src.statuses, realtime.ProviderStatusPayload{Provider: "claude", Status: "cooldown", CooldownUntil: &until})
		src.mu.Unlock()
		src.notify()
	}, 50*time.Millisecond)

	var init realtime.ProvidersStatusPayload
	decode(t, frameDataFor(t, frames, "providers", 0), &init)
	if len(init.Providers) != 1 || init.Providers[0].Provider != "openai" {
		t.Fatalf("init providers = %+v, want openai", init.Providers)
	}
	if countEvent(t, frames, "providerStatus") != 1 {
		t.Fatalf("providerStatus events = %d, want 1 (quiet refresh)", countEvent(t, frames, "providerStatus"))
	}
	if !src.cancelled {
		t.Error("providers source Cancel not called on disconnect")
	}
}

// TestStreamsSessionCookieOnly asserts the P1-5 auth boundary: every stream
// rejects a PAT bearer (no session cookie) with 401, while a session cookie
// reaches the stream, and the JSON export surface remains PAT-accessible.
func TestStreamsSessionCookieOnly(t *testing.T) {
	src := newUsageSource()
	h := buildRouter(&realtime.Config{UsageSource: src})

	for _, path := range []string{
		"/api/admin/v1/usage/stream",
		"/api/admin/v1/console/stream",
		"/api/admin/v1/providers/stream",
		"/api/admin/v1/jobs/stream",
	} {
		ctx, cancel := context.WithCancel(context.Background())
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req = req.WithContext(ctx)
		req.Header.Set("Authorization", "Bearer pat-token")
		rr := httptest.NewRecorder()
		done := make(chan struct{})
		go func() { h.ServeHTTP(rr, req); close(done) }()
		time.Sleep(20 * time.Millisecond)
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("stream %s did not return on cancel", path)
		}
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("%s with PAT bearer (no session) = %d, want 401 (streams must be session-cookie-only)", path, rr.Code)
		}
	}

	// A session cookie reaches the stream (streams are session-ok).
	frames, _ := runSSE(t, h, "/api/admin/v1/usage/stream", true, nil, 30*time.Millisecond)
	if countEvent(t, frames, "stats") == 0 {
		t.Error("usage stream with session cookie emitted no stats event")
	}

	// The JSON export surface accepts a PAT (P1-5: PAT consumers use the
	// exports): PAT bearer yields the backend-unavailable 503 (auth passed),
	// while no credential is rejected 401.
	noAuth := doExport(t, h, "/api/admin/v1/usage/request-logs", false)
	if noAuth.Code != http.StatusUnauthorized {
		t.Errorf("export without credential = %d, want 401", noAuth.Code)
	}
	pat := doExport(t, h, "/api/admin/v1/usage/request-logs", true)
	if pat.Code == http.StatusUnauthorized || pat.Code == http.StatusForbidden {
		t.Errorf("export with PAT = %d, want authenticated (503 backend-unavailable) not 401/403", pat.Code)
	}
}

func doExport(t *testing.T, h http.Handler, path string, pat bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if pat {
		req.Header.Set("Authorization", "Bearer pat-token")
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

// pstr returns a pointer to s.
func pstr(s string) *string { return &s }
