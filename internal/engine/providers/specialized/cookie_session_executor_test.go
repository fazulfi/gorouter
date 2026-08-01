package specialized

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"gorouter/internal/domain/engine"
	"gorouter/internal/domain/provider"
)

// cookieRT is a recording RoundTripper for cookie-session executors.
type cookieRT struct {
	captured *http.Request
	status   int
	body     string
	usesBody *trackingBody
}

func (r *cookieRT) RoundTrip(req *http.Request) (*http.Response, error) {
	r.captured = req
	body := r.body
	if body == "" {
		body = "{}"
	}
	tb := newTrackingBody(body)
	r.usesBody = tb
	status := r.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     make(http.Header),
		Body:       tb,
	}, nil
}

func newCookieRT(status int, body string) *cookieRT {
	return &cookieRT{status: status, body: body}
}

// grokNDJSONBody is a minimal valid Grok NDJSON completion body.
const grokNDJSONBody = `{"result":{"response":{"modelResponse":{"message":"hello from grok"}}}}` + "\n"

// newCookieRequest builds a chat request with the OpenAI wire shape the
// cookie executors expect.
func newCookieRequest(model, messagesJSON string) *engine.Request {
	req := newTestRequest(engine.FormatOpenAIChat, model, []byte(`{"messages":`+messagesJSON+`}`))
	req.Headers = map[string]string{}
	return req
}

// assertErrorBody checks a cookie-executor error response body shape.
func assertErrorBody(t *testing.T, resp *engine.Response, wantStatus int, wantSubstr string) {
	t.Helper()
	if resp == nil {
		t.Fatal("expected error response, got nil")
	}
	if resp.StatusCode != wantStatus {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, wantStatus)
	}
	var payload struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		t.Fatalf("error body is not JSON: %v", err)
	}
	if payload.Error.Message == "" {
		t.Error("error.message is empty")
	}
	if !strings.Contains(payload.Error.Message, wantSubstr) {
		t.Errorf("error.message = %q, want substring %q", payload.Error.Message, wantSubstr)
	}
}

// TestGrokWebExecutor_CookieConstruction verifies the SSO cookie contract:
// a bare token is prefixed with sso=, an already-prefixed token is preserved,
// and no cookie header is sent without an account.
func TestGrokWebExecutor_CookieConstruction(t *testing.T) {
	tests := []struct {
		name       string
		credential string
		account    bool
		wantCookie string
	}{
		{"bare token gets sso= prefix", "abc123", true, "sso=abc123"},
		{"prefixed token preserved", "sso=abc123", true, "sso=abc123"},
		{"empty credential no cookie", "", true, ""},
		{"nil account no cookie", "", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := newCookieRT(http.StatusOK, grokNDJSONBody)
			exe := NewGrokWebExecutor(rt)
			req := newCookieRequest("grok-3", `[{"role":"user","content":"hi"}]`)

			var account = newTestAccount(tt.credential)
			if !tt.account {
				account = nil
			}
			if _, err := exe.Execute(context.Background(), req, account); err != nil {
				t.Fatalf("Execute failed: %v", err)
			}
			if got := rt.captured.Header.Get("Cookie"); got != tt.wantCookie {
				t.Errorf("Cookie = %q, want %q", got, tt.wantCookie)
			}
		})
	}
}

// TestGrokWebExecutor_WireShape verifies the Grok chat endpoint contract:
// browser-like headers, X-Request-ID, and the grok-specific payload mapping.
func TestGrokWebExecutor_WireShape(t *testing.T) {
	rt := newCookieRT(http.StatusOK, grokNDJSONBody)
	exe := NewGrokWebExecutor(rt)
	req := newCookieRequest("grok-3", `[{"role":"user","content":"hi there"}]`)

	resp, err := exe.Execute(context.Background(), req, newTestAccount("sso-secret-token"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if got := rt.captured.URL.String(); got != "https://grok.com/rest/app/chat" {
		t.Errorf("URL = %q, want https://grok.com/rest/app/chat", got)
	}
	if rt.captured.Method != http.MethodPost {
		t.Errorf("Method = %q, want POST", rt.captured.Method)
	}
	if got := rt.captured.Header.Get("Origin"); got != "https://grok.com" {
		t.Errorf("Origin = %q, want https://grok.com", got)
	}
	if got := rt.captured.Header.Get("Referer"); got != "https://grok.com/" {
		t.Errorf("Referer = %q, want https://grok.com/", got)
	}
	if got := rt.captured.Header.Get("User-Agent"); !strings.HasPrefix(got, "Mozilla/5.0") {
		t.Errorf("User-Agent = %q, want browser UA", got)
	}
	if got := rt.captured.Header.Get("X-Request-ID"); len(got) != 32 {
		t.Errorf("X-Request-ID = %q, want 32 hex chars", got)
	}

	var payload map[string]interface{}
	raw, err := io.ReadAll(rt.captured.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if got := payload["modelName"]; got != "grok-3" {
		t.Errorf("modelName = %v, want grok-3", got)
	}
	if got := payload["modelMode"]; got != "MODEL_MODE_GROK_3" {
		t.Errorf("modelMode = %v, want MODEL_MODE_GROK_3", got)
	}
	if got := payload["message"]; got != "user: hi there" {
		t.Errorf("message = %v, want %q", got, "user: hi there")
	}
	if payload["temporary"] != true {
		t.Errorf("temporary = %v, want true", payload["temporary"])
	}

	// Non-stream Execute must parse NDJSON into an OpenAI completion.
	if resp == nil {
		t.Fatal("nil response")
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200", resp.StatusCode)
	}
	var oai struct {
		Object  string `json:"object"`
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(resp.Body, &oai); err != nil {
		t.Fatalf("response body not OpenAI completion: %v", err)
	}
	if oai.Object != "chat.completion" {
		t.Errorf("object = %q, want chat.completion", oai.Object)
	}
	if len(oai.Choices) != 1 || oai.Choices[0].Message.Content != "hello from grok" {
		t.Errorf("choices = %+v, want extracted grok message", oai.Choices)
	}
}

// TestPerplexityWebExecutor_AuthHeaderSelection verifies the auth contract:
// oauth accounts or Bearer-prefixed credentials go in Authorization; anything
// else goes in the __Secure-next-auth.session-token cookie.
func TestPerplexityWebExecutor_AuthHeaderSelection(t *testing.T) {
	tests := []struct {
		name       string
		authType   string
		credential string
		wantAuth   string
		wantCookie string
	}{
		{"cookie auth type", "cookie", "pplx-session-1", "", "__Secure-next-auth.session-token=pplx-session-1"},
		{"api_key auth type", "api_key", "pplx-session-2", "", "__Secure-next-auth.session-token=pplx-session-2"},
		{"oauth auth type", "oauth", "Bearer pplx-oauth-token", "Bearer pplx-oauth-token", ""},
		{"bearer-prefixed credential", "cookie", "Bearer pplx-oauth-token", "Bearer pplx-oauth-token", ""},
		{"nil account", "", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := newCookieRT(http.StatusOK, "{}")
			exe := NewPerplexityWebExecutor(rt)
			req := newCookieRequest("pplx-sonar", `[{"role":"user","content":"hi"}]`)

			var account = newTestAccount(tt.credential)
			account.AuthType = tt.authType
			if tt.name == "nil account" {
				account = nil
			}
			if _, err := exe.Execute(context.Background(), req, account); err != nil {
				t.Fatalf("Execute failed: %v", err)
			}
			if got := rt.captured.Header.Get("Authorization"); got != tt.wantAuth {
				t.Errorf("Authorization = %q, want %q", got, tt.wantAuth)
			}
			if got := rt.captured.Header.Get("Cookie"); got != tt.wantCookie {
				t.Errorf("Cookie = %q, want %q", got, tt.wantCookie)
			}
		})
	}
}

// TestPerplexityWebExecutor_WireShape verifies the Perplexity SSE endpoint,
// browser headers, and body shape.
func TestPerplexityWebExecutor_WireShape(t *testing.T) {
	rt := newCookieRT(http.StatusOK, "{}")
	exe := NewPerplexityWebExecutor(rt)
	req := newCookieRequest("pplx-auto", `[{"role":"user","content":"what is the capital of france?"}]`)

	resp, err := exe.Execute(context.Background(), req, newTestAccount("pplx-session-9"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if got := rt.captured.URL.String(); got != "https://www.perplexity.ai/sse/chat" {
		t.Errorf("URL = %q, want https://www.perplexity.ai/sse/chat", got)
	}
	if got := rt.captured.Header.Get("Origin"); got != "https://www.perplexity.ai" {
		t.Errorf("Origin = %q, want https://www.perplexity.ai", got)
	}
	if got := rt.captured.Header.Get("X-App-ApiClient"); got != "default" {
		t.Errorf("X-App-ApiClient = %q, want default", got)
	}
	if got := rt.captured.Header.Get("X-App-ApiVersion"); got != "2.18" {
		t.Errorf("X-App-ApiVersion = %q, want 2.18", got)
	}
	if got := rt.captured.Header.Get("Accept"); got != "text/event-stream" {
		t.Errorf("Accept = %q, want text/event-stream", got)
	}

	raw, err := io.ReadAll(rt.captured.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var body struct {
		QueryStr string `json:"query_str"`
		Params   struct {
			Mode            string   `json:"mode"`
			ModelPreference string   `json:"model_preference"`
			SearchFocus     string   `json:"search_focus"`
			Sources         []string `json:"sources"`
		} `json:"params"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	if body.QueryStr != "what is the capital of france?" {
		t.Errorf("query_str = %q", body.QueryStr)
	}
	if body.Params.Mode != "concise" {
		t.Errorf("params.mode = %q, want concise (pplx-auto)", body.Params.Mode)
	}
	if body.Params.ModelPreference != "pplx_pro" {
		t.Errorf("params.model_preference = %q, want pplx_pro", body.Params.ModelPreference)
	}
	if body.Params.SearchFocus != "internet" {
		t.Errorf("params.search_focus = %q, want internet", body.Params.SearchFocus)
	}
	if len(body.Params.Sources) != 1 || body.Params.Sources[0] != "web" {
		t.Errorf("params.sources = %v, want [web]", body.Params.Sources)
	}
	if resp == nil || resp.StatusCode != http.StatusOK {
		t.Errorf("resp = %+v, want 200 response", resp)
	}
}

// TestPerplexityWebExecutor_UnknownModelFallback verifies that models absent
// from the map fall back to copilot mode with the raw model preference.
func TestPerplexityWebExecutor_UnknownModelFallback(t *testing.T) {
	rt := newCookieRT(http.StatusOK, "{}")
	exe := NewPerplexityWebExecutor(rt)
	req := newCookieRequest("pplx-custom-model", `[{"role":"user","content":"hi"}]`)

	if _, err := exe.Execute(context.Background(), req, newTestAccount("tok")); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	raw, _ := io.ReadAll(rt.captured.Body)
	var body struct {
		Params struct {
			Mode            string `json:"mode"`
			ModelPreference string `json:"model_preference"`
		} `json:"params"`
	}
	_ = json.Unmarshal(raw, &body)
	if body.Params.Mode != "copilot" {
		t.Errorf("params.mode = %q, want copilot fallback", body.Params.Mode)
	}
	if body.Params.ModelPreference != "pplx-custom-model" {
		t.Errorf("params.model_preference = %q, want raw model", body.Params.ModelPreference)
	}
}

// TestGrokWebExecutor_StatusErrors verifies Grok's non-200 handling: an error
// payload is returned with a human-readable message and the upstream body is
// closed — including the double-close path where Execute closes the body
// explicitly and again via defer.
func TestGrokWebExecutor_StatusErrors(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		wantSubstr string
	}{
		{"auth-expired", http.StatusUnauthorized, "Grok auth failed - SSO cookie may be expired"},
		{"forbidden", http.StatusForbidden, "Grok auth failed - SSO cookie may be expired"},
		{"rate-limited", http.StatusTooManyRequests, "Grok rate limited"},
		{"server-error", http.StatusInternalServerError, "Grok returned HTTP 500"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := newCookieRT(tt.status, `{"error":{"message":"upstream says no"}}`)
			exe := NewGrokWebExecutor(rt)
			req := newCookieRequest("grok-3", `[{"role":"user","content":"hi"}]`)

			resp, err := exe.Execute(context.Background(), req, newTestAccount("sso-secret-token"))
			if err != nil {
				t.Fatalf("Execute returned error (want response): %v", err)
			}
			assertErrorBody(t, resp, tt.status, tt.wantSubstr)
			if !rt.usesBody.isClosed() {
				t.Error("upstream body was not closed after status error")
			}
			assertNoCredentialLeak(t, "sso-secret-token", string(resp.Body))
		})
	}
}

// TestGrokWebExecutor_StreamStatusErrorClosesBody probes the stream error
// path for body closure and credential sanitation.
func TestGrokWebExecutor_StreamStatusErrorClosesBody(t *testing.T) {
	rt := newCookieRT(http.StatusUnauthorized, `{"error":{"message":"nope"}}`)
	exe := NewGrokWebExecutor(rt)
	req := newCookieRequest("grok-3", `[{"role":"user","content":"hi"}]`)

	resp, err := exe.ExecuteStream(context.Background(), req, newTestAccount("sso-secret-token"))
	if err != nil {
		t.Fatalf("ExecuteStream returned error (want response): %v", err)
	}
	assertErrorBody(t, resp, http.StatusUnauthorized, "Grok auth failed")
	if !rt.usesBody.isClosed() {
		t.Error("upstream body was not closed after stream status error")
	}
	assertNoCredentialLeak(t, "sso-secret-token", string(resp.Body))
}

// TestPerplexityWebExecutor_StatusErrors verifies Perplexity's non-200
// handling and body closure on both execute paths.
func TestPerplexityWebExecutor_StatusErrors(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		wantSubstr string
	}{
		{"auth-expired", http.StatusUnauthorized, "Perplexity auth failed - session cookie may be expired"},
		{"forbidden", http.StatusForbidden, "Perplexity auth failed - session cookie may be expired"},
		{"rate-limited", http.StatusTooManyRequests, "Perplexity returned HTTP 429"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := newCookieRT(tt.status, `{}`)
			exe := NewPerplexityWebExecutor(rt)
			req := newCookieRequest("pplx-sonar", `[{"role":"user","content":"hi"}]`)

			resp, err := exe.Execute(context.Background(), req, newTestAccount("pplx-session-secret"))
			if err != nil {
				t.Fatalf("Execute returned error (want response): %v", err)
			}
			assertErrorBody(t, resp, tt.status, tt.wantSubstr)
			if !rt.usesBody.isClosed() {
				t.Error("upstream body was not closed after status error")
			}
			assertNoCredentialLeak(t, "pplx-session-secret", string(resp.Body))
		})
	}
}

// TestSanitizeErrorString verifies that credential-like substrings are
// redacted from error messages before surfacing to callers.
func TestSanitizeErrorString(t *testing.T) {
	input := "auth failed sso=abc123 with Bearer xyz token=zzz credential_ref=ref api_key=key session-token=st"
	got := sanitizeErrorString(input)
	for _, pat := range credentialPatterns {
		if strings.Contains(got, pat) {
			t.Errorf("pattern %q not redacted in %q", pat, got)
		}
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Errorf("expected [REDACTED] markers in %q", got)
	}
	if strings.Contains(got, "abc123") || strings.Contains(got, "xyz") || strings.Contains(got, "zzz") {
		t.Errorf("credential values leaked: %q", got)
	}
}

// TestSanitizeErrorString_CleanMessagePassthrough verifies that messages
// without credential patterns pass through unmodified.
func TestSanitizeErrorString_CleanMessagePassthrough(t *testing.T) {
	msg := "rate limit exceeded for user 42"
	if got := sanitizeErrorString(msg); got != msg {
		t.Errorf("sanitize = %q, want unchanged %q", got, msg)
	}
}

// TestGrokNDJSONErrorSanitization verifies that a Grok NDJSON error line
// containing a credential is sanitized end-to-end by parseGrokNDJSONResponse.
func TestGrokNDJSONErrorSanitization(t *testing.T) {
	body := []byte(`{"error":{"message":"invalid sso=secret-token-here Bearer leak"}}` + "\n")
	_, err := parseGrokNDJSONResponse(body)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "secret-token-here") {
		t.Errorf("credential leaked through NDJSON error: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "[REDACTED]") {
		t.Errorf("expected [REDACTED] in %q", err.Error())
	}
}

// TestGrokWebExecutor_StreamDeliversNDJSON verifies the executor-level NDJSON
// stream: every line becomes a chunk and the stream terminates with final.
func TestGrokWebExecutor_StreamDeliversNDJSON(t *testing.T) {
	ndjson := `{"result":{"response":{"modelResponse":{"message":"a"}}}}` + "\n" +
		`{"result":{"response":{"modelResponse":{"message":"b"}}}}` + "\n"
	rt := newCookieRT(http.StatusOK, ndjson)
	exe := NewGrokWebExecutor(rt)
	req := newCookieRequest("grok-4", `[{"role":"user","content":"hi"}]`)

	resp, err := exe.ExecuteStream(context.Background(), req, newTestAccount("sso-tok"))
	if err != nil {
		t.Fatalf("ExecuteStream failed: %v", err)
	}
	if resp.Stream == nil {
		t.Fatal("nil stream")
	}
	chunks := readAllStream(t, resp.Stream)
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks (2 lines + final), got %d", len(chunks))
	}
	if !chunks[2].IsFinal {
		t.Error("last chunk should be final")
	}
	if string(chunks[0].Data) != `{"result":{"response":{"modelResponse":{"message":"a"}}}}` {
		t.Errorf("chunk[0].Data = %s", chunks[0].Data)
	}
}

// TestGrokWebExecutor_StreamModelMapping verifies streaming uses the model
// map and that unknown models fall back to grok-4.1-fast.
func TestGrokWebExecutor_StreamModelMapping(t *testing.T) {
	tests := []struct {
		model     string
		wantModel string
		wantMode  string
	}{
		{"grok-3-mini", "grok-3", "MODEL_MODE_GROK_3_MINI_THINKING"},
		{"grok-4-thinking", "grok-4", "MODEL_MODE_GROK_4_THINKING"},
		{"not-a-real-model", "grok-4-1-thinking-1129", "MODEL_MODE_FAST"},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			rt := newCookieRT(http.StatusOK, grokNDJSONBody)
			exe := NewGrokWebExecutor(rt)
			req := newCookieRequest(tt.model, `[{"role":"user","content":"hi"}]`)

			if _, err := exe.ExecuteStream(context.Background(), req, newTestAccount("tok")); err != nil {
				t.Fatalf("ExecuteStream failed: %v", err)
			}
			raw, _ := io.ReadAll(rt.captured.Body)
			var payload map[string]interface{}
			_ = json.Unmarshal(raw, &payload)
			if got := payload["modelName"]; got != tt.wantModel {
				t.Errorf("modelName = %v, want %v", got, tt.wantModel)
			}
			if got := payload["modelMode"]; got != tt.wantMode {
				t.Errorf("modelMode = %v, want %v", got, tt.wantMode)
			}
		})
	}
}

// TestPerplexityWebExecutor_StreamDeliversSSE verifies executor-level SSE
// delivery: data lines are pushed as chunks and [DONE] ends the stream.
func TestPerplexityWebExecutor_StreamDeliversSSE(t *testing.T) {
	sse := "event: text\ndata: {\"text\":\"first\"}\n\nevent: text\ndata: {\"text\":\"second\"}\n\nevent: done\ndata: [DONE]\n\n"
	rt := newCookieRT(http.StatusOK, sse)
	exe := NewPerplexityWebExecutor(rt)
	req := newCookieRequest("pplx-sonar", `[{"role":"user","content":"hi"}]`)

	resp, err := exe.ExecuteStream(context.Background(), req, newTestAccount("pplx-session-1"))
	if err != nil {
		t.Fatalf("ExecuteStream failed: %v", err)
	}
	chunks := readAllStream(t, resp.Stream)
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks (2 data + final), got %d", len(chunks))
	}
	if chunks[0].Event != "text" {
		t.Errorf("chunk[0].Event = %q, want text", chunks[0].Event)
	}
	if !strings.Contains(string(chunks[0].Data), "first") {
		t.Errorf("chunk[0].Data = %s, want first payload", chunks[0].Data)
	}
	if !chunks[2].IsFinal {
		t.Error("last chunk should be final")
	}
}

// TestGrokStreamClosesBodyOnCompletion verifies the Grok stream path closes
// the upstream body once the stream completes.
func TestGrokStreamClosesBodyOnCompletion(t *testing.T) {
	rt := newCookieRT(http.StatusOK, grokNDJSONBody)
	exe := NewGrokWebExecutor(rt)
	req := newCookieRequest("grok-3", `[{"role":"user","content":"hi"}]`)

	resp, err := exe.ExecuteStream(context.Background(), req, newTestAccount("tok"))
	if err != nil {
		t.Fatalf("ExecuteStream failed: %v", err)
	}
	readAllStream(t, resp.Stream)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if rt.usesBody.isClosed() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("upstream body was not closed after stream completion")
}

// TestPerplexityStreamClosesBodyOnCompletion verifies the Perplexity stream
// path closes the upstream body after [DONE].
func TestPerplexityStreamClosesBodyOnCompletion(t *testing.T) {
	rt := newCookieRT(http.StatusOK, "event: done\ndata: [DONE]\n\n")
	exe := NewPerplexityWebExecutor(rt)
	req := newCookieRequest("pplx-sonar", `[{"role":"user","content":"hi"}]`)

	resp, err := exe.ExecuteStream(context.Background(), req, newTestAccount("tok"))
	if err != nil {
		t.Fatalf("ExecuteStream failed: %v", err)
	}
	readAllStream(t, resp.Stream)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if rt.usesBody.isClosed() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("upstream body was not closed after [DONE]")
}

// TestGrokWebExecutor_EmptyMessages verifies validation of the request body
// shape: missing or empty messages produce an error response without any
// upstream call.
func TestGrokWebExecutor_EmptyMessages(t *testing.T) {
	rt := newCookieRT(http.StatusOK, grokNDJSONBody)
	exe := NewGrokWebExecutor(rt)
	for name, body := range map[string]string{
		"empty":       `{}`,
		"no-messages": `{"messages":[]}`,
		"bad-json":    `not json`,
	} {
		t.Run(name, func(t *testing.T) {
			req := newTestRequest(engine.FormatOpenAIChat, "grok-3", []byte(body))
			resp, err := exe.Execute(context.Background(), req, newTestAccount("tok"))
			if err != nil {
				t.Fatalf("Execute returned error (want response): %v", err)
			}
			assertErrorBody(t, resp, http.StatusBadRequest, "Missing or empty messages array")
			if rt.captured != nil {
				t.Error("upstream must not be called for invalid request body")
			}
		})
	}
}

// TestGrokWebExecutor_ProviderIdentity pins the cookie executor identities.
func TestGrokWebExecutor_ProviderIdentity(t *testing.T) {
	g := NewGrokWebExecutor(nil)
	if g.ProviderType() != provider.ProviderGrokWeb {
		t.Errorf("ProviderType = %q, want %q", g.ProviderType(), provider.ProviderGrokWeb)
	}
	if !g.SupportsFormat(engine.FormatOpenAIChat) || !g.SupportsFormat(engine.FormatOpenAICompat) {
		t.Error("GrokWeb must support both OpenAI chat formats")
	}
	p := NewPerplexityWebExecutor(nil)
	if p.ProviderType() != provider.ProviderPerplexityWeb {
		t.Errorf("ProviderType = %q, want %q", p.ProviderType(), provider.ProviderPerplexityWeb)
	}
	if !p.SupportsFormat(engine.FormatOpenAIChat) || !p.SupportsFormat(engine.FormatOpenAICompat) {
		t.Error("PerplexityWeb must support both OpenAI chat formats")
	}
}

// TestPplxModelMapCoverage verifies every mapped model maps to a non-empty
// mode/preference pair.
func TestPplxModelMapCoverage(t *testing.T) {
	for model, pref := range pplxModelMap {
		if pref[0] == "" || pref[1] == "" {
			t.Errorf("pplx model %q has empty mapping %v", model, pref)
		}
	}
}

// TestGrokModelMapCoverage verifies every mapped model maps to a valid grok
// model and mode.
func TestGrokModelMapCoverage(t *testing.T) {
	for model, entry := range grokModelMap {
		if entry.grokModel == "" || entry.modelMode == "" {
			t.Errorf("grok model %q has empty mapping %+v", model, entry)
		}
	}
}

// TestGrokWebExecutor_Execute_ClosesBodyExactlyOnce pins the close-once
// contract on the GrokWeb non-streaming path: success and error paths each
// close the upstream body exactly once (no deferred + explicit double close).
func TestGrokWebExecutor_Execute_ClosesBodyExactlyOnce(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"success", http.StatusOK, grokNDJSONBody},
		{"auth-error", http.StatusUnauthorized, `{"error":"no"}`},
		{"upstream-error", http.StatusInternalServerError, `{"error":"no"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt := newCookieRT(tc.status, tc.body)
			exe := NewGrokWebExecutor(rt)
			req := newCookieRequest("grok-3", `[{"role":"user","content":"hi"}]`)

			if _, err := exe.Execute(context.Background(), req, newTestAccount("sso=test-token")); err != nil {
				t.Fatalf("Execute returned error: %v", err)
			}
			if got := rt.usesBody.closeCalls(); got != 1 {
				t.Errorf("response body closed %d times, want exactly 1", got)
			}
		})
	}
}

// TestPerplexityWebExecutor_Execute_ClosesBodyExactlyOnce pins the close-once
// contract on the PerplexityWeb non-streaming path.
func TestPerplexityWebExecutor_Execute_ClosesBodyExactlyOnce(t *testing.T) {
	rt := newCookieRT(http.StatusOK, `{"result":"ok"}`)
	exe := NewPerplexityWebExecutor(rt)
	req := newCookieRequest("pplx-sonar", `[{"role":"user","content":"hi"}]`)

	if _, err := exe.Execute(context.Background(), req, newTestAccount("session-token-1")); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if got := rt.usesBody.closeCalls(); got != 1 {
		t.Errorf("response body closed %d times, want exactly 1", got)
	}
}

// TestGrokWebExecutor_ExecuteStream_ErrorClosesBodyExactlyOnce pins the
// close-once contract on the GrokWeb streaming error path.
func TestGrokWebExecutor_ExecuteStream_ErrorClosesBodyExactlyOnce(t *testing.T) {
	rt := newCookieRT(http.StatusUnauthorized, `{"error":"no"}`)
	exe := NewGrokWebExecutor(rt)
	req := newCookieRequest("grok-3", `[{"role":"user","content":"hi"}]`)

	resp, err := exe.ExecuteStream(context.Background(), req, newTestAccount("sso=secret-cookie-value"))
	if err != nil {
		t.Fatalf("ExecuteStream returned error: %v", err)
	}
	if got := rt.usesBody.closeCalls(); got != 1 {
		t.Errorf("response body closed %d times, want exactly 1", got)
	}
	assertNoCredentialLeak(t, "secret-cookie-value", string(resp.Body))
}
