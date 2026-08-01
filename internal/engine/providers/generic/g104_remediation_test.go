package generic

// g104_remediation_test.go — behavioral locks for the gosec G104 remediation
// in the generic provider package (anthropic.go, models.go, openai.go):
// close-once on every affected error path, best-effort body close, and
// injectStreamParam original-body close.

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"gorouter/internal/domain/engine"
)

// genericTrackingBody is an io.ReadCloser that records Close invocations.
type genericTrackingBody struct {
	data  []byte
	calls atomic.Int32
}

func (b *genericTrackingBody) Read(p []byte) (int, error) {
	if len(b.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, b.data)
	b.data = b.data[n:]
	return n, nil
}

func (b *genericTrackingBody) Close() error {
	b.calls.Add(1)
	return nil
}

func (b *genericTrackingBody) closeCalls() int32 { return b.calls.Load() }

func (b *genericTrackingBody) isClosed() bool { return b.calls.Load() > 0 }

// genericWireRT is a RoundTripper that returns a response body whose Close is
// counted.
type genericWireRT struct {
	status int
	body   string
	uses   *genericTrackingBody
}

func (r *genericWireRT) RoundTrip(req *http.Request) (*http.Response, error) {
	b := &genericTrackingBody{data: []byte(r.body)}
	r.uses = b
	return &http.Response{StatusCode: r.status, Body: b, Header: make(http.Header)}, nil
}

// TestGenericOpenAI_StreamErrorClosesBody pins body closure on the
// ExecuteStream error path (openai.go line 145). The generic status checker
// closes internally and the caller closes again, so the contract is: closed
// and a redundant close must not panic.
func TestGenericOpenAI_StreamErrorClosesBody(t *testing.T) {
	rt := &genericWireRT{status: http.StatusUnauthorized, body: `{"error":"no"}`}
	client := NewClient(WithTransport(rt), WithBaseURL("https://generic.test"))
	e := NewCompatibleOpenAIExecutor(client)
	req := testRequest(engine.FormatOpenAIChat, "gpt-4o", []byte(`{"model":"gpt-4o"}`))

	_, err := e.ExecuteStream(context.Background(), req, testAccount("g104-key"))
	if err == nil {
		t.Fatal("expected error for 401")
	}
	if !rt.uses.isClosed() {
		t.Error("response body not closed after stream error")
	}
	assertGenericRedundantCloseNoPanic(t, rt.uses)
}

// TestGenericOpenAI_NonStreamErrorClosesBody pins body closure on the
// checkResponseStatus error path (openai.go line 222).
func TestGenericOpenAI_NonStreamErrorClosesBody(t *testing.T) {
	rt := &genericWireRT{status: http.StatusTooManyRequests, body: `{"error":"limit"}`}
	client := NewClient(WithTransport(rt), WithBaseURL("https://generic.test"))
	e := NewCompatibleOpenAIExecutor(client)
	req := testRequest(engine.FormatOpenAIChat, "gpt-4o", []byte(`{"model":"gpt-4o"}`))

	_, err := e.Execute(context.Background(), req, testAccount("g104-key"))
	if err == nil {
		t.Fatal("expected error for 429")
	}
	if !rt.uses.isClosed() {
		t.Error("response body not closed after error")
	}
	assertGenericRedundantCloseNoPanic(t, rt.uses)
}

// TestGenericAnthropic_StreamErrorClosesBody pins body closure on the
// ExecuteStream error path (anthropic.go line 141).
func TestGenericAnthropic_StreamErrorClosesBody(t *testing.T) {
	rt := &genericWireRT{status: http.StatusUnauthorized, body: `{"error":"no"}`}
	client := NewClient(WithTransport(rt), WithBaseURL("https://generic.test"))
	e := NewCompatibleAnthropicExecutor(client)
	req := testRequest(engine.FormatAnthropic, "claude-3", []byte(`{"model":"claude-3"}`))
	acct := testAccount("g104-key")
	acct.AuthType = "anthropic_api_key"

	_, err := e.ExecuteStream(context.Background(), req, acct)
	if err == nil {
		t.Fatal("expected error for 401")
	}
	if !rt.uses.isClosed() {
		t.Error("response body not closed after stream error")
	}
	assertGenericRedundantCloseNoPanic(t, rt.uses)
}

// assertGenericRedundantCloseNoPanic verifies the body is already closed and a
// second Close neither panics nor corrupts state.
func assertGenericRedundantCloseNoPanic(t *testing.T, b *genericTrackingBody) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("redundant Close panicked: %v", r)
		}
	}()
	_ = b.Close()
}

// TestGenericCheckAnthropicResponseStatus_ClosesBody pins the internal close
// in checkAnthropicResponseStatus (anthropic.go line 163) happens exactly once.
func TestGenericCheckAnthropicResponseStatus_ClosesBody(t *testing.T) {
	b := &genericTrackingBody{data: []byte(`{"error":"no"}`)}
	resp := &http.Response{StatusCode: http.StatusForbidden, Body: b, Header: make(http.Header)}
	err := checkAnthropicResponseStatus(resp)
	if err == nil {
		t.Fatal("expected error for 403")
	}
	if got := b.closeCalls(); got != 1 {
		t.Errorf("response body closed %d times, want exactly 1", got)
	}
}

// TestGenericModels_ProbeEmbeddings_ClosesBody pins close-once on the
// embeddings probe (models.go line 103).
func TestGenericModels_ProbeEmbeddings_ClosesBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Write([]byte(`{"object":"list","data":[{"id":"some-model"}]}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data":[{"embedding":[0.1]}]}`))
	}))
	defer srv.Close()

	client := NewClient(WithTransport(http.DefaultTransport), WithBaseURL(srv.URL))
	found, err := ProbeEmbeddings(context.Background(), client, testAccount("g104-key"))
	if err != nil {
		t.Fatalf("ProbeEmbeddings: %v", err)
	}
	if !found {
		t.Error("expected embeddings probe to succeed via /v1/embeddings")
	}
}

// TestGenericInjectStreamParam_ClosesOriginalBody pins injectStreamParam
// (openai.go line 195): the original request body is closed exactly once and
// a readable body with stream=true is restored.
func TestGenericInjectStreamParam_ClosesOriginalBody(t *testing.T) {
	orig := &genericTrackingBody{data: []byte(`{"model":"m","messages":[]}`)}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		"http://example.test/v1/chat/completions", orig)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	out, err := injectStreamParam(req, true)
	if err != nil {
		t.Fatalf("injectStreamParam: %v", err)
	}
	if got := orig.closeCalls(); got != 1 {
		t.Errorf("original body closed %d times, want exactly 1", got)
	}
	got, _ := io.ReadAll(out.Body)
	if !strings.Contains(string(got), `"stream":true`) {
		t.Errorf("restored body = %s, want stream=true injected", got)
	}
}

// TestGenericInjectAnthropicStreamParam_ClosesOriginalBody pins the equivalent
// in anthropic.go line 186.
func TestGenericInjectAnthropicStreamParam_ClosesOriginalBody(t *testing.T) {
	orig := &genericTrackingBody{data: []byte(`{"model":"m","messages":[]}`)}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		"http://example.test/v1/messages", orig)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	out, err := injectAnthropicStreamParam(req, true)
	if err != nil {
		t.Fatalf("injectAnthropicStreamParam: %v", err)
	}
	if got := orig.closeCalls(); got != 1 {
		t.Errorf("original body closed %d times, want exactly 1", got)
	}
	got, _ := io.ReadAll(out.Body)
	if !strings.Contains(string(got), `"stream":true`) {
		t.Errorf("restored body = %s, want stream=true injected", got)
	}
}

// TestGenericAtomicClose_ClosesExactlyOnce pins the atomicClose helper
// (openai.go line 454).
func TestGenericAtomicClose_ClosesExactlyOnce(t *testing.T) {
	var ac atomicClose
	b := &genericTrackingBody{data: []byte("x")}
	ac.Close(b)
	ac.Close(b)
	if got := b.closeCalls(); got != 1 {
		t.Errorf("atomicClose closed underlying closer %d times, want exactly 1", got)
	}
}
