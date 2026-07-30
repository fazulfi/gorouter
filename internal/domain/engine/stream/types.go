// Package stream provides a thread-safe, peekable SSE stream type used by the
// engine layer to relay LLM provider responses chunk-by-chunk to the transport layer.
package stream

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// State represents the lifecycle phase of a stream.
type State int

const (
	StateInit      State = iota // stream created, not yet started
	StateRunning                // stream actively receiving chunks
	StatePeeked                 // first chunk has been peeked
	StateDone                   // stream completed normally
	StateErrored                // stream terminated with an error
	StateCancelled              // stream cancelled by the caller
)

// Chunk is a single SSE data frame (or error) received from an upstream provider.
type Chunk struct {
	Data    []byte
	Event   string
	IsFinal bool
	Error   error
}

// Stream provides a thread-safe, peekable channel for consuming SSE data chunks.
// It supports first-chunk peek (for early error detection), keepalive tickers,
// and cancellation propagation.
type Stream struct {
	id         uuid.UUID
	mu         sync.Mutex
	state      State
	chunks     chan Chunk
	done       chan struct{}
	cancel     context.CancelFunc
	firstChunk *Chunk
	peeked     bool
	createdAt  time.Time
	keepalive  *time.Ticker
}

// NewStream creates a new Stream with the given channel buffer size.
// The provided context is used for cancellation propagation.
func NewStream(ctx context.Context, bufferSize int) *Stream {
	ctx, cancel := context.WithCancel(ctx)
	return &Stream{
		id:        uuid.New(),
		state:     StateInit,
		chunks:    make(chan Chunk, bufferSize),
		done:      make(chan struct{}),
		cancel:    cancel,
		createdAt: time.Now(),
	}
}

// Push writes a chunk to the stream channel. Returns false if the stream has
// been closed, errored, or cancelled.
func (s *Stream) Push(chunk Chunk) bool {
	s.mu.Lock()
	if s.state == StateDone || s.state == StateErrored || s.state == StateCancelled {
		s.mu.Unlock()
		return false
	}
	s.mu.Unlock()

	defer func() {
		recover() //nolint:errcheck // guard against concurrent Close
	}()

	s.chunks <- chunk
	return true
}

// Chunks returns a read-only channel of chunks for the consumer to range over.
func (s *Stream) Chunks() <-chan Chunk {
	return s.chunks
}

// Peek reads the first chunk without consuming it from the main channel.
// Subsequent calls return the same cached chunk. Returns an error if the
// context expires or the stream closes before a chunk arrives.
func (s *Stream) Peek(ctx context.Context) (*Chunk, error) {
	s.mu.Lock()
	if s.peeked && s.firstChunk != nil {
		s.mu.Unlock()
		return s.firstChunk, nil
	}
	s.mu.Unlock()

	select {
	case chunk, ok := <-s.chunks:
		if !ok {
			return nil, ErrStreamClosed
		}
		s.mu.Lock()
		s.firstChunk = &chunk
		s.peeked = true
		if s.state == StateInit || s.state == StateRunning {
			s.state = StatePeeked
		}
		s.mu.Unlock()
		return &chunk, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.done:
		return nil, ErrStreamClosed
	}
}

// State returns the current lifecycle state of the stream.
func (s *Stream) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// Close marks the stream as successfully completed and signals consumers
// that no more chunks will arrive.
func (s *Stream) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == StateDone || s.state == StateErrored || s.state == StateCancelled {
		return
	}
	s.state = StateDone
	close(s.done)
	close(s.chunks)
	if s.keepalive != nil {
		s.keepalive.Stop()
	}
}

// Cancel terminates the stream with the given error, propagating the cancellation
// to all consumers. It is safe to call multiple times.
func (s *Stream) Cancel(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == StateDone || s.state == StateErrored || s.state == StateCancelled {
		return
	}
	s.state = StateCancelled
	if err != nil {
		// Non-blocking send of the error chunk.
		select {
		case s.chunks <- Chunk{Error: err, IsFinal: true}:
		default:
		}
	}
	s.cancel()
	close(s.done)
	close(s.chunks)
	if s.keepalive != nil {
		s.keepalive.Stop()
	}
}

// WithKeepalive enables a periodic keepalive ticker that writes empty chunks
// at the given interval to prevent stream timeouts on the consumer side.
func (s *Stream) WithKeepalive(interval time.Duration) *Stream {
	s.mu.Lock()
	if s.keepalive != nil {
		s.keepalive.Stop()
	}
	s.keepalive = time.NewTicker(interval)
	s.mu.Unlock()

	go func() {
		for range s.keepalive.C {
			s.mu.Lock()
			if s.state == StateDone || s.state == StateErrored || s.state == StateCancelled {
				s.mu.Unlock()
				return
			}
			s.mu.Unlock()

			func() {
				defer func() {
					recover() //nolint:errcheck // guard against send on closed channel
				}()
				select {
				case s.chunks <- Chunk{Event: "keepalive"}:
				default:
				}
			}()
		}
	}()
	return s
}

// StreamID returns the stream's unique identifier. Implements the engine.StreamRef
// interface.
func (s *Stream) StreamID() uuid.UUID {
	return s.id
}

// ID returns the stream's unique identifier (alias for StreamID).
func (s *Stream) ID() uuid.UUID {
	return s.id
}

// Done returns a channel that is closed when the stream reaches a terminal
// state (Done, Errored, or Cancelled). Callers can use this to wait for stream
// completion without polling.
func (s *Stream) Done() <-chan struct{} {
	return s.done
}
