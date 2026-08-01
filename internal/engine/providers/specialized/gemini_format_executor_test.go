package specialized

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"gorouter/internal/domain/engine"
	"gorouter/internal/domain/provider"
	"gorouter/internal/shared"
)

// geminiOKBody is a minimal Gemini generateContent response.
const geminiOKBody = `{"candidates":[{"content":{"parts":[{"text":"hello from gemini"}]}}]}`

// errTransport simulates a client-level transport failure.
var errTransport = errors.New("dial tcp: connection refused")

// newGeminiRequest builds a FormatGemini request with an optional base URL
// override and a Gemini-shaped payload.
func newGeminiRequest(baseURL, model, payload string) *engine.Request {
	req := newTestRequest(engine.FormatGemini, model, []byte(payload))
	req.Headers = map[string]string{}
	if baseURL != "" {
		req.Headers["X-Base-URL"] = baseURL
	}
	return req
}

// decodeBody reads and decodes a captured request body.
func decodeBody(t *testing.T, rt *wireRT) map[string]interface{} {
	t.Helper()
	return capturedJSONBody(t, rt)
}

// TestGeminiCLIExecutor_WireShape verifies the GeminiCLI non-stream contract:
// endpoint, {project, model, request} wrapper, Google API headers, and
// application/json accept.
func TestGeminiCLIExecutor_WireShape(t *testing.T) {
	rt := newWireRT(http.StatusOK, geminiOKBody)
	exe := NewGeminiCLIExecutor(rt)
	account := newTestAccount("gemini-cli-token")

	req := newGeminiRequest("https://gemini.test", "gemini-2.5-pro", `{"contents":[{"parts":[{"text":"hi"}]}]}`)
	resp, err := exe.Execute(context.Background(), req, account)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if got := rt.captured.URL.String(); got != "https://gemini.test/v1/models:generateContent" {
		t.Errorf("URL = %q, want %q", got, "https://gemini.test/v1/models:generateContent")
	}
	if rt.captured.Method != http.MethodPost {
		t.Errorf("Method = %q, want POST", rt.captured.Method)
	}
	if got := rt.captured.Header.Get("Authorization"); got != "Bearer gemini-cli-token" {
		t.Errorf("Authorization = %q, want Bearer gemini-cli-token", got)
	}
	if got := rt.captured.Header.Get("User-Agent"); got != "GeminiCLI/1.0" {
		t.Errorf("User-Agent = %q, want GeminiCLI/1.0", got)
	}
	if got := rt.captured.Header.Get("X-Goog-Api-Client"); got != "gemini-cli" {
		t.Errorf("X-Goog-Api-Client = %q, want gemini-cli", got)
	}
	if got := rt.captured.Header.Get("Accept"); got != "application/json" {
		t.Errorf("Accept = %q, want application/json", got)
	}

	wrapped := decodeBody(t, rt)
	if got := wrapped["model"]; got != "gemini-2.5-pro" {
		t.Errorf("wrapped.model = %v, want gemini-2.5-pro", got)
	}
	if got := wrapped["project"]; got != account.ID.String() {
		t.Errorf("wrapped.project = %v, want account ID %s", got, account.ID.String())
	}
	inner, ok := wrapped["request"].(map[string]interface{})
	if !ok {
		t.Fatalf("wrapped.request missing: %v", wrapped)
	}
	if _, ok := inner["contents"]; !ok {
		t.Errorf("wrapped.request.contents missing: %v", inner)
	}

	if resp == nil {
		t.Fatal("nil response")
	}
	if string(resp.Body) != geminiOKBody {
		t.Errorf("resp.Body = %s, want raw passthrough", resp.Body)
	}
	if resp.Model != "gemini-2.5-pro" {
		t.Errorf("resp.Model = %q, want gemini-2.5-pro", resp.Model)
	}
}

// TestGeminiCLIExecutor_StreamWireShape verifies the streaming contract:
// streamGenerateContent?alt=sse endpoint, SSE accept, and chunk delivery.
func TestGeminiCLIExecutor_StreamWireShape(t *testing.T) {
	sse := "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"a\"}]}}]}\n\n" +
		"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"b\"}]}}]}\n\n" +
		"data: [DONE]\n\n"
	rt := newWireRT(http.StatusOK, sse)
	exe := NewGeminiCLIExecutor(rt)
	req := newGeminiRequest("https://gemini.test", "gemini-2.5-flash", `{"contents":[]}`)

	resp, err := exe.ExecuteStream(context.Background(), req, newTestAccount("gemini-cli-token"))
	if err != nil {
		t.Fatalf("ExecuteStream failed: %v", err)
	}

	if got := rt.captured.URL.String(); got != "https://gemini.test/v1/models:streamGenerateContent?alt=sse" {
		t.Errorf("URL = %q, want %q", got, "https://gemini.test/v1/models:streamGenerateContent?alt=sse")
	}
	if got := rt.captured.Header.Get("Accept"); got != "text/event-stream" {
		t.Errorf("Accept = %q, want text/event-stream", got)
	}
	if got := rt.captured.Header.Get("X-Goog-Api-Client"); got != "gemini-cli" {
		t.Errorf("X-Goog-Api-Client = %q, want gemini-cli", got)
	}
	wrapped := decodeBody(t, rt)
	if got := wrapped["model"]; got != "gemini-2.5-flash" {
		t.Errorf("wrapped.model = %v, want gemini-2.5-flash", got)
	}

	chunks := readAllStream(t, resp.Stream)
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks (2 data + final), got %d", len(chunks))
	}
	if !chunks[2].IsFinal {
		t.Error("last chunk should be final")
	}
	if !strings.Contains(string(chunks[0].Data), "\"text\":\"a\"") {
		t.Errorf("chunk[0].Data = %s, want first delta", chunks[0].Data)
	}
}

// TestGeminiCLIExecutor_NoAccountOmitsProject verifies that without an
// account the wrapper carries no project and no Authorization header.
func TestGeminiCLIExecutor_NoAccountOmitsProject(t *testing.T) {
	rt := newWireRT(http.StatusOK, geminiOKBody)
	exe := NewGeminiCLIExecutor(rt)
	req := newGeminiRequest("https://gemini.test", "", `{"contents":[]}`)

	if _, err := exe.Execute(context.Background(), req, nil); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if got := rt.captured.Header.Get("Authorization"); got != "" {
		t.Errorf("Authorization = %q, want empty", got)
	}
	wrapped := decodeBody(t, rt)
	if _, ok := wrapped["project"]; ok {
		t.Errorf("wrapped.project present without account: %v", wrapped["project"])
	}
}

// TestGeminiCLIExecutor_ErrorMappingAndClosure verifies status classification,
// body closure, and credential sanitation on the GeminiCLI error paths.
func TestGeminiCLIExecutor_ErrorMappingAndClosure(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		wantCode   shared.ErrorCode
		wantStatus int
	}{
		{"unauthorized", http.StatusUnauthorized, shared.ErrUnauthorized, http.StatusUnauthorized},
		{"forbidden", http.StatusForbidden, shared.ErrUnauthorized, http.StatusForbidden},
		{"rate-limited", http.StatusTooManyRequests, shared.ErrRateLimited, http.StatusTooManyRequests},
		{"internal", http.StatusInternalServerError, shared.ErrInternal, http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := newWireRT(tt.status, `{"error":{"code":400,"message":"bad request"}}`)
			exe := NewGeminiCLIExecutor(rt)
			req := newGeminiRequest("https://gemini.test", "", `{"contents":[]}`)

			_, err := exe.ExecuteStream(context.Background(), req, newTestAccount("gemini-cli-token"))
			assertAppErrorCode(t, err, tt.wantCode, tt.wantStatus)
			if !rt.usesBody.isClosed() {
				t.Error("body not closed after stream error")
			}
			assertNoCredentialLeak(t, "gemini-cli-token", err.Error())
		})
	}
}

// TestGeminiCLIExecutor_DefaultBaseURL pins the production Gemini CLI
// authority: generativelanguage.googleapis.com.
func TestGeminiCLIExecutor_DefaultBaseURL(t *testing.T) {
	rt := newWireRT(http.StatusOK, geminiOKBody)
	exe := NewGeminiCLIExecutor(rt)
	req := newGeminiRequest("", "gemini-2.5-pro", `{"contents":[]}`)

	if _, err := exe.Execute(context.Background(), req, newTestAccount("t")); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	want := "https://generativelanguage.googleapis.com/v1/models:generateContent"
	if got := rt.captured.URL.String(); got != want {
		t.Errorf("URL = %q, want %q", got, want)
	}
}

// TestGeminiCLIExecutor_AuthorityHost verifies the HTTP/2 authority (Host)
// header matches the production host even when the path is served locally.
func TestGeminiCLIExecutor_AuthorityHost(t *testing.T) {
	rt := newWireRT(http.StatusOK, geminiOKBody)
	exe := NewGeminiCLIExecutor(rt)
	req := newGeminiRequest("", "gemini-2.5-pro", `{"contents":[]}`)

	if _, err := exe.Execute(context.Background(), req, newTestAccount("t")); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if got := rt.captured.URL.Host; got != "generativelanguage.googleapis.com" {
		t.Errorf("authority host = %q, want generativelanguage.googleapis.com", got)
	}
	if got := rt.captured.URL.Path; got != "/v1/models:generateContent" {
		t.Errorf("path = %q, want /v1/models:generateContent", got)
	}
}

// TestAntigravityExecutor_WireShape verifies the Antigravity v1internal
// contract: endpoint, wrapper fields (project/userAgent/requestType/requestId),
// and identity headers.
func TestAntigravityExecutor_WireShape(t *testing.T) {
	rt := newWireRT(http.StatusOK, geminiOKBody)
	exe := NewAntigravityExecutor(rt)
	account := newTestAccount("antigravity-token")

	req := newGeminiRequest("https://ag.test", "antigravity-model", `{"contents":[{"parts":[{"text":"hi"}]}]}`)
	resp, err := exe.Execute(context.Background(), req, account)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if got := rt.captured.URL.String(); got != "https://ag.test/v1internal:generateContent" {
		t.Errorf("URL = %q, want %q", got, "https://ag.test/v1internal:generateContent")
	}
	if got := rt.captured.Header.Get("User-Agent"); got != "Antigravity/1.0" {
		t.Errorf("User-Agent = %q, want Antigravity/1.0", got)
	}
	if got := rt.captured.Header.Get("Authorization"); got != "Bearer antigravity-token" {
		t.Errorf("Authorization = %q, want Bearer antigravity-token", got)
	}
	if got := rt.captured.Header.Get("Accept"); got != "" {
		t.Errorf("Accept = %q, want empty for non-stream", got)
	}

	wrapped := decodeBody(t, rt)
	if got := wrapped["model"]; got != "antigravity-model" {
		t.Errorf("wrapped.model = %v, want antigravity-model", got)
	}
	if got := wrapped["project"]; got != "antigravity" {
		t.Errorf("wrapped.project = %v, want literal antigravity", got)
	}
	if got := wrapped["userAgent"]; got != "antigravity" {
		t.Errorf("wrapped.userAgent = %v, want antigravity", got)
	}
	if got := wrapped["requestType"]; got != "agent" {
		t.Errorf("wrapped.requestType = %v, want agent", got)
	}
	if got := wrapped["requestId"]; got != req.ID.String() {
		t.Errorf("wrapped.requestId = %v, want %s", got, req.ID.String())
	}
	if _, ok := wrapped["request"].(map[string]interface{}); !ok {
		t.Errorf("wrapped.request missing: %v", wrapped)
	}
	if resp == nil || string(resp.Body) != geminiOKBody {
		t.Errorf("resp.Body = %v, want raw passthrough", resp)
	}
}

// TestAntigravityExecutor_StreamWireShape verifies the Antigravity streaming
// endpoint and SSE chunk delivery.
func TestAntigravityExecutor_StreamWireShape(t *testing.T) {
	rt := newWireRT(http.StatusOK, "data: {\"c\":1}\n\ndata: [DONE]\n\n")
	exe := NewAntigravityExecutor(rt)
	req := newGeminiRequest("https://ag.test", "", `{"contents":[]}`)

	resp, err := exe.ExecuteStream(context.Background(), req, newTestAccount("antigravity-token"))
	if err != nil {
		t.Fatalf("ExecuteStream failed: %v", err)
	}
	if got := rt.captured.URL.String(); got != "https://ag.test/v1internal:streamGenerateContent?alt=sse" {
		t.Errorf("URL = %q, want %q", got, "https://ag.test/v1internal:streamGenerateContent?alt=sse")
	}
	if got := rt.captured.Header.Get("Accept"); got != "text/event-stream" {
		t.Errorf("Accept = %q, want text/event-stream", got)
	}
	chunks := readAllStream(t, resp.Stream)
	if len(chunks) != 2 || !chunks[1].IsFinal {
		t.Fatalf("expected 2 chunks ending in final, got %d", len(chunks))
	}
}

// TestAntigravityExecutor_ErrorClosure verifies Antigravity error handling
// closes the body and keeps credentials out of the error.
func TestAntigravityExecutor_ErrorClosure(t *testing.T) {
	rt := newWireRT(http.StatusTooManyRequests, `{"error":{"message":"quota"}}`)
	exe := NewAntigravityExecutor(rt)
	req := newGeminiRequest("https://ag.test", "", `{"contents":[]}`)

	_, err := exe.ExecuteStream(context.Background(), req, newTestAccount("antigravity-token"))
	assertAppErrorCode(t, err, shared.ErrRateLimited, http.StatusTooManyRequests)
	if !rt.usesBody.isClosed() {
		t.Error("body not closed after stream error")
	}
	assertNoCredentialLeak(t, "antigravity-token", err.Error())
}

// TestAntigravityExecutor_DefaultBaseURL pins the Antigravity production
// authority: api.antigravity.ai with the v1internal prefix.
func TestAntigravityExecutor_DefaultBaseURL(t *testing.T) {
	rt := newWireRT(http.StatusOK, geminiOKBody)
	exe := NewAntigravityExecutor(rt)
	req := newGeminiRequest("", "m", `{"contents":[]}`)

	if _, err := exe.Execute(context.Background(), req, newTestAccount("t")); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	want := "https://api.antigravity.ai/v1internal:generateContent"
	if got := rt.captured.URL.String(); got != want {
		t.Errorf("URL = %q, want %q", got, want)
	}
}

// TestVertexExecutor_WireShape verifies the Vertex path contract:
// /v1/projects/{project}/locations/{location}/publishers/google/models/{model}
// with the payload passed through unwrapped.
func TestVertexExecutor_WireShape(t *testing.T) {
	rt := newWireRT(http.StatusOK, geminiOKBody)
	exe := NewVertexExecutor(rt, false)
	req := newGeminiRequest("https://vertex.test", "gemini-1.5-pro", `{"contents":[{"parts":[{"text":"hi"}]}]}`)

	resp, err := exe.Execute(context.Background(), req, newTestAccount("vertex-token"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	wantPath := "/v1/projects/default-project/locations/us-central1/publishers/google/models/gemini-1.5-pro:generateContent"
	if got := rt.captured.URL.Path; got != wantPath {
		t.Errorf("URL path = %q, want %q", got, wantPath)
	}
	if got := rt.captured.URL.String(); got != "https://vertex.test"+wantPath {
		t.Errorf("URL = %q, want %q", got, "https://vertex.test"+wantPath)
	}
	if got := rt.captured.Header.Get("Authorization"); got != "Bearer vertex-token" {
		t.Errorf("Authorization = %q, want Bearer vertex-token", got)
	}
	if got := rt.captured.Header.Get("Accept"); got != "" {
		t.Errorf("Accept = %q, want empty for non-stream", got)
	}

	// Vertex passes the payload through unwrapped — no {project, model, request}
	// wrapper and no GeminiCLI-style headers.
	payload := decodeBody(t, rt)
	if _, ok := payload["request"]; ok {
		t.Errorf("payload must not be wrapped: %v", payload)
	}
	if got := rt.captured.Header.Get("X-Goog-Api-Client"); got != "" {
		t.Errorf("X-Goog-Api-Client = %q, want empty (not GeminiCLI)", got)
	}
	if resp == nil || string(resp.Body) != geminiOKBody {
		t.Errorf("resp.Body = %v, want raw passthrough", resp)
	}
}

// TestVertexExecutor_StreamWireShape verifies Vertex streaming uses the
// :streamGenerateContent action without the alt=sse query parameter.
func TestVertexExecutor_StreamWireShape(t *testing.T) {
	rt := newWireRT(http.StatusOK, "data: {\"c\":1}\n\ndata: [DONE]\n\n")
	exe := NewVertexExecutor(rt, false)
	req := newGeminiRequest("https://vertex.test", "gemini-1.5-flash", `{"contents":[]}`)

	resp, err := exe.ExecuteStream(context.Background(), req, newTestAccount("vertex-token"))
	if err != nil {
		t.Fatalf("ExecuteStream failed: %v", err)
	}
	wantPath := "/v1/projects/default-project/locations/us-central1/publishers/google/models/gemini-1.5-flash:streamGenerateContent"
	if got := rt.captured.URL.Path; got != wantPath {
		t.Errorf("URL path = %q, want %q", got, wantPath)
	}
	if got := rt.captured.URL.RawQuery; got != "" {
		t.Errorf("RawQuery = %q, want empty (no alt=sse for Vertex)", got)
	}
	if got := rt.captured.Header.Get("Accept"); got != "text/event-stream" {
		t.Errorf("Accept = %q, want text/event-stream", got)
	}
	chunks := readAllStream(t, resp.Stream)
	if len(chunks) != 2 || !chunks[1].IsFinal {
		t.Fatalf("expected 2 chunks ending in final, got %d", len(chunks))
	}
}

// TestVertexExecutor_PartnerIdentity verifies the partner flag flips the
// provider identity while keeping the same wire contract.
func TestVertexExecutor_PartnerIdentity(t *testing.T) {
	exe := NewVertexExecutor(nil, true)
	if exe.ProviderType() != provider.ProviderVertexPartner {
		t.Errorf("ProviderType = %q, want %q", exe.ProviderType(), provider.ProviderVertexPartner)
	}
	if !exe.SupportsFormat(engine.FormatGemini) {
		t.Error("Vertex partner must support FormatGemini")
	}
	plain := NewVertexExecutor(nil, false)
	if plain.ProviderType() != provider.ProviderVertex {
		t.Errorf("ProviderType = %q, want %q", plain.ProviderType(), provider.ProviderVertex)
	}
}

// TestVertexExecutor_DefaultBaseURL pins the Vertex production authority:
// us-central1-aiplatform.googleapis.com.
func TestVertexExecutor_DefaultBaseURL(t *testing.T) {
	rt := newWireRT(http.StatusOK, geminiOKBody)
	exe := NewVertexExecutor(rt, false)
	req := newGeminiRequest("", "gemini-1.5-pro", `{"contents":[]}`)

	if _, err := exe.Execute(context.Background(), req, newTestAccount("t")); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	want := "https://us-central1-aiplatform.googleapis.com/v1/projects/default-project/locations/us-central1/publishers/google/models/gemini-1.5-pro:generateContent"
	if got := rt.captured.URL.String(); got != want {
		t.Errorf("URL = %q, want %q", got, want)
	}
}

// TestVertexExecutor_ErrorMappingAndClosure verifies Vertex status
// classification and body closure on stream errors.
func TestVertexExecutor_ErrorMappingAndClosure(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		wantCode   shared.ErrorCode
		wantStatus int
	}{
		{"forbidden", http.StatusForbidden, shared.ErrUnauthorized, http.StatusForbidden},
		{"rate-limited", http.StatusTooManyRequests, shared.ErrRateLimited, http.StatusTooManyRequests},
		{"internal", http.StatusInternalServerError, shared.ErrInternal, http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := newWireRT(tt.status, `{"error":{"message":"boom"}}`)
			exe := NewVertexExecutor(rt, false)
			req := newGeminiRequest("https://vertex.test", "", `{"contents":[]}`)

			_, err := exe.ExecuteStream(context.Background(), req, newTestAccount("vertex-token"))
			assertAppErrorCode(t, err, tt.wantCode, tt.wantStatus)
			if !rt.usesBody.isClosed() {
				t.Error("body not closed after stream error")
			}
			assertNoCredentialLeak(t, "vertex-token", err.Error())
		})
	}
}

// TestGeminiFormatProviderTypes pins the provider identities and format
// support of the three Gemini-format executors.
func TestGeminiFormatProviderTypes(t *testing.T) {
	tests := []struct {
		name string
		exec engine.Executor
		want provider.ProviderType
	}{
		{"gemini-cli", NewGeminiCLIExecutor(nil), provider.ProviderGeminiCli},
		{"antigravity", NewAntigravityExecutor(nil), provider.ProviderAntigravity},
		{"vertex", NewVertexExecutor(nil, false), provider.ProviderVertex},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.exec.ProviderType(); got != tt.want {
				t.Errorf("ProviderType() = %q, want %q", got, tt.want)
			}
			if !tt.exec.SupportsFormat(engine.FormatGemini) {
				t.Error("must support FormatGemini")
			}
			for _, f := range []engine.RequestFormat{engine.FormatOpenAIChat, engine.FormatAnthropic, engine.FormatCodexResponses} {
				if tt.exec.SupportsFormat(f) {
					t.Errorf("must NOT support format %q", f)
				}
			}
		})
	}
}

// TestGeminiFormatRequestIDPropagation verifies request IDs flow through to
// responses for all three Gemini-format executors.
func TestGeminiFormatRequestIDPropagation(t *testing.T) {
	execs := []struct {
		name string
		exec engine.Executor
	}{
		{"gemini-cli", NewGeminiCLIExecutor(newWireRT(http.StatusOK, geminiOKBody))},
		{"antigravity", NewAntigravityExecutor(newWireRT(http.StatusOK, geminiOKBody))},
		{"vertex", NewVertexExecutor(newWireRT(http.StatusOK, geminiOKBody), false)},
	}
	for _, tt := range execs {
		t.Run(tt.name, func(t *testing.T) {
			req := newGeminiRequest("", "m", `{"contents":[]}`)
			resp, err := tt.exec.Execute(context.Background(), req, newTestAccount("cred"))
			if err != nil {
				t.Fatalf("Execute failed: %v", err)
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

// TestGeminiFormatTransportError verifies client-level failures are wrapped
// without leaking credentials across the Gemini format family.
func TestGeminiFormatTransportError(t *testing.T) {
	execs := []struct {
		name string
		exec engine.Executor
	}{
		{"gemini-cli", NewGeminiCLIExecutor(&wireRT{err: errTransport})},
		{"antigravity", NewAntigravityExecutor(&wireRT{err: errTransport})},
		{"vertex", NewVertexExecutor(&wireRT{err: errTransport}, false)},
	}
	for _, tt := range execs {
		t.Run(tt.name, func(t *testing.T) {
			req := newGeminiRequest("", "m", `{"contents":[]}`)
			_, err := tt.exec.Execute(context.Background(), req, newTestAccount("secret-token"))
			if err == nil {
				t.Fatal("expected transport error")
			}
			assertNoCredentialLeak(t, "secret-token", err.Error())
		})
	}
}

// TestGeminiFormatExecutors_Execute_ErrorClosesExactlyOnce pins the close-once
// contract on the non-streaming error path for every Gemini-format executor.
func TestGeminiFormatExecutors_Execute_ErrorClosesExactlyOnce(t *testing.T) {
	for name, newExec := range map[string]func(http.RoundTripper) engine.Executor{
		"geminicli":   func(rt http.RoundTripper) engine.Executor { return NewGeminiCLIExecutor(rt) },
		"antigravity": func(rt http.RoundTripper) engine.Executor { return NewAntigravityExecutor(rt) },
		"vertex":      func(rt http.RoundTripper) engine.Executor { return NewVertexExecutor(rt, false) },
	} {
		t.Run(name, func(t *testing.T) {
			rt := newWireRT(http.StatusUnauthorized, `{"error":{"message":"boom"}}`)
			exe := newExec(rt)
			req := newGeminiRequest("", "m", `{"contents":[]}`)

			_, err := exe.Execute(context.Background(), req, newTestAccount("gemini-key-1"))
			if err == nil {
				t.Fatal("expected error for 401")
			}
			if got := rt.usesBody.closeCalls(); got != 1 {
				t.Errorf("response body closed %d times, want exactly 1", got)
			}
		})
	}
}

// TestGeminiFormatExecutors_ExecuteStream_ErrorClosesExactlyOnce pins the
// close-once contract on the streaming error path for every Gemini-format
// executor.
func TestGeminiFormatExecutors_ExecuteStream_ErrorClosesExactlyOnce(t *testing.T) {
	for name, newExec := range map[string]func(http.RoundTripper) engine.Executor{
		"geminicli":   func(rt http.RoundTripper) engine.Executor { return NewGeminiCLIExecutor(rt) },
		"antigravity": func(rt http.RoundTripper) engine.Executor { return NewAntigravityExecutor(rt) },
		"vertex":      func(rt http.RoundTripper) engine.Executor { return NewVertexExecutor(rt, false) },
	} {
		t.Run(name, func(t *testing.T) {
			rt := newWireRT(http.StatusForbidden, `{"error":{"message":"boom"}}`)
			exe := newExec(rt)
			req := newGeminiRequest("", "m", `{"contents":[]}`)

			_, err := exe.ExecuteStream(context.Background(), req, newTestAccount("gemini-key-2"))
			if err == nil {
				t.Fatal("expected error for 403")
			}
			if got := rt.usesBody.closeCalls(); got != 1 {
				t.Errorf("response body closed %d times, want exactly 1", got)
			}
		})
	}
}
