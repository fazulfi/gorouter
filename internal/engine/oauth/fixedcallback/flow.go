// Package fixedcallback implements the fixed-port OAuth proxy callback flow
// used by Codex (port 1455) and xAI (port 56121).
package fixedcallback

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"gorouter/internal/domain/oauth"
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

// Flow implements the fixed-port proxy callback flow.
type Flow struct {
	repo      oauth.Repository
	fixedPort int
}

// NewFlow creates a fixed-port callback Flow for the given port.
func NewFlow(repo oauth.Repository, fixedPort int) *Flow {
	return &Flow{repo: repo, fixedPort: fixedPort}
}

// StartProxy starts the fixed-port proxy listener.
// Returns the server, a buffered callback result channel, and a close function.
// The caller MUST call closeFn when done to release resources.
// The result channel delivers at most one CallbackResult, then is closed by
// closeFn.
func (f *Flow) StartProxy(flowID oauth.FlowID) (*http.Server, <-chan CallbackResult, func(), error) {
	addr := fmt.Sprintf("127.0.0.1:%d", f.fixedPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("listen fixed port %d: %w", f.fixedPort, err)
	}

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
	mux.HandleFunc("/", handler)

	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go server.Serve(listener) //nolint:errcheck

	var closed bool
	closeFn := func() {
		if !closed {
			closed = true
			_ = server.Close()
			close(ch)
		}
	}

	return server, ch, closeFn, nil
}

// AwaitCallback blocks waiting for a callback result with timeout.
// Validates the authorization code, state, session expiry, and replay status.
func AwaitCallback(ctx context.Context, results <-chan CallbackResult, session *oauth.Session, timeout time.Duration) (*CallbackResult, error) {
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
			return nil, engineoauth.NewRedactedError("OAuth session expired", oauth.ErrSessionExpired)
		}
		if cr.State != session.State {
			return nil, engineoauth.ErrStateMismatch
		}
		return &cr, nil
	}
}

// CreateSession creates a pending OAuth session with PKCE for the fixed-port
// proxy flow.
func (f *Flow) CreateSession(ctx context.Context, providerID uuid.UUID, flowID oauth.FlowID) (*oauth.Session, error) {
	state, err := engineoauth.GenerateState()
	if err != nil {
		return nil, err
	}
	pkce, err := engineoauth.GeneratePKCE()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	s := &oauth.Session{
		ID:            uuid.New(),
		ProviderID:    providerID,
		FlowID:        flowID,
		Mechanism:     oauth.MechanismAuthCodePKCE,
		State:         state,
		CodeVerifier:  pkce.CodeVerifier,
		CodeChallenge: pkce.CodeChallenge,
		RedirectURI:   fmt.Sprintf("http://127.0.0.1:%d/callback", f.fixedPort),
		Status:        oauth.OAuthStatePending,
		ExpiresAt:     now.Add(5 * time.Minute),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := f.repo.Create(ctx, s); err != nil {
		return nil, fmt.Errorf("create proxy session: %w", err)
	}
	return s, nil
}
