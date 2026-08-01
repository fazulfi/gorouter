// Package loopback implements the standard authorization-code loopback flow
// used by providers with find_free port behavior (Claude, OpenAI, Gemini CLI,
// Antigravity, Iflow, GitLab, Cline, Clinepass).
package loopback

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"time"

	domainoauth "gorouter/internal/domain/oauth"
	engineoauth "gorouter/internal/engine/oauth"

	"github.com/google/uuid"
)

// CallbackResult carries the parsed OAuth callback parameters.
type CallbackResult struct {
	Code      string
	State     string
	Error     string
	ErrorDesc string
}

// Flow implements the loopback authorization-code flow with find_free port.
type Flow struct {
	repo domainoauth.Repository
}

// NewFlow creates a new loopback Flow.
func NewFlow(repo domainoauth.Repository) *Flow {
	return &Flow{repo: repo}
}

// StartLocalServer starts an HTTP server on a free port for the callback.
// Returns the server, port, a buffered callback result channel, and a close
// function. The caller MUST call closeFn when done to release resources.
// The result channel delivers at most one CallbackResult, then is closed by
// the server on shutdown.
func StartLocalServer(ctx context.Context) (server *http.Server, port int, results <-chan CallbackResult, closeFn func(), err error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, 0, nil, nil, fmt.Errorf("listen loopback: %w", err)
	}
	port = listener.Addr().(*net.TCPAddr).Port

	ch := make(chan CallbackResult, 1)

	mux := http.NewServeMux()
	handler := func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		cr := CallbackResult{
			Code:      q.Get("code"),
			State:     q.Get("state"),
			Error:     q.Get("error"),
			ErrorDesc: q.Get("error_description"),
		}
		if cr.Error != "" {
			select {
			case ch <- cr:
			default:
			}
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `<html><body><h1>Auth Error</h1><p>%s: %s</p><script>window.close()</script></body></html>`,
				cr.Error, cr.ErrorDesc)
			return
		}
		select {
		case ch <- cr:
		default:
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `<html><body><h1>Authorization Successful</h1><p>You may close this window.</p><script>window.close()</script></body></html>`)
	}
	mux.HandleFunc("/callback", handler)
	mux.HandleFunc("/auth/callback", handler)

	server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go server.Serve(listener) //nolint:errcheck

	closed := false
	closeFn = func() {
		if !closed {
			closed = true
			_ = server.Close()
			close(ch)
		}
	}

	return server, port, ch, closeFn, nil
}

// AwaitCallback blocks waiting for a callback result with timeout.
// Validates the authorization code, state, session expiry, and replay status.
func AwaitCallback(ctx context.Context, results <-chan CallbackResult, session *domainoauth.Session, timeout time.Duration) (*CallbackResult, error) {
	if session == nil {
		return nil, fmt.Errorf("session is nil")
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return nil, fmt.Errorf("callback timeout after %v", timeout)
	case cr, ok := <-results:
		if !ok {
			return nil, fmt.Errorf("callback channel closed")
		}
		if cr.Error != "" {
			return nil, engineoauth.NewRedactedError("OAuth callback rejected by provider",
				fmt.Errorf("%s: %s", cr.Error, cr.ErrorDesc))
		}
		if cr.Code == "" {
			return nil, engineoauth.NewRedactedError("OAuth callback missing authorization code", nil)
		}
		if cr.State == "" {
			return nil, engineoauth.NewRedactedError("OAuth callback missing state parameter", nil)
		}
		if session.IsTerminal() {
			return nil, engineoauth.ErrReplayDetected
		}
		if time.Now().After(session.ExpiresAt) {
			return nil, engineoauth.NewRedactedError("OAuth session expired", domainoauth.ErrSessionExpired)
		}
		if cr.State != session.State {
			return nil, engineoauth.ErrStateMismatch
		}
		return &cr, nil
	}
}

// CreateSession creates a new OAuth session for a loopback flow.
// Uses the shared PKCE primitive from internal/engine/oauth.
func (f *Flow) CreateSession(ctx context.Context, providerID uuid.UUID, flowID domainoauth.FlowID, redirectURI string) (*domainoauth.Session, error) {
	state, err := generateState()
	if err != nil {
		return nil, err
	}
	codeVerifier := ""
	codeChallenge := ""
	fd := domainoauth.LookupFlow(flowID)
	if fd != nil && fd.PKCE {
		pkce, err := engineoauth.GeneratePKCE()
		if err != nil {
			return nil, err
		}
		codeVerifier = pkce.CodeVerifier
		codeChallenge = pkce.CodeChallenge
	}
	now := time.Now()
	fm := domainoauth.MechanismAuthCodePKCE
	if fd != nil {
		fm = fd.Mechanism
	}
	s := &domainoauth.Session{
		ID:            uuid.New(),
		ProviderID:    providerID,
		FlowID:        flowID,
		Mechanism:     fm,
		State:         state,
		CodeVerifier:  codeVerifier,
		CodeChallenge: codeChallenge,
		RedirectURI:   redirectURI,
		Status:        domainoauth.OAuthStatePending,
		ExpiresAt:     now.Add(10 * time.Minute),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := f.repo.Create(ctx, s); err != nil {
		return nil, fmt.Errorf("create oauth session: %w", err)
	}
	return s, nil
}

func generateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
