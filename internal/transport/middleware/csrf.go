package middleware

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
)

const (
	csrfCookieName = "gorouter_csrf"
	csrfHeaderName = "X-CSRF-Token"
)

var safeMethods = map[string]bool{
	http.MethodGet:     true,
	http.MethodHead:    true,
	http.MethodOptions: true,
	http.MethodTrace:   true,
}

// CSRFConfig holds optional configuration for the CSRF middleware.
type CSRFConfig struct {
	// CookieName overrides the default CSRF cookie name.
	CookieName string
	// HeaderName overrides the default CSRF header name.
	HeaderName string
	// Secure sets the Secure flag on the CSRF cookie.
	Secure bool
}

func generateCSRFToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// CSRF returns a double-submit cookie CSRF middleware.
// Safe methods (GET, HEAD, OPTIONS, TRACE) set the CSRF cookie.
// Mutating methods require the cookie value to be echoed in the configured header.
func CSRF(cfg CSRFConfig) func(http.Handler) http.Handler {
	cookieName := cfg.CookieName
	if cookieName == "" {
		cookieName = csrfCookieName
	}
	headerName := cfg.HeaderName
	if headerName == "" {
		headerName = csrfHeaderName
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if safeMethods[r.Method] {
				needSet := true
				if c, err := r.Cookie(cookieName); err == nil && c.Value != "" {
					needSet = false
				}
				if needSet {
					token, err := generateCSRFToken()
					if err != nil {
						http.Error(w, "internal error", http.StatusInternalServerError)
						return
					}
					http.SetCookie(w, &http.Cookie{
						Name:     cookieName,
						Value:    token,
						Path:     "/",
						Secure:   cfg.Secure,
						HttpOnly: false,
						SameSite: http.SameSiteLaxMode,
					})
				}
				next.ServeHTTP(w, r)
				return
			}

			cookie, err := r.Cookie(cookieName)
			if err != nil || cookie.Value == "" {
				writeCSRFError(w, "missing CSRF cookie")
				return
			}

			headerVal := r.Header.Get(headerName)
			if headerVal == "" || subtle.ConstantTimeCompare([]byte(headerVal), []byte(cookie.Value)) != 1 {
				writeCSRFError(w, "invalid CSRF token")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func writeCSRFError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":   "csrf_error",
		"message": msg,
	})
}
