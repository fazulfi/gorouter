package specialized

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gorouter/internal/domain/engine/stream"
)

func TestStreamSSE_SingleTerminal(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"done-terminal", "data: hello\n\ndata: [DONE]\n\n"},
		{"eof-terminal", "data: hello\n\n"},
		{"multi-done", "data: a\n\ndata: [DONE]\n\ndata: [DONE]\n\n"},
		{"empty-then-done", "\n\ndata: [DONE]\n\n"},
		{"event-then-done", "event: test\ndata: {}\n\ndata: [DONE]\n\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			st := stream.NewStream(ctx, 64)
			go streamSSE(ctx, st, strings.NewReader(tt.input), &nopCloser{},
				DefaultStreamingConfig(), DefaultStreamingConfig().FirstChunkTimeout)

			var terminalCount int32
			for ch := range st.Chunks() {
				if ch.IsFinal {
					atomic.AddInt32(&terminalCount, 1)
				}
			}

			if n := atomic.LoadInt32(&terminalCount); n != 1 {
				t.Errorf("expected exactly 1 terminal chunk, got %d", n)
			}
		})
	}
}

func TestStreamSSE_NoDoubleTerminal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	st := stream.NewStream(ctx, 64)
	input := "data: chunk1\n\ndata: [DONE]\n\ndata: trailing\n\n"
	go streamSSE(ctx, st, strings.NewReader(input), &nopCloser{},
		DefaultStreamingConfig(), DefaultStreamingConfig().FirstChunkTimeout)

	var terminalCount int32
	var dataCount int32
	for ch := range st.Chunks() {
		if ch.IsFinal {
			atomic.AddInt32(&terminalCount, 1)
		}
		if len(ch.Data) > 0 {
			atomic.AddInt32(&dataCount, 1)
		}
	}

	if n := atomic.LoadInt32(&terminalCount); n != 1 {
		t.Errorf("expected exactly 1 terminal chunk, got %d", n)
	}
	if n := atomic.LoadInt32(&dataCount); n != 1 {
		t.Errorf("expected exactly 1 data chunk, got %d (trailing data after [DONE] leaked)", n)
	}
}

func TestStreamSSE_StallTimeout(t *testing.T) {
	pr, pw := io.Pipe()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := StreamingConfig{
		FirstChunkTimeout: 200 * time.Second,
		StallTimeout:      50 * time.Millisecond,
		DisconnectGrace:   0,
	}

	st := stream.NewStream(ctx, 64)
	start := time.Now()
	go streamSSE(ctx, st, pr, pr, cfg, cfg.FirstChunkTimeout)

	pw.Write([]byte("data: hello\n\n"))
	time.Sleep(20 * time.Millisecond)

	<-st.Done()
	elapsed := time.Since(start)

	if elapsed > 3*time.Second {
		t.Errorf("stall timeout took too long: %v (expected ~50ms)", elapsed)
	}
	_ = st.State()
	pw.Close()
}

func TestStreamSSE_StallTimeout_NoFirstChunk(t *testing.T) {
	pr, _ := io.Pipe()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := StreamingConfig{
		FirstChunkTimeout: 50 * time.Millisecond,
		StallTimeout:      200 * time.Second,
		DisconnectGrace:   0,
	}

	st := stream.NewStream(ctx, 64)
	go streamSSE(ctx, st, pr, pr, cfg, cfg.FirstChunkTimeout)

	<-st.Done()
}

func TestStreamSSE_ContextCancellation(t *testing.T) {
	pr, pw := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())

	cfg := DefaultStreamingConfig()
	st := stream.NewStream(ctx, 64)
	go streamSSE(ctx, st, pr, pr, cfg, cfg.FirstChunkTimeout)

	pw.Write([]byte("data: hello\n\n"))
	cancel()

	<-st.Done()
	state := st.State()
	if state != stream.StateCancelled && state != stream.StateDone {
		t.Errorf("expected Cancelled or Done, got %v", state)
	}
	pw.Close()
}

func TestStreamSSE_FirstChunkTimeout(t *testing.T) {
	pr, _ := io.Pipe()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := StreamingConfig{
		FirstChunkTimeout: 50 * time.Millisecond,
		StallTimeout:      0,
		DisconnectGrace:   0,
	}

	st := stream.NewStream(ctx, 64)
	go streamSSE(ctx, st, pr, pr, cfg, cfg.FirstChunkTimeout)

	<-st.Done()
}

func TestStreamSSE_AllFourGuards(t *testing.T) {
	input := "data: first\n\ndata: second\n\ndata: [DONE]\n\n"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := StreamingConfig{
		FirstChunkTimeout: 200 * time.Second,
		StallTimeout:      6 * time.Minute,
		DisconnectGrace:   500 * time.Millisecond,
	}

	st := stream.NewStream(ctx, 64)
	go streamSSE(ctx, st, strings.NewReader(input), &nopCloser{}, cfg, cfg.FirstChunkTimeout)

	var chunks []stream.Chunk
	for ch := range st.Chunks() {
		chunks = append(chunks, ch)
	}

	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks (2 data + 1 final), got %d", len(chunks))
	}
	if string(chunks[0].Data) != "first" {
		t.Errorf("chunk[0] = %q, want %q", string(chunks[0].Data), "first")
	}
	if string(chunks[1].Data) != "second" {
		t.Errorf("chunk[1] = %q, want %q", string(chunks[1].Data), "second")
	}
	if !chunks[2].IsFinal {
		t.Error("chunk[2] should be IsFinal=true")
	}
}

func TestStreamSSE_EventPassthrough(t *testing.T) {
	input := "event: completion\ndata: {\"text\":\"hello\"}\n\ndata: [DONE]\n\n"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	st := stream.NewStream(ctx, 64)
	go streamSSE(ctx, st, strings.NewReader(input), &nopCloser{},
		DefaultStreamingConfig(), DefaultStreamingConfig().FirstChunkTimeout)

	var chunks []stream.Chunk
	for ch := range st.Chunks() {
		chunks = append(chunks, ch)
	}

	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if chunks[0].Event != "completion" {
		t.Errorf("chunk[0].Event = %q, want %q", chunks[0].Event, "completion")
	}
}

func TestStreamSSE_Stress(t *testing.T) {
	pr, pw := io.Pipe()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg := StreamingConfig{
		FirstChunkTimeout: 200 * time.Second,
		StallTimeout:      0,
		DisconnectGrace:   0,
	}

	st := stream.NewStream(ctx, 64)
	go streamSSE(ctx, st, pr, pr, cfg, cfg.FirstChunkTimeout)

	go func() {
		for i := 0; i < 1000; i++ {
			fmt.Fprintf(pw, "data: line-%d\n\n", i)
		}
		fmt.Fprintf(pw, "data: [DONE]\n\n")
		pw.Close()
	}()

	var count int
	for ch := range st.Chunks() {
		if ch.IsFinal {
			break
		}
		count++
	}

	if count != 1000 {
		t.Errorf("expected 1000 data chunks, got %d", count)
	}
}
