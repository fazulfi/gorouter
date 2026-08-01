package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"time"

	"gorouter/internal/domain/oauth"
)

// PKCEParams holds S256 PKCE challenge parameters per RFC 7636.
type PKCEParams struct {
	CodeVerifier  string
	CodeChallenge string
	Method        string
}

// GeneratePKCE creates S256 PKCE parameters.
func GeneratePKCE() (*PKCEParams, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("generate code verifier: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(h[:])
	return &PKCEParams{
		CodeVerifier:  verifier,
		CodeChallenge: challenge,
		Method:        "S256",
	}, nil
}

// GenerateState creates a cryptographically random OAuth state parameter.
func GenerateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate state: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// HashToken returns the SHA-256 hex digest of a token.
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// SessionTTL returns the TTL duration for an OAuth session based on flow type.
func SessionTTL(flowID oauth.FlowID) time.Duration {
	switch flowID {
	case oauth.FlowCodex, oauth.FlowXAI:
		return 5 * time.Minute
	case oauth.FlowKimchi, oauth.FlowCursor:
		return 2 * time.Minute
	default:
		return 10 * time.Minute
	}
}

// VerificationURIFromDevice generates a verification URI from device code response.
func VerificationURIFromDevice(baseURI, userCode string) string {
	v := url.Values{}
	v.Set("user_code", userCode)
	return baseURI + "?" + v.Encode()
}
