package middleware

import (
	"context"
	"net"
	"net/http"
	"strings"

	"gorouter/internal/domain/auth"
	"gorouter/internal/domain/keys"
)

func SessionAuth(validate func(context.Context, string) (*auth.Actor, error), cookieName string) func(http.Handler) http.Handler {
	if cookieName == "" {
		cookieName = "gorouter_session"
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, e := r.Cookie(cookieName)
			if e != nil {
				http.Error(w, "unauthorized", 401)
				return
			}
			a, e := validate(r.Context(), c.Value)
			if e != nil || a == nil {
				http.Error(w, "unauthorized", 401)
				return
			}
			a.Kind = auth.ActorKindSession
			a.Origin = auth.ActorOriginRemote
			a.UserAgent = r.UserAgent()
			a.IP = net.ParseIP(realIP(r))
			next.ServeHTTP(w, r.WithContext(auth.ContextWithActor(r.Context(), a)))
		})
	}
}

func PATAuth(validate func(context.Context, string) (*keys.PAT, error)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			v := r.Header.Get("Authorization")
			if !strings.HasPrefix(v, "Bearer ") {
				http.Error(w, "unauthorized", 401)
				return
			}
			token := strings.TrimSpace(strings.TrimPrefix(v, "Bearer "))
			if token == "" {
				http.Error(w, "unauthorized", 401)
				return
			}
			p, e := validate(r.Context(), token)
			if e != nil || p == nil {
				http.Error(w, "unauthorized", 401)
				return
			}
			a := &auth.Actor{
				UserID:    p.UserID,
				Kind:      auth.ActorKindPAT,
				Origin:    auth.ActorOriginRemote,
				UserAgent: r.UserAgent(),
				IP:        net.ParseIP(realIP(r)),
			}
			next.ServeHTTP(w, r.WithContext(auth.ContextWithActor(r.Context(), a)))
		})
	}
}
