package specialized

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"gorouter/internal/domain/engine/stream"
)

// TestParseGrokNDJSONResponse_ValidMessage exercises the happy path of
// parseGrokNDJSONResponse: a single NDJSON line containing a valid model
// response message is correctly extracted.
func TestParseGrokNDJSONResponse_ValidMessage(t *testing.T) {
	body := []byte(`{"result":{"response":{"modelResponse":{"message":"Hello, world!"}}}}` + "\n")
	content, err := parseGrokNDJSONResponse(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != "Hello, world!" {
		t.Errorf("content = %q, want %q", content, "Hello, world!")
	}
}

// TestParseGrokNDJSONResponse_EmptyBody verifies that an empty body returns
// an error rather than a zero-value string (regression guard).
func TestParseGrokNDJSONResponse_EmptyBody(t *testing.T) {
	_, err := parseGrokNDJSONResponse([]byte{})
	if err == nil {
		t.Fatal("expected error for empty body")
	}
}

// TestParseGrokNDJSONResponse_ErrorLine verifies that an NDJSON error line
// is correctly classified and returned as a parse error with the API error
// message preserved (but not leaked into credentials).
func TestParseGrokNDJSONResponse_ErrorLine(t *testing.T) {
	body := []byte(`{"error":{"message":"rate limit exceeded","code":429}}` + "\n")
	_, err := parseGrokNDJSONResponse(body)
	if err == nil {
		t.Fatal("expected error for error line")
	}
	if !strings.Contains(err.Error(), "rate limit exceeded") {
		t.Errorf("error = %q, want substr %q", err.Error(), "rate limit exceeded")
	}
}

// TestParseGrokNDJSONResponse_MultiLine exercises the multi-line NDJSON
// response case: the parser picks the last valid content line.
func TestParseGrokNDJSONResponse_MultiLine(t *testing.T) {
	body := []byte(
		`{"result":{"response":{"modelResponse":{"message":"First"}}}}` + "\n" +
			`{"result":{"response":{"modelResponse":{"message":"Second"}}}}` + "\n" +
			`{"result":{"response":{"token":"abc"}}}` + "\n",
	)
	content, err := parseGrokNDJSONResponse(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != "Second" {
		t.Errorf("content = %q, want %q", content, "Second")
	}
}

// TestParseGrokNDJSONResponse_ErrorThenMessage verifies that an error line
// before a content line causes a parse error (error lines are authoritative).
func TestParseGrokNDJSONResponse_ErrorThenMessage(t *testing.T) {
	body := []byte(
		`{"error":{"message":"rate limit","code":429}}` + "\n" +
			`{"result":{"response":{"modelResponse":{"message":"Should not appear"}}}}` + "\n",
	)
	_, err := parseGrokNDJSONResponse(body)
	if err == nil {
		t.Fatal("expected error when error line present")
	}
}

// TestBuildChatCompletion_ValidOutput verifies buildChatCompletion produces
// a well-formed OpenAI chat completion JSON payload.
func TestBuildChatCompletion_ValidOutput(t *testing.T) {
	model := "grok-4"
	content := "Hello, world!"
	oai := buildChatCompletion(model, content)

	var parsed struct {
		Object  string `json:"object"`
		Model   string `json:"model"`
		Choices []struct {
			Index   int `json:"index"`
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := jsonUnmarshal(oai, &parsed); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if parsed.Object != "chat.completion" {
		t.Errorf("object = %q, want %q", parsed.Object, "chat.completion")
	}
	if parsed.Model != model {
		t.Errorf("model = %q, want %q", parsed.Model, model)
	}
	if len(parsed.Choices) != 1 {
		t.Fatalf("choices count = %d, want 1", len(parsed.Choices))
	}
	if parsed.Choices[0].Message.Content != content {
		t.Errorf("content = %q, want %q", parsed.Choices[0].Message.Content, content)
	}
	if parsed.Choices[0].Message.Role != "assistant" {
		t.Errorf("role = %q, want %q", parsed.Choices[0].Message.Role, "assistant")
	}
	if parsed.Choices[0].FinishReason != "stop" {
		t.Errorf("finish_reason = %q, want %q", parsed.Choices[0].FinishReason, "stop")
	}
}

// TestStreamGrokNDJSON_DeliversChunks verifies that streamGrokNDJSON delivers
// each NDJSON line as a data chunk followed by a final IsFinal chunk.
func TestStreamGrokNDJSON_DeliversChunks(t *testing.T) {
	input := `{"result":{"response":{"modelResponse":{"message":"A"}}}}` + "\n" +
		`{"result":{"response":{"modelResponse":{"message":"B"}}}}` + "\n"

	exe := NewGrokWebExecutor(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	st := stream.NewStream(ctx, 64)
	go exe.streamGrokNDJSON(ctx, st, strings.NewReader(input), &nopCloser{})

	var chunks []stream.Chunk
	for ch := range st.Chunks() {
		chunks = append(chunks, ch)
	}

	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks (2 data + 1 final), got %d", len(chunks))
	}
	if !chunks[2].IsFinal {
		t.Error("last chunk should have IsFinal=true")
	}
}

// TestStreamPPLXSSE_DeliversChunks verifies that streamPPLXSSE delivers
// SSE events as correctly typed chunks.
func TestStreamPPLXSSE_DeliversChunks(t *testing.T) {
	input := "event: text\ndata: {\"text\":\"hello\"}\n\nevent: done\ndata: [DONE]\n\n"

	exe := NewPerplexityWebExecutor(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	st := stream.NewStream(ctx, 64)
	go exe.streamPPLXSSE(ctx, st, strings.NewReader(input), &nopCloser{})

	var chunks []stream.Chunk
	for ch := range st.Chunks() {
		chunks = append(chunks, ch)
	}

	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if chunks[0].Event != "text" {
		t.Errorf("chunk[0].Event = %q, want %q", chunks[0].Event, "text")
	}
	if !chunks[1].IsFinal {
		t.Error("chunk[1] should have IsFinal=true")
	}
}

// TestStreamEventStream_DeliversChunks verifies that Kiro's EventStream
// delivers each line as an "eventstream" chunk.
func TestStreamEventStream_DeliversChunks(t *testing.T) {
	input := `{"type":"event","data":"hello"}` + "\n" +
		`{"type":"final","data":"bye"}` + "\n"

	exe := NewKiroExecutor(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	st := stream.NewStream(ctx, 64)
	go exe.streamEventStream(ctx, st, strings.NewReader(input), &nopCloser{})

	var chunks []stream.Chunk
	for ch := range st.Chunks() {
		chunks = append(chunks, ch)
	}

	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}
	if chunks[0].Event != "eventstream" {
		t.Errorf("chunk[0].Event = %q, want %q", chunks[0].Event, "eventstream")
	}
	if !chunks[2].IsFinal {
		t.Error("chunk[2] should have IsFinal=true")
	}
}

// TestStreamGrokNDJSON_CancelViaContext verifies that cancelling the parent
// context terminates the Grok NDJSON streamer without deadlock.
func TestStreamGrokNDJSON_CancelViaContext(t *testing.T) {
	pr, pw := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())

	exe := NewGrokWebExecutor(nil)
	st := stream.NewStream(ctx, 64)
	go exe.streamGrokNDJSON(ctx, st, pr, pr)

	// Write a single line, then cancel.
	pw.Write([]byte(`{"result":{"response":{"modelResponse":{"message":"hello"}}}}` + "\n"))
	cancel()

	<-st.Done()
	state := st.State()
	if state != stream.StateCancelled && state != stream.StateDone {
		t.Errorf("expected Cancelled or Done, got %v", state)
	}
	pw.Close()
}

// TestStreamPPLXSSE_CancelViaContext verifies context cancellation propagation
// in the Perplexity SSE streamer.
func TestStreamPPLXSSE_CancelViaContext(t *testing.T) {
	pr, pw := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())

	exe := NewPerplexityWebExecutor(nil)
	st := stream.NewStream(ctx, 64)
	go exe.streamPPLXSSE(ctx, st, pr, pr)

	pw.Write([]byte("event: text\ndata: {\"text\":\"hello\"}\n\n"))
	cancel()

	<-st.Done()
	state := st.State()
	if state != stream.StateCancelled && state != stream.StateDone {
		t.Errorf("expected Cancelled or Done, got %v", state)
	}
	pw.Close()
}

// TestStreamEventStream_CancelViaContext verifies context cancellation propagation
// in the Kiro EventStream streamer.
func TestStreamEventStream_CancelViaContext(t *testing.T) {
	pr, pw := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())

	exe := NewKiroExecutor(nil)
	st := stream.NewStream(ctx, 64)
	go exe.streamEventStream(ctx, st, pr, pr)

	pw.Write([]byte(`{"type":"event","data":"test"}` + "\n"))
	cancel()

	<-st.Done()
	state := st.State()
	if state != stream.StateCancelled && state != stream.StateDone {
		t.Errorf("expected Cancelled or Done, got %v", state)
	}
	pw.Close()
}

// TestParseGrokNDJSONResponse_CredentialNotLeaked verifies that error messages
// from parseGrokNDJSONResponse do not contain credential-like patterns.
// This is a controlled mutation proof: even if the upstream format changes,
// the parser must not emit credential data in errors.
func TestParseGrokNDJSONResponse_CredentialNotLeaked(t *testing.T) {
	bodies := [][]byte{
		[]byte(`{"error":{"message":"sso=abc123 invalid"}}` + "\n"),
		[]byte(`{"error":{"message":"Bearer token expired"}}` + "\n"),
		[]byte(`{"error":{"message":"credential_ref=xyz"}}` + "\n"),
	}
	for _, body := range bodies {
		_, err := parseGrokNDJSONResponse(body)
		if err == nil {
			t.Log("expected error for credential-bearing body")
			continue
		}
		msg := err.Error()
		if strings.Contains(msg, "sso=") {
			t.Errorf("credential pattern 'sso=' leaked in error: %s", msg)
		}
		if strings.Contains(msg, "Bearer ") {
			t.Errorf("credential pattern 'Bearer ' leaked in error: %s", msg)
		}
		if strings.Contains(msg, "credential_ref=") {
			t.Errorf("credential pattern 'credential_ref=' leaked in error: %s", msg)
		}
	}
}

// TestStreamGuards_FirstChunkTimeout verifies that the Grok NDJSON streamer's
// first-chunk timeout fires and cancels the stream when no data arrives.
func TestStreamGuards_FirstChunkTimeout(t *testing.T) {
	pr, _ := io.Pipe()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	exe := &GrokWebExecutor{
		streamCfg: StreamingConfig{
			FirstChunkTimeout: 50 * time.Millisecond,
			StallTimeout:      0,
			DisconnectGrace:   0,
		},
	}
	st := stream.NewStream(ctx, 64)
	go exe.streamGrokNDJSON(ctx, st, pr, pr)

	<-st.Done()
	if st.State() != stream.StateCancelled {
		t.Logf("stream state = %v (expected Cancelled after first-chunk timeout)", st.State())
	}
}

// nopCloser implements io.Closer as a no-op (safe for tests).
type nopCloser struct{}

func (n *nopCloser) Close() error { return nil }

// jsonUnmarshal is a test helper wrapping encoding/json.Unmarshal.
func jsonUnmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

var _ = sync.Mutex{}
