package fixedcallback

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net"
	"net/http"
	"testing"
	"time"

	domainoauth "gorouter/internal/domain/oauth"
	engineoauth "gorouter/internal/engine/oauth"

	"github.com/google/uuid"
)

type noopRepo struct{}

func (n *noopRepo) Create(_ context.Context, _ *domainoauth.Session) error { return nil }
func (n *noopRepo) FindByID(_ context.Context, _ uuid.UUID) (*domainoauth.Session, error) {
	return nil, nil
}
func (n *noopRepo) FindByState(_ context.Context, _ string) (*domainoauth.Session, error) {
	return nil, nil
}
func (n *noopRepo) FindPendingByProvider(_ context.Context, _ uuid.UUID) ([]domainoauth.Session, error) {
	return nil, nil
}
func (n *noopRepo) UpdateStatus(_ context.Context, _ uuid.UUID, _ domainoauth.OAuthState, _ *string) error {
	return nil
}
func (n *noopRepo) Complete(_ context.Context, _ uuid.UUID, _, _ string, _ time.Time) error {
	return nil
}
func (n *noopRepo) CancelPending(_ context.Context) (int64, error) { return 0, nil }
func (n *noopRepo) DeleteExpired(_ context.Context) (int64, error) { return 0, nil }
func (n *noopRepo) Cleanup(_ context.Context) (int64, error)       { return 0, nil }

func findFreePort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

func TestNewFlow(t *testing.T) {
	f := NewFlow(&noopRepo{}, 1455)
	if f == nil {
		t.Fatal("NewFlow returned nil")
	}
	if f.fixedPort != 1455 {
		t.Errorf("fixedPort = %d, want 1455", f.fixedPort)
	}
}

func TestStartProxy_StartsOnFixedPort(t *testing.T) {
	port := findFreePort()
	if port == 0 {
		t.Skip("no free port available")
	}
	f := NewFlow(&noopRepo{}, port)
	server, results, closeFn, err := f.StartProxy(domainoauth.FlowCodex)
	if err != nil {
		t.Fatalf("StartProxy: %v", err)
	}
	defer closeFn()
	if server == nil {
		t.Fatal("expected non-nil server")
	}
	if results == nil {
		t.Fatal("expected non-nil results channel")
	}
}

func TestStartProxy_ReturnsBoundedChannel(t *testing.T) {
	port := findFreePort()
	if port == 0 {
		t.Skip("no free port available")
	}
	f := NewFlow(&noopRepo{}, port)
	_, results, closeFn, err := f.StartProxy(domainoauth.FlowCodex)
	if err != nil {
		t.Fatalf("StartProxy: %v", err)
	}
	defer closeFn()
	if results == nil {
		t.Fatal("expected results channel")
	}
	if cap(results) < 1 {
		t.Error("results channel should be buffered (size >= 1)")
	}
}

// TestStartProxy_ReadHeaderTimeout verifies the proxy server sets a
// ReadHeaderTimeout to avoid slowloris-style header starvation.
func TestStartProxy_ReadHeaderTimeout(t *testing.T) {
	port := findFreePort()
	if port == 0 {
		t.Skip("no free port available")
	}
	f := NewFlow(&noopRepo{}, port)
	server, _, closeFn, err := f.StartProxy(domainoauth.FlowCodex)
	if err != nil {
		t.Fatalf("StartProxy: %v", err)
	}
	defer closeFn()
	if server.ReadHeaderTimeout <= 0 {
		t.Error("ReadHeaderTimeout should be set to defend against slowloris")
	}
}

func TestStartProxy_CloseReleasesResources(t *testing.T) {
	port := findFreePort()
	if port == 0 {
		t.Skip("no free port available")
	}
	f := NewFlow(&noopRepo{}, port)
	server, _, closeFn, err := f.StartProxy(domainoauth.FlowCodex)
	if err != nil {
		t.Fatalf("StartProxy: %v", err)
	}
	closeFn()
	closeFn()
	_ = server
}

func TestStartProxy_CloseIsIdempotent(t *testing.T) {
	port := findFreePort()
	if port == 0 {
		t.Skip("no free port available")
	}
	f := NewFlow(&noopRepo{}, port)
	_, _, closeFn, err := f.StartProxy(domainoauth.FlowCodex)
	if err != nil {
		t.Fatalf("StartProxy: %v", err)
	}
	closeFn()
	closeFn()
	closeFn()
}

func TestStartProxy_PortInUse(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("cannot create listener for conflict test")
	}
	port := l.Addr().(*net.TCPAddr).Port
	defer l.Close()

	f := NewFlow(&noopRepo{}, port)
	_, _, _, err = f.StartProxy(domainoauth.FlowCodex)
	if err == nil {
		t.Error("expected error when port is already in use")
	}
}

func TestAwaitCallback_ValidatesState(t *testing.T) {
	ctx := context.Background()
	session := &domainoauth.Session{
		ID:        uuid.New(),
		State:     "expected-state",
		Status:    domainoauth.OAuthStatePending,
		ExpiresAt: time.Now().Add(10 * time.Minute),
	}
	results := make(chan CallbackResult, 1)
	results <- CallbackResult{Code: "auth-code", State: "wrong-state"}

	_, err := AwaitCallback(ctx, results, session, 5*time.Second)
	if err == nil {
		t.Fatal("expected state mismatch error, got nil")
	}
	if err != engineoauth.ErrStateMismatch {
		t.Errorf("expected ErrStateMismatch, got %v", err)
	}
}

func TestAwaitCallback_RejectsExpiredSession(t *testing.T) {
	ctx := context.Background()
	session := &domainoauth.Session{
		ID:        uuid.New(),
		State:     "state",
		Status:    domainoauth.OAuthStatePending,
		ExpiresAt: time.Now().Add(-1 * time.Minute),
	}
	results := make(chan CallbackResult, 1)
	results <- CallbackResult{Code: "code", State: "state"}

	_, err := AwaitCallback(ctx, results, session, 5*time.Second)
	if err == nil {
		t.Fatal("expected expired session error, got nil")
	}
}

func TestAwaitCallback_RejectsReplay(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	session := &domainoauth.Session{
		ID:          uuid.New(),
		State:       "state",
		Status:      domainoauth.OAuthStateCompleted,
		ExpiresAt:   time.Now().Add(10 * time.Minute),
		CompletedAt: &now,
	}
	results := make(chan CallbackResult, 1)
	results <- CallbackResult{Code: "code", State: "state"}

	_, err := AwaitCallback(ctx, results, session, 5*time.Second)
	if err == nil {
		t.Fatal("expected replay detection error, got nil")
	}
	if err != engineoauth.ErrReplayDetected {
		t.Errorf("expected ErrReplayDetected, got %v", err)
	}
}

func TestAwaitCallback_ReturnsValidCallback(t *testing.T) {
	ctx := context.Background()
	session := &domainoauth.Session{
		ID:        uuid.New(),
		State:     "correct-state",
		Status:    domainoauth.OAuthStatePending,
		ExpiresAt: time.Now().Add(10 * time.Minute),
	}
	results := make(chan CallbackResult, 1)
	results <- CallbackResult{Code: "valid-code", State: "correct-state"}

	cr, err := AwaitCallback(ctx, results, session, 5*time.Second)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if cr.Code != "valid-code" {
		t.Errorf("Code = %s, want valid-code", cr.Code)
	}
	if cr.State != "correct-state" {
		t.Errorf("State = %s, want correct-state", cr.State)
	}
}

func TestAwaitCallback_RejectsNilSession(t *testing.T) {
	ctx := context.Background()
	results := make(chan CallbackResult, 1)
	_, err := AwaitCallback(ctx, results, nil, 5*time.Second)
	if err == nil {
		t.Fatal("expected error for nil session")
	}
}

func TestAwaitCallback_RejectsErrorCallback(t *testing.T) {
	ctx := context.Background()
	session := &domainoauth.Session{
		ID:        uuid.New(),
		State:     "state",
		Status:    domainoauth.OAuthStatePending,
		ExpiresAt: time.Now().Add(10 * time.Minute),
	}
	results := make(chan CallbackResult, 1)
	results <- CallbackResult{Error: "access_denied", ErrorDesc: "user cancelled"}

	_, err := AwaitCallback(ctx, results, session, 5*time.Second)
	if err == nil {
		t.Fatal("expected error from callback error")
	}
}

func TestAwaitCallback_RejectsMissingCode(t *testing.T) {
	ctx := context.Background()
	session := &domainoauth.Session{
		ID:        uuid.New(),
		State:     "state",
		Status:    domainoauth.OAuthStatePending,
		ExpiresAt: time.Now().Add(10 * time.Minute),
	}
	results := make(chan CallbackResult, 1)
	results <- CallbackResult{State: "state"}

	_, err := AwaitCallback(ctx, results, session, 5*time.Second)
	if err == nil {
		t.Fatal("expected error for missing authorization code")
	}
}

func TestAwaitCallback_RejectsMissingState(t *testing.T) {
	ctx := context.Background()
	session := &domainoauth.Session{
		ID:        uuid.New(),
		State:     "session-state",
		Status:    domainoauth.OAuthStatePending,
		ExpiresAt: time.Now().Add(10 * time.Minute),
	}
	results := make(chan CallbackResult, 1)
	results <- CallbackResult{Code: "code"}

	_, err := AwaitCallback(ctx, results, session, 5*time.Second)
	if err == nil {
		t.Fatal("expected error for missing state parameter")
	}
}

func TestAwaitCallback_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	session := &domainoauth.Session{
		ID:        uuid.New(),
		State:     "state",
		Status:    domainoauth.OAuthStatePending,
		ExpiresAt: time.Now().Add(10 * time.Minute),
	}
	results := make(chan CallbackResult, 1)
	cancel()

	_, err := AwaitCallback(ctx, results, session, 5*time.Second)
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
}

func TestAwaitCallback_Timeout(t *testing.T) {
	ctx := context.Background()
	session := &domainoauth.Session{
		ID:        uuid.New(),
		State:     "state",
		Status:    domainoauth.OAuthStatePending,
		ExpiresAt: time.Now().Add(10 * time.Minute),
	}
	results := make(chan CallbackResult, 1)

	_, err := AwaitCallback(ctx, results, session, 10*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestAwaitCallback_RejectsClosedChannel(t *testing.T) {
	ctx := context.Background()
	session := &domainoauth.Session{
		ID:        uuid.New(),
		State:     "state",
		Status:    domainoauth.OAuthStatePending,
		ExpiresAt: time.Now().Add(10 * time.Minute),
	}
	results := make(chan CallbackResult)
	close(results)

	_, err := AwaitCallback(ctx, results, session, 5*time.Second)
	if err == nil {
		t.Fatal("expected error for closed channel")
	}
}

func TestHTTPCallbackHandler_DeliversToChannel(t *testing.T) {
	port := findFreePort()
	if port == 0 {
		t.Skip("no free port available")
	}
	f := NewFlow(&noopRepo{}, port)
	_, results, closeFn, err := f.StartProxy(domainoauth.FlowCodex)
	if err != nil {
		t.Fatalf("StartProxy: %v", err)
	}
	defer closeFn()

	resp, err := http.Get("http://127.0.0.1:" + itoa(port) + "/callback?code=test-code-123&state=test-state-456")
	if err != nil {
		t.Fatalf("HTTP callback failed: %v", err)
	}
	resp.Body.Close()

	select {
	case cr := <-results:
		if cr.Code != "test-code-123" {
			t.Errorf("Code = %s, want test-code-123", cr.Code)
		}
		if cr.State != "test-state-456" {
			t.Errorf("State = %s, want test-state-456", cr.State)
		}
		if cr.Error != "" {
			t.Errorf("Error = %s, want empty", cr.Error)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for callback result")
	}
}

func TestHTTPCallbackHandler_ErrorCallback(t *testing.T) {
	port := findFreePort()
	if port == 0 {
		t.Skip("no free port available")
	}
	f := NewFlow(&noopRepo{}, port)
	_, results, closeFn, err := f.StartProxy(domainoauth.FlowCodex)
	if err != nil {
		t.Fatalf("StartProxy: %v", err)
	}
	defer closeFn()

	resp, err := http.Get("http://127.0.0.1:" + itoa(port) + "/callback?error=access_denied&error_description=user+cancelled")
	if err != nil {
		t.Fatalf("HTTP callback failed: %v", err)
	}
	resp.Body.Close()

	select {
	case cr := <-results:
		if cr.Error != "access_denied" {
			t.Errorf("Error = %s, want access_denied", cr.Error)
		}
		if cr.ErrorDesc != "user cancelled" {
			t.Errorf("ErrorDesc = %s, want user cancelled", cr.ErrorDesc)
		}
		if cr.Code != "" {
			t.Errorf("Code = %s, want empty", cr.Code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for callback error")
	}
}

func TestHTTPCallbackHandler_RootPath(t *testing.T) {
	port := findFreePort()
	if port == 0 {
		t.Skip("no free port available")
	}
	f := NewFlow(&noopRepo{}, port)
	_, results, closeFn, err := f.StartProxy(domainoauth.FlowCodex)
	if err != nil {
		t.Fatalf("StartProxy: %v", err)
	}
	defer closeFn()

	resp, err := http.Get("http://127.0.0.1:" + itoa(port) + "/?code=code-1&state=state-1")
	if err != nil {
		t.Fatalf("HTTP root callback failed: %v", err)
	}
	resp.Body.Close()

	select {
	case cr := <-results:
		if cr.Code != "code-1" {
			t.Errorf("Code = %s, want code-1", cr.Code)
		}
		if cr.State != "state-1" {
			t.Errorf("State = %s, want state-1", cr.State)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for root callback")
	}
}

func TestHTTPCallbackHandler_StatusCodes(t *testing.T) {
	port := findFreePort()
	if port == 0 {
		t.Skip("no free port available")
	}
	f := NewFlow(&noopRepo{}, port)
	_, _, closeFn, err := f.StartProxy(domainoauth.FlowCodex)
	if err != nil {
		t.Fatalf("StartProxy: %v", err)
	}
	defer closeFn()

	t.Run("success returns 200", func(t *testing.T) {
		resp, err := http.Get("http://127.0.0.1:" + itoa(port) + "/callback?code=ok&state=ok")
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("StatusCode = %d, want 200", resp.StatusCode)
		}
	})

	t.Run("error returns 400", func(t *testing.T) {
		resp, err := http.Get("http://127.0.0.1:" + itoa(port) + "/callback?error=denied")
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("StatusCode = %d, want 400", resp.StatusCode)
		}
	})
}

func TestFlow_CreateSession_GeneratesStateAndPKCE(t *testing.T) {
	f := NewFlow(&noopRepo{}, 1455)
	pid := uuid.New()
	s, err := f.CreateSession(context.Background(), pid, domainoauth.FlowCodex)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if s.FlowID != domainoauth.FlowCodex {
		t.Errorf("FlowID = %s, want %s", s.FlowID, domainoauth.FlowCodex)
	}
	if s.Status != domainoauth.OAuthStatePending {
		t.Errorf("Status = %s, want pending", s.Status)
	}
	if s.State == "" {
		t.Error("State should not be empty")
	}
	if s.CodeVerifier == "" {
		t.Error("CodeVerifier should not be empty for PKCE flow")
	}
	if s.CodeChallenge == "" {
		t.Error("CodeChallenge should not be empty for PKCE flow")
	}
	h := sha256.Sum256([]byte(s.CodeVerifier))
	wantChallenge := base64.RawURLEncoding.EncodeToString(h[:])
	if s.CodeChallenge != wantChallenge {
		t.Errorf("CodeChallenge mismatch: got %s, want %s", s.CodeChallenge, wantChallenge)
	}
	if s.Mechanism != domainoauth.MechanismAuthCodePKCE {
		t.Errorf("Mechanism = %s, want %s", s.Mechanism, domainoauth.MechanismAuthCodePKCE)
	}
}

func TestFlow_CreateSession_CodexPort(t *testing.T) {
	f := NewFlow(&noopRepo{}, 1455)
	pid := uuid.New()
	s, err := f.CreateSession(context.Background(), pid, domainoauth.FlowCodex)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if s.RedirectURI != "http://127.0.0.1:1455/callback" {
		t.Errorf("RedirectURI = %s, want http://127.0.0.1:1455/callback", s.RedirectURI)
	}
}

func TestFlow_CreateSession_XAIPort(t *testing.T) {
	f := NewFlow(&noopRepo{}, 56121)
	pid := uuid.New()
	s, err := f.CreateSession(context.Background(), pid, domainoauth.FlowXAI)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if s.RedirectURI != "http://127.0.0.1:56121/callback" {
		t.Errorf("RedirectURI = %s, want http://127.0.0.1:56121/callback", s.RedirectURI)
	}
}

func TestFlow_CreateSession_UniqueStateAndPKCE(t *testing.T) {
	f := NewFlow(&noopRepo{}, 1455)
	pid := uuid.New()

	s1, err := f.CreateSession(context.Background(), pid, domainoauth.FlowCodex)
	if err != nil {
		t.Fatalf("CreateSession 1: %v", err)
	}
	s2, err := f.CreateSession(context.Background(), pid, domainoauth.FlowCodex)
	if err != nil {
		t.Fatalf("CreateSession 2: %v", err)
	}
	if s1.State == s2.State {
		t.Error("state values should be unique across calls")
	}
	if s1.CodeVerifier == s2.CodeVerifier {
		t.Error("code verifiers should be unique across calls")
	}
	if s1.CodeChallenge == s2.CodeChallenge {
		t.Error("code challenges should be unique across calls")
	}
}

func TestFlow_CreateSession_SessionTTL(t *testing.T) {
	f := NewFlow(&noopRepo{}, 1455)
	pid := uuid.New()
	s, err := f.CreateSession(context.Background(), pid, domainoauth.FlowCodex)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	expectedTTL := 5 * time.Minute
	actualTTL := s.ExpiresAt.Sub(s.CreatedAt)
	if actualTTL != expectedTTL {
		t.Errorf("session TTL = %v, want %v", actualTTL, expectedTTL)
	}
}

func TestFlow_CreateSession_ExpiresAtFuture(t *testing.T) {
	f := NewFlow(&noopRepo{}, 1455)
	pid := uuid.New()
	s, err := f.CreateSession(context.Background(), pid, domainoauth.FlowCodex)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if !s.ExpiresAt.After(time.Now()) {
		t.Error("ExpiresAt should be in the future")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
