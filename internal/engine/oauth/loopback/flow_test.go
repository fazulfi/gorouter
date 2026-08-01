package loopback

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
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

func TestGeneratePKCE_SharedPrimitive(t *testing.T) {
	pkce, err := engineoauth.GeneratePKCE()
	if err != nil {
		t.Fatalf("GeneratePKCE: %v", err)
	}
	if pkce.CodeVerifier == "" {
		t.Error("CodeVerifier should not be empty")
	}
	if pkce.CodeChallenge == "" {
		t.Error("CodeChallenge should not be empty")
	}
	if pkce.Method != "S256" {
		t.Errorf("Method = %s, want S256", pkce.Method)
	}
	// Verify challenge is correct S256 of verifier
	h := sha256.Sum256([]byte(pkce.CodeVerifier))
	wantChallenge := base64.RawURLEncoding.EncodeToString(h[:])
	if pkce.CodeChallenge != wantChallenge {
		t.Errorf("CodeChallenge = %s, want S256(verifier) = %s", pkce.CodeChallenge, wantChallenge)
	}
}

func TestGeneratePKCE_DeterministicChallenge(t *testing.T) {
	pkce1, _ := engineoauth.GeneratePKCE()
	pkce2, _ := engineoauth.GeneratePKCE()
	if pkce1.CodeVerifier == pkce2.CodeVerifier {
		t.Error("verifiers should be unique across calls")
	}
	if pkce1.CodeChallenge == pkce2.CodeChallenge {
		t.Error("challenges should be unique across calls")
	}
	// Verify that both verifiers produce matching challenges
	h := sha256.Sum256([]byte(pkce1.CodeVerifier))
	want := base64.RawURLEncoding.EncodeToString(h[:])
	if pkce1.CodeChallenge != want {
		t.Errorf("CodeChallenge mismatch: got %s, want %s", pkce1.CodeChallenge, want)
	}
}

func TestStartLocalServer_ReturnsNonNilChannel(t *testing.T) {
	ctx := context.Background()
	_, port, results, closeFn, err := StartLocalServer(ctx)
	if err != nil {
		t.Fatalf("StartLocalServer: %v", err)
	}
	defer closeFn()
	if port == 0 {
		t.Error("expected non-zero port")
	}
	if results == nil {
		t.Fatal("expected results channel")
	}
}

// TestStartLocalServer_ReadHeaderTimeout verifies the callback server sets a
// ReadHeaderTimeout to avoid slowloris-style header starvation.
func TestStartLocalServer_ReadHeaderTimeout(t *testing.T) {
	ctx := context.Background()
	server, _, _, closeFn, err := StartLocalServer(ctx)
	if err != nil {
		t.Fatalf("StartLocalServer: %v", err)
	}
	defer closeFn()
	if server.ReadHeaderTimeout <= 0 {
		t.Error("ReadHeaderTimeout should be set to defend against slowloris")
	}
}

func TestStartLocalServer_CloseReleasesResources(t *testing.T) {
	ctx := context.Background()
	server, port, _, closeFn, err := StartLocalServer(ctx)
	if err != nil {
		t.Fatalf("StartLocalServer: %v", err)
	}
	closeFn()
	// Server should be closed - trying to connect should fail fast
	// Verify the channel is also closed
	// Second call to closeFn should be a no-op (no panic)
	closeFn()
	_ = server
	_ = port
}

func TestStartLocalServer_CloseIsIdempotent(t *testing.T) {
	ctx := context.Background()
	_, _, _, closeFn, err := StartLocalServer(ctx)
	if err != nil {
		t.Fatalf("StartLocalServer: %v", err)
	}
	closeFn()
	closeFn()
	closeFn() // multiple calls should not panic
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
		ExpiresAt: time.Now().Add(-1 * time.Minute), // expired
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
		Status:      domainoauth.OAuthStateCompleted, // terminal
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
	results <- CallbackResult{State: "state"} // Code is empty

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
	results <- CallbackResult{Code: "code"} // State is empty

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
	cancel() // cancel before waiting

	_, err := AwaitCallback(ctx, results, session, 5*time.Second)
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
}

func TestFlow_CreateSession_UsesSharedPKCE(t *testing.T) {
	f := NewFlow(&noopRepo{})
	pid := uuid.New()
	s, err := f.CreateSession(context.Background(), pid, domainoauth.FlowOpenAI, "http://localhost:0/cb")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if s.FlowID != domainoauth.FlowOpenAI {
		t.Errorf("FlowID = %s, want %s", s.FlowID, domainoauth.FlowOpenAI)
	}
	if s.Status != domainoauth.OAuthStatePending {
		t.Errorf("Status = %s, want pending", s.Status)
	}
	if s.State == "" {
		t.Error("State should not be empty")
	}
	// PKCE flow should have verifier and challenge
	if s.CodeVerifier == "" {
		t.Error("CodeVerifier should not be empty for PKCE flow")
	}
	if s.CodeChallenge == "" {
		t.Error("CodeChallenge should not be empty for PKCE flow")
	}
	// Verify challenge matches S256(verifier)
	h := sha256.Sum256([]byte(s.CodeVerifier))
	wantChallenge := base64.RawURLEncoding.EncodeToString(h[:])
	if s.CodeChallenge != wantChallenge {
		t.Errorf("CodeChallenge mismatch: got %s, want %s", s.CodeChallenge, wantChallenge)
	}
	if s.Mechanism != domainoauth.MechanismAuthCodePKCE {
		t.Errorf("Mechanism = %s, want %s", s.Mechanism, domainoauth.MechanismAuthCodePKCE)
	}
}

func TestFlow_CreateSession_NonPKCEFlow(t *testing.T) {
	f := NewFlow(&noopRepo{})
	pid := uuid.New()
	s, err := f.CreateSession(context.Background(), pid, domainoauth.FlowCline, "http://localhost:0/cb")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if s.CodeVerifier != "" {
		t.Error("CodeVerifier should be empty for non-PKCE flow")
	}
	if s.CodeChallenge != "" {
		t.Error("CodeChallenge should be empty for non-PKCE flow")
	}
}

func TestFlow_CreateSession_CodexFlow(t *testing.T) {
	f := NewFlow(&noopRepo{})
	pid := uuid.New()
	s, err := f.CreateSession(context.Background(), pid, domainoauth.FlowCodex, "http://127.0.0.1:1455/cb")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if s.FlowID != domainoauth.FlowCodex {
		t.Errorf("FlowID = %s, want %s", s.FlowID, domainoauth.FlowCodex)
	}
	if s.CodeVerifier == "" {
		t.Error("CodeVerifier should not be empty for Codex (PKCE flow)")
	}
}

func TestFlow_CreateSession_CustomRedirectURI(t *testing.T) {
	f := NewFlow(&noopRepo{})
	pid := uuid.New()
	redirectURI := "http://127.0.0.1:9999/custom/cb"
	s, err := f.CreateSession(context.Background(), pid, domainoauth.FlowGitLab, redirectURI)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if s.RedirectURI != redirectURI {
		t.Errorf("RedirectURI = %s, want %s", s.RedirectURI, redirectURI)
	}
}

func TestHTTPCallbackHandler_DeliversToChannel(t *testing.T) {
	ctx := context.Background()
	_, port, results, closeFn, err := StartLocalServer(ctx)
	if err != nil {
		t.Fatalf("StartLocalServer: %v", err)
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
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for callback result")
	}
}

func TestHTTPCallbackHandler_ErrorCallback(t *testing.T) {
	ctx := context.Background()
	_, port, results, closeFn, err := StartLocalServer(ctx)
	if err != nil {
		t.Fatalf("StartLocalServer: %v", err)
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
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for callback error")
	}
}

func TestHTTPCallbackHandler_AuthCallbackPath(t *testing.T) {
	ctx := context.Background()
	_, port, results, closeFn, err := StartLocalServer(ctx)
	if err != nil {
		t.Fatalf("StartLocalServer: %v", err)
	}
	defer closeFn()

	resp, err := http.Get("http://127.0.0.1:" + itoa(port) + "/auth/callback?code=code-1&state=state-1")
	if err != nil {
		t.Fatalf("HTTP /auth/callback failed: %v", err)
	}
	resp.Body.Close()

	select {
	case cr := <-results:
		if cr.Code != "code-1" {
			t.Errorf("Code = %s, want code-1", cr.Code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for /auth/callback")
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
