package specialized

// Wire-level tests for the native Gemini executor (provider.ProviderGemini).
//
// Authority: registry/gemini.js @ decolua/9router
// 79918c7830695bbca4a45c9fea4a42c3e9fd73d1 (open-sse/providers/registry/gemini.js):
//
//	transport.baseUrl = "https://generativelanguage.googleapis.com/v1beta/models"
//	transport.format   = "gemini"
//	transport.auth     = { apiKey: { header: "x-goog-api-key", scheme: "raw" },
//	                       oauth:  { header: "Authorization", scheme: "bearer" } }
//
// The default executor (open-sse/executors/default.js) builds
// `${baseUrl}/${model}:${stream ? "streamGenerateContent?alt=sse" : "generateContent"}`
// and passes the GenerateContentRequest body through unchanged.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

	"github.com/google/uuid"
)

const geminiNativeAPIKey = "AIzaSyTESTKEY1234567890"
const geminiNativeOAuthToken = "ya29.GEMINITESTTOKEN"

func newGeminiOAuthAccount(credentialRef string) *provider.Account {
	return &provider.Account{
		ID:            uuid.New(),
		CredentialRef: credentialRef,
		AuthType:      "oauth",
		IsEnabled:     true,
	}
}

// geminiNativeExecutor resolves the ProviderGemini executor through the same
// dispatch the registry factory uses (specialized.GetExecutor).
func geminiNativeExecutor(t *testing.T) engine.Executor {
	t.Helper()
	exec, ok := GetExecutor(provider.ProviderGemini, http.DefaultTransport)
	if !ok {
		t.Fatal("GetExecutor(ProviderGemini) not dispatched")
	}
	return exec
}

func geminiNativeGenerateRequest() []byte {
	return []byte(`{"contents":[{"parts":[{"text":"Hello"}]}],"generationConfig":{"temperature":0.7}}`)
}

func TestGeminiNativeExecutor_ProviderType(t *testing.T) {
	exec := geminiNativeExecutor(t)
	if got := exec.ProviderType(); got != provider.ProviderGemini {
		t.Errorf("ProviderType() = %q, want %q (native identity)", got, provider.ProviderGemini)
	}
}

func TestGeminiNativeExecutor_SupportsFormat(t *testing.T) {
	exec := geminiNativeExecutor(t)
	if !exec.SupportsFormat(engine.FormatGemini) {
		t.Error("expected to support FormatGemini")
	}
	for _, f := range []engine.RequestFormat{engine.FormatOpenAIChat, engine.FormatOpenAICompat, engine.FormatCodexResponses, engine.FormatAnthropic} {
		if exec.SupportsFormat(f) {
			t.Errorf("expected NOT to support %s", f)
		}
	}
}

// TestGeminiNativeExecutor_Execute_WireContract asserts the pinned native URL
// shape ({baseUrl}/{model}:generateContent), raw GenerateContentRequest body
// passthrough, and API-key auth (x-goog-api-key, raw scheme).
func TestGeminiNativeExecutor_Execute_WireContract(t *testing.T) {
	var (
		gotPath    string
		gotQuery   string
		gotAuthKey string
		gotBearer  string
		gotBody    []byte
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotAuthKey = r.Header.Get("x-goog-api-key")
		gotBearer = r.Header.Get("Authorization")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"candidates":[{"content":{"parts":[{"text":"hi"}]}}]}`)
	}))
	defer srv.Close()

	exec := geminiNativeExecutor(t)
	req := newTestRequest(engine.FormatGemini, "gemini-2.5-flash", geminiNativeGenerateRequest())
	req.Headers = map[string]string{"X-Base-URL": srv.URL}

	resp, err := exec.Execute(context.Background(), req, newTestAccount(geminiNativeAPIKey))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if resp.Model != "gemini-2.5-flash" {
		t.Errorf("response model = %q, want gemini-2.5-flash", resp.Model)
	}

	if gotPath != "/gemini-2.5-flash:generateContent" {
		t.Errorf("path = %q, want /gemini-2.5-flash:generateContent (model in path)", gotPath)
	}
	if gotQuery != "" {
		t.Errorf("query = %q, want empty (no key query param; header auth only)", gotQuery)
	}
	if gotAuthKey != geminiNativeAPIKey {
		t.Errorf("x-goog-api-key = %q, want %q (raw scheme)", gotAuthKey, geminiNativeAPIKey)
	}
	if gotBearer != "" {
		t.Errorf("Authorization = %q, want empty for api_key account", gotBearer)
	}
	if string(gotBody) != string(geminiNativeGenerateRequest()) {
		t.Errorf("body passthrough mismatch:\n got: %s\nwant: %s", gotBody, geminiNativeGenerateRequest())
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("response body not JSON: %v", err)
	}
	for _, wrapKey := range []string{"model", "request", "project"} {
		if _, ok := payload[wrapKey]; ok {
			t.Errorf("body wrapped with %q key; native Gemini sends raw GenerateContentRequest", wrapKey)
		}
	}
	if _, ok := payload["contents"]; !ok {
		t.Errorf("body missing GenerateContentRequest contents field: %s", gotBody)
	}
}

// TestGeminiNativeExecutor_Execute_OAuthAuth asserts the oauth branch of the
// pinned split auth descriptor: Authorization: Bearer, no x-goog-api-key.
func TestGeminiNativeExecutor_Execute_OAuthAuth(t *testing.T) {
	var (
		gotAuthKey string
		gotBearer  string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthKey = r.Header.Get("x-goog-api-key")
		gotBearer = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"candidates":[]}`)
	}))
	defer srv.Close()

	exec := geminiNativeExecutor(t)
	req := newTestRequest(engine.FormatGemini, "gemini-2.5-flash", geminiNativeGenerateRequest())
	req.Headers = map[string]string{"X-Base-URL": srv.URL}

	if _, err := exec.Execute(context.Background(), req, newGeminiOAuthAccount(geminiNativeOAuthToken)); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if gotBearer != "Bearer "+geminiNativeOAuthToken {
		t.Errorf("Authorization = %q, want %q", gotBearer, "Bearer "+geminiNativeOAuthToken)
	}
	if gotAuthKey != "" {
		t.Errorf("x-goog-api-key = %q, want empty for oauth account", gotAuthKey)
	}
}

// TestGeminiNativeExecutor_ExecuteStream_WireContract asserts the pinned
// streaming URL ({model}:streamGenerateContent?alt=sse), Accept header, and
// SSE relay with exactly one terminal chunk.
func TestGeminiNativeExecutor_ExecuteStream_WireContract(t *testing.T) {
	var (
		gotPath    string
		gotQuery   string
		gotAccept  string
		gotAuthKey string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotAccept = r.Header.Get("Accept")
		gotAuthKey = r.Header.Get("x-goog-api-key")
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"a\"}]}}]}\n\n")
		fmt.Fprint(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"b\"}]}}]}\n\n")
	}))
	defer srv.Close()

	exec := geminiNativeExecutor(t)
	req := newTestRequest(engine.FormatGemini, "gemini-2.5-flash", geminiNativeGenerateRequest())
	req.Headers = map[string]string{"X-Base-URL": srv.URL}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := exec.ExecuteStream(ctx, req, newTestAccount(geminiNativeAPIKey))
	if err != nil {
		t.Fatalf("ExecuteStream failed: %v", err)
	}
	if resp.Stream == nil {
		t.Fatal("expected non-nil Stream")
	}
	st := resp.Stream.(*stream.Stream)

	if gotPath != "/gemini-2.5-flash:streamGenerateContent" {
		t.Errorf("path = %q, want /gemini-2.5-flash:streamGenerateContent (model in path)", gotPath)
	}
	if gotQuery != "alt=sse" {
		t.Errorf("query = %q, want alt=sse", gotQuery)
	}
	if gotAccept != "text/event-stream" {
		t.Errorf("Accept = %q, want text/event-stream", gotAccept)
	}
	if gotAuthKey != geminiNativeAPIKey {
		t.Errorf("x-goog-api-key = %q, want %q", gotAuthKey, geminiNativeAPIKey)
	}

	var dataChunks, terminalChunks int
	for ch := range st.Chunks() {
		if ch.IsFinal {
			terminalChunks++
			continue
		}
		if len(ch.Data) > 0 {
			dataChunks++
		}
	}
	if dataChunks != 2 {
		t.Errorf("data chunks = %d, want 2", dataChunks)
	}
	if terminalChunks != 1 {
		t.Errorf("terminal chunks = %d, want exactly 1", terminalChunks)
	}
}

// TestGeminiNativeExecutor_ExecuteStream_CancellationAndClosure asserts that
// cancelling the parent context terminates the stream and closes the upstream
// response body.
func TestGeminiNativeExecutor_ExecuteStream_CancellationAndClosure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		fmt.Fprint(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"a\"}]}}]}\n\n")
		flusher.Flush()
		<-r.Context().Done() // hold open until the client cancels
	}))
	defer srv.Close()

	transport := newBodyCloseTrackingTransport(srv.Client().Transport)
	exec, ok := GetExecutor(provider.ProviderGemini, transport)
	if !ok {
		t.Fatal("GetExecutor(ProviderGemini) not dispatched")
	}
	req := newTestRequest(engine.FormatGemini, "gemini-2.5-flash", geminiNativeGenerateRequest())
	req.Headers = map[string]string{"X-Base-URL": srv.URL}

	ctx, cancel := context.WithCancel(context.Background())
	resp, err := exec.ExecuteStream(ctx, req, newTestAccount(geminiNativeAPIKey))
	if err != nil {
		t.Fatalf("ExecuteStream failed: %v", err)
	}
	st := resp.Stream.(*stream.Stream)

	first := <-st.Chunks()
	if len(first.Data) == 0 {
		t.Fatal("expected first data chunk")
	}

	cancel()

	select {
	case <-st.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("stream did not terminate after context cancellation")
	}
	select {
	case <-transport.bodyClosed:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream response body not closed after cancellation")
	}
}

// TestGeminiNativeExecutor_Execute_ErrorClassification asserts the shared
// error taxonomy (auth / rate limit / upstream) with upstream body capture.
func TestGeminiNativeExecutor_Execute_ErrorClassification(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantCode   shared.ErrorCode
		wantStatus int
	}{
		{"unauthorized", http.StatusUnauthorized, `{"error":{"message":"API key not valid"}}`, shared.ErrUnauthorized, http.StatusUnauthorized},
		{"forbidden", http.StatusForbidden, `{"error":{"message":"permission denied"}}`, shared.ErrUnauthorized, http.StatusForbidden},
		{"rate-limit", http.StatusTooManyRequests, `{"error":{"message":"rate limit"}}`, shared.ErrRateLimited, http.StatusTooManyRequests},
		{"upstream", http.StatusInternalServerError, `{"error":{"message":"boom"}}`, shared.ErrInternal, http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				fmt.Fprint(w, tt.body)
			}))
			defer srv.Close()

			exec := geminiNativeExecutor(t)
			req := newTestRequest(engine.FormatGemini, "gemini-2.5-flash", geminiNativeGenerateRequest())
			req.Headers = map[string]string{"X-Base-URL": srv.URL}

			_, err := exec.Execute(context.Background(), req, newTestAccount(geminiNativeAPIKey))
			if err == nil {
				t.Fatal("expected error")
			}
			var ae *shared.AppError
			if !errors.As(err, &ae) {
				t.Fatalf("error is %T, want *shared.AppError", err)
			}
			if ae.Code != tt.wantCode {
				t.Errorf("Code = %q, want %q", ae.Code, tt.wantCode)
			}
			if ae.HTTPStatus != tt.wantStatus {
				t.Errorf("HTTPStatus = %d, want %d", ae.HTTPStatus, tt.wantStatus)
			}
			details, ok := ae.Details.(map[string]interface{})
			if !ok {
				t.Fatalf("Details = %T, want map", ae.Details)
			}
			body, _ := details["body"].(string)
			if !strings.Contains(body, tt.body) {
				t.Errorf("Details body = %q, want to contain upstream body", body)
			}
		})
	}
}

// TestGeminiNativeExecutor_Execute_CredentialRedaction asserts the credential
// never leaks through surfaced errors, even when the upstream echoes it.
func TestGeminiNativeExecutor_Execute_CredentialRedaction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprintf(w, `{"error":{"message":"Invalid API key %s"}}`, geminiNativeAPIKey)
	}))
	defer srv.Close()

	exec := geminiNativeExecutor(t)
	req := newTestRequest(engine.FormatGemini, "gemini-2.5-flash", geminiNativeGenerateRequest())
	req.Headers = map[string]string{"X-Base-URL": srv.URL}

	_, err := exec.Execute(context.Background(), req, newTestAccount(geminiNativeAPIKey))
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), geminiNativeAPIKey) {
		t.Errorf("credential leaked through error message: %v", err)
	}
	var ae *shared.AppError
	if errors.As(err, &ae) {
		if strings.Contains(ae.Message, geminiNativeAPIKey) {
			t.Errorf("credential leaked through AppError.Message: %q", ae.Message)
		}
		if details, ok := ae.Details.(map[string]interface{}); ok {
			if body, ok := details["body"].(string); ok {
				if strings.Contains(body, geminiNativeAPIKey) {
					t.Errorf("credential leaked through error Details body: %q", body)
				}
			}
		}
	}
}

// TestGeminiNativeExecutor_DefaultBaseURL asserts the pinned default transport
// base URL and model-in-path URL construction without any test override.
func TestGeminiNativeExecutor_DefaultBaseURL(t *testing.T) {
	exec := NewGeminiExecutor(http.DefaultTransport)
	req := newTestRequest(engine.FormatGemini, "gemini-2.5-flash", geminiNativeGenerateRequest())

	httpReq, err := exec.buildRequest(context.Background(), req, newTestAccount(geminiNativeAPIKey), geminiNativeGenerateRequest(), false)
	if err != nil {
		t.Fatalf("buildRequest failed: %v", err)
	}
	want := "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:generateContent"
	if httpReq.URL.String() != want {
		t.Errorf("non-stream URL = %q, want %q", httpReq.URL.String(), want)
	}

	httpReq, err = exec.buildRequest(context.Background(), req, newTestAccount(geminiNativeAPIKey), geminiNativeGenerateRequest(), true)
	if err != nil {
		t.Fatalf("buildRequest(stream) failed: %v", err)
	}
	want = "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:streamGenerateContent?alt=sse"
	if httpReq.URL.String() != want {
		t.Errorf("stream URL = %q, want %q", httpReq.URL.String(), want)
	}
}

// bodyCloseTrackingTransport wraps a RoundTripper and signals when a response
// body is closed.
type bodyCloseTrackingTransport struct {
	rt         http.RoundTripper
	bodyClosed chan struct{}
	once       sync.Once
}

func newBodyCloseTrackingTransport(rt http.RoundTripper) *bodyCloseTrackingTransport {
	return &bodyCloseTrackingTransport{rt: rt, bodyClosed: make(chan struct{})}
}

func (t *bodyCloseTrackingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.rt.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	resp.Body = &bodyCloseTracker{ReadCloser: resp.Body, t: t}
	return resp, nil
}

func (t *bodyCloseTrackingTransport) markClosed() {
	t.once.Do(func() { close(t.bodyClosed) })
}

type bodyCloseTracker struct {
	io.ReadCloser
	t *bodyCloseTrackingTransport
}

func (b *bodyCloseTracker) Close() error {
	b.t.markClosed()
	return b.ReadCloser.Close()
}
