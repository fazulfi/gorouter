// Package oauth defines domain types and repository contract for OAuth/import
// flow sessions used across all provider OAuth families.
package oauth

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// FlowID identifies a registered OAuth flow family from the OAuth matrix.
type FlowID string

const (
	FlowClaude      FlowID = "claude"
	FlowCodex       FlowID = "codex"
	FlowOpenAI      FlowID = "openai"
	FlowGeminiCLI   FlowID = "gemini-cli"
	FlowAntigravity FlowID = "antigravity"
	FlowIflow       FlowID = "iflow"
	FlowQwen        FlowID = "qwen"
	FlowQoder       FlowID = "qoder"
	FlowGitHub      FlowID = "github"
	FlowKiro        FlowID = "kiro"
	FlowKimi        FlowID = "kimi"
	FlowKilocode    FlowID = "kilocode"
	FlowCodebuddyCN FlowID = "codebuddy-cn"
	FlowGrokCLI     FlowID = "grok-cli"
	FlowGitLab      FlowID = "gitlab"
	FlowCline       FlowID = "cline"
	FlowClinepass   FlowID = "clinepass"
	FlowKimchi      FlowID = "kimchi"
	FlowCursor      FlowID = "cursor"
	FlowXAI         FlowID = "xai"
)

// Mechanism identifies the OAuth/import mechanism used by a flow family.
type Mechanism string

const (
	MechanismAuthCodePKCE Mechanism = "authorization_code_pkce"
	MechanismAuthCode     Mechanism = "authorization_code"
	MechanismDeviceCode   Mechanism = "device_code"
	MechanismDevicePKCE   Mechanism = "device_code_pkce"
	MechanismCookieImport Mechanism = "cookie_import"
	MechanismTokenImport  Mechanism = "token_import"
	MechanismPAT          Mechanism = "pat"
)

// OAuthState represents the lifecycle state of an OAuth session.
type OAuthState string

const (
	OAuthStatePending   OAuthState = "pending"
	OAuthStateCompleted OAuthState = "completed"
	OAuthStateFailed    OAuthState = "failed"
	OAuthStateCancelled OAuthState = "cancelled"
	OAuthStateExpired   OAuthState = "expired"
)

// IsTerminal returns true when the session cannot transition to another state.
func (s OAuthState) IsTerminal() bool {
	switch s {
	case OAuthStateCompleted, OAuthStateFailed, OAuthStateCancelled, OAuthStateExpired:
		return true
	default:
		return false
	}
}

// Session represents a single OAuth authorization or import flow session.
//
// Security invariants:
//   - State is a cryptographically random CSRF token.
//   - CodeVerifier is the PKCE code challenge verifier, redacted in logs.
//   - CodeChallenge is the S256 PKCE challenge derived from CodeVerifier.
//   - TokenHash is SHA-256 of access token, never raw token.
//   - RefreshToken stored as restricted plaintext per decision #120.
//   - Expired/replayed callbacks are rejected before any state mutation.
type Session struct {
	ID              uuid.UUID
	ProviderID      uuid.UUID
	AccountID       *uuid.UUID
	FlowID          FlowID
	Mechanism       Mechanism
	State           string
	CodeVerifier    string
	CodeChallenge   string
	RedirectURI     string
	Status          OAuthState
	ErrorDetail     *string
	TokenHash       *string
	RefreshToken    *string
	TokenExpiry     *time.Time
	DeviceCode      *string
	UserCode        *string
	VerificationURI *string
	ExpiresAt       time.Time
	CompletedAt     *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// IsTerminal returns true when the session is in a terminal state.
func (s *Session) IsTerminal() bool { return s.Status.IsTerminal() }

// FlowDefinition describes a registered OAuth flow family.
type FlowDefinition struct {
	FlowID       FlowID
	Mechanism    Mechanism
	PKCE         bool
	PortBehavior string // "find_free", "fixed:1455", "fixed:56121", "none"
	FixedPort    int
	Providers    []string
}

// Repository is the persistence contract for OAuth sessions.
type Repository interface {
	Create(ctx context.Context, s *Session) error
	FindByID(ctx context.Context, id uuid.UUID) (*Session, error)
	FindByState(ctx context.Context, state string) (*Session, error)
	FindPendingByProvider(ctx context.Context, providerID uuid.UUID) ([]Session, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status OAuthState, errorDetail *string) error
	Complete(ctx context.Context, id uuid.UUID, tokenHash, refreshToken string, tokenExpiry time.Time) error
	CancelPending(ctx context.Context) (int64, error)
	DeleteExpired(ctx context.Context) (int64, error)
	Cleanup(ctx context.Context) (int64, error)
}

// FlowDefinitions returns the authoritative set of 20 flow definitions.
func FlowDefinitions() []FlowDefinition {
	return []FlowDefinition{
		{FlowClaude, MechanismAuthCodePKCE, true, "find_free", 0, []string{"claude"}},
		{FlowCodex, MechanismAuthCodePKCE, true, "fixed:1455", 1455, []string{"codex"}},
		{FlowOpenAI, MechanismAuthCodePKCE, true, "find_free", 0, []string{"openai"}},
		{FlowGeminiCLI, MechanismAuthCode, false, "find_free", 0, []string{"gemini-cli"}},
		{FlowAntigravity, MechanismAuthCode, false, "find_free", 0, []string{"antigravity"}},
		{FlowIflow, MechanismAuthCode, false, "find_free", 0, []string{"iflow"}},
		{FlowQwen, MechanismDevicePKCE, true, "none", 0, []string{"qwen"}},
		{FlowQoder, MechanismDevicePKCE, true, "none", 0, []string{"qoder"}},
		{FlowGitHub, MechanismDeviceCode, false, "none", 0, []string{"github"}},
		{FlowKiro, MechanismDeviceCode, false, "none", 0, []string{"kiro"}},
		{FlowKimi, MechanismDeviceCode, false, "none", 0, []string{"kimi"}},
		{FlowKilocode, MechanismDeviceCode, false, "none", 0, []string{"kilocode"}},
		{FlowCodebuddyCN, MechanismDeviceCode, false, "none", 0, []string{"codebuddy-cn"}},
		{FlowGrokCLI, MechanismDeviceCode, false, "none", 0, []string{"grok-cli"}},
		{FlowGitLab, MechanismAuthCodePKCE, true, "find_free", 0, []string{"gitlab"}},
		{FlowCline, MechanismAuthCode, false, "find_free", 0, []string{"cline"}},
		{FlowClinepass, MechanismAuthCode, false, "find_free", 0, []string{"clinepass"}},
		{FlowKimchi, MechanismCookieImport, false, "none", 0, []string{"kimchi"}},
		{FlowCursor, MechanismTokenImport, false, "none", 0, []string{"cursor"}},
		{FlowXAI, MechanismAuthCodePKCE, true, "fixed:56121", 56121, []string{"xai"}},
	}
}

// LookupFlow returns the flow definition for the given flow ID, or nil.
func LookupFlow(id FlowID) *FlowDefinition {
	for _, fd := range FlowDefinitions() {
		if fd.FlowID == id {
			return &fd
		}
	}
	return nil
}

// ProviderFlow returns the flow definition for the given provider name, or nil.
func ProviderFlow(provider string) *FlowDefinition {
	for _, fd := range FlowDefinitions() {
		for _, p := range fd.Providers {
			if p == provider {
				return &fd
			}
		}
	}
	return nil
}
