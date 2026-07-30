package api

import (
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"

	"gorouter/internal/domain/engine"
	"gorouter/internal/domain/engine/stream"
)

func (h *Handler) HandleChatCompletions(w http.ResponseWriter, r *http.Request) {
	log := h.logger.With().Str("handler", "chat_completions").Logger()

	// Strip untrusted headers that could influence upstream routing.
	stripUntrustedHeaders(r)

	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Warn().Err(err).Msg("failed to read request body")
		writeJSONError(w, r, http.StatusBadRequest, "failed to read request body")
		return
	}
	defer r.Body.Close()

	req, err := h.translateSvc.TranslateChatToRequest(r.Context(), body)
	if err != nil {
		log.Warn().Err(err).Msg("invalid chat request")
		writeJSONError(w, r, http.StatusBadRequest, "request body invalid")
		return
	}

	req.ID = uuid.New()
	req.Format = engine.FormatOpenAIChat
	req.RawBody = body
	setUserIDFromContext(req, r)

	logUpdated := log.With().Str("model", req.Model).Bool("stream", req.Stream).Logger()
	logUpdated.Info().Str("request_id", req.ID.String()).Msg("executing chat request")

	resp, err := h.orchestrator.ExecuteRequest(r.Context(), req)
	if err != nil {
		logUpdated.Error().Err(err).Msg("orchestrator execution failed")
		writeJSONError(w, r, http.StatusInternalServerError, "request execution failed")
		return
	}

	if resp.Stream != nil {
		st, ok := resp.Stream.(*stream.Stream)
		if !ok {
			logUpdated.Error().Msg("response stream is not a *stream.Stream")
			writeJSONError(w, r, http.StatusInternalServerError, "internal error")
			return
		}
		if err := writeSSEStream(w, r, st, h.config.StreamKeepalive, 500*time.Millisecond, logUpdated); err != nil {
			logUpdated.Error().Err(err).Msg("sse stream write failed")
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if resp.StatusCode > 0 {
		w.WriteHeader(resp.StatusCode)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	if len(resp.Body) > 0 {
		_, _ = w.Write(resp.Body)
	}
}
