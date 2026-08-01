// Package api provides HTTP handlers for the gorouter model API.
// Authentication is enforced by the middleware layer; handlers in this
// package assume a valid auth.Actor is present in the request context.
package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"gorouter/internal/domain/auth"
	"gorouter/internal/domain/engine"
	"gorouter/internal/shared"
)

// Config holds configuration for the API handler.
type Config struct {
	DefaultModel    string
	StreamKeepalive time.Duration
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		DefaultModel:    "gpt-4",
		StreamKeepalive: 15 * time.Second,
	}
}

// Handler is the HTTP handler for model API endpoints.
// Authentication is enforced externally via middleware.ModelKeyAuth.
type Handler struct {
	config       Config
	orchestrator engine.Orchestrator
	logger       zerolog.Logger
}

const maxBodySize = 1 << 20

// New creates an API Handler with the given dependencies.
func New(cfg Config, orch engine.Orchestrator, logger zerolog.Logger) *Handler {
	if cfg.StreamKeepalive <= 0 {
		cfg.StreamKeepalive = 15 * time.Second
	}
	if cfg.DefaultModel == "" {
		cfg.DefaultModel = "gpt-4"
	}
	return &Handler{
		config:       cfg,
		orchestrator: orch,
		logger:       logger.With().Str("component", "api_handler").Logger(),
	}
}

// RegisterRoutes registers all model API and legacy compatibility routes.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/v1/chat/completions", h.HandleChatCompletions)
	r.Post("/v1/responses", h.HandleCodexResponses)
	r.Get("/v1/models", h.HandleListModels)

	// Legacy compatibility paths preserved permanently per PARITY-LEDGER §2
	// row 26 and DECISIONS #110.
	r.Post("/v1/v1/chat/completions", h.HandleChatCompletions)
	r.Post("/v1/v1/responses", h.HandleCodexResponses)
	r.Post("/responses", h.HandleCodexResponses)
	r.Get("/codex/models", h.HandleListModels)
	r.Post("/codex/chat/completions", h.HandleChatCompletions)
	r.Post("/codex/responses", h.HandleCodexResponses)
}

func stripUntrustedHeaders(r *http.Request) {
	r.Header.Del("X-Base-URL")
	r.Header.Del("X-Base-Url")
	r.Header.Del("x-base-url")
}

func setUserIDFromContext(req *engine.Request, r *http.Request) {
	if actor, ok := auth.FromContext(r.Context()); ok {
		req.UserID = &actor.UserID
	}
}

func writeJSONError(w http.ResponseWriter, r *http.Request, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	safe := message
	if status >= 500 {
		safe = http.StatusText(status)
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{
			"type":       errorTypeFromStatus(status),
			"code":       errorCodeFromStatus(status),
			"message":    safe,
			"request_id": getRequestID(r),
			"retryable":  status >= 500,
		},
	})
}

func getRequestID(r *http.Request) string {
	if id := r.Header.Get("X-Request-ID"); id != "" {
		return id
	}
	if id := r.Header.Get("X-Correlation-ID"); id != "" {
		return id
	}
	type ctxKey string
	const key ctxKey = "request_id"
	if v, ok := r.Context().Value(key).(string); ok {
		return v
	}
	return ""
}

func errorTypeFromStatus(status int) string {
	if status >= 500 {
		return "internal_error"
	}
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "authentication_error"
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	default:
		return "invalid_request_error"
	}
}

func errorCodeFromStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return string(shared.ErrValidation)
	case http.StatusUnauthorized:
		return string(shared.ErrUnauthorized)
	case http.StatusForbidden:
		return string(shared.ErrForbidden)
	case http.StatusNotFound:
		return string(shared.ErrNotFound)
	case http.StatusTooManyRequests:
		return string(shared.ErrRateLimited)
	case http.StatusGatewayTimeout:
		return string(shared.ErrTimeout)
	default:
		return string(shared.ErrInternal)
	}
}
