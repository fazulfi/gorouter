// Package auth provides domain types for authentication and authorization.
package auth

import (
	"context"
	"net"
	"time"

	"github.com/google/uuid"
)

// contextKey is used for storing auth values in request contexts.
type contextKey string

func (c contextKey) String() string { return "gorouter.auth." + string(c) }

// ActorKey is the context key for storing the authenticated Actor.
const ActorKey contextKey = "actor"

// FromContext extracts an Actor from the context. Returns nil, false when no
// actor is present.
func FromContext(ctx context.Context) (*Actor, bool) {
	v := ctx.Value(ActorKey)
	if v == nil {
		return nil, false
	}
	a, ok := v.(*Actor)
	return a, ok
}

// User represents an authenticated user in the system.
type User struct {
	ID           uuid.UUID
	Email        string
	PasswordHash string
	DisplayName  *string
	IsAdmin      bool
	IsActive     bool
	LastLoginAt  *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Session represents an active user session.
type Session struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	TokenHash string
	IPAddress net.IP
	UserAgent string
	ExpiresAt time.Time
	RevokedAt *time.Time
	CreatedAt time.Time
}

// Actor represents the authenticated principal making a request.
type Actor struct {
	UserID    uuid.UUID
	SessionID uuid.UUID
	IsAdmin   bool
	Scopes    []string
}

// UserRepository defines persistence operations for users.
type UserRepository interface {
	FindByID(ctx context.Context, id uuid.UUID) (*User, error)
	FindByEmail(ctx context.Context, email string) (*User, error)
	Create(ctx context.Context, user *User) error
	Update(ctx context.Context, user *User) error
	UpdateLastLogin(ctx context.Context, id uuid.UUID) error
}

// SessionRepository defines persistence operations for sessions.
type SessionRepository interface {
	FindByID(ctx context.Context, id uuid.UUID) (*Session, error)
	FindByTokenHash(ctx context.Context, hash string) (*Session, error)
	Create(ctx context.Context, session *Session) error
	Revoke(ctx context.Context, id uuid.UUID) error
	UpdateExpiry(ctx context.Context, id uuid.UUID, expiresAt time.Time) error
	DeleteExpired(ctx context.Context) error
}

// PasswordReset represents a password reset token.
type PasswordReset struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	TokenHash string
	ExpiresAt time.Time
	UsedAt    *time.Time
	CreatedAt time.Time
}

// AuthProvider represents an external OAuth/OIDC provider configuration.
type AuthProvider struct {
	ID           uuid.UUID
	Name         string
	ProviderType string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string
	IsEnabled    bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
