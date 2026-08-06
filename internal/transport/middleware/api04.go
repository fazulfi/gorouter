package middleware

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"strings"

	"gorouter/internal/domain/auth"
)

// nonceCtxKey is the request-context key under which SecurityHeaders records
// the per-response Content-Security-Policy nonce. Downstream embed handlers
// (API-11) read it via NonceFromContext to author inline script/style nonces.
const nonceCtxKey contextKey = "nonce"

// NonceFromContext returns the CSP nonce recorded by SecurityHeaders for the
// current request, or "" when no nonce was generated.
func NonceFromContext(ctx context.Context) string {
	v, _ := ctx.Value(nonceCtxKey).(string)
	return v
}

type HostConfig struct {
	Public       bool
	AllowedHosts []string
}

func HostCheck(cfg HostConfig) func(http.Handler) http.Handler {
	allowed := map[string]bool{}
	for _, h := range cfg.AllowedHosts {
		allowed[strings.ToLower(strings.TrimSpace(h))] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cfg.Public && !allowed[strings.ToLower(r.Host)] {
				http.Error(w, "invalid host", http.StatusMisdirectedRequest)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func SecurityHeaders() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b := make([]byte, 18)
			_, _ = rand.Read(b)
			nonce := base64.RawURLEncoding.EncodeToString(b)
			w.Header().Set("Content-Security-Policy", fmt.Sprintf("default-src 'self'; script-src 'self' 'nonce-%s'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'", nonce))
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Referrer-Policy", "same-origin")
			w.Header().Set("Permissions-Policy", "()")
			// HSTS only when the request arrived over HTTPS: TLS terminated at
			// this server, or a trusted proxy signalling HTTPS via
			// X-Forwarded-Proto. Never emit for plain HTTP.
			if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
				w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), nonceCtxKey, nonce)))
		})
	}
}

func StampActor(kind auth.ActorKind) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			actor := &auth.Actor{Kind: kind, Origin: auth.ActorOriginRemote, UserAgent: r.UserAgent()}
			actor.IP = net.ParseIP(realIP(r))
			next.ServeHTTP(w, r.WithContext(auth.ContextWithActor(r.Context(), actor)))
		})
	}
}

func realIP(r *http.Request) string {
	if v := GetRealIP(r.Context()); v != "" {
		return v
	}
	return strings.Split(r.RemoteAddr, ":")[0]
}
