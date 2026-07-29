package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/google/uuid"
)

var (
	ErrSessionExpired  = errors.New("session has expired")
	ErrSessionRevoked  = errors.New("session has been revoked")
	ErrSessionNotFound = errors.New("session not found")
)

const (
	defaultSessionDuration = 30 * 24 * time.Hour
	tokenBytes             = 32
)

// SessionService manages session lifecycle: creation, validation, and revocation.
type SessionService struct {
	repo       SessionRepository
	sessionDur time.Duration
}

// NewSessionService creates a SessionService. If sessionDur is zero, defaults to 30d.
func NewSessionService(repo SessionRepository, sessionDur time.Duration) *SessionService {
	if sessionDur <= 0 {
		sessionDur = defaultSessionDuration
	}
	return &SessionService{
		repo:       repo,
		sessionDur: sessionDur,
	}
}

// Create generates a new session for the given user. It produces a random token,
// stores its SHA-256 hash, and returns the raw token exactly once. The caller
// must present the raw token on subsequent requests.
func (s *SessionService) Create(ctx context.Context, userID uuid.UUID, ip net.IP, userAgent string, duration time.Duration) (*Session, string, error) {
	if duration <= 0 {
		duration = s.sessionDur
	}

	rawToken, err := generateRandomHex(tokenBytes)
	if err != nil {
		return nil, "", fmt.Errorf("generate session token: %w", err)
	}

	tokenHash := hashToken(rawToken)

	session := &Session{
		ID:        uuid.New(),
		UserID:    userID,
		TokenHash: tokenHash,
		IPAddress: ip,
		UserAgent: userAgent,
		ExpiresAt: time.Now().Add(duration),
		CreatedAt: time.Now(),
	}

	if err := s.repo.Create(ctx, session); err != nil {
		return nil, "", fmt.Errorf("persist session: %w", err)
	}

	return session, rawToken, nil
}

// Validate checks that the raw token corresponds to a valid (non-expired,
// non-revoked) session. Returns the session on success. On success it also
// extends the session expiry (sliding window) so that active sessions do not
// expire after 30 days of continuous activity. To avoid a database write on
// every request, the extension is throttled: the expiry is only extended when
// less than half of the session duration remains.
func (s *SessionService) Validate(ctx context.Context, rawToken string) (*Session, error) {
	tokenHash := hashToken(rawToken)

	session, err := s.repo.FindByTokenHash(ctx, tokenHash)
	if err != nil {
		return nil, fmt.Errorf("lookup session: %w", err)
	}
	if session == nil {
		return nil, ErrSessionNotFound
	}

	now := time.Now()
	if now.After(session.ExpiresAt) {
		return nil, ErrSessionExpired
	}

	if session.RevokedAt != nil {
		return nil, ErrSessionRevoked
	}

	// Throttled sliding expiry: only extend when less than half of the session
	// duration remains. This preserves the 30-day sliding window for active
	// users while avoiding a database write on every request.
	remaining := session.ExpiresAt.Sub(now)
	if remaining < s.sessionDur/2 {
		newExpiry := now.Add(s.sessionDur)
		if err := s.repo.UpdateExpiry(ctx, session.ID, newExpiry); err != nil {
			return nil, fmt.Errorf("extend session expiry: %w", err)
		}
		session.ExpiresAt = newExpiry
	}

	return session, nil
}

// Revoke marks a session as revoked so it can no longer be used.
func (s *SessionService) Revoke(ctx context.Context, id uuid.UUID) error {
	return s.repo.Revoke(ctx, id)
}

// generateRandomHex generates n cryptographically random bytes and returns them
// as a hex-encoded string.
func generateRandomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// hashToken returns the SHA-256 hex digest of the token.
func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
