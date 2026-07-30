package executor

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gorouter/internal/domain/engine/stream"
)

// StreamingConfig controls timeout behaviour for streaming SSE responses.
// Defaults match the upstream: 200s first-chunk, 6m stall, 500ms disconnect.
type StreamingConfig struct {
	FirstChunkTimeout time.Duration
	StallTimeout      time.Duration
	DisconnectGrace   time.Duration
}

// DefaultStreamingConfig returns a StreamingConfig with upstream-standard defaults.
func DefaultStreamingConfig() StreamingConfig {
	return StreamingConfig{
		FirstChunkTimeout: 200 * time.Second,
		StallTimeout:      6 * time.Minute,
		DisconnectGrace:   500 * time.Millisecond,
	}
}

// streamSSEBody reads SSE lines from reader, parses them, and pushes chunks
// into the given stream. It owns the closer: when the goroutine exits the
// closer is closed (safe to call multiple times). First-chunk timeout is
// applied when firstChunkTimeout > 0. Stall timeout resets on every
// successfully read byte. Both timeouts close the underlying closer to
// interrupt blocking reads.
//
// Terminal events ([DONE] or error) are delivered exactly once. The function
// returns when the reader is exhausted, the stream is cancelled, or a
// timeout fires.
func streamSSEBody(
	ctx context.Context,
	st *stream.Stream,
	reader io.Reader,
	closer io.Closer,
	cfg StreamingConfig,
	firstChunkTimeout time.Duration,
) {
	var closeOnce sync.Once
	closeCloser := func() { closeOnce.Do(func() { _ = closer.Close() }) }

	defer func() {
		closeCloser()
		if r := recover(); r != nil {
			st.Cancel(fmt.Errorf("panic in SSE streamer: %v", r))
		}
	}()

	// Track whether the first data chunk has been observed (used to
	// distinguish first-chunk timeout from stall timeout).
	var gotFirstChunk atomic.Bool

	// Monitor context cancellation to interrupt blocking reads.
	go func() {
		select {
		case <-ctx.Done():
			closeCloser()
		case <-st.Done():
		}
	}()

	// First-chunk timeout: if no data line arrives before the deadline,
	// close the closer to unblock the scanner.
	if firstChunkTimeout > 0 {
		go func() {
			timer := time.NewTimer(firstChunkTimeout)
			defer timer.Stop()
			select {
			case <-timer.C:
				if !gotFirstChunk.Load() {
					closeCloser()
				}
			case <-st.Done():
			}
		}()
	}

	// Stall monitor: uses atomic.Int64 to track the last byte time, avoiding
	// TOCTOU races inherent in timer.Reset patterns. The monitor goroutine
	// wakes every 1s and closes the reader if no byte has been received
	// within the stall timeout.
	var lastByteTime atomic.Int64
	if cfg.StallTimeout > 0 {
		lastByteTime.Store(time.Now().UnixNano())
		go func() {
			ticker := time.NewTicker(1 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					if gotFirstChunk.Load() {
						lbt := time.Unix(0, lastByteTime.Load())
						if time.Since(lbt) > cfg.StallTimeout {
							closeCloser()
							return
						}
					}
				case <-st.Done():
					return
				}
			}
		}()
	}

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)

	var eventType string

	for scanner.Scan() {
		// Update last-byte timestamp for stall monitor.
		if cfg.StallTimeout > 0 {
			lastByteTime.Store(time.Now().UnixNano())
		}

		line := scanner.Text()

		if line == "" {
			eventType = ""
			continue
		}

		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
			continue
		}

		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			gotFirstChunk.Store(true)

			if data == "[DONE]" {
				st.Push(stream.Chunk{IsFinal: true})
				st.Close()
				return
			}

			chunk := stream.Chunk{
				Data:  []byte(data),
				Event: eventType,
			}

			if !st.Push(chunk) {
				return
			}
			continue
		}

		// Ignore other SSE fields (id:, retry:, etc.)
	}

	// Determine why scanning stopped.
	err := scanner.Err()

	if err != nil || !gotFirstChunk.Load() {
		if !gotFirstChunk.Load() {
			st.Cancel(stream.ErrPeekTimeout)
			return
		}
		if err != nil {
			select {
			case <-ctx.Done():
				st.Cancel(ctx.Err())
			case <-st.Done():
			default:
				st.Cancel(fmt.Errorf("SSE scanner error: %w", err))
			}
			return
		}
	}

	// Normal EOF without [DONE] sentinel.
	st.Push(stream.Chunk{IsFinal: true})
	st.Close()
}

// copyChunkData returns a chunk whose Data field points to an independent
// byte slice. Use this when the source data buffer may be reused.
func copyChunkData(c stream.Chunk) stream.Chunk {
	data := make([]byte, len(c.Data))
	copy(data, c.Data)
	c.Data = data
	return c
}
