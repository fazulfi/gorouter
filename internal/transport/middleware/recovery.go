package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"runtime/debug"

	"github.com/rs/zerolog/log"
)

type panicResponse struct {
	Error     string `json:"error"`
	RequestID string `json:"request_id"`
}

func Recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				reqID := GetRequestID(r.Context())
				if reqID == "" {
					reqID = r.Header.Get("X-Request-ID")
				}
				if reqID == "" {
					reqID = generateRecoveryID()
				}
				log.Error().
					Interface("panic", rec).
					Str("request_id", reqID).
					Bytes("stack", debug.Stack()).
					Msg("http handler panicked")

				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusInternalServerError)

				resp := panicResponse{
					Error:     "internal_server_error",
					RequestID: reqID,
				}
				_ = json.NewEncoder(w).Encode(resp)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func generateRecoveryID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
