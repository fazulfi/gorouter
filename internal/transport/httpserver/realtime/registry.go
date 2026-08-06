// Package realtime owns the shared Server-Sent Events wire registry and the
// per-domain realtime streams (Usage, Console, Providers, Jobs) served under
// the Admin API v1 surface.
//
// Wire contract:
//   - content type is text/event-stream; Cache-Control is no-cache;
//   - a `: keepalive` comment line is emitted at the 25 second bound;
//   - the streams are session-cookie-only (an EventSource cannot send an
//     Authorization header); PAT consumers use the JSON export endpoints;
//   - on an authenticated reconnect the server re-pushes current state; no
//     reconnection token is invented and no duplicate history is emitted;
//     Last-Event-ID is used only as a server cursor, never credentials;
//   - every outbound payload passes through a single redaction pass
//     (internal/shared/redact) before it is written.
//
// The event registry below is the single source of truth for event names,
// payload shapes, initial/update ordering and the full-array vs incremental
// payload rule for each stream. No stream is implemented with event names or
// payload shapes outside this registry.
package realtime

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"gorouter/internal/shared/redact"
)

// KeepaliveInterval is the SSE comment keepalive cadence: a `: keepalive`
// comment line is emitted on every stream at this bound.
const KeepaliveInterval = 25 * time.Second

// Event names (product terms; the registry contract for every stream).
const (
	// Usage stream events (full stats push + lightweight updates; the
	// recent requests array is replaced in full on every update).
	UsageStatsEvent          = "stats"
	UsageActiveRequestsEvent = "activeRequests"
	UsageRecentRequestsEvent = "recentRequests"
	UsageErrorProviderEvent  = "errorProvider"

	// Console stream events: init replays buffered lines, line emits each
	// new line after the last delivered sequence.
	ConsoleInitEvent = "init"
	ConsoleLineEvent = "line"

	// Providers stream events: providers is the initial full-array status
	// push; providerStatus emits a quiet per-provider status or cooldown
	// refresh.
	ProvidersInitEvent  = "providers"
	ProviderStatusEvent = "providerStatus"

	// Jobs stream events: jobs is the initial current/last snapshot;
	// jobStatus emits a quiet per-job status transition.
	JobsInitEvent  = "jobs"
	JobStatusEvent = "jobStatus"
)

// Canceler is implemented by a stream source that holds a subscription or
// resource that must be released when the client disconnects. Each handler
// calls Cancel when the request context is done, enabling cancel cleanup on
// r.Context().Done().
type Canceler interface {
	Cancel()
}

// Config wires the four per-domain stream sources into the admin router.
// A nil source leaves its stream route resolving with backend-unavailable.
// Keepalive overrides the registry keepalive cadence (25 s) for tests and
// alternate deployments; zero selects the registry cadence.
type Config struct {
	Keepalive       time.Duration
	UsageSource     UsageStreamSource
	ConsoleSource   ConsoleStreamSource
	ProvidersSource ProvidersStreamSource
	JobsSource      JobsStreamSource
}

// openStream sets the SSE wire headers and returns the response flusher. It
// is the single choke point that fixes text/event-stream and
// Cache-Control: no-cache for every stream.
func openStream(w http.ResponseWriter) (http.Flusher, error) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	f, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("realtime: response writer does not support flushing")
	}
	return f, nil
}

// interval returns keepalive when positive, otherwise the registry cadence.
func interval(keepalive time.Duration) time.Duration {
	if keepalive > 0 {
		return keepalive
	}
	return KeepaliveInterval
}

// writeEvent writes one SSE event. The payload is JSON-encoded and passed
// through a single outbound redaction pass before it is written, so a
// credential-shaped substring can never surface to the client.
func writeEvent(w http.ResponseWriter, f http.Flusher, event string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("realtime: marshal %s payload: %w", event, err)
	}
	redacted := redact.Redact(string(data))
	if event != "" {
		fmt.Fprintf(w, "event: %s\n", event)
	}
	fmt.Fprintf(w, "data: %s\n\n", redacted)
	f.Flush()
	return nil
}

// writeKeepalive emits the `: keepalive` comment line.
func writeKeepalive(w http.ResponseWriter, f http.Flusher) {
	fmt.Fprint(w, ": keepalive\n\n")
	f.Flush()
}
