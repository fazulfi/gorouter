package middleware

import (
	"net/http"
	"strconv"
	"strings"
)

type CORSConfig struct {
	AllowedOrigins   []string
	AllowedMethods   []string
	AllowedHeaders   []string
	ExposedHeaders   []string
	MaxAge           int
	AllowCredentials bool
}

var defaultModelHeaders = []string{
	"Content-Type",
	"Authorization",
	"X-Request-ID",
	"X-Correlation-ID",
	"X-Trace-ID",
	"OpenAI-Provider",
	"OpenAI-Project",
	"OpenAI-Organization",
}

var defaultModelExposeHeaders = []string{
	"X-Request-ID",
	"X-RateLimit-Limit",
	"X-RateLimit-Remaining",
}

var defaultModelMethods = []string{
	http.MethodGet,
	http.MethodPost,
	http.MethodPut,
	http.MethodDelete,
	http.MethodPatch,
	http.MethodOptions,
	http.MethodHead,
}

var defaultAdminMethods = []string{
	http.MethodGet,
	http.MethodPost,
	http.MethodPut,
	http.MethodDelete,
	http.MethodPatch,
}

var defaultAdminHeaders = []string{
	"Content-Type",
	"Authorization",
	"X-Request-ID",
	"X-CSRF-Token",
}

func isLocalhostOrigin(origin string) bool {
	return strings.HasPrefix(origin, "http://localhost") ||
		strings.HasPrefix(origin, "http://127.0.0.1") ||
		strings.HasPrefix(origin, "http://[::1]")
}

func ModelCORS() func(http.Handler) http.Handler {
	return corsMiddleware(true, CORSConfig{
		AllowedHeaders: defaultModelHeaders,
		ExposedHeaders: defaultModelExposeHeaders,
		AllowedMethods: defaultModelMethods,
	})
}

func AdminCORS(cfg CORSConfig) func(http.Handler) http.Handler {
	useLocalhostDefault := len(cfg.AllowedOrigins) == 0
	if len(cfg.AllowedMethods) == 0 {
		cfg.AllowedMethods = defaultAdminMethods
	}
	if len(cfg.AllowedHeaders) == 0 {
		cfg.AllowedHeaders = defaultAdminHeaders
	}
	return adminCORSMiddleware(useLocalhostDefault, cfg)
}

func adminCORSMiddleware(useLocalhostDefault bool, cfg CORSConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			var allowed bool
			if useLocalhostDefault {
				allowed = isLocalhostOrigin(origin)
			} else {
				allowed = isOriginAllowed(origin, cfg.AllowedOrigins)
			}
			if !allowed {
				next.ServeHTTP(w, r)
				return
			}

			w.Header().Set("Access-Control-Allow-Origin", origin)
			if cfg.AllowCredentials {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
			w.Header().Set("Vary", "Origin")

			if len(cfg.ExposedHeaders) > 0 {
				w.Header().Set("Access-Control-Expose-Headers",
					strings.Join(cfg.ExposedHeaders, ", "))
			}

			if r.Method == http.MethodOptions &&
				r.Header.Get("Access-Control-Request-Method") != "" {
				w.Header().Set("Access-Control-Allow-Methods",
					strings.Join(cfg.AllowedMethods, ", "))
				w.Header().Set("Access-Control-Allow-Headers",
					strings.Join(cfg.AllowedHeaders, ", "))
				if cfg.MaxAge > 0 {
					w.Header().Set("Access-Control-Max-Age",
						strconv.Itoa(cfg.MaxAge))
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func corsMiddleware(allowAll bool, cfg CORSConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			if allowAll {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			} else {
				if !isOriginAllowed(origin, cfg.AllowedOrigins) {
					next.ServeHTTP(w, r)
					return
				}
				w.Header().Set("Access-Control-Allow-Origin", origin)
				if cfg.AllowCredentials {
					w.Header().Set("Access-Control-Allow-Credentials", "true")
				}
			}

			w.Header().Set("Vary", "Origin")

			if len(cfg.ExposedHeaders) > 0 {
				w.Header().Set("Access-Control-Expose-Headers",
					strings.Join(cfg.ExposedHeaders, ", "))
			}

			if r.Method == http.MethodOptions &&
				r.Header.Get("Access-Control-Request-Method") != "" {
				w.Header().Set("Access-Control-Allow-Methods",
					strings.Join(cfg.AllowedMethods, ", "))
				w.Header().Set("Access-Control-Allow-Headers",
					strings.Join(cfg.AllowedHeaders, ", "))
				if cfg.MaxAge > 0 {
					w.Header().Set("Access-Control-Max-Age",
						strconv.Itoa(cfg.MaxAge))
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func isOriginAllowed(origin string, allowed []string) bool {
	for _, a := range allowed {
		if a == origin {
			return true
		}
	}
	return false
}
