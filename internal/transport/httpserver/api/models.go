package api

import (
	"encoding/json"
	"net/http"
	"time"
)

type modelEntry struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

var builtInModels = []modelEntry{
	{ID: "gpt-4", Object: "model", Created: time.Date(2023, 3, 14, 0, 0, 0, 0, time.UTC).Unix(), OwnedBy: "openai"},
	{ID: "gpt-4-turbo", Object: "model", Created: time.Date(2023, 11, 6, 0, 0, 0, 0, time.UTC).Unix(), OwnedBy: "openai"},
	{ID: "gpt-3.5-turbo", Object: "model", Created: time.Date(2022, 11, 30, 0, 0, 0, 0, time.UTC).Unix(), OwnedBy: "openai"},
	{ID: "claude-3-opus", Object: "model", Created: time.Date(2024, 3, 4, 0, 0, 0, 0, time.UTC).Unix(), OwnedBy: "anthropic"},
	{ID: "claude-3-sonnet", Object: "model", Created: time.Date(2024, 3, 4, 0, 0, 0, 0, time.UTC).Unix(), OwnedBy: "anthropic"},
	{ID: "claude-3-haiku", Object: "model", Created: time.Date(2024, 3, 4, 0, 0, 0, 0, time.UTC).Unix(), OwnedBy: "anthropic"},
}

func (h *Handler) HandleListModels(w http.ResponseWriter, r *http.Request) {
	resp := map[string]interface{}{
		"object": "list",
		"data":   builtInModels,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}
