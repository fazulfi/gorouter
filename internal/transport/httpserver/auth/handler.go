package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	appauth "gorouter/internal/app/auth"
	domauth "gorouter/internal/domain/auth"
	"gorouter/internal/transport/middleware"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	sessionCookieName       = "gorouter_session"
	sessionCookieNameSecure = "__Host-gorouter_session"
	csrfCookieName          = "gorouter_csrf"
	lockoutKeyPrefix        = "login:lockout:"
)

// Config carries the transport-level cookie policy for the auth handlers.
// Secure selects the Secure attribute on issued cookies and the __Host-
// session cookie name (P2-10); loopback deployments disable it for HTTP.
type Config struct {
	Secure bool
}

// AuthService is the application-layer surface the handlers map onto. The
// concrete *appauth.Service satisfies it; fakes drive the transport tests.
type AuthService interface {
	Login(ctx context.Context, email, password string) (*domauth.Session, string, error)
	Logout(ctx context.Context, sessionID uuid.UUID) error
	ValidateSession(ctx context.Context, rawToken string) (*domauth.Actor, error)
	GetCurrentUser(ctx context.Context, userID uuid.UUID) (*domauth.User, error)
}

// Handler serves the admin auth routes: login, logout, me, status. It is
// mapping-only; all credentials/session decisions live in the app service.
type Handler struct {
	svc   AuthService
	store LockoutStore
	cfg   Config
	now   func() time.Time

	// OIDC surface (security P1-8). A zero-valued oidcCfg means OIDC is not
	// configured: Start/Callback/Test fail with 400 instead of leaking or
	// redirecting. WithOIDC wires the surface.
	oidcCfg  OIDCConfig
	oidcSvc  OIDCService
	oidcSt   OIDCStateStore
	oidcHTTP oidcHTTPClient
	oidcKeys *oidcJWKSCache
}

// New creates the auth handler. store defaults to an in-process lockout store
// and now defaults to time.Now when nil (the fake-clock seam for tests).
func New(svc AuthService, store LockoutStore, cfg Config, now func() time.Time) *Handler {
	if store == nil {
		store = NewMemoryLockoutStore()
	}
	if now == nil {
		now = time.Now
	}
	return &Handler{svc: svc, store: store, cfg: cfg, now: now}
}

// SessionCookieName returns the issued session cookie name: the __Host-
// variant when Secure is set (P2-10), the plain name for loopback HTTP.
func (h *Handler) SessionCookieName() string {
	if h.cfg.Secure {
		return sessionCookieNameSecure
	}
	return sessionCookieName
}

// ValidateSession adapts the app service for the SessionAuth middleware.
func (h *Handler) ValidateSession(ctx context.Context, rawToken string) (*domauth.Actor, error) {
	return h.svc.ValidateSession(ctx, rawToken)
}

// Routes registers the auth routes without transport middleware. The admin
// mount (httpserver) applies the P2-9 middleware groups around these handlers.
func (h *Handler) Routes(r chi.Router) {
	r.Post("/auth/login", h.Login)
	r.Post("/auth/logout", h.Logout)
	r.Get("/auth/me", h.Me)
	r.Get("/auth/status", h.Status)
	h.OIDCRoutes(r)
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// Login authenticates email/password through the app service, enforces the
// per-IP progressive lockout, and issues the session and CSRF cookies.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	key := h.lockoutKey(r)
	st, err := h.store.Load(r.Context(), key)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if st == nil {
		st = &LockoutState{}
	}

	now := h.now()
	denied, retryAfter := recordLoginFailure(st, now)
	if denied {
		h.writeLockout(w, retryAfter)
		return
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSONError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}

	session, rawToken, err := h.svc.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		if isCredentialFailure(err) {
			if saveErr := h.store.Save(r.Context(), key, st); saveErr != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			h.writeJSONError(w, http.StatusUnauthorized, "invalid_credentials", "invalid email or password")
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	user, err := h.svc.GetCurrentUser(r.Context(), session.UserID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Success resets the lockout state.
	if err := h.store.Save(r.Context(), key, &LockoutState{}); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	h.setSessionCookie(w, rawToken)
	h.setCSRFCookie(w)
	h.writeJSON(w, http.StatusOK, map[string]any{
		"session_id": session.ID.String(),
		"user":       projectUser(user),
	})
}

// Logout revokes the current session and clears the session and CSRF cookies.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	actor, ok := domauth.FromContext(r.Context())
	if !ok || actor.SessionID == uuid.Nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := h.svc.Logout(r.Context(), actor.SessionID); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	clearCookie(w, h.SessionCookieName(), h.cfg.Secure)
	clearCookie(w, csrfCookieName, h.cfg.Secure)
	h.writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// Me returns the authenticated user as a credential-safe projection.
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	actor, ok := domauth.FromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	user, err := h.svc.GetCurrentUser(r.Context(), actor.UserID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h.writeJSON(w, http.StatusOK, projectUser(user))
}

// Status returns the authentication status, the current user, and the CSRF
// token the client echoes on mutations.
func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	actor, ok := domauth.FromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	user, err := h.svc.GetCurrentUser(r.Context(), actor.UserID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	csrf := ""
	if c, err := r.Cookie(csrfCookieName); err == nil {
		csrf = c.Value
	}
	h.writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"user":          projectUser(user),
		"session_id":    actor.SessionID.String(),
		"csrf_token":    csrf,
	})
}

func (h *Handler) writeLockout(w http.ResponseWriter, retryAfter time.Duration) {
	w.Header().Set("Retry-After", strconv.FormatInt(int64(math.Ceil(retryAfter.Seconds())), 10))
	h.writeJSONError(w, http.StatusTooManyRequests, "locked", "too many failed login attempts")
}

func (h *Handler) lockoutKey(r *http.Request) string {
	ip := middleware.GetRealIP(r.Context())
	if ip == "" {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		ip = strings.Trim(host, "[]")
	}
	if parsed := net.ParseIP(ip); parsed != nil {
		ip = parsed.String()
	}
	return lockoutKeyPrefix + strings.TrimSpace(ip)
}

func (h *Handler) setSessionCookie(w http.ResponseWriter, value string) {
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- Secure is configurable for loopback HTTP (P2-10)
		Name:     h.SessionCookieName(),
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cfg.Secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *Handler) setCSRFCookie(w http.ResponseWriter) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return
	}
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- Secure is configurable for loopback HTTP (P2-10)
		Name:     csrfCookieName,
		Value:    hex.EncodeToString(b),
		Path:     "/",
		HttpOnly: false,
		Secure:   h.cfg.Secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearCookie(w http.ResponseWriter, name string, secure bool) {
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- mirrors the cookie being cleared
		Name:     name,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(1, 0),
	})
}

func isCredentialFailure(err error) bool {
	return errors.Is(err, appauth.ErrInvalidCredentials) ||
		errors.Is(err, appauth.ErrUserInactive) ||
		errors.Is(err, appauth.ErrNotAdmin)
}

// projectUser returns the credential-safe user projection: the password hash
// is never included.
func projectUser(u *domauth.User) map[string]any {
	var display *string
	if u.DisplayName != nil {
		display = u.DisplayName
	}
	return map[string]any{
		"id":           u.ID.String(),
		"email":        u.Email,
		"display_name": display,
		"is_admin":     u.IsAdmin,
		"created_at":   u.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (h *Handler) writeJSONError(w http.ResponseWriter, status int, code, msg string) {
	h.writeJSON(w, status, map[string]string{"error": code, "message": msg})
}
