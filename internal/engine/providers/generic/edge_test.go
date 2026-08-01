package generic

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"gorouter/internal/domain/engine"
	"gorouter/internal/domain/engine/stream"
	"gorouter/internal/domain/provider"
	"gorouter/internal/shared"
)

// --- shared test doubles -------------------------------------------------------

type trackingCloser struct {
	mu     sync.Mutex
	closed int
	inner  io.Closer
}

func (c *trackingCloser) Close() error {
	c.mu.Lock()
	c.closed++
	c.mu.Unlock()
	if c.inner != nil {
		return c.inner.Close()
	}
	return nil
}

func (c *trackingCloser) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

type failTransport struct {
	err error
}

func (f *failTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, f.err
}

type readErrBody struct{ err error }

func (b *readErrBody) Read([]byte) (int, error) { return 0, b.err }
func (b *readErrBody) Close() error             { return nil }

// releaseReader returns data once, then blocks until release, then fails.
type releaseReader struct {
	mu      sync.Mutex
	data    []byte
	release chan struct{}
	err     error
}

func (r *releaseReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	if len(r.data) > 0 {
		n := copy(p, r.data)
		r.data = r.data[n:]
		r.mu.Unlock()
		return n, nil
	}
	r.mu.Unlock()
	<-r.release
	r.mu.Lock()
	defer r.mu.Unlock()
	return 0, r.err
}

// drainStream consumes all chunks until the channel closes and returns the
// terminal stream state.
func drainStream(t *testing.T, st *stream.Stream) ([]stream.Chunk, stream.State) {
	t.Helper()
	var chunks []stream.Chunk
	done := make(chan struct{})
	go func() {
		for c := range st.Chunks() {
			chunks = append(chunks, c)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out draining stream")
	}
	return chunks, st.State()
}

// errChunk returns the first chunk carrying a non-nil error.
func errChunk(chunks []stream.Chunk) *stream.Chunk {
	for i := range chunks {
		if chunks[i].Error != nil {
			return &chunks[i]
		}
	}
	return nil
}

var errConsumerDone = errors.New("consumer done")

// appErr extracts the *shared.AppError from err.
func appErr(t *testing.T, err error) *shared.AppError {
	t.Helper()
	ae, ok := shared.AsAppError(err)
	if !ok {
		t.Fatalf("expected *shared.AppError, got %T: %v", err, err)
	}
	return ae
}

// --- Client basics ---------------------------------------------------------------

func TestClient_Transport(t *testing.T) {
	tr := &failTransport{err: errors.New("nope")}
	c := NewClient(WithTransport(tr))
	if c.Transport() != tr {
		t.Error("Transport() did not return the configured transport")
	}
}

func TestClient_ResolveBaseURL(t *testing.T) {
	c := NewClient()
	if got := c.resolveBaseURL(nil); got != "" {
		t.Errorf("resolveBaseURL() = %q, want empty", got)
	}
	c2 := NewClient(WithBaseURL("https://example.com/"))
	if got := c2.resolveBaseURL(nil); got != "https://example.com" {
		t.Errorf("resolveBaseURL() = %q, want trailing slash trimmed", got)
	}
}

func TestClient_BuildRequest_InvalidPath(t *testing.T) {
	c := NewClient(WithBaseURL("https://example.com"))
	_, err := c.BuildRequest(context.Background(), http.MethodPost, "/v1/chat/completions\x07", []byte(`{}`), testAccount("key"))
	if err == nil {
		t.Fatal("expected error for invalid URL path")
	}
	if !strings.Contains(err.Error(), "build request") {
		t.Errorf("expected build request wrap, got %v", err)
	}
}

func TestClient_BuildRequest_NilAccount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("expected no Authorization header for nil account")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(WithBaseURL(srv.URL))
	req, err := c.BuildRequest(context.Background(), http.MethodGet, "/v1/models", nil, nil)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	if req.Header.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q", req.Header.Get("Content-Type"))
	}
}

func TestClient_BuildRequest_ResolverError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(
		WithBaseURL(srv.URL),
		WithCredentialResolver(CredentialResolverFunc(func(*provider.Account) (string, error) {
			return "", errors.New("secret store unreachable")
		})),
	)
	_, err := c.BuildRequest(context.Background(), http.MethodGet, "/v1/models", nil, testAccount("ref"))
	if err == nil || !strings.Contains(err.Error(), "resolve credential") {
		t.Fatalf("expected resolve credential error, got %v", err)
	}
	if strings.Contains(err.Error(), "ref") {
		t.Error("credential reference leaked in error")
	}
}

func TestClient_BuildRequest_UnknownAuthTypeDefaultsBearer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer custom-token" {
			t.Errorf("Authorization = %q, want Bearer custom-token", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	acct := testAccount("custom-token")
	acct.AuthType = "weird_auth"
	c := NewClient(WithBaseURL(srv.URL))
	req, err := c.BuildRequest(context.Background(), http.MethodGet, "/v1/models", nil, acct)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	_, err = c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
}

// --- OpenAI executor edge paths -------------------------------------------------

func TestCompatibleOpenAIExecutor_ExecuteStream_401(t *testing.T) {
	closeCalled := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"bad key"}}`))
	}))
	defer srv.Close()

	client := NewClient(
		WithTransport(&closeTrackingTransport{rt: http.DefaultTransport, closeCalled: closeCalled}),
		WithBaseURL(srv.URL),
	)
	e := NewCompatibleOpenAIExecutor(client)
	body, _ := json.Marshal(map[string]interface{}{"model": "gpt-4o"})
	req := testRequest(engine.FormatOpenAIChat, "gpt-4o", body)

	_, err := e.ExecuteStream(context.Background(), req, testAccount("sk-secret-401"))
	if err == nil {
		t.Fatal("expected error for 401 stream")
	}
	ae := appErr(t, err)
	if ae.Code != shared.ErrUnauthorized || ae.HTTPStatus != http.StatusUnauthorized {
		t.Errorf("AppError = %s/%d, want UNAUTHORIZED/401", ae.Code, ae.HTTPStatus)
	}
	if strings.Contains(err.Error(), "sk-secret-401") {
		t.Error("credential leaked in stream 401 error")
	}
	select {
	case <-closeCalled:
	case <-time.After(time.Second):
		t.Fatal("response body not closed on stream status error")
	}
}

func TestCompatibleOpenAIExecutor_ExecuteStream_TransportError(t *testing.T) {
	e := NewCompatibleOpenAIExecutor(NewClient(WithTransport(&failTransport{err: errors.New("dial refused")})))
	body, _ := json.Marshal(map[string]interface{}{"model": "gpt-4o"})
	req := testRequest(engine.FormatOpenAIChat, "gpt-4o", body)

	_, err := e.ExecuteStream(context.Background(), req, testAccount("key"))
	if err == nil || !strings.Contains(err.Error(), "execute stream request") {
		t.Fatalf("expected transport error wrap, got %v", err)
	}
}

func TestCompatibleOpenAIExecutor_Execute_StatusCodes(t *testing.T) {
	cases := []struct {
		status int
		code   shared.ErrorCode
	}{
		{http.StatusForbidden, shared.ErrUnauthorized},
		{http.StatusTooManyRequests, shared.ErrRateLimited},
		{http.StatusInternalServerError, shared.ErrInternal},
		{http.StatusBadGateway, shared.ErrInternal},
	}
	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				w.Write([]byte(`{"error":"upstream says no"}`))
			}))
			defer srv.Close()

			e := NewCompatibleOpenAIExecutor(testClient(srv))
			body, _ := json.Marshal(map[string]interface{}{"model": "gpt-4o"})
			req := testRequest(engine.FormatOpenAIChat, "gpt-4o", body)

			_, err := e.Execute(context.Background(), req, testAccount("secret-key"))
			if err == nil {
				t.Fatal("expected error")
			}
			ae := appErr(t, err)
			if ae.Code != tc.code || ae.HTTPStatus != tc.status {
				t.Errorf("AppError = %s/%d, want %s/%d", ae.Code, ae.HTTPStatus, tc.code, tc.status)
			}
			if ae.Retryable {
				t.Error("AppError should not be retryable for this path")
			}
			if strings.Contains(err.Error(), "secret-key") {
				t.Error("credential leaked in status error")
			}
			raw, err := json.Marshal(ae)
			if err != nil {
				t.Fatalf("marshal AppError: %v", err)
			}
			if strings.Contains(string(raw), "secret-key") {
				t.Error("credential leaked in marshaled AppError")
			}
		})
	}
}

func TestCompatibleOpenAIExecutor_Execute_BuildRequestError(t *testing.T) {
	client := NewClient(WithCredentialResolver(CredentialResolverFunc(func(*provider.Account) (string, error) {
		return "", errors.New("no creds")
	})))
	e := NewCompatibleOpenAIExecutor(client)
	body, _ := json.Marshal(map[string]interface{}{"model": "gpt-4o"})
	req := testRequest(engine.FormatOpenAIChat, "gpt-4o", body)

	_, err := e.Execute(context.Background(), req, testAccount("key"))
	if err == nil || !strings.Contains(err.Error(), "build request") {
		t.Fatalf("expected build request error, got %v", err)
	}
}

func TestCompatibleOpenAIExecutor_Execute_ReadError(t *testing.T) {
	e := NewCompatibleOpenAIExecutor(NewClient(WithTransport(&readErrTransport{err: errors.New("body read failed")})))
	body, _ := json.Marshal(map[string]interface{}{"model": "gpt-4o"})
	req := testRequest(engine.FormatOpenAIChat, "gpt-4o", body)

	_, err := e.Execute(context.Background(), req, testAccount("key"))
	if err == nil || !strings.Contains(err.Error(), "read response body") {
		t.Fatalf("expected read body error, got %v", err)
	}
}

type readErrTransport struct{ err error }

func (t *readErrTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       &readErrBody{err: t.err},
		Header:     http.Header{},
	}, nil
}

func TestCompatibleOpenAIExecutor_Execute_ParseError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{not json`))
	}))
	defer srv.Close()

	e := NewCompatibleOpenAIExecutor(testClient(srv))
	body, _ := json.Marshal(map[string]interface{}{"model": "gpt-4o"})
	req := testRequest(engine.FormatOpenAIChat, "gpt-4o", body)

	_, err := e.Execute(context.Background(), req, testAccount("key"))
	if err == nil || !strings.Contains(err.Error(), "parse response JSON") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

func TestCompatibleOpenAIExecutor_ExecuteStream_WithOpenAIFormat(t *testing.T) {
	e := NewCompatibleOpenAIExecutor(NewClient(), WithOpenAIFormat(engine.FormatOpenAICompat))
	if e.format != engine.FormatOpenAICompat {
		t.Errorf("format = %s, want openai_compat", e.format)
	}
	e2 := NewCompatibleOpenAIExecutor(NewClient(), WithOpenAIModel("custom-model"))
	if e2.model != "custom-model" {
		t.Errorf("model = %q, want custom-model", e2.model)
	}
	cfg := StreamingConfig{FirstChunkTimeout: time.Second, StallTimeout: 2 * time.Second, DisconnectGrace: time.Millisecond}
	e3 := NewCompatibleOpenAIExecutor(NewClient(), WithOpenAIStreamingConfig(cfg))
	if e3.streamCfg != cfg {
		t.Errorf("streamCfg = %+v, want %+v", e3.streamCfg, cfg)
	}
}

// --- OpenAI SSE streamer edge paths ---------------------------------------------

func TestStreamSSE_FirstChunkTimeout(t *testing.T) {
	pr, pw := io.Pipe()
	defer pr.Close()
	closer := &trackingCloser{inner: pw}

	ctx := context.Background()
	st := stream.NewStream(ctx, 64)
	cfg := StreamingConfig{StallTimeout: 0, DisconnectGrace: 0}

	done := make(chan struct{})
	go func() {
		streamSSE(ctx, st, pr, closer, cfg, 50*time.Millisecond)
		close(done)
	}()

	chunks, state := drainStream(t, st)
	<-done
	if state != stream.StateCancelled {
		t.Errorf("state = %v, want cancelled", state)
	}
	ec := errChunk(chunks)
	if ec == nil || !errors.Is(ec.Error, stream.ErrPeekTimeout) {
		t.Errorf("expected ErrPeekTimeout chunk, got %+v", ec)
	}
	if closer.count() != 1 {
		t.Errorf("closer closed %d times, want 1", closer.count())
	}
}

func TestStreamSSE_StallTimeout(t *testing.T) {
	pr, pw := io.Pipe()
	closer := &trackingCloser{inner: pw}

	ctx := context.Background()
	st := stream.NewStream(ctx, 64)
	cfg := StreamingConfig{StallTimeout: 100 * time.Millisecond, DisconnectGrace: 0}

	go func() {
		pw.Write([]byte("data: first\n\n"))
	}()

	done := make(chan struct{})
	go func() {
		streamSSE(ctx, st, pr, closer, cfg, 0)
		close(done)
	}()

	chunks, state := drainStream(t, st)
	<-done
	if state != stream.StateDone {
		t.Errorf("state = %v, want done", state)
	}
	if len(chunks) < 2 || !chunks[len(chunks)-1].IsFinal {
		t.Errorf("expected data chunk then final chunk, got %d chunks", len(chunks))
	}
	if closer.count() != 1 {
		t.Errorf("closer closed %d times, want 1", closer.count())
	}
}

func TestStreamSSE_Cancellation(t *testing.T) {
	rd := &releaseReader{
		data:    []byte("data: hello\n"),
		release: make(chan struct{}),
		err:     errors.New("read interrupted"),
	}
	closer := &trackingCloser{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st := stream.NewStream(ctx, 64)
	cfg := StreamingConfig{StallTimeout: 0, DisconnectGrace: 0}

	done := make(chan struct{})
	go func() {
		streamSSE(ctx, st, rd, closer, cfg, 0)
		close(done)
	}()

	select {
	case <-st.Chunks():
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first chunk")
	}

	cancel()
	close(rd.release)

	chunks, state := drainStream(t, st)
	<-done
	if state != stream.StateCancelled {
		t.Errorf("state = %v, want cancelled", state)
	}
	if closer.count() != 1 {
		t.Errorf("closer closed %d times, want 1", closer.count())
	}
	ec := errChunk(chunks)
	if ec == nil || !errors.Is(ec.Error, context.Canceled) {
		t.Errorf("expected context.Canceled chunk, got %+v", ec)
	}
}

func TestStreamSSE_ScannerError(t *testing.T) {
	body := "data: first\n\n" + strings.Repeat("x", 300*1024) + "\n"
	closer := &trackingCloser{}

	ctx := context.Background()
	st := stream.NewStream(ctx, 64)
	cfg := StreamingConfig{StallTimeout: 0, DisconnectGrace: 0}

	done := make(chan struct{})
	go func() {
		streamSSE(ctx, st, strings.NewReader(body), closer, cfg, 0)
		close(done)
	}()

	chunks, state := drainStream(t, st)
	<-done
	if state != stream.StateCancelled {
		t.Errorf("state = %v, want cancelled", state)
	}
	ec := errChunk(chunks)
	if ec == nil || !strings.Contains(ec.Error.Error(), "SSE scanner error") {
		t.Errorf("expected SSE scanner error chunk, got %+v", ec)
	}
	if closer.count() != 1 {
		t.Errorf("closer closed %d times, want 1", closer.count())
	}
}

func TestStreamSSE_EOFWithoutTerminal(t *testing.T) {
	closer := &trackingCloser{}
	ctx := context.Background()
	st := stream.NewStream(ctx, 64)
	cfg := StreamingConfig{StallTimeout: 0, DisconnectGrace: 0}

	done := make(chan struct{})
	go func() {
		streamSSE(ctx, st, strings.NewReader("data: hello\n\n"), closer, cfg, 0)
		close(done)
	}()

	chunks, state := drainStream(t, st)
	<-done
	if state != stream.StateDone {
		t.Errorf("state = %v, want done", state)
	}
	if len(chunks) != 2 || chunks[0].IsFinal || !chunks[1].IsFinal {
		t.Errorf("expected data + final chunks, got %+v", chunks)
	}
	if closer.count() != 1 {
		t.Errorf("closer closed %d times, want 1", closer.count())
	}
}

func TestStreamSSE_EventAndEmptyLine(t *testing.T) {
	body := "event: role_assistant\ndata: {\"delta\":\"x\"}\n\n\n\n" +
		"event: role_assistant\ndata: {\"delta\":\"y\"}\n\n"
	closer := &trackingCloser{}
	ctx := context.Background()
	st := stream.NewStream(ctx, 64)

	done := make(chan struct{})
	go func() {
		streamSSE(ctx, st, strings.NewReader(body), closer, cfgSSE(), 0)
		close(done)
	}()

	chunks, _ := drainStream(t, st)
	<-done
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}
	if chunks[0].Event != "role_assistant" || string(chunks[0].Data) != `{"delta":"x"}` {
		t.Errorf("chunk[0] = %+v", chunks[0])
	}
	if chunks[1].Event != "role_assistant" || string(chunks[1].Data) != `{"delta":"y"}` {
		t.Errorf("chunk[1] = %+v", chunks[1])
	}
	if !chunks[2].IsFinal {
		t.Error("expected final chunk at EOF")
	}
}

func cfgSSE() StreamingConfig {
	return StreamingConfig{StallTimeout: 0, DisconnectGrace: 0}
}

func TestStreamSSE_PushFailureStopsScanner(t *testing.T) {
	pr, pw := io.Pipe()
	closer := &trackingCloser{inner: pw}
	ctx := context.Background()
	st := stream.NewStream(ctx, 64)

	done := make(chan struct{})
	go func() {
		streamSSE(ctx, st, pr, closer, cfgSSE(), 0)
		close(done)
	}()
	go func() {
		pw.Write([]byte("data: a\n\n"))
	}()

	select {
	case <-st.Chunks():
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first chunk")
	}

	st.Cancel(errConsumerDone)
	pw.Write([]byte("data: b\n\n"))

	<-done
	if closer.count() != 1 {
		t.Errorf("closer closed %d times, want 1", closer.count())
	}
	if st.State() != stream.StateCancelled {
		t.Errorf("state = %v, want cancelled", st.State())
	}
}

type panicReader struct{}

func (panicReader) Read([]byte) (int, error) { panic("boom") }

func TestStreamSSE_PanicRecovered(t *testing.T) {
	closer := &trackingCloser{}
	ctx := context.Background()
	st := stream.NewStream(ctx, 64)

	done := make(chan struct{})
	go func() {
		streamSSE(ctx, st, panicReader{}, closer, cfgSSE(), 0)
		close(done)
	}()

	chunks, state := drainStream(t, st)
	<-done
	if state != stream.StateCancelled {
		t.Errorf("state = %v, want cancelled", state)
	}
	ec := errChunk(chunks)
	if ec == nil || !strings.Contains(ec.Error.Error(), "panic in generic SSE streamer") {
		t.Errorf("expected panic error chunk, got %+v", ec)
	}
	if closer.count() != 1 {
		t.Errorf("closer closed %d times, want 1", closer.count())
	}
}

func TestStreamSSE_ScanErrorAfterStreamClosed(t *testing.T) {
	rd := &releaseReader{
		data:    []byte("data: a\n"),
		release: make(chan struct{}),
		err:     errors.New("cut"),
	}
	closer := &trackingCloser{}
	ctx := context.Background()
	st := stream.NewStream(ctx, 64)

	done := make(chan struct{})
	go func() {
		streamSSE(ctx, st, rd, closer, cfgSSE(), 0)
		close(done)
	}()

	select {
	case <-st.Chunks():
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first chunk")
	}

	st.Cancel(errConsumerDone)
	close(rd.release)

	chunks, state := drainStream(t, st)
	<-done
	if state != stream.StateCancelled {
		t.Errorf("state = %v, want cancelled", state)
	}
	ec := errChunk(chunks)
	if ec == nil || !errors.Is(ec.Error, errConsumerDone) {
		t.Errorf("expected consumer cancel error chunk, got %+v", ec)
	}
	if closer.count() != 1 {
		t.Errorf("closer closed %d times, want 1", closer.count())
	}
}

func TestInjectStreamParam_ReadError(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "http://x/v1/chat/completions", nil)
	req.Body = &readErrBody{err: errors.New("read failed")}
	out, err := injectStreamParam(req, true)
	if err != nil {
		t.Fatalf("injectStreamParam read error: %v", err)
	}
	if out != req {
		t.Error("expected same request returned on read error")
	}
}

func TestCompatibleOpenAIExecutor_Execute_TransportError(t *testing.T) {
	e := NewCompatibleOpenAIExecutor(NewClient(WithTransport(&failTransport{err: errors.New("dial refused")})))
	body, _ := json.Marshal(map[string]interface{}{"model": "gpt-4o"})
	req := testRequest(engine.FormatOpenAIChat, "gpt-4o", body)

	_, err := e.Execute(context.Background(), req, testAccount("key"))
	if err == nil || !strings.Contains(err.Error(), "execute request") {
		t.Fatalf("expected transport error wrap, got %v", err)
	}
}

func TestDefaultResolver_NilAccount(t *testing.T) {
	cred, err := defaultResolver.Resolve(nil)
	if err != nil || cred != "" {
		t.Errorf("defaultResolver(nil) = %q, %v; want empty, nil", cred, err)
	}
}

func TestListModels_ReadBodyError(t *testing.T) {
	client := NewClient(WithTransport(&readErrTransport{err: errors.New("body read failed")}))
	_, err := ListModels(context.Background(), client, testAccount("key"))
	if err == nil || !strings.Contains(err.Error(), "list models read body") {
		t.Fatalf("expected read body error, got %v", err)
	}
}

func TestProbeEmbeddings_TransportError(t *testing.T) {
	client := NewClient(WithTransport(&failTransport{err: errors.New("network down")}))
	found, err := ProbeEmbeddings(context.Background(), client, testAccount("key"))
	if err != nil {
		t.Fatalf("ProbeEmbeddings transport error should return nil error, got %v", err)
	}
	if found {
		t.Error("expected embeddings probe to be false on transport error")
	}
}

func TestStreamAnthropicSSE_PushFailureStopsScanner(t *testing.T) {
	pr, pw := io.Pipe()
	closer := &trackingCloser{inner: pw}
	ctx := context.Background()
	st := stream.NewStream(ctx, 64)

	done := make(chan struct{})
	go func() {
		streamAnthropicSSE(ctx, st, pr, closer, cfgSSE(), 0)
		close(done)
	}()
	go func() {
		pw.Write([]byte("event: content_block_delta\ndata: {\"t\":1}\n\n"))
	}()

	select {
	case <-st.Chunks():
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first chunk")
	}

	st.Cancel(errConsumerDone)
	pw.Write([]byte("event: content_block_delta\ndata: {\"t\":2}\n\n"))

	<-done
	if closer.count() != 1 {
		t.Errorf("closer closed %d times, want 1", closer.count())
	}
	if st.State() != stream.StateCancelled {
		t.Errorf("state = %v, want cancelled", st.State())
	}
}

func TestStreamAnthropicSSE_PanicRecovered(t *testing.T) {
	closer := &trackingCloser{}
	ctx := context.Background()
	st := stream.NewStream(ctx, 64)

	done := make(chan struct{})
	go func() {
		streamAnthropicSSE(ctx, st, panicReader{}, closer, cfgSSE(), 0)
		close(done)
	}()

	chunks, state := drainStream(t, st)
	<-done
	if state != stream.StateCancelled {
		t.Errorf("state = %v, want cancelled", state)
	}
	ec := errChunk(chunks)
	if ec == nil || !strings.Contains(ec.Error.Error(), "panic in Anthropic SSE streamer") {
		t.Errorf("expected panic error chunk, got %+v", ec)
	}
	if closer.count() != 1 {
		t.Errorf("closer closed %d times, want 1", closer.count())
	}
}

func TestStreamAnthropicSSE_ScanErrorAfterStreamClosed(t *testing.T) {
	rd := &releaseReader{
		data:    []byte("event: content_block_delta\ndata: {\"t\":1}\n\n"),
		release: make(chan struct{}),
		err:     errors.New("cut"),
	}
	closer := &trackingCloser{}
	ctx := context.Background()
	st := stream.NewStream(ctx, 64)

	done := make(chan struct{})
	go func() {
		streamAnthropicSSE(ctx, st, rd, closer, cfgSSE(), 0)
		close(done)
	}()

	select {
	case <-st.Chunks():
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first chunk")
	}

	st.Cancel(errConsumerDone)
	close(rd.release)

	chunks, state := drainStream(t, st)
	<-done
	if state != stream.StateCancelled {
		t.Errorf("state = %v, want cancelled", state)
	}
	ec := errChunk(chunks)
	if ec == nil || !errors.Is(ec.Error, errConsumerDone) {
		t.Errorf("expected consumer cancel error chunk, got %+v", ec)
	}
	if closer.count() != 1 {
		t.Errorf("closer closed %d times, want 1", closer.count())
	}
}

// --- Anthropic executor edge paths ----------------------------------------------

func TestCompatibleAnthropicExecutor_ExecuteStream_Success(t *testing.T) {
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "ant-edge-key" {
			t.Errorf("x-api-key = %q", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Error("missing anthropic-version")
		}
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("Accept = %q", r.Header.Get("Accept"))
		}
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmtStream(w,
			"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\"}}\n\n",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"Hi\"}}\n\n",
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
		)
	}))
	defer srv.Close()

	client := NewClient(WithTransport(http.DefaultTransport), WithBaseURL(srv.URL))
	e := NewCompatibleAnthropicExecutor(client, WithAnthropicModel("claude-edge"))
	body, _ := json.Marshal(map[string]interface{}{"model": "claude-3", "messages": []map[string]interface{}{{"role": "user", "content": "hi"}}})
	req := testRequest(engine.FormatAnthropic, "claude-3", body)

	acct := testAccount("ant-edge-key")
	acct.AuthType = "api_key"

	resp, err := e.ExecuteStream(context.Background(), req, acct)
	if err != nil {
		t.Fatalf("ExecuteStream: %v", err)
	}
	if resp.Stream == nil {
		t.Fatal("expected stream")
	}
	if resp.Model != "claude-edge" {
		t.Errorf("Model = %q, want claude-edge override", resp.Model)
	}
	if gotBody["stream"] != true {
		t.Errorf("stream param = %v, want true", gotBody["stream"])
	}

	chunks, state := drainStream(t, resp.Stream.(*stream.Stream))
	if state != stream.StateDone {
		t.Errorf("state = %v, want done", state)
	}
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}
	if chunks[0].Event != "message_start" {
		t.Errorf("chunk[0].Event = %q", chunks[0].Event)
	}
	if !chunks[2].IsFinal || chunks[2].Event != "message_stop" {
		t.Errorf("terminal chunk = %+v", chunks[2])
	}
}

func fmtStream(w http.ResponseWriter, events ...string) {
	for _, e := range events {
		io.WriteString(w, e)
	}
}

func TestCompatibleAnthropicExecutor_ExecuteStream_401(t *testing.T) {
	closeCalled := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"type":"error","error":{"type":"authentication_error"}}`))
	}))
	defer srv.Close()

	client := NewClient(
		WithTransport(&closeTrackingTransport{rt: http.DefaultTransport, closeCalled: closeCalled}),
		WithBaseURL(srv.URL),
	)
	e := NewCompatibleAnthropicExecutor(client)
	body, _ := json.Marshal(map[string]interface{}{"model": "claude-3"})
	req := testRequest(engine.FormatAnthropic, "claude-3", body)

	acct := testAccount("sk-ant-edge-401")
	acct.AuthType = "anthropic_api_key"

	_, err := e.ExecuteStream(context.Background(), req, acct)
	if err == nil {
		t.Fatal("expected 401 error")
	}
	ae := appErr(t, err)
	if ae.Code != shared.ErrUnauthorized || ae.HTTPStatus != http.StatusUnauthorized {
		t.Errorf("AppError = %s/%d, want UNAUTHORIZED/401", ae.Code, ae.HTTPStatus)
	}
	if strings.Contains(err.Error(), "sk-ant-edge-401") {
		t.Error("credential leaked in anthropic stream error")
	}
	select {
	case <-closeCalled:
	case <-time.After(time.Second):
		t.Fatal("response body not closed on anthropic stream status error")
	}
}

func TestCompatibleAnthropicExecutor_Execute_TransportError(t *testing.T) {
	e := NewCompatibleAnthropicExecutor(NewClient(WithTransport(&failTransport{err: errors.New("dial refused")})))
	body, _ := json.Marshal(map[string]interface{}{"model": "claude-3"})
	req := testRequest(engine.FormatAnthropic, "claude-3", body)

	acct := testAccount("key")
	acct.AuthType = "anthropic_api_key"

	_, err := e.Execute(context.Background(), req, acct)
	if err == nil || !strings.Contains(err.Error(), "execute request") {
		t.Fatalf("expected transport error wrap, got %v", err)
	}
}

func TestCompatibleAnthropicExecutor_Execute_ReadError(t *testing.T) {
	e := NewCompatibleAnthropicExecutor(NewClient(WithTransport(&readErrTransport{err: errors.New("body read failed")})))
	body, _ := json.Marshal(map[string]interface{}{"model": "claude-3"})
	req := testRequest(engine.FormatAnthropic, "claude-3", body)

	acct := testAccount("key")
	acct.AuthType = "anthropic_api_key"

	_, err := e.Execute(context.Background(), req, acct)
	if err == nil || !strings.Contains(err.Error(), "read response body") {
		t.Fatalf("expected read body error, got %v", err)
	}
}

func TestCompatibleAnthropicExecutor_ExecuteStream_BuildRequestError(t *testing.T) {
	client := NewClient(WithCredentialResolver(CredentialResolverFunc(func(*provider.Account) (string, error) {
		return "", errors.New("no creds")
	})))
	e := NewCompatibleAnthropicExecutor(client)
	body, _ := json.Marshal(map[string]interface{}{"model": "claude-3"})
	req := testRequest(engine.FormatAnthropic, "claude-3", body)

	acct := testAccount("key")
	acct.AuthType = "anthropic_api_key"

	_, err := e.ExecuteStream(context.Background(), req, acct)
	if err == nil || !strings.Contains(err.Error(), "build stream request") {
		t.Fatalf("expected build stream request error, got %v", err)
	}
}

func TestCompatibleAnthropicExecutor_ExecuteStream_TransportError(t *testing.T) {
	e := NewCompatibleAnthropicExecutor(NewClient(WithTransport(&failTransport{err: errors.New("dial refused")})))
	body, _ := json.Marshal(map[string]interface{}{"model": "claude-3"})
	req := testRequest(engine.FormatAnthropic, "claude-3", body)

	acct := testAccount("key")
	acct.AuthType = "anthropic_api_key"

	_, err := e.ExecuteStream(context.Background(), req, acct)
	if err == nil || !strings.Contains(err.Error(), "execute stream request") {
		t.Fatalf("expected transport error wrap, got %v", err)
	}
}

func TestInjectAnthropicStreamParam_ReadError(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "http://x/v1/messages", nil)
	req.Body = &readErrBody{err: errors.New("read failed")}
	out, err := injectAnthropicStreamParam(req, true)
	if err != nil {
		t.Fatalf("injectAnthropicStreamParam read error: %v", err)
	}
	if out != req {
		t.Error("expected same request returned on read error")
	}
}

func TestCompatibleAnthropicExecutor_Execute_429And500(t *testing.T) {
	cases := []struct {
		status int
		code   shared.ErrorCode
	}{
		{http.StatusTooManyRequests, shared.ErrRateLimited},
		{http.StatusInternalServerError, shared.ErrInternal},
	}
	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				w.Write([]byte(`{"type":"error","error":{"message":"nope"}}`))
			}))
			defer srv.Close()

			e := NewCompatibleAnthropicExecutor(testClient(srv))
			body, _ := json.Marshal(map[string]interface{}{"model": "claude-3"})
			req := testRequest(engine.FormatAnthropic, "claude-3", body)

			acct := testAccount("sk-ant-secret-2")
			acct.AuthType = "anthropic_api_key"

			_, err := e.Execute(context.Background(), req, acct)
			if err == nil {
				t.Fatal("expected error")
			}
			ae := appErr(t, err)
			if ae.Code != tc.code || ae.HTTPStatus != tc.status {
				t.Errorf("AppError = %s/%d, want %s/%d", ae.Code, ae.HTTPStatus, tc.code, tc.status)
			}
			if strings.Contains(err.Error(), "sk-ant-secret-2") {
				t.Error("credential leaked")
			}
		})
	}
}

func TestCompatibleAnthropicExecutor_Execute_BuildRequestError(t *testing.T) {
	client := NewClient(WithCredentialResolver(CredentialResolverFunc(func(*provider.Account) (string, error) {
		return "", errors.New("no creds")
	})))
	e := NewCompatibleAnthropicExecutor(client)
	body, _ := json.Marshal(map[string]interface{}{"model": "claude-3"})
	req := testRequest(engine.FormatAnthropic, "claude-3", body)

	_, err := e.Execute(context.Background(), req, testAccount("key"))
	if err == nil || !strings.Contains(err.Error(), "build request") {
		t.Fatalf("expected build request error, got %v", err)
	}
}

func TestCompatibleAnthropicExecutor_Execute_ParseError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`not json at all`))
	}))
	defer srv.Close()

	e := NewCompatibleAnthropicExecutor(testClient(srv))
	body, _ := json.Marshal(map[string]interface{}{"model": "claude-3"})
	req := testRequest(engine.FormatAnthropic, "claude-3", body)

	acct := testAccount("key")
	acct.AuthType = "anthropic_api_key"

	_, err := e.Execute(context.Background(), req, acct)
	if err == nil || !strings.Contains(err.Error(), "parse Anthropic response JSON") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

func TestCompatibleAnthropicExecutor_WithAnthropicStreamingConfig(t *testing.T) {
	cfg := StreamingConfig{FirstChunkTimeout: time.Second, StallTimeout: 2 * time.Second, DisconnectGrace: time.Millisecond}
	e := NewCompatibleAnthropicExecutor(NewClient(), WithAnthropicStreamingConfig(cfg))
	if e.streamCfg != cfg {
		t.Errorf("streamCfg = %+v, want %+v", e.streamCfg, cfg)
	}
}

// --- Anthropic SSE streamer edge paths ------------------------------------------

func TestStreamAnthropicSSE_MessageStopTerminal(t *testing.T) {
	body := "event: message_start\ndata: {\"type\":\"message_start\"}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	closer := &trackingCloser{}
	ctx := context.Background()
	st := stream.NewStream(ctx, 64)

	done := make(chan struct{})
	go func() {
		streamAnthropicSSE(ctx, st, strings.NewReader(body), closer, cfgSSE(), 0)
		close(done)
	}()

	chunks, state := drainStream(t, st)
	<-done
	if state != stream.StateDone {
		t.Errorf("state = %v, want done", state)
	}
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if !chunks[1].IsFinal || chunks[1].Event != "message_stop" {
		t.Errorf("terminal chunk = %+v", chunks[1])
	}
	if closer.count() != 1 {
		t.Errorf("closer closed %d times, want 1", closer.count())
	}
}

func TestStreamAnthropicSSE_ErrorEvent(t *testing.T) {
	body := "event: content_block_delta\ndata: {\"type\":\"content_block_delta\"}\n\n" +
		"event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\"}}\n\n"
	closer := &trackingCloser{}
	ctx := context.Background()
	st := stream.NewStream(ctx, 64)

	done := make(chan struct{})
	go func() {
		streamAnthropicSSE(ctx, st, strings.NewReader(body), closer, cfgSSE(), 0)
		close(done)
	}()

	chunks, state := drainStream(t, st)
	<-done
	if state != stream.StateCancelled {
		t.Errorf("state = %v, want cancelled", state)
	}
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks (data, error terminal, error chunk), got %d", len(chunks))
	}
	if !chunks[1].IsFinal || chunks[1].Event != "error" {
		t.Errorf("terminal chunk = %+v", chunks[1])
	}
	ec := errChunk(chunks)
	if ec == nil || !strings.Contains(ec.Error.Error(), "Anthropic stream error") {
		t.Errorf("expected Anthropic stream error chunk, got %+v", ec)
	}
	if closer.count() != 1 {
		t.Errorf("closer closed %d times, want 1", closer.count())
	}
}

func TestStreamAnthropicSSE_FirstChunkTimeout(t *testing.T) {
	pr, pw := io.Pipe()
	defer pr.Close()
	closer := &trackingCloser{inner: pw}
	ctx := context.Background()
	st := stream.NewStream(ctx, 64)

	done := make(chan struct{})
	go func() {
		streamAnthropicSSE(ctx, st, pr, closer, cfgSSE(), 50*time.Millisecond)
		close(done)
	}()

	chunks, state := drainStream(t, st)
	<-done
	if state != stream.StateCancelled {
		t.Errorf("state = %v, want cancelled", state)
	}
	ec := errChunk(chunks)
	if ec == nil || !errors.Is(ec.Error, stream.ErrPeekTimeout) {
		t.Errorf("expected ErrPeekTimeout chunk, got %+v", ec)
	}
}

func TestStreamAnthropicSSE_StallTimeout(t *testing.T) {
	pr, pw := io.Pipe()
	closer := &trackingCloser{inner: pw}
	ctx := context.Background()
	st := stream.NewStream(ctx, 64)
	cfg := StreamingConfig{StallTimeout: 100 * time.Millisecond, DisconnectGrace: 0}

	go func() {
		pw.Write([]byte("event: content_block_delta\ndata: {\"text\":\"hi\"}\n\n"))
	}()

	done := make(chan struct{})
	go func() {
		streamAnthropicSSE(ctx, st, pr, closer, cfg, 0)
		close(done)
	}()

	chunks, state := drainStream(t, st)
	<-done
	if state != stream.StateDone {
		t.Errorf("state = %v, want done", state)
	}
	if len(chunks) < 2 || !chunks[len(chunks)-1].IsFinal {
		t.Errorf("expected data + final chunks, got %d chunks", len(chunks))
	}
	if closer.count() != 1 {
		t.Errorf("closer closed %d times, want 1", closer.count())
	}
}

func TestStreamAnthropicSSE_Cancellation(t *testing.T) {
	rd := &releaseReader{
		data:    []byte("event: content_block_delta\ndata: {\"t\":1}\n\n"),
		release: make(chan struct{}),
		err:     errors.New("stream cut"),
	}
	closer := &trackingCloser{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st := stream.NewStream(ctx, 64)

	done := make(chan struct{})
	go func() {
		streamAnthropicSSE(ctx, st, rd, closer, cfgSSE(), 0)
		close(done)
	}()

	select {
	case <-st.Chunks():
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first chunk")
	}

	cancel()
	close(rd.release)

	chunks, state := drainStream(t, st)
	<-done
	if state != stream.StateCancelled {
		t.Errorf("state = %v, want cancelled", state)
	}
	if closer.count() != 1 {
		t.Errorf("closer closed %d times, want 1", closer.count())
	}
	ec := errChunk(chunks)
	if ec == nil || !errors.Is(ec.Error, context.Canceled) {
		t.Errorf("expected context.Canceled chunk, got %+v", ec)
	}
}

func TestStreamAnthropicSSE_ScannerError(t *testing.T) {
	body := "event: content_block_delta\ndata: {\"t\":1}\n\n" + strings.Repeat("y", 300*1024) + "\n"
	closer := &trackingCloser{}
	ctx := context.Background()
	st := stream.NewStream(ctx, 64)

	done := make(chan struct{})
	go func() {
		streamAnthropicSSE(ctx, st, strings.NewReader(body), closer, cfgSSE(), 0)
		close(done)
	}()

	chunks, state := drainStream(t, st)
	<-done
	if state != stream.StateCancelled {
		t.Errorf("state = %v, want cancelled", state)
	}
	ec := errChunk(chunks)
	if ec == nil || !strings.Contains(ec.Error.Error(), "Anthropic SSE scanner error") {
		t.Errorf("expected Anthropic SSE scanner error chunk, got %+v", ec)
	}
	if closer.count() != 1 {
		t.Errorf("closer closed %d times, want 1", closer.count())
	}
}

// --- status helpers -------------------------------------------------------------

func TestCheckResponseStatus_ExactCodes(t *testing.T) {
	cases := []struct {
		status int
		code   shared.ErrorCode
	}{
		{http.StatusUnauthorized, shared.ErrUnauthorized},
		{http.StatusForbidden, shared.ErrUnauthorized},
		{http.StatusTooManyRequests, shared.ErrRateLimited},
		{http.StatusInternalServerError, shared.ErrInternal},
		{http.StatusBadRequest, shared.ErrInternal},
	}
	for _, tc := range cases {
		resp := &http.Response{
			StatusCode: tc.status,
			Body:       io.NopCloser(strings.NewReader(`{"error":"boom"}`)),
			Header:     http.Header{},
		}
		err := checkResponseStatus(resp)
		if err == nil {
			t.Fatalf("status %d: expected error", tc.status)
		}
		ae := appErr(t, err)
		if ae.Code != tc.code || ae.HTTPStatus != tc.status {
			t.Errorf("status %d: AppError = %s/%d, want %s/%d", tc.status, ae.Code, ae.HTTPStatus, tc.code, tc.status)
		}
	}
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok")), Header: http.Header{}}
	if err := checkResponseStatus(resp); err != nil {
		t.Errorf("2xx should not error, got %v", err)
	}
}

func TestCheckAnthropicResponseStatus_ExactCodes(t *testing.T) {
	cases := []struct {
		status int
		code   shared.ErrorCode
	}{
		{http.StatusUnauthorized, shared.ErrUnauthorized},
		{http.StatusForbidden, shared.ErrUnauthorized},
		{http.StatusTooManyRequests, shared.ErrRateLimited},
		{http.StatusServiceUnavailable, shared.ErrInternal},
	}
	for _, tc := range cases {
		resp := &http.Response{
			StatusCode: tc.status,
			Body:       io.NopCloser(strings.NewReader(`{"type":"error"}`)),
			Header:     http.Header{},
		}
		err := checkAnthropicResponseStatus(resp)
		if err == nil {
			t.Fatalf("status %d: expected error", tc.status)
		}
		ae := appErr(t, err)
		if ae.Code != tc.code || ae.HTTPStatus != tc.status {
			t.Errorf("status %d: AppError = %s/%d, want %s/%d", tc.status, ae.Code, ae.HTTPStatus, tc.code, tc.status)
		}
	}
}

func TestTruncateString(t *testing.T) {
	if got := truncateString("short", 512); got != "short" {
		t.Errorf("short string changed: %q", got)
	}
	long := strings.Repeat("a", 600)
	got := truncateString(long, 100)
	if len(got) != 103 || !strings.HasSuffix(got, "...") {
		t.Errorf("truncateString = len %d, want 103 with suffix", len(got))
	}
}

// --- helpers: selectBody / resolveModel / injectStreamParam -----------------------

func TestSelectBody(t *testing.T) {
	req := &engine.Request{RawBody: []byte("raw"), MappedBody: []byte("mapped")}
	if got := string(selectBody(req)); got != "mapped" {
		t.Errorf("selectBody = %q, want mapped", got)
	}
	req2 := &engine.Request{RawBody: []byte("raw")}
	if got := string(selectBody(req2)); got != "raw" {
		t.Errorf("selectBody = %q, want raw", got)
	}
}

func TestResolveModel(t *testing.T) {
	if got := resolveModel("override", &engine.Request{Model: "req-model"}); got != "override" {
		t.Errorf("resolveModel override = %q", got)
	}
	if got := resolveModel("", &engine.Request{Model: "req-model"}); got != "req-model" {
		t.Errorf("resolveModel req = %q", got)
	}
	if got := resolveModel("", nil); got != "gpt-4o" {
		t.Errorf("resolveModel default = %q, want gpt-4o", got)
	}
}

func TestInjectStreamParam(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "http://x/v1/chat/completions", strings.NewReader(`{"model":"gpt-4"}`))
	out, err := injectStreamParam(req, true)
	if err != nil {
		t.Fatalf("injectStreamParam: %v", err)
	}
	body, _ := io.ReadAll(out.Body)
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload["stream"] != true {
		t.Errorf("stream = %v, want true", payload["stream"])
	}
	if out.ContentLength != int64(len(body)) {
		t.Errorf("ContentLength = %d, want %d", out.ContentLength, len(body))
	}
}

func TestInjectStreamParam_NilBody(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "http://x/v1/chat/completions", nil)
	out, err := injectStreamParam(req, true)
	if err != nil {
		t.Fatalf("injectStreamParam nil body: %v", err)
	}
	if out != req {
		t.Error("expected same request returned for nil body")
	}
}

func TestInjectStreamParam_InvalidJSON(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "http://x/v1/chat/completions", strings.NewReader(`not json`))
	out, err := injectStreamParam(req, true)
	if err != nil {
		t.Fatalf("injectStreamParam invalid json: %v", err)
	}
	body, _ := io.ReadAll(out.Body)
	if string(body) != "not json" {
		t.Errorf("body = %q, want unchanged", body)
	}
}

func TestInjectAnthropicStreamParam(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "http://x/v1/messages", strings.NewReader(`{"model":"claude-3"}`))
	out, err := injectAnthropicStreamParam(req, true)
	if err != nil {
		t.Fatalf("injectAnthropicStreamParam: %v", err)
	}
	body, _ := io.ReadAll(out.Body)
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload["stream"] != true {
		t.Errorf("stream = %v, want true", payload["stream"])
	}
}

func TestInjectAnthropicStreamParam_InvalidJSON(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "http://x/v1/messages", strings.NewReader(`not json`))
	out, err := injectAnthropicStreamParam(req, true)
	if err != nil {
		t.Fatalf("injectAnthropicStreamParam invalid json: %v", err)
	}
	body, _ := io.ReadAll(out.Body)
	if string(body) != "not json" {
		t.Errorf("body = %q, want unchanged", body)
	}
}

func TestInjectAnthropicStreamParam_NilBody(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "http://x/v1/messages", nil)
	out, err := injectAnthropicStreamParam(req, true)
	if err != nil {
		t.Fatalf("injectAnthropicStreamParam nil body: %v", err)
	}
	if out != req {
		t.Error("expected same request returned for nil body")
	}
}

// --- parse helpers ----------------------------------------------------------------

func TestParseResponse_MalformedJSON(t *testing.T) {
	req := testRequest(engine.FormatOpenAIChat, "gpt-4o", []byte(`{}`))
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}}
	if _, err := parseResponse(req, resp, []byte(`{oops`)); err == nil ||
		!strings.Contains(err.Error(), "parse response JSON") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

func TestParseAnthropicResponse_MalformedJSON(t *testing.T) {
	req := testRequest(engine.FormatAnthropic, "claude-3", []byte(`{}`))
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}}
	if _, err := parseAnthropicResponse(req, resp, []byte(`{oops`)); err == nil ||
		!strings.Contains(err.Error(), "parse Anthropic response JSON") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

// --- model probing ----------------------------------------------------------------

func TestListModels_Non2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":"down"}`))
	}))
	defer srv.Close()

	client := NewClient(WithBaseURL(srv.URL))
	_, err := ListModels(context.Background(), client, testAccount("key"))
	if err == nil || !strings.Contains(err.Error(), "unexpected status 503") {
		t.Fatalf("expected unexpected status error, got %v", err)
	}
}

func TestListModels_ParseError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{bad`))
	}))
	defer srv.Close()

	client := NewClient(WithBaseURL(srv.URL))
	_, err := ListModels(context.Background(), client, testAccount("key"))
	if err == nil || !strings.Contains(err.Error(), "list models parse response") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

func TestListModels_TransportError(t *testing.T) {
	client := NewClient(WithTransport(&failTransport{err: errors.New("network down")}))
	_, err := ListModels(context.Background(), client, testAccount("key"))
	if err == nil || !strings.Contains(err.Error(), "list models request") {
		t.Fatalf("expected transport error wrap, got %v", err)
	}
}

func TestListModels_BuildRequestError(t *testing.T) {
	client := NewClient(WithCredentialResolver(CredentialResolverFunc(func(*provider.Account) (string, error) {
		return "", errors.New("no creds")
	})))
	_, err := ListModels(context.Background(), client, testAccount("ref"))
	if err == nil || !strings.Contains(err.Error(), "build models request") {
		t.Fatalf("expected build models request error, got %v", err)
	}
}

func TestProbeModel_PropagatesListError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"forbidden"}`))
	}))
	defer srv.Close()

	client := NewClient(WithBaseURL(srv.URL))
	_, err := ProbeModel(context.Background(), client, testAccount("key"), "gpt-4o")
	if err == nil || !strings.Contains(err.Error(), "probe model") {
		t.Fatalf("expected probe model wrap, got %v", err)
	}
}

func TestProbeEmbeddings_ModelsErrorFallsBackToEndpoint(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch r.URL.Path {
		case "/v1/models":
			w.WriteHeader(http.StatusInternalServerError)
		case "/v1/embeddings":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":[]}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	client := NewClient(WithBaseURL(srv.URL))
	found, err := ProbeEmbeddings(context.Background(), client, testAccount("key"))
	if err != nil {
		t.Fatalf("ProbeEmbeddings: %v", err)
	}
	if !found {
		t.Error("expected embeddings probe fallback to succeed")
	}
	if calls != 2 {
		t.Errorf("expected 2 calls (models + embeddings), got %d", calls)
	}
}

func TestProbeEmbeddings_EndpointNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.WriteHeader(http.StatusInternalServerError)
		case "/v1/embeddings":
			w.WriteHeader(http.StatusNotFound)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	client := NewClient(WithBaseURL(srv.URL))
	found, err := ProbeEmbeddings(context.Background(), client, testAccount("key"))
	if err != nil {
		t.Fatalf("ProbeEmbeddings: %v", err)
	}
	if found {
		t.Error("expected embeddings probe to fail for 404 endpoint")
	}
}

func TestProbeEmbeddings_ResolverFailure(t *testing.T) {
	client := NewClient(WithCredentialResolver(CredentialResolverFunc(func(*provider.Account) (string, error) {
		return "", errors.New("vault down")
	})))
	found, err := ProbeEmbeddings(context.Background(), client, testAccount("ref"))
	if err != nil {
		t.Fatalf("ProbeEmbeddings resolver failure should return nil error, got %v", err)
	}
	if found {
		t.Error("expected embeddings probe to be false on resolver failure")
	}
}
