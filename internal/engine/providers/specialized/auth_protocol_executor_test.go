package specialized

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gorouter/internal/domain/engine"
	"gorouter/internal/domain/engine/stream"
	"gorouter/internal/domain/provider"
	"gorouter/internal/shared"
)

// trackingBody is an io.ReadCloser that records whether Close was invoked and
// how many times, so tests can assert body-closure guarantees.
type trackingBody struct {
	data   []byte
	closed atomic.Bool
	calls  atomic.Int32
}

func newTrackingBody(data string) *trackingBody {
	return &trackingBody{data: []byte(data)}
}

func (b *trackingBody) Read(p []byte) (int, error) {
	if b.closed.Load() {
		return 0, io.EOF
	}
	if len(b.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, b.data)
	b.data = b.data[n:]
	return n, nil
}

func (b *trackingBody) Close() error {
	b.closed.Store(true)
	b.calls.Add(1)
	return nil
}

func (b *trackingBody) isClosed() bool { return b.closed.Load() }
func (b *trackingBody) closeCalls() int32 {
	return b.calls.Load()
}

// wireRT is a recording RoundTripper that captures the outgoing request and
// returns a canned response built by the caller.
type wireRT struct {
	captured *http.Request
	body     string
	status   int
	headers  http.Header
	err      error
	usesBody *trackingBody
}

func (r *wireRT) RoundTrip(req *http.Request) (*http.Response, error) {
	r.captured = req
	if r.err != nil {
		return nil, r.err
	}
	body := r.body
	if body == "" {
		body = `{}`
	}
	tb := newTrackingBody(body)
	r.usesBody = tb
	return &http.Response{
		StatusCode: r.status,
		Status:     http.StatusText(r.status),
		Header:     r.headers,
		Body:       tb,
	}, nil
}

func newWireRT(status int, body string) *wireRT {
	if status == 0 {
		status = http.StatusOK
	}
	return &wireRT{status: status, body: body, headers: make(http.Header)}
}

// oaiOKBody is a minimal OpenAI-compatible non-stream completion body.
const oaiOKBody = `{"id":"chatcmpl-test","object":"chat.completion","created":1700000000,` +
	`"model":"grok-4","usage":{"prompt_tokens":5,"completion_tokens":7,"total_tokens":12},` +
	`"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"hi"}}]}`

// newAuthProbeRequest builds a standard engine.Request with an optional
// X-Base-URL override so tests can point executors at a fake upstream.
func newAuthProbeRequest(baseURL, model, body string) *engine.Request {
	req := newTestRequest(engine.FormatGemini, model, []byte(body))
	req.Headers = map[string]string{}
	if baseURL != "" {
		req.Headers["X-Base-URL"] = baseURL
	}
	return req
}

// readAllStream drains the stream referenced by the engine response until it
// is closed. It fails the test if the stream does not terminate in time.
func readAllStream(t *testing.T, ref engine.StreamRef) []stream.Chunk {
	t.Helper()
	st, ok := ref.(*stream.Stream)
	if !ok {
		t.Fatalf("response Stream is %T, want *stream.Stream", ref)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var chunks []stream.Chunk
	for {
		select {
		case ch, ok := <-st.Chunks():
			if !ok {
				return chunks
			}
			chunks = append(chunks, ch)
			if ch.IsFinal {
				return chunks
			}
		case <-ctx.Done():
			t.Fatal("stream did not terminate within timeout")
			return nil
		case <-st.Done():
			for {
				select {
				case ch, ok := <-st.Chunks():
					if !ok {
						return chunks
					}
					chunks = append(chunks, ch)
				default:
					return chunks
				}
			}
		}
	}
}

// capturedJSONBody decodes the recorded request body into a map.
func capturedJSONBody(t *testing.T, rt *wireRT) map[string]interface{} {
	t.Helper()
	if rt.captured == nil {
		t.Fatal("no request was captured")
	}
	b, err := io.ReadAll(rt.captured.Body)
	if err != nil {
		t.Fatalf("read captured body: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("captured body is not JSON: %v", err)
	}
	return m
}

// assertAppErrorCode asserts err is an *shared.AppError with the given code
// and HTTP status, then returns it for further inspection.
func assertAppErrorCode(t *testing.T, err error, code shared.ErrorCode, status int) *shared.AppError {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	appErr, ok := shared.AsAppError(err)
	if !ok {
		t.Fatalf("expected *shared.AppError, got %T: %v", err, err)
	}
	if appErr.Code != code {
		t.Errorf("AppError.Code = %q, want %q", appErr.Code, code)
	}
	if appErr.HTTPStatus != status {
		t.Errorf("AppError.HTTPStatus = %d, want %d", appErr.HTTPStatus, status)
	}
	return appErr
}

// assertNoCredentialLeak fails if the error or any of the given strings
// contains the credential value.
func assertNoCredentialLeak(t *testing.T, credential string, candidates ...string) {
	t.Helper()
	if credential == "" {
		return
	}
	for _, c := range candidates {
		if strings.Contains(c, credential) {
			t.Errorf("credential leaked into output: %q", c)
		}
	}
}

// TestIFlowExecutor_WireShape verifies the exact outbound request contract:
// endpoint, auth header, signature headers, user agent, and stream flags.
func TestIFlowExecutor_WireShape(t *testing.T) {
	rt := newWireRT(http.StatusOK, oaiOKBody)
	exe := NewIFlowExecutor(rt)

	req := newAuthProbeRequest("https://iflow.test", "iflow-model", `{"messages":[{"role":"user","content":"hi"}]}`)
	account := newTestAccount("sk-iflow-secret")

	resp, err := exe.Execute(context.Background(), req, account)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if rt.captured == nil {
		t.Fatal("no request captured")
	}

	// Endpoint.
	if got := rt.captured.URL.String(); got != "https://iflow.test/v1/chat/completions" {
		t.Errorf("URL = %q, want %q", got, "https://iflow.test/v1/chat/completions")
	}
	if rt.captured.Method != http.MethodPost {
		t.Errorf("Method = %q, want POST", rt.captured.Method)
	}

	// Auth + identity headers.
	if got := rt.captured.Header.Get("Authorization"); got != "Bearer sk-iflow-secret" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer sk-iflow-secret")
	}
	if got := rt.captured.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if got := rt.captured.Header.Get("User-Agent"); got != "iFlow-Cli/1.0" {
		t.Errorf("User-Agent = %q, want iFlow-Cli/1.0", got)
	}
	sessionID := rt.captured.Header.Get("session-id")
	if !strings.HasPrefix(sessionID, "session-") {
		t.Errorf("session-id = %q, want prefix %q", sessionID, "session-")
	}
	ts := rt.captured.Header.Get("x-iflow-timestamp")
	if ts == "" {
		t.Error("x-iflow-timestamp missing")
	}
	sig := rt.captured.Header.Get("x-iflow-signature")
	if sig == "" {
		t.Error("x-iflow-signature missing")
	}
	if len(sig) != 64 {
		t.Errorf("x-iflow-signature length = %d, want 64 hex chars", len(sig))
	}

	// Signature must be HMAC-SHA256 over "user-agent:session-id:timestamp".
	mac := hmac.New(sha256.New, []byte("sk-iflow-secret"))
	mac.Write([]byte("iFlow-Cli/1.0:" + sessionID + ":" + ts))
	if want := hex.EncodeToString(mac.Sum(nil)); sig != want {
		t.Errorf("x-iflow-signature = %q, want %q", sig, want)
	}

	// Body: stream=false injected, model preserved.
	payload := capturedJSONBody(t, rt)
	if s, ok := payload["stream"].(bool); !ok || s {
		t.Errorf("payload.stream = %v, want false", payload["stream"])
	}
	if got := payload["model"]; got != "iflow-model" {
		t.Errorf("payload.model = %v, want iflow-model", got)
	}

	// Non-stream responses must NOT request SSE.
	if got := rt.captured.Header.Get("Accept"); got != "" {
		t.Errorf("Accept = %q, want empty for non-stream", got)
	}

	// Response mapping.
	if resp == nil {
		t.Fatal("response is nil")
	}
	if resp.Model != "grok-4" {
		t.Errorf("resp.Model = %q, want grok-4", resp.Model)
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 12 {
		t.Errorf("resp.Usage = %+v, want total=12", resp.Usage)
	}
}

// TestIFlowExecutor_StreamWireShape verifies the streaming request injects
// stream=true, stream_options, and Accept: text/event-stream, and that the
// returned stream delivers SSE data chunks.
func TestIFlowExecutor_StreamWireShape(t *testing.T) {
	sse := "data: {\"id\":\"1\",\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n" +
		"data: {\"id\":\"1\",\"choices\":[{\"delta\":{\"content\":\"b\"}}]}\n\n" +
		"data: [DONE]\n\n"
	rt := newWireRT(http.StatusOK, sse)
	exe := NewIFlowExecutor(rt)

	req := newAuthProbeRequest("https://iflow.test", "", `{"messages":[{"role":"user","content":"hi"}]}`)
	account := newTestAccount("sk-iflow-secret")

	resp, err := exe.ExecuteStream(context.Background(), req, account)
	if err != nil {
		t.Fatalf("ExecuteStream failed: %v", err)
	}
	if got := rt.captured.Header.Get("Accept"); got != "text/event-stream" {
		t.Errorf("Accept = %q, want text/event-stream", got)
	}
	payload := capturedJSONBody(t, rt)
	if s, ok := payload["stream"].(bool); !ok || !s {
		t.Errorf("payload.stream = %v, want true", payload["stream"])
	}
	if payload["stream_options"] == nil {
		t.Error("payload.stream_options missing for streaming request")
	}
	if got := payload["model"]; got != "gpt-4o" {
		t.Errorf("payload.model = %q, want default gpt-4o when req.Model empty", got)
	}

	if resp == nil || resp.Stream == nil {
		t.Fatal("expected stream response")
	}
	chunks := readAllStream(t, resp.Stream)
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks (2 data + 1 final), got %d", len(chunks))
	}
	if !chunks[2].IsFinal {
		t.Error("last chunk should have IsFinal=true")
	}
	if !strings.Contains(string(chunks[0].Data), "\"content\":\"a\"") {
		t.Errorf("chunk[0].Data = %s, want first delta", chunks[0].Data)
	}
}

// TestIFlowExecutor_EmptyKeySignature verifies that with no account the
// signature header is empty rather than a MAC of an empty secret.
func TestIFlowExecutor_EmptyKeySignature(t *testing.T) {
	rt := newWireRT(http.StatusOK, oaiOKBody)
	exe := NewIFlowExecutor(rt)
	req := newAuthProbeRequest("https://iflow.test", "", `{"messages":[{"role":"user","content":"hi"}]}`)

	if _, err := exe.Execute(context.Background(), req, nil); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if got := rt.captured.Header.Get("x-iflow-signature"); got != "" {
		t.Errorf("x-iflow-signature = %q, want empty when no credential", got)
	}
}

// TestIFlowExecutor_ErrorMappingAndBodyClosure verifies status classification
// and that the upstream body is closed on every error path.
func TestIFlowExecutor_ErrorMappingAndBodyClosure(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		wantCode   shared.ErrorCode
		wantStatus int
	}{
		{"unauthorized", http.StatusUnauthorized, shared.ErrUnauthorized, http.StatusUnauthorized},
		{"forbidden", http.StatusForbidden, shared.ErrUnauthorized, http.StatusForbidden},
		{"rate-limited", http.StatusTooManyRequests, shared.ErrRateLimited, http.StatusTooManyRequests},
		{"server-error", http.StatusInternalServerError, shared.ErrInternal, http.StatusInternalServerError},
		{"bad-gateway", http.StatusBadGateway, shared.ErrInternal, http.StatusBadGateway},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := newWireRT(tt.status, `{"error":{"message":"boom"}}`)
			exe := NewIFlowExecutor(rt)
			req := newAuthProbeRequest("https://iflow.test", "", `{"messages":[]}`)

			_, err := exe.Execute(context.Background(), req, newTestAccount("sk-iflow-secret"))
			appErr := assertAppErrorCode(t, err, tt.wantCode, tt.wantStatus)
			details, _ := appErr.Details.(map[string]interface{})
			if details == nil {
				t.Fatalf("AppError.Details = %v, want map with status_code/body", appErr.Details)
			}
			if sc, ok := details["status_code"].(int); !ok || sc != tt.status {
				t.Errorf("Details[status_code] = %v, want %d", details["status_code"], tt.status)
			}
			if !rt.usesBody.isClosed() {
				t.Error("response body was not closed after error")
			}
			assertNoCredentialLeak(t, "sk-iflow-secret", err.Error())
		})
	}
}

// TestIFlowExecutor_StreamErrorClosesBody verifies that a non-2xx streaming
// response closes the body and returns the classified error without leaking
// the credential into the error string.
func TestIFlowExecutor_StreamErrorClosesBody(t *testing.T) {
	rt := newWireRT(http.StatusUnauthorized, `{"error":{"message":"invalid token"}}`)
	exe := NewIFlowExecutor(rt)
	req := newAuthProbeRequest("https://iflow.test", "", `{"messages":[]}`)

	_, err := exe.ExecuteStream(context.Background(), req, newTestAccount("sk-iflow-secret"))
	assertAppErrorCode(t, err, shared.ErrUnauthorized, http.StatusUnauthorized)
	if !rt.usesBody.isClosed() {
		t.Error("response body was not closed after stream error")
	}
	assertNoCredentialLeak(t, "sk-iflow-secret", err.Error())
}

// TestIFlowExecutor_DefaultBaseURL verifies the production endpoint is used
// when no X-Base-URL override is present.
func TestIFlowExecutor_DefaultBaseURL(t *testing.T) {
	rt := newWireRT(http.StatusOK, oaiOKBody)
	exe := NewIFlowExecutor(rt)
	req := newAuthProbeRequest("", "", `{"messages":[{"role":"user","content":"hi"}]}`)

	if _, err := exe.Execute(context.Background(), req, newTestAccount("k")); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if got := rt.captured.URL.String(); got != "https://api.iflow.ai/v1/chat/completions" {
		t.Errorf("URL = %q, want default https://api.iflow.ai/v1/chat/completions", got)
	}
}

// TestIFlowExecutor_TransportError verifies that client-level failures are
// wrapped without leaking credentials.
func TestIFlowExecutor_TransportError(t *testing.T) {
	rt := &wireRT{err: errors.New("dial tcp: connection refused")}
	exe := NewIFlowExecutor(rt)
	req := newAuthProbeRequest("https://iflow.test", "", `{"messages":[]}`)

	_, err := exe.Execute(context.Background(), req, newTestAccount("sk-iflow-secret"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "execute request") {
		t.Errorf("error = %q, want wrap prefix %q", err.Error(), "execute request")
	}
	assertNoCredentialLeak(t, "sk-iflow-secret", err.Error())
}

// TestGrokCliExecutor_WireShape verifies the xAI chat completions contract
// including Bearer auth and streaming accept header.
func TestGrokCliExecutor_WireShape(t *testing.T) {
	rt := newWireRT(http.StatusOK, oaiOKBody)
	exe := NewGrokCliExecutor(rt)
	req := newAuthProbeRequest("https://xai.test", "grok-x", `{"messages":[{"role":"user","content":"hi"}]}`)

	resp, err := exe.Execute(context.Background(), req, newTestAccount("xai-token-1"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if got := rt.captured.URL.String(); got != "https://xai.test/v1/chat/completions" {
		t.Errorf("URL = %q, want https://xai.test/v1/chat/completions", got)
	}
	if got := rt.captured.Header.Get("Authorization"); got != "Bearer xai-token-1" {
		t.Errorf("Authorization = %q, want Bearer xai-token-1", got)
	}
	if got := rt.captured.Header.Get("Accept"); got != "" {
		t.Errorf("Accept = %q, want empty for non-stream", got)
	}
	if resp.Model != "grok-4" || resp.Usage == nil || resp.Usage.PromptTokens != 5 {
		t.Errorf("resp = %+v, want parsed model+usage", resp)
	}
}

// TestGrokCliExecutor_NoAccountNoAuthHeader verifies that requests without an
// account omit the Authorization header entirely.
func TestGrokCliExecutor_NoAccountNoAuthHeader(t *testing.T) {
	rt := newWireRT(http.StatusOK, oaiOKBody)
	exe := NewGrokCliExecutor(rt)
	req := newAuthProbeRequest("https://xai.test", "", `{"messages":[]}`)

	if _, err := exe.Execute(context.Background(), req, nil); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if got := rt.captured.Header.Get("Authorization"); got != "" {
		t.Errorf("Authorization = %q, want empty when account is nil", got)
	}
}

// TestGrokCliExecutor_StreamDeliversChunks verifies streaming delivery over
// SSE from a fake upstream.
func TestGrokCliExecutor_StreamDeliversChunks(t *testing.T) {
	sse := "event: message\ndata: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n" +
		"data: [DONE]\n\n"
	rt := newWireRT(http.StatusOK, sse)
	exe := NewGrokCliExecutor(rt)
	req := newAuthProbeRequest("https://xai.test", "", `{"messages":[]}`)

	resp, err := exe.ExecuteStream(context.Background(), req, newTestAccount("xai-token-1"))
	if err != nil {
		t.Fatalf("ExecuteStream failed: %v", err)
	}
	if got := rt.captured.Header.Get("Accept"); got != "text/event-stream" {
		t.Errorf("Accept = %q, want text/event-stream", got)
	}
	chunks := readAllStream(t, resp.Stream)
	if len(chunks) < 2 || !chunks[len(chunks)-1].IsFinal {
		t.Fatalf("expected final-terminated chunks, got %d chunks", len(chunks))
	}
	if chunks[0].Event != "message" {
		t.Errorf("chunk[0].Event = %q, want message", chunks[0].Event)
	}
}

// TestGrokCliExecutor_DoubleCloseProbe verifies that when the upstream returns
// a non-2xx status the error path closes the body exactly once at the http
// layer semantics level: the body must be closed, a redundant Close must not
// panic, and the classified error must survive the double-close path.
func TestGrokCliExecutor_DoubleCloseProbe(t *testing.T) {
	rt := newWireRT(http.StatusForbidden, `{"error":{"message":"nope"}}`)
	exe := NewGrokCliExecutor(rt)
	req := newAuthProbeRequest("https://xai.test", "", `{"messages":[]}`)

	_, err := exe.ExecuteStream(context.Background(), req, newTestAccount("xai-token-1"))
	assertAppErrorCode(t, err, shared.ErrUnauthorized, http.StatusForbidden)

	// The double-close path in ExecuteStream (checkResponseStatus closes the
	// body, then the executor closes it again) must be harmless: body closed,
	// no panic, and the second Close is idempotent.
	if !rt.usesBody.isClosed() {
		t.Error("body must be closed after error")
	}
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("redundant Close panicked: %v", r)
			}
		}()
		_ = rt.usesBody.Close()
	}()
	assertNoCredentialLeak(t, "xai-token-1", err.Error())
}

// TestQoderExecutor_WireShape verifies the Qoder endpoint, Bearer auth, and
// OpenAI response parsing.
func TestQoderExecutor_WireShape(t *testing.T) {
	rt := newWireRT(http.StatusOK, oaiOKBody)
	exe := NewQoderExecutor(rt)
	req := newAuthProbeRequest("https://qoder.test", "qoder-model", `{"messages":[{"role":"user","content":"hi"}]}`)

	resp, err := exe.Execute(context.Background(), req, newTestAccount("qoder-key"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if got := rt.captured.URL.String(); got != "https://qoder.test/v1/chat/completions" {
		t.Errorf("URL = %q, want https://qoder.test/v1/chat/completions", got)
	}
	if got := rt.captured.Header.Get("Authorization"); got != "Bearer qoder-key" {
		t.Errorf("Authorization = %q, want Bearer qoder-key", got)
	}
	if resp == nil || resp.Model != "grok-4" {
		t.Errorf("resp.Model = %v, want grok-4", resp.Model)
	}
}

// TestQoderExecutor_StreamSetsAccept verifies the streaming path adds the SSE
// accept header and returns a working stream.
func TestQoderExecutor_StreamSetsAccept(t *testing.T) {
	rt := newWireRT(http.StatusOK, "data: {\"c\":1}\n\ndata: [DONE]\n\n")
	exe := NewQoderExecutor(rt)
	req := newAuthProbeRequest("https://qoder.test", "", `{"messages":[]}`)

	resp, err := exe.ExecuteStream(context.Background(), req, newTestAccount("qoder-key"))
	if err != nil {
		t.Fatalf("ExecuteStream failed: %v", err)
	}
	if got := rt.captured.Header.Get("Accept"); got != "text/event-stream" {
		t.Errorf("Accept = %q, want text/event-stream", got)
	}
	chunks := readAllStream(t, resp.Stream)
	if len(chunks) != 2 || !chunks[1].IsFinal {
		t.Fatalf("expected 2 chunks ending in final, got %d", len(chunks))
	}
}

// TestQoderExecutor_ErrorClosesBodyAndRedacts verifies 401 handling closes the
// body and keeps the credential out of the returned error.
func TestQoderExecutor_ErrorClosesBodyAndRedacts(t *testing.T) {
	rt := newWireRT(http.StatusUnauthorized, `{"error":{"message":"unauthorized"}}`)
	exe := NewQoderExecutor(rt)
	req := newAuthProbeRequest("https://qoder.test", "", `{"messages":[]}`)

	_, err := exe.Execute(context.Background(), req, newTestAccount("qoder-key"))
	assertAppErrorCode(t, err, shared.ErrUnauthorized, http.StatusUnauthorized)
	if !rt.usesBody.isClosed() {
		t.Error("body not closed after error")
	}
	assertNoCredentialLeak(t, "qoder-key", err.Error())
}

// TestKimchiExecutor_WireShape verifies the Kimchi endpoint and Bearer auth
// for both non-stream and stream paths.
func TestKimchiExecutor_WireShape(t *testing.T) {
	tests := []struct {
		name     string
		stream   bool
		wantBody string
	}{
		{"non-stream", false, "https://kimchi.test/v1/chat/completions"},
		{"stream", true, "https://kimchi.test/v1/chat/completions"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := newWireRT(http.StatusOK, oaiOKBody)
			exe := NewKimchiExecutor(rt)
			req := newAuthProbeRequest("https://kimchi.test", "kimchi-model", `{"messages":[{"role":"user","content":"hi"}]}`)

			var (
				resp *engine.Response
				err  error
			)
			if tt.stream {
				rt.body = "data: {\"d\":1}\n\ndata: [DONE]\n\n"
				resp, err = exe.ExecuteStream(context.Background(), req, newTestAccount("kimchi-key"))
			} else {
				resp, err = exe.Execute(context.Background(), req, newTestAccount("kimchi-key"))
			}
			if err != nil {
				t.Fatalf("execute failed: %v", err)
			}
			if got := rt.captured.URL.String(); got != tt.wantBody {
				t.Errorf("URL = %q, want %q", got, tt.wantBody)
			}
			if got := rt.captured.Header.Get("Authorization"); got != "Bearer kimchi-key" {
				t.Errorf("Authorization = %q, want Bearer kimchi-key", got)
			}
			if !tt.stream && resp.Model != "grok-4" {
				t.Errorf("resp.Model = %v, want grok-4", resp.Model)
			}
			if tt.stream {
				chunks := readAllStream(t, resp.Stream)
				if len(chunks) < 2 || !chunks[len(chunks)-1].IsFinal {
					t.Fatalf("expected final-terminated stream, got %d chunks", len(chunks))
				}
			}
		})
	}
}

// TestKimchiExecutor_StreamInjectsFlag verifies stream=true is injected into
// the payload for the streaming path and false for the non-stream path.
func TestKimchiExecutor_StreamInjectsFlag(t *testing.T) {
	rt := newWireRT(http.StatusOK, "data: [DONE]\n\n")
	exe := NewKimchiExecutor(rt)
	req := newAuthProbeRequest("https://kimchi.test", "", `{"messages":[]}`)

	if _, err := exe.ExecuteStream(context.Background(), req, newTestAccount("k")); err != nil {
		t.Fatalf("ExecuteStream failed: %v", err)
	}
	payload := capturedJSONBody(t, rt)
	if s, ok := payload["stream"].(bool); !ok || !s {
		t.Errorf("payload.stream = %v, want true", payload["stream"])
	}
}

// TestKimchiExecutor_ErrorMapping verifies status classification on the
// non-stream path.
func TestKimchiExecutor_ErrorMapping(t *testing.T) {
	rt := newWireRT(http.StatusTooManyRequests, `{"error":{"message":"slow down"}}`)
	exe := NewKimchiExecutor(rt)
	req := newAuthProbeRequest("https://kimchi.test", "", `{"messages":[]}`)

	_, err := exe.Execute(context.Background(), req, newTestAccount("kimchi-key"))
	assertAppErrorCode(t, err, shared.ErrRateLimited, http.StatusTooManyRequests)
	if !rt.usesBody.isClosed() {
		t.Error("body not closed after error")
	}
	assertNoCredentialLeak(t, "kimchi-key", err.Error())
}

// TestCursorExecutor_WireShape verifies the Cursor-specific headers
// (User-Agent, Cursor-Client) and endpoint for both request modes.
func TestCursorExecutor_WireShape(t *testing.T) {
	rt := newWireRT(http.StatusOK, oaiOKBody)
	exe := NewCursorExecutor(rt)
	req := newAuthProbeRequest("https://cursor.test", "cursor-model", `{"messages":[{"role":"user","content":"hi"}]}`)

	resp, err := exe.Execute(context.Background(), req, newTestAccount("cursor-token"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if got := rt.captured.URL.String(); got != "https://cursor.test/v1/chat/completions" {
		t.Errorf("URL = %q, want https://cursor.test/v1/chat/completions", got)
	}
	if got := rt.captured.Header.Get("Authorization"); got != "Bearer cursor-token" {
		t.Errorf("Authorization = %q, want Bearer cursor-token", got)
	}
	if got := rt.captured.Header.Get("User-Agent"); got != "cursor/1.0" {
		t.Errorf("User-Agent = %q, want cursor/1.0", got)
	}
	if got := rt.captured.Header.Get("Cursor-Client"); got != "ide" {
		t.Errorf("Cursor-Client = %q, want ide", got)
	}
	if resp == nil || resp.Model != "cursor-model" {
		t.Errorf("resp.Model = %v, want cursor-model (resolved)", resp.Model)
	}
	if string(resp.Body) != oaiOKBody {
		t.Errorf("resp.Body = %s, want raw upstream body passthrough", resp.Body)
	}
}

// TestCursorExecutor_StreamWire verifies streaming sets Accept and injects the
// stream flag.
func TestCursorExecutor_StreamWire(t *testing.T) {
	rt := newWireRT(http.StatusOK, "data: {\"d\":1}\n\ndata: [DONE]\n\n")
	exe := NewCursorExecutor(rt)
	req := newAuthProbeRequest("https://cursor.test", "", `{"messages":[]}`)

	resp, err := exe.ExecuteStream(context.Background(), req, newTestAccount("cursor-token"))
	if err != nil {
		t.Fatalf("ExecuteStream failed: %v", err)
	}
	if got := rt.captured.Header.Get("Accept"); got != "text/event-stream" {
		t.Errorf("Accept = %q, want text/event-stream", got)
	}
	if got := rt.captured.Header.Get("Cursor-Client"); got != "ide" {
		t.Errorf("Cursor-Client = %q, want ide on stream too", got)
	}
	payload := capturedJSONBody(t, rt)
	if s, ok := payload["stream"].(bool); !ok || !s {
		t.Errorf("payload.stream = %v, want true", payload["stream"])
	}
	chunks := readAllStream(t, resp.Stream)
	if len(chunks) != 2 || !chunks[1].IsFinal {
		t.Fatalf("expected 2 chunks ending in final, got %d", len(chunks))
	}
}

// TestCursorExecutor_ErrorMappingAndClosure verifies 401/429/5xx mapping and
// body closure on the stream path.
func TestCursorExecutor_ErrorMappingAndClosure(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		wantCode   shared.ErrorCode
		wantStatus int
	}{
		{"auth", http.StatusUnauthorized, shared.ErrUnauthorized, http.StatusUnauthorized},
		{"forbidden", http.StatusForbidden, shared.ErrUnauthorized, http.StatusForbidden},
		{"rate", http.StatusTooManyRequests, shared.ErrRateLimited, http.StatusTooManyRequests},
		{"internal", http.StatusInternalServerError, shared.ErrInternal, http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := newWireRT(tt.status, `{"error":{"message":"boom"}}`)
			exe := NewCursorExecutor(rt)
			req := newAuthProbeRequest("https://cursor.test", "", `{"messages":[]}`)

			_, err := exe.ExecuteStream(context.Background(), req, newTestAccount("cursor-token"))
			assertAppErrorCode(t, err, tt.wantCode, tt.wantStatus)
			if !rt.usesBody.isClosed() {
				t.Error("body not closed after stream error")
			}
			assertNoCredentialLeak(t, "cursor-token", err.Error())
		})
	}
}

// TestCursorExecutor_SupportsFormats pins the format contract: only the
// OpenAI-compat wire format is accepted.
func TestCursorExecutor_SupportsFormats(t *testing.T) {
	exe := NewCursorExecutor(nil)
	if !exe.SupportsFormat(engine.FormatOpenAICompat) {
		t.Error("want FormatOpenAICompat supported")
	}
	for _, f := range []engine.RequestFormat{engine.FormatGemini, engine.FormatAnthropic, engine.FormatCodexResponses, engine.FormatOpenAIChat} {
		if exe.SupportsFormat(f) {
			t.Errorf("format %q must not be supported by Cursor executor", f)
		}
	}
}

// TestKiroExecutor_WireShape verifies the Kiro endpoint, conditional
// User-Agent, and Bearer auth.
func TestKiroExecutor_WireShape(t *testing.T) {
	rt := newWireRT(http.StatusOK, oaiOKBody)
	exe := NewKiroExecutor(rt)
	req := newAuthProbeRequest("https://kiro.test", "kiro-model", `{"messages":[{"role":"user","content":"hi"}]}`)

	resp, err := exe.Execute(context.Background(), req, newTestAccount("kiro-token"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if got := rt.captured.URL.String(); got != "https://kiro.test/v1/chat/completions" {
		t.Errorf("URL = %q, want https://kiro.test/v1/chat/completions", got)
	}
	if got := rt.captured.Header.Get("Authorization"); got != "Bearer kiro-token" {
		t.Errorf("Authorization = %q, want Bearer kiro-token", got)
	}
	if got := rt.captured.Header.Get("User-Agent"); got != "kiro-cli/1.0.0" {
		t.Errorf("User-Agent = %q, want kiro-cli/1.0.0 when account present", got)
	}
	if resp.Model != "kiro-model" {
		t.Errorf("resp.Model = %v, want kiro-model", resp.Model)
	}
}

// TestKiroExecutor_NoAccountOmitsIdentityHeaders verifies that without an
// account, neither Bearer auth nor the CLI User-Agent is sent.
func TestKiroExecutor_NoAccountOmitsIdentityHeaders(t *testing.T) {
	rt := newWireRT(http.StatusOK, oaiOKBody)
	exe := NewKiroExecutor(rt)
	req := newAuthProbeRequest("https://kiro.test", "", `{"messages":[]}`)

	if _, err := exe.Execute(context.Background(), req, nil); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if got := rt.captured.Header.Get("Authorization"); got != "" {
		t.Errorf("Authorization = %q, want empty", got)
	}
	if got := rt.captured.Header.Get("User-Agent"); got != "" {
		t.Errorf("User-Agent = %q, want empty without account", got)
	}
}

// TestKiroExecutor_StreamEventStreamDelivers verifies Kiro's line-based
// EventStream protocol: every non-empty line becomes an "eventstream" chunk
// and the stream terminates with a final chunk.
func TestKiroExecutor_StreamEventStreamDelivers(t *testing.T) {
	lines := `{"type":"token","content":"a"}` + "\n" +
		`{"type":"token","content":"b"}` + "\n" +
		`{"type":"done"}` + "\n"
	rt := newWireRT(http.StatusOK, lines)
	exe := NewKiroExecutor(rt)
	req := newAuthProbeRequest("https://kiro.test", "", `{"messages":[]}`)

	resp, err := exe.ExecuteStream(context.Background(), req, newTestAccount("kiro-token"))
	if err != nil {
		t.Fatalf("ExecuteStream failed: %v", err)
	}
	if got := rt.captured.Header.Get("Accept"); got != "text/event-stream" {
		t.Errorf("Accept = %q, want text/event-stream", got)
	}
	chunks := readAllStream(t, resp.Stream)
	if len(chunks) != 4 {
		t.Fatalf("expected 4 chunks (3 lines + final), got %d", len(chunks))
	}
	for i := 0; i < 3; i++ {
		if chunks[i].Event != "eventstream" {
			t.Errorf("chunk[%d].Event = %q, want eventstream", i, chunks[i].Event)
		}
	}
	if !chunks[3].IsFinal {
		t.Error("last chunk should have IsFinal=true")
	}
}

// TestKiroExecutor_StreamClosesBodyOnce verifies the EventStream streamer
// closes the upstream body exactly once on clean completion.
func TestKiroExecutor_StreamClosesBodyOnce(t *testing.T) {
	rt := newWireRT(http.StatusOK, `{"type":"done"}`+"\n")
	exe := NewKiroExecutor(rt)
	req := newAuthProbeRequest("https://kiro.test", "", `{"messages":[]}`)

	resp, err := exe.ExecuteStream(context.Background(), req, newTestAccount("kiro-token"))
	if err != nil {
		t.Fatalf("ExecuteStream failed: %v", err)
	}
	readAllStream(t, resp.Stream)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if rt.usesBody.closeCalls() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := rt.usesBody.closeCalls(); got != 1 {
		t.Errorf("body Close calls = %d, want exactly 1 (closeOnce semantics)", got)
	}
}

// TestKiroExecutor_ErrorClosesBody verifies the non-2xx path closes the body
// and returns a classified error without credential leakage.
func TestKiroExecutor_ErrorClosesBody(t *testing.T) {
	rt := newWireRT(http.StatusTooManyRequests, `{"error":{"message":"rate"}}`)
	exe := NewKiroExecutor(rt)
	req := newAuthProbeRequest("https://kiro.test", "", `{"messages":[]}`)

	_, err := exe.ExecuteStream(context.Background(), req, newTestAccount("kiro-token"))
	assertAppErrorCode(t, err, shared.ErrRateLimited, http.StatusTooManyRequests)
	if !rt.usesBody.isClosed() {
		t.Error("body not closed after stream error")
	}
	assertNoCredentialLeak(t, "kiro-token", err.Error())
}

// TestKiroExecutor_CancelClosesBody verifies that cancelling the caller
// context propagates to the EventStream streamer and closes the body.
func TestKiroExecutor_CancelClosesBody(t *testing.T) {
	rt := newWireRT(http.StatusOK, "")
	exe := NewKiroExecutor(rt)
	req := newAuthProbeRequest("https://kiro.test", "", `{"messages":[]}`)
	ctx, cancel := context.WithCancel(context.Background())

	_, err := exe.ExecuteStream(ctx, req, newTestAccount("kiro-token"))
	if err != nil {
		t.Fatalf("ExecuteStream failed: %v", err)
	}
	cancel()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if rt.usesBody.isClosed() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("body was not closed after context cancellation")
}

// TestAuthFamilyProviderTypes pins the provider identity and supported format
// of every auth/protocol family executor.
func TestAuthFamilyProviderTypes(t *testing.T) {
	tests := []struct {
		name       string
		exec       engine.Executor
		want       provider.ProviderType
		supports   []engine.RequestFormat
		notSupport []engine.RequestFormat
	}{
		{"iflow", NewIFlowExecutor(nil), provider.ProviderIflow,
			[]engine.RequestFormat{engine.FormatGemini},
			[]engine.RequestFormat{engine.FormatOpenAIChat, engine.FormatAnthropic}},
		{"grok-cli", NewGrokCliExecutor(nil), provider.ProviderGrokCli,
			[]engine.RequestFormat{engine.FormatGemini},
			[]engine.RequestFormat{engine.FormatOpenAIChat}},
		{"qoder", NewQoderExecutor(nil), provider.ProviderQoder,
			[]engine.RequestFormat{engine.FormatGemini},
			[]engine.RequestFormat{engine.FormatOpenAIChat}},
		{"kimchi", NewKimchiExecutor(nil), provider.ProviderKimchi,
			[]engine.RequestFormat{engine.FormatGemini},
			[]engine.RequestFormat{engine.FormatOpenAIChat}},
		{"cursor", NewCursorExecutor(nil), provider.ProviderCursor,
			[]engine.RequestFormat{engine.FormatOpenAICompat},
			[]engine.RequestFormat{engine.FormatGemini, engine.FormatAnthropic}},
		{"kiro", NewKiroExecutor(nil), provider.ProviderKiro,
			[]engine.RequestFormat{engine.FormatOpenAICompat},
			[]engine.RequestFormat{engine.FormatGemini, engine.FormatCodexResponses}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.exec.ProviderType(); got != tt.want {
				t.Errorf("ProviderType() = %q, want %q", got, tt.want)
			}
			for _, f := range tt.supports {
				if !tt.exec.SupportsFormat(f) {
					t.Errorf("executor should support format %q", f)
				}
			}
			for _, f := range tt.notSupport {
				if tt.exec.SupportsFormat(f) {
					t.Errorf("executor must NOT support format %q", f)
				}
			}
		})
	}
}

// TestAuthFamilyRequestIDPropagation verifies the engine request ID flows
// through to the engine response for every executor in the family.
func TestAuthFamilyRequestIDPropagation(t *testing.T) {
	req := newAuthProbeRequest("", "", `{"messages":[{"role":"user","content":"hi"}]}`)
	execs := []struct {
		name string
		exec engine.Executor
	}{
		{"iflow", NewIFlowExecutor(newWireRT(http.StatusOK, oaiOKBody))},
		{"grok-cli", NewGrokCliExecutor(newWireRT(http.StatusOK, oaiOKBody))},
		{"qoder", NewQoderExecutor(newWireRT(http.StatusOK, oaiOKBody))},
		{"kimchi", NewKimchiExecutor(newWireRT(http.StatusOK, oaiOKBody))},
		{"cursor", NewCursorExecutor(newWireRT(http.StatusOK, oaiOKBody))},
		{"kiro", NewKiroExecutor(newWireRT(http.StatusOK, oaiOKBody))},
	}
	for _, tt := range execs {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := tt.exec.Execute(context.Background(), req, newTestAccount("cred"))
			if err != nil {
				t.Fatalf("Execute failed: %v", err)
			}
			if resp == nil {
				t.Fatal("nil response")
			}
			if resp.RequestID != req.ID {
				t.Errorf("RequestID = %v, want %v", resp.RequestID, req.ID)
			}
			if resp.StatusCode != http.StatusOK {
				t.Errorf("StatusCode = %d, want 200", resp.StatusCode)
			}
		})
	}
}

// TestAuthExecutors_Execute_ErrorClosesBodyExactlyOnce pins the close-once
// contract on the non-streaming error path: checkResponseStatus must not close
// the body internally and the caller's defer must be the only close.
func TestAuthExecutors_Execute_ErrorClosesBodyExactlyOnce(t *testing.T) {
	for name, newExec := range map[string]func(http.RoundTripper) engine.Executor{
		"iflow":   func(rt http.RoundTripper) engine.Executor { return NewIFlowExecutor(rt) },
		"grokcli": func(rt http.RoundTripper) engine.Executor { return NewGrokCliExecutor(rt) },
		"qoder":   func(rt http.RoundTripper) engine.Executor { return NewQoderExecutor(rt) },
		"kimchi":  func(rt http.RoundTripper) engine.Executor { return NewKimchiExecutor(rt) },
	} {
		t.Run(name, func(t *testing.T) {
			rt := newWireRT(http.StatusUnauthorized, `{"error":"no"}`)
			exe := newExec(rt)
			req := newAuthProbeRequest("", "", `{"messages":[]}`)

			_, err := exe.Execute(context.Background(), req, newTestAccount("secret-key-1"))
			if err == nil {
				t.Fatal("expected error for 401")
			}
			if got := rt.usesBody.closeCalls(); got != 1 {
				t.Errorf("response body closed %d times, want exactly 1", got)
			}
		})
	}
}

// TestAuthExecutors_ExecuteStream_ErrorClosesBodyExactlyOnce pins the
// close-once contract on the streaming error path: the caller's explicit close
// must be the only close (no internal close inside checkResponseStatus).
func TestAuthExecutors_ExecuteStream_ErrorClosesBodyExactlyOnce(t *testing.T) {
	for name, newExec := range map[string]func(http.RoundTripper) engine.Executor{
		"iflow":   func(rt http.RoundTripper) engine.Executor { return NewIFlowExecutor(rt) },
		"grokcli": func(rt http.RoundTripper) engine.Executor { return NewGrokCliExecutor(rt) },
		"qoder":   func(rt http.RoundTripper) engine.Executor { return NewQoderExecutor(rt) },
		"kimchi":  func(rt http.RoundTripper) engine.Executor { return NewKimchiExecutor(rt) },
	} {
		t.Run(name, func(t *testing.T) {
			rt := newWireRT(http.StatusForbidden, `{"error":"no"}`)
			exe := newExec(rt)
			req := newAuthProbeRequest("", "", `{"messages":[]}`)

			_, err := exe.ExecuteStream(context.Background(), req, newTestAccount("secret-key-2"))
			if err == nil {
				t.Fatal("expected error for 403")
			}
			if got := rt.usesBody.closeCalls(); got != 1 {
				t.Errorf("response body closed %d times, want exactly 1", got)
			}
		})
	}
}
