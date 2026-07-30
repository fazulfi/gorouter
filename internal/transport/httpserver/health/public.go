// Package health provides HTTP health check endpoints.
//
// The public endpoint (GET /health) returns a minimal 200 OK liveness probe
// with no sensitive information. The detailed endpoint
// (GET /health/detailed) returns system information and requires
// authentication.
package health

import (
	"encoding/json"
	"net/http"
	"time"
)

// PublicHandler returns a simple liveness check handler.
//
//	GET /health -> 200 {"status": "ok", "timestamp": "..."}
//
// This endpoint never exposes version, uptime, database status, or any other
// sensitive information. It always returns 200 even when the database is
// unavailable — it is a liveness probe only.
func PublicHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		resp := map[string]string{
			"status":    "ok",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		}
		_ = json.NewEncoder(w).Encode(resp)
	}
}
