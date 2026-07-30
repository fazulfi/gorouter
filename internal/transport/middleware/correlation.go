package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

const requestIDKey contextKey = "request_id"

func GetRequestID(ctx context.Context) string {
	v, _ := ctx.Value(requestIDKey).(string)
	return v
}

func Correlation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = r.Header.Get("X-Correlation-ID")
		}
		if id == "" {
			id = r.Header.Get("X-Trace-ID")
		}
		if id == "" {
			id = generateID()
		}

		ctx := context.WithValue(r.Context(), requestIDKey, id)
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func generateID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
