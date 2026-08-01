package specialized

// g104_remediation_test.go — behavioral locks for the gosec G104 remediation.
// Every previously-flagged statement-position error call became an explicit
// best-effort discard (`_ = ...`). These tests pin the semantics those discards
// preserve: best-effort request/response JSON parsing (nil-map fallback), and
// close-once on every stream error path.

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"gorouter/internal/domain/engine"
)

// g104JSONExec describes one executor whose request-building path performs a
// best-effort json.Unmarshal of the request body.
//
// execInjects reports whether the non-stream Execute path injects a top-level
// "stream" key; streamInjects whether ExecuteStream does; execModel whether
// Execute injects a top-level "model" key.
type g104JSONExec struct {
	name          string
	newExec       func(http.RoundTripper) engine.Executor
	model         string
	execInjects   bool
	streamInjects bool
	execModel     bool
}

func g104JSONExecutors() []g104JSONExec {
	openAICompat := func(name string, newExec func(http.RoundTripper) engine.Executor) g104JSONExec {
		return g104JSONExec{name: name, newExec: newExec, model: "model-x", execInjects: true, streamInjects: true, execModel: true}
	}
	return []g104JSONExec{
		openAICompat("deepseek", func(rt http.RoundTripper) engine.Executor { return NewDeepSeekExecutor(rt) }),
		openAICompat("fireworks", func(rt http.RoundTripper) engine.Executor { return NewFireworksExecutor(rt) }),
		openAICompat("groq", func(rt http.RoundTripper) engine.Executor { return NewGroqExecutor(rt) }),
		openAICompat("mistral", func(rt http.RoundTripper) engine.Executor { return NewMistralExecutor(rt) }),
		openAICompat("perplexity", func(rt http.RoundTripper) engine.Executor { return NewPerplexityExecutor(rt) }),
		openAICompat("together", func(rt http.RoundTripper) engine.Executor { return NewTogetherExecutor(rt) }),
		openAICompat("xai", func(rt http.RoundTripper) engine.Executor { return NewXAIExecutor(rt) }),
		openAICompat("iflow", func(rt http.RoundTripper) engine.Executor { return NewIFlowExecutor(rt) }),
		openAICompat("grokcli", func(rt http.RoundTripper) engine.Executor { return NewGrokCliExecutor(rt) }),
		openAICompat("qoder", func(rt http.RoundTripper) engine.Executor { return NewQoderExecutor(rt) }),
		openAICompat("kimchi", func(rt http.RoundTripper) engine.Executor { return NewKimchiExecutor(rt) }),
		openAICompat("cursor", func(rt http.RoundTripper) engine.Executor { return NewCursorExecutor(rt) }),
		openAICompat("kiro", func(rt http.RoundTripper) engine.Executor { return NewKiroExecutor(rt) }),
		openAICompat("ollama", func(rt http.RoundTripper) engine.Executor { return NewOllamaLocalExecutor(rt) }),
		openAICompat("commandcode", func(rt http.RoundTripper) engine.Executor { return NewCommandCodeExecutor(rt) }),
		openAICompat("xiaomi", func(rt http.RoundTripper) engine.Executor { return NewXiaomiTokenplanExecutor(rt) }),
		openAICompat("mimo", func(rt http.RoundTripper) engine.Executor { return NewMimoFreeExecutor(rt) }),
		openAICompat("codebuddy", func(rt http.RoundTripper) engine.Executor { return NewCodeBuddyExecutor(rt) }),

		// GitHub executeChat injects stream but never model (missing_executors
		// buildRequest injects model; executeChat does not).
		{name: "github", newExec: func(rt http.RoundTripper) engine.Executor { return NewGitHubExecutor(rt) },
			model: "gpt-5", execInjects: true, streamInjects: true, execModel: false},
		// GitHub claude routes to executeMessages (non-stream: no stream key)
		// and executeMessagesStream (stream: stream=true).
		{name: "github-claude", newExec: func(rt http.RoundTripper) engine.Executor { return NewGitHubExecutor(rt) },
			model: "claude-sonnet-4-20250514", execInjects: false, streamInjects: true, execModel: false},
		// GeminiCLI/Antigravity wrap the payload: top-level model, no stream.
		{name: "geminicli", newExec: func(rt http.RoundTripper) engine.Executor { return NewGeminiCLIExecutor(rt) },
			model: "model-x", execInjects: false, streamInjects: false, execModel: true},
		{name: "antigravity", newExec: func(rt http.RoundTripper) engine.Executor { return NewAntigravityExecutor(rt) },
			model: "model-x", execInjects: false, streamInjects: false, execModel: true},
		// Vertex passes the payload through unwrapped.
		{name: "vertex", newExec: func(rt http.RoundTripper) engine.Executor { return NewVertexExecutor(rt, false) },
			model: "model-x", execInjects: false, streamInjects: false, execModel: false},
		// PerplexityWeb builds a nested pplxBody.
		{name: "perplexityweb", newExec: func(rt http.RoundTripper) engine.Executor { return NewPerplexityWebExecutor(rt) },
			model: "pplx-auto", execInjects: false, streamInjects: false, execModel: false},
	}
}

// TestBestEffortInvalidJSONBody_Execute verifies that a non-JSON request body
// does not fail Execute for every executor with a best-effort request-body
// json.Unmarshal: the empty-map fallback still produces a valid outbound
// payload with the requested params injected.
func TestBestEffortInvalidJSONBody_Execute(t *testing.T) {
	for _, tc := range g104JSONExecutors() {
		t.Run(tc.name, func(t *testing.T) {
			rt := newWireRT(http.StatusOK, oaiOKBody)
			exe := tc.newExec(rt)
			req := newAuthProbeRequest("", tc.model, "not-json")

			resp, err := exe.Execute(context.Background(), req, newTestAccount("g104-key"))
			if err != nil {
				t.Fatalf("Execute with invalid JSON body: %v", err)
			}
			if resp == nil || resp.StatusCode != http.StatusOK {
				t.Fatalf("resp = %+v, want 200", resp)
			}

			payload := capturedJSONBody(t, rt)
			if tc.execInjects {
				if s, ok := payload["stream"].(bool); !ok || s {
					t.Errorf("payload.stream = %v, want false", payload["stream"])
				}
			}
			if tc.execModel {
				if got := payload["model"]; got != tc.model {
					t.Errorf("payload.model = %v, want %q", got, tc.model)
				}
			}
		})
	}
}

// TestBestEffortInvalidJSONBody_ExecuteStream verifies the same best-effort
// request-body parse on the streaming path: a working stream is still produced.
func TestBestEffortInvalidJSONBody_ExecuteStream(t *testing.T) {
	for _, tc := range g104JSONExecutors() {
		t.Run(tc.name, func(t *testing.T) {
			rt := newWireRT(http.StatusOK, sseChatBody)
			exe := tc.newExec(rt)
			req := newAuthProbeRequest("", tc.model, "not-json")

			resp, err := exe.ExecuteStream(context.Background(), req, newTestAccount("g104-key"))
			if err != nil {
				t.Fatalf("ExecuteStream with invalid JSON body: %v", err)
			}
			if resp == nil || resp.Stream == nil {
				t.Fatal("expected stream response")
			}
			chunks := readAllStream(t, resp.Stream)
			if len(chunks) == 0 || !chunks[len(chunks)-1].IsFinal {
				t.Fatalf("expected final-terminated stream, got %d chunks", len(chunks))
			}

			payload := capturedJSONBody(t, rt)
			if tc.streamInjects {
				if s, ok := payload["stream"].(bool); !ok || !s {
					t.Errorf("payload.stream = %v, want true", payload["stream"])
				}
			}
		})
	}
}

// TestGrokCliExecutor_InvalidResponseBody_BestEffort locks the best-effort
// response-body json.Unmarshal in GrokCli Execute (custom_auth.go): an invalid
// response body is passed through raw and the request succeeds.
func TestGrokCliExecutor_InvalidResponseBody_BestEffort(t *testing.T) {
	rt := newWireRT(http.StatusOK, "not-json")
	exe := NewGrokCliExecutor(rt)
	req := newAuthProbeRequest("", "grok-x", `{"model":"grok-x"}`)

	resp, err := exe.Execute(context.Background(), req, newTestAccount("g104-key"))
	if err != nil {
		t.Fatalf("Execute with invalid response body must not error: %v", err)
	}
	if resp == nil || string(resp.Body) != "not-json" {
		t.Errorf("resp.Body = %v, want raw passthrough of invalid body", resp.Body)
	}
	if resp.Model != "" {
		t.Errorf("resp.Model = %q, want empty when response parse fails (best-effort)", resp.Model)
	}
}

// TestG104ErrorPathClosesBodyExactlyOnce pins the close-once contract for the
// stream error paths that previously carried an unhandled resp.Body.Close().
func TestG104ErrorPathClosesBodyExactlyOnce(t *testing.T) {
	streamExecs := []struct {
		name    string
		newExec func(http.RoundTripper) engine.Executor
		// syntheticResponse marks executors that return a status-carrying
		// engine.Response with nil error on non-2xx (GrokWeb, PerplexityWeb).
		syntheticResponse bool
	}{
		{"deepseek", func(rt http.RoundTripper) engine.Executor { return NewDeepSeekExecutor(rt) }, false},
		{"fireworks", func(rt http.RoundTripper) engine.Executor { return NewFireworksExecutor(rt) }, false},
		{"groq", func(rt http.RoundTripper) engine.Executor { return NewGroqExecutor(rt) }, false},
		{"mistral", func(rt http.RoundTripper) engine.Executor { return NewMistralExecutor(rt) }, false},
		{"perplexity", func(rt http.RoundTripper) engine.Executor { return NewPerplexityExecutor(rt) }, false},
		{"together", func(rt http.RoundTripper) engine.Executor { return NewTogetherExecutor(rt) }, false},
		{"xai", func(rt http.RoundTripper) engine.Executor { return NewXAIExecutor(rt) }, false},
		{"azure", func(rt http.RoundTripper) engine.Executor { return NewAzureExecutor(rt) }, false},
		{"github", func(rt http.RoundTripper) engine.Executor { return NewGitHubExecutor(rt) }, false},
		{"qwen", func(rt http.RoundTripper) engine.Executor { return NewQwenExecutor(rt) }, false},
		{"ollama", func(rt http.RoundTripper) engine.Executor { return NewOllamaLocalExecutor(rt) }, false},
		{"commandcode", func(rt http.RoundTripper) engine.Executor { return NewCommandCodeExecutor(rt) }, false},
		{"xiaomi", func(rt http.RoundTripper) engine.Executor { return NewXiaomiTokenplanExecutor(rt) }, false},
		{"mimo", func(rt http.RoundTripper) engine.Executor { return NewMimoFreeExecutor(rt) }, false},
		{"codebuddy", func(rt http.RoundTripper) engine.Executor { return NewCodeBuddyExecutor(rt) }, false},
		{"grokweb", func(rt http.RoundTripper) engine.Executor { return NewGrokWebExecutor(rt) }, true},
		{"perplexityweb", func(rt http.RoundTripper) engine.Executor { return NewPerplexityWebExecutor(rt) }, true},
	}
	for _, tc := range streamExecs {
		t.Run(tc.name, func(t *testing.T) {
			rt := newWireRT(http.StatusUnauthorized, `{"error":"no"}`)
			exe := tc.newExec(rt)
			body := `{"model":"model-x"}`
			if tc.syntheticResponse {
				body = `{"model":"model-x","messages":[{"role":"user","content":"hi"}]}`
			}
			req := newAuthProbeRequest("", "model-x", body)

			resp, err := exe.ExecuteStream(context.Background(), req, newTestAccount("g104-key"))
			if tc.syntheticResponse {
				if err != nil {
					t.Fatalf("synthetic-response executor returned error: %v", err)
				}
				if resp == nil || resp.StatusCode != http.StatusUnauthorized {
					t.Fatalf("resp = %+v, want synthetic 401 response", resp)
				}
			} else {
				if err == nil {
					t.Fatal("expected error for 401")
				}
			}
			if got := rt.usesBody.closeCalls(); got != 1 {
				t.Errorf("response body closed %d times, want exactly 1", got)
			}
		})
	}
}

// TestGitHubExecutor_ClaudeMessages_ClosesBody pins handleResponse close-once
// on both the error and success paths of the /v1/messages route.
func TestGitHubExecutor_ClaudeMessages_ClosesBody(t *testing.T) {
	req := newAuthProbeRequest("", "claude-sonnet-4-20250514", `{"model":"claude-sonnet-4-20250514"}`)

	rt := newWireRT(http.StatusUnauthorized, `{"error":"no"}`)
	exe := NewGitHubExecutor(rt)
	if _, err := exe.Execute(context.Background(), req, newTestAccount("g104-key")); err == nil {
		t.Fatal("expected error for 401 on /v1/messages route")
	}
	if got := rt.usesBody.closeCalls(); got != 1 {
		t.Errorf("401 path closed body %d times, want exactly 1", got)
	}

	rtOK := newWireRT(http.StatusOK, `{"id":"msg_1"}`)
	exeOK := NewGitHubExecutor(rtOK)
	if _, err := exeOK.Execute(context.Background(), req, newTestAccount("g104-key")); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := rtOK.usesBody.closeCalls(); got != 1 {
		t.Errorf("200 path closed body %d times, want exactly 1", got)
	}
}

// TestSpecializedAtomicClose_ClosesExactlyOnce pins the atomicClose helper used
// by the streamers (specialized.go).
func TestSpecializedAtomicClose_ClosesExactlyOnce(t *testing.T) {
	var ac atomicClose
	b := newTrackingBody("x")
	ac.Close(b)
	ac.Close(b)
	if got := b.closeCalls(); got != 1 {
		t.Errorf("atomicClose closed underlying closer %d times, want exactly 1", got)
	}
}

// TestSpecializedInjectStreamParam_ClosesOriginalBody pins that
// injectStreamParam closes the original request body exactly once and restores
// a readable body carrying the injected stream flag.
func TestSpecializedInjectStreamParam_ClosesOriginalBody(t *testing.T) {
	orig := newTrackingBody(`{"model":"m","messages":[]}`)
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
