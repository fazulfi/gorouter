// Package video implements the async video generation, edit, extension, and
// status modalities. Upstream semantics are a transparent async proxy:
// POST /v1/videos/{generations|edits|extensions} returns a job; status is
// polled via GET /v1/videos/{request_id}; terminal states are the upstream's
// own, passed through verbatim (decolua/9router @ 79918c7830695bbca4a45c9fea4a42c3e9fd73d1,
// open-sse/handlers/videoCore.js, audit/01-http-contracts.md:554-557).
package video

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"gorouter/internal/domain/provider"
	"gorouter/internal/engine/media"
)

// Kind selects the async video action (upstream videoCore.js:14
// VIDEO_ACTIONS = generations|edits|extensions).
type Kind string

const (
	KindGeneration Kind = "generations"
	KindEdit       Kind = "edits"
	KindExtension  Kind = "extensions"
)

// CreateRequest is the video job body. The upstream forwards the body
// byte-for-byte (videoGeneration.js:45-61); only the canonical fields are
// decoded locally for validation and the Raw payload is forwarded verbatim.
type CreateRequest struct {
	Kind  Kind
	Model string          `json:"model"`
	Raw   json.RawMessage `json:"-"`
}

// VideoRef is the upstream job video result (videoCore.js:58 comment:
// upstream JSON carries video.url).
type VideoRef struct {
	URL string `json:"url"`
}

// Job is the upstream job payload passed through verbatim
// (videoCore.js:155-165: request_id, status, video, error).
type Job struct {
	RequestID string          `json:"request_id"`
	Status    string          `json:"status"`
	Video     *VideoRef       `json:"video,omitempty"`
	Error     string          `json:"error,omitempty"`
	Raw       json.RawMessage `json:"-"`
}

// DecodeCreateRequest validates a video job body for the given kind.
func DecodeCreateRequest(body []byte, kind Kind) (*CreateRequest, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("video: %w", media.ErrMalformedRequest)
	}
	switch kind {
	case KindGeneration, KindEdit, KindExtension:
	default:
		return nil, fmt.Errorf("video: %w: unknown kind %q", media.ErrMalformedRequest, kind)
	}
	var probe struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return nil, fmt.Errorf("video: %w", media.ErrMalformedRequest)
	}
	if probe.Model == "" {
		return nil, fmt.Errorf("video: %w: missing model", media.ErrMalformedRequest)
	}
	return &CreateRequest{Kind: kind, Model: probe.Model, Raw: body}, nil
}

// DecodeJob parses an upstream job status payload. The upstream passes
// request_id/status/video/error through verbatim (videoCore.js:155-165);
// unknown fields are preserved in Raw.
func DecodeJob(body []byte) (*Job, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("video: %w", media.ErrMalformedRequest)
	}
	var job Job
	if err := json.Unmarshal(body, &job); err != nil {
		return nil, fmt.Errorf("video: %w", media.ErrMalformedRequest)
	}
	job.Raw = body
	return &job, nil
}

// Executor executes video jobs against the async proxy
// (POST /v1/videos/{action}, GET /v1/videos/{request_id}).
type Executor struct {
	client        *media.Client
	basePath      string
	maxResponse   int64
	ceilingAuth   string
	roundTripMs   int64
	pollInterval  time.Duration
	waitTimeout   time.Duration
	idempotencyFn func() string
}

// Option configures an Executor.
type Option func(*Executor)

// WithResponseCeiling bounds upstream job payloads.
func WithResponseCeiling(max int64, authority string) Option {
	return func(e *Executor) { e.maxResponse, e.ceilingAuth = max, authority }
}

// WithPolling overrides the client-side poll defaults (CLI platform
// authorities: 5s interval / 600s timeout, audit/08-cli-host-integration.md:142,435).
func WithPolling(interval, timeout time.Duration) Option {
	return func(e *Executor) { e.pollInterval, e.waitTimeout = interval, timeout }
}

// WithRoundTripTimeout overrides the per-request round-trip ceiling
// (upstream VIDEO_FETCH_TIMEOUT_MS = 120000, videoCore.js:8). Values <= 0
// disable the ceiling.
func WithRoundTripTimeout(d time.Duration) Option {
	return func(e *Executor) { e.roundTripMs = int64(d / time.Millisecond) }
}

// NewExecutor creates a video executor with the upstream round-trip timeout
// (videoCore.js:8, 120000ms) and CLI poll defaults.
func NewExecutor(client *media.Client, opts ...Option) *Executor {
	e := &Executor{
		client:       client,
		basePath:     "/v1/videos",
		maxResponse:  media.BodyCeiling,
		ceilingAuth:  media.BodyCeilingAuthority,
		roundTripMs:  120000,
		pollInterval: 5 * time.Second,
		waitTimeout:  600 * time.Second,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Create submits a video job (POST /v1/videos/{generations|edits|extensions},
// audit/01-http-contracts.md:554-556). The upstream may return a job payload
// directly; errors are classified with the upstream 2000-byte error slice
// (videoCore.js:152). The round trip is bounded by the upstream
// VIDEO_FETCH_TIMEOUT_MS ceiling (videoCore.js:8).
func (e *Executor) Create(ctx context.Context, req *CreateRequest, account *provider.Account) (*Job, error) {
	if req == nil {
		return nil, fmt.Errorf("video: %w: nil request", media.ErrMalformedRequest)
	}
	ctx, cancel := e.withRoundTripTimeout(ctx)
	defer cancel()
	path := e.basePath + "/" + string(req.Kind)
	httpReq, err := e.client.BuildRequest(ctx, http.MethodPost, path, bytes.NewReader(req.Raw), "application/json", account)
	if err != nil {
		return nil, err
	}
	if e.idempotencyFn != nil {
		httpReq.Header.Set("Idempotency-Key", e.idempotencyFn())
	}
	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := classifyStatus(resp); err != nil {
		return nil, err
	}
	data, err := media.ReadLimited(resp.Body, e.maxResponse, e.ceilingAuth)
	if err != nil {
		return nil, err
	}
	return DecodeJob(data)
}

// Status polls a job once (GET /v1/videos/{request_id},
// audit/01-http-contracts.md:557; upstream videoCore.js:33-36). The round
// trip is bounded by the upstream VIDEO_FETCH_TIMEOUT_MS ceiling
// (videoCore.js:8).
func (e *Executor) Status(ctx context.Context, requestID string, account *provider.Account) (*Job, error) {
	if requestID == "" {
		return nil, fmt.Errorf("video: %w: missing request_id", media.ErrMalformedRequest)
	}
	ctx, cancel := e.withRoundTripTimeout(ctx)
	defer cancel()
	httpReq, err := e.client.BuildRequest(ctx, http.MethodGet, e.basePath+"/"+requestID, nil, "", account)
	if err != nil {
		return nil, err
	}
	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := classifyStatus(resp); err != nil {
		return nil, err
	}
	data, err := media.ReadLimited(resp.Body, e.maxResponse, e.ceilingAuth)
	if err != nil {
		return nil, err
	}
	return DecodeJob(data)
}

// Wait polls a job until it reaches a terminal upstream status or the
// timeout/context ends. Poll defaults are CLI platform authorities
// (audit/08-cli-host-integration.md:142,435). Cancellation aborts polling
// with ctx.Err().
func (e *Executor) Wait(ctx context.Context, requestID string, account *provider.Account) (*Job, error) {
	if e.waitTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.waitTimeout)
		defer cancel()
	}
	ticker := time.NewTicker(e.pollInterval)
	defer ticker.Stop()
	for {
		job, err := e.Status(ctx, requestID, account)
		if err != nil {
			return nil, err
		}
		if IsTerminal(job.Status) {
			return job, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

// TerminalStatuses are the upstream terminal job statuses observed across the
// video/image async adapters (falAi.js:28,32 COMPLETED/FAILED;
// blackForestLabs.js:34-35 Ready/Error|Failed; runwayml.js:39-40
// SUCCEEDED/FAILED|CANCELLED; nanobanana.js:50-51). Statuses outside this
// set are treated as in-progress, matching the verbatim-passthrough contract.
var TerminalStatuses = map[string]bool{
	"COMPLETED": true,
	"FAILED":    true,
	"Ready":     true,
	"Error":     true,
	"Failed":    true,
	"SUCCEEDED": true,
	"CANCELLED": true,
	"error":     true,
}

// IsTerminal reports whether an upstream job status is terminal.
func IsTerminal(status string) bool {
	return TerminalStatuses[status]
}

// withRoundTripTimeout returns a context bounded by the upstream
// VIDEO_FETCH_TIMEOUT_MS ceiling (videoCore.js:8). The returned cancel must
// be deferred by the caller; it is a no-op when the ceiling is disabled.
func (e *Executor) withRoundTripTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if e.roundTripMs <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, time.Duration(e.roundTripMs)*time.Millisecond)
}

func classifyStatus(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	msg := media.RedactErrorString(string(body))
	if len(msg) > 2000 {
		msg = msg[:2000]
	}
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return media.NewAuthError(resp.StatusCode, msg)
	case http.StatusTooManyRequests:
		return media.NewRateLimitError(resp.StatusCode, msg)
	default:
		return media.NewUpstreamError(resp.StatusCode, msg)
	}
}
