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

// ActorKind identifies the credential kind of an authenticated principal
// (design §6 P2-15, DECISIONS #357): a dashboard session, a personal access
// token, the local CLI, a scheduler job, or a generic user principal. The
// zero value ("") is not a valid kind and must never satisfy a kind check.
type ActorKind string

const (
	// ActorKindUser is a generic user-derived principal (for example a model
	// API key lookup that yields a user identity without a session).
	ActorKindUser ActorKind = "user"
	// ActorKindSession is a dashboard session-cookie principal.
	ActorKindSession ActorKind = "session"
	// ActorKindPAT is a personal access token principal.
	ActorKindPAT ActorKind = "pat"
	// ActorKindCLI is the host-local command-line principal.
	ActorKindCLI ActorKind = "cli"
	// ActorKindJob is an internal scheduler job principal.
	ActorKindJob ActorKind = "job"
)

// ActorOrigin identifies where a principal's identity was established. The
// zero value ("") is not a valid origin and must never satisfy an origin
// check.
type ActorOrigin string

const (
	// ActorOriginLocal is a host-local origin (the CLI on the server host).
	ActorOriginLocal ActorOrigin = "local"
	// ActorOriginRemote is any network origin (dashboard session, PAT, API
	// key).
	ActorOriginRemote ActorOrigin = "remote"
)

// Actor represents the authenticated principal making a request. The
// additive Kind/Origin contract (design §6 P2-15) identifies the credential
// kind and establishment origin; the legacy UserID/SessionID/IsAdmin/Scopes
// fields are retained for compatibility until every consumer is migrated.
type Actor struct {
	UserID    uuid.UUID
	SessionID uuid.UUID
	IsAdmin   bool
	Scopes    []string
	Kind      ActorKind
	Origin    ActorOrigin
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
