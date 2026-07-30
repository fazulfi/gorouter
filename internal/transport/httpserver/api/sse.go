package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/rs/zerolog"

	"gorouter/internal/domain/engine/stream"
)

func writeSSEStream(w http.ResponseWriter, r *http.Request, st *stream.Stream, keepalive time.Duration, disconnectGrace time.Duration, log zerolog.Logger) error {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("response writer does not support flushing")
	}

	flusher.Flush()

	firstChunk, err := st.Peek(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("sse stream peek failed")
		// Sanitize: do not leak internal stream details to the client.
		writeJSONError(w, r, http.StatusInternalServerError, "stream error")
		return err
	}

	if firstChunk.Error != nil {
		log.Error().Err(firstChunk.Error).Msg("sse stream first chunk error")
		// Sanitize: use the safe error contract — never echo upstream errors
		// verbatim as they may contain credential fragments or internal URLs.
		writeJSONError(w, r, http.StatusInternalServerError, "stream error")
		return firstChunk.Error
	}

	st.WithKeepalive(keepalive)

	remaining := st.Chunks()

	if err := writeChunkAsSSE(w, flusher, firstChunk); err != nil {
		st.Cancel(err)
		return err
	}

	for {
		select {
		case <-r.Context().Done():
			// Client disconnect grace: drain any buffered chunks within
			// the grace window before tearing down the stream.
			select {
			case chunk, ok := <-remaining:
				if ok && len(chunk.Data) > 0 {
					_ = writeChunkAsSSE(w, flusher, &chunk)
				}
			case <-time.After(disconnectGrace):
			}
			st.Cancel(r.Context().Err())
			return r.Context().Err()
		case chunk, ok := <-remaining:
			if !ok {
				return nil
			}

			if chunk.Event == "keepalive" {
				_, err := fmt.Fprintf(w, ": keepalive\n\n")
				if err != nil {
					st.Cancel(err)
					return err
				}
				flusher.Flush()
				continue
			}

			if chunk.IsFinal && chunk.Error != nil {
				log.Warn().Err(chunk.Error).Msg("sse stream chunk error")
				// Sanitize: use a safe terminal message; never leak upstream
				// error details that may contain credentials or internal paths.
				writeSSEEvent(w, "error", "upstream_error")
				flusher.Flush()
				return chunk.Error
			}

			if chunk.IsFinal {
				writeSSEEvent(w, "done", "[DONE]")
				flusher.Flush()
				return nil
			}

			if err := writeChunkAsSSE(w, flusher, &chunk); err != nil {
				st.Cancel(err)
				return err
			}
		}
	}
}

func writeSSEEvent(w http.ResponseWriter, event, data string) {
	if event != "" {
		fmt.Fprintf(w, "event: %s\n", event)
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
}

func writeChunkAsSSE(w http.ResponseWriter, flusher http.Flusher, chunk *stream.Chunk) error {
	if chunk == nil {
		return nil
	}
	if len(chunk.Data) > 0 {
		writeSSEEvent(w, "chunk", string(chunk.Data))
	}
	flusher.Flush()
	return nil
}
