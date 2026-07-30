package api

import (
	"io"
	"net/http"

	"github.com/google/uuid"

	"gorouter/internal/domain/engine"
	"gorouter/internal/domain/engine/stream"
)

func (h *Handler) HandleCodexResponses(w http.ResponseWriter, r *http.Request) {
	log := h.logger.With().Str("handler", "codex_responses").Logger()

	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Warn().Err(err).Msg("failed to read request body")
		writeJSONError(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	defer r.Body.Close()

	req, err := h.translateSvc.TranslateCodexToRequest(r.Context(), body)
	if err != nil {
		log.Warn().Err(err).Msg("invalid codex request")
		writeJSONError(w, http.StatusBadRequest, "request body invalid")
		return
	}

	req.ID = uuid.New()
	req.Format = engine.FormatCodexResponses
	req.RawBody = body
	setUserIDFromContext(req, r)

	logUpdated := log.With().Str("model", req.Model).Bool("stream", req.Stream).Logger()
	logUpdated.Info().Str("request_id", req.ID.String()).Msg("executing codex request")

	resp, err := h.orchestrator.ExecuteRequest(r.Context(), req)
	if err != nil {
		logUpdated.Error().Err(err).Msg("orchestrator execution failed")
		writeJSONError(w, http.StatusInternalServerError, "request execution failed")
		return
	}

	if resp.Stream != nil {
		st, ok := resp.Stream.(*stream.Stream)
		if !ok {
			logUpdated.Error().Msg("response stream is not a *stream.Stream")
			writeJSONError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if err := writeSSEStream(w, r, st, h.config.StreamKeepalive, logUpdated); err != nil {
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
