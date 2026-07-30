package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"gorouter/internal/app/translate"
	"gorouter/internal/domain/auth"
	"gorouter/internal/domain/engine"
)

type Config struct {
	DefaultModel    string
	StreamKeepalive time.Duration
}

func DefaultConfig() Config {
	return Config{
		DefaultModel:    "gpt-4",
		StreamKeepalive: 15 * time.Second,
	}
}

type Handler struct {
	config       Config
	orchestrator engine.Orchestrator
	translateSvc *translate.Service
	logger       zerolog.Logger
}

const maxBodySize = 1 << 20 // 1 MB

func New(cfg Config, orch engine.Orchestrator, translateSvc *translate.Service, logger zerolog.Logger) *Handler {
	if cfg.StreamKeepalive <= 0 {
		cfg.StreamKeepalive = 15 * time.Second
	}
	if cfg.DefaultModel == "" {
		cfg.DefaultModel = "gpt-4"
	}
	return &Handler{
		config:       cfg,
		orchestrator: orch,
		translateSvc: translateSvc,
		logger:       logger.With().Str("component", "api_handler").Logger(),
	}
}

func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/v1/chat/completions", h.HandleChatCompletions)
	r.Post("/v1/responses", h.HandleCodexResponses)
	r.Get("/v1/models", h.HandleListModels)
}

func setUserIDFromContext(req *engine.Request, r *http.Request) {
	if actor, ok := auth.FromContext(r.Context()); ok {
		req.UserID = &actor.UserID
	}
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	safe := message
	if status >= 500 {
		safe = http.StatusText(status)
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{
			"message": safe,
			"type":    errorTypeFromStatus(status),
		},
	})
}

func errorTypeFromStatus(status int) string {
	if status >= 500 {
		return "internal_error"
	}
	return "invalid_request_error"
}
