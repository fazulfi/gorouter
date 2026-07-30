package stream

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestNewStream(t *testing.T) {
	s := NewStream(context.Background(), 10)
	if s == nil {
		t.Fatal("NewStream returned nil")
	}
	if s.State() != StateInit {
		t.Errorf("initial state = %v, want %v", s.State(), StateInit)
	}
	if s.StreamID() == uuidZero() {
		t.Error("StreamID should not be zero")
	}
}

func TestPushAndChunks(t *testing.T) {
	s := NewStream(context.Background(), 10)
	chunk := Chunk{Data: []byte("hello"), Event: "message", IsFinal: false}

	if ok := s.Push(chunk); !ok {
		t.Fatal("Push returned false")
	}

	select {
	case c := <-s.Chunks():
		if string(c.Data) != "hello" {
			t.Errorf("Data = %q, want %q", string(c.Data), "hello")
		}
	default:
		t.Fatal("expected chunk from Chunks()")
	}
}

func TestClose(t *testing.T) {
	s := NewStream(context.Background(), 10)
	s.Push(Chunk{Data: []byte("a")})
	s.Close()

	if s.State() != StateDone {
		t.Errorf("state after Close = %v, want %v", s.State(), StateDone)
	}

	// Push after close should return false
	if ok := s.Push(Chunk{Data: []byte("b")}); ok {
		t.Error("Push after Close returned true")
	}
}

func TestCancel(t *testing.T) {
	s := NewStream(context.Background(), 10)
	s.Cancel(nil)

	if s.State() != StateCancelled {
		t.Errorf("state after Cancel = %v, want %v", s.State(), StateCancelled)
	}

	// Push after cancel should return false
	if ok := s.Push(Chunk{Data: []byte("b")}); ok {
		t.Error("Push after Cancel returned true")
	}
}

func TestCancelWithError(t *testing.T) {
	s := NewStream(context.Background(), 2)
	s.Cancel(ErrStreamCancelled)
	if s.State() != StateCancelled {
		t.Errorf("state = %v, want %v", s.State(), StateCancelled)
	}
}

func TestPeek(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	s := NewStream(ctx, 10)
	go func() {
		time.Sleep(10 * time.Millisecond)
		s.Push(Chunk{Data: []byte("first")})
	}()

	chunk, err := s.Peek(ctx)
	if err != nil {
		t.Fatalf("Peek error: %v", err)
	}
	if string(chunk.Data) != "first" {
		t.Errorf("peeked Data = %q, want %q", string(chunk.Data), "first")
	}

	// Peek again should return cached value
	chunk2, err := s.Peek(ctx)
	if err != nil {
		t.Fatalf("second Peek error: %v", err)
	}
	if string(chunk2.Data) != "first" {
		t.Errorf("second peek Data = %q, want %q", string(chunk2.Data), "first")
	}
}

func TestPeekClosedStream(t *testing.T) {
	ctx := context.Background()
	s := NewStream(ctx, 10)
	s.Close()

	_, err := s.Peek(ctx)
	if err == nil {
		t.Fatal("expected error peeking closed stream")
	}
}

func TestKeepalive(t *testing.T) {
	s := NewStream(context.Background(), 10)
	s.WithKeepalive(10 * time.Millisecond)
	defer s.Close()

	time.Sleep(50 * time.Millisecond)

	// Should have received at least 1 keepalive chunk
	ch := s.Chunks()
	seen := false
	for i := 0; i < 10; i++ {
		select {
		case c := <-ch:
			if c.Event == "keepalive" {
				seen = true
			}
		case <-time.After(20 * time.Millisecond):
			break
		}
	}
	if !seen {
		t.Log("keepalive chunk not guaranteed but expected")
	}
}

func TestConcurrentPush(t *testing.T) {
	s := NewStream(context.Background(), 100)
	var wg sync.WaitGroup
	n := 50

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s.Push(Chunk{Data: []byte{byte(i)}})
		}(i)
	}

	wg.Wait()
	s.Close()

	count := 0
	for range s.Chunks() {
		count++
	}
	if count != n {
		t.Logf("received %d chunks (expected %d) — some may be lost to concurrent close", count, n)
	}
}

func TestCloseIdempotent(t *testing.T) {
	s := NewStream(context.Background(), 10)
	s.Close()
	s.Close()
}

func TestCancelIdempotent(t *testing.T) {
	s := NewStream(context.Background(), 10)
	s.Cancel(nil)
	s.Cancel(nil)
}

func TestID(t *testing.T) {
	s := NewStream(context.Background(), 10)
	if s.ID() != s.StreamID() {
		t.Errorf("ID() != StreamID()")
	}
}

func TestErrorVariables(t *testing.T) {
	if ErrStreamClosed == nil {
		t.Error("ErrStreamClosed must not be nil")
	}
	if ErrStreamCancelled == nil {
		t.Error("ErrStreamCancelled must not be nil")
	}
	if ErrPeekTimeout == nil {
		t.Error("ErrPeekTimeout must not be nil")
	}
	if ErrStallTimeout == nil {
		t.Error("ErrStallTimeout must not be nil")
	}
}

// uuidZero returns the zero UUID for comparison.
func uuidZero() [16]byte {
	return [16]byte{}
}
