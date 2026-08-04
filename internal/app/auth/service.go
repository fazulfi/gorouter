// Package auth provides the application-layer authentication service that
// orchestrates password hashing, session management, and user lookup.
package auth

import (
	"context"
	"errors"
	"fmt"

	domain "gorouter/internal/domain/auth"

	"github.com/google/uuid"
)

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrUserInactive       = errors.New("user account is inactive")
	ErrNotAdmin           = errors.New("user is not an admin")
	ErrSessionRequired    = errors.New("valid session required")
)

// Service is the application-layer authentication service.
type Service struct {
	passwordSvc *domain.PasswordService
	sessionSvc  *domain.SessionService
	users       domain.UserRepository
	sessions    domain.SessionRepository
}

// NewAuthService creates a new AuthService with the given dependencies.
func NewAuthService(
	passwordSvc *domain.PasswordService,
	sessionSvc *domain.SessionService,
	users domain.UserRepository,
	sessions domain.SessionRepository,
) *Service {
	return &Service{
		passwordSvc: passwordSvc,
		sessionSvc:  sessionSvc,
		users:       users,
		sessions:    sessions,
	}
}

// Login verifies credentials and creates a session. Returns the session and
// raw token that must be presented on subsequent requests.
func (s *Service) Login(ctx context.Context, email, password string) (*domain.Session, string, error) {
	user, err := s.users.FindByEmail(ctx, email)
	if err != nil {
		return nil, "", fmt.Errorf("lookup user: %w", err)
	}
	if user == nil {
		return nil, "", ErrInvalidCredentials
	}

	if !user.IsActive {
		return nil, "", ErrUserInactive
	}

	if !s.passwordSvc.VerifyPassword(user.PasswordHash, password) {
		return nil, "", ErrInvalidCredentials
	}

	if err := s.users.UpdateLastLogin(ctx, user.ID); err != nil {
		return nil, "", fmt.Errorf("update last login: %w", err)
	}

	session, rawToken, err := s.sessionSvc.Create(ctx, user.ID, nil, "", 0)
	if err != nil {
		return nil, "", fmt.Errorf("create session: %w", err)
	}

	return session, rawToken, nil
}

// LoginAdmin verifies admin credentials and creates a session.
func (s *Service) LoginAdmin(ctx context.Context, email, password string) (*domain.Session, string, error) {
	user, err := s.users.FindByEmail(ctx, email)
	if err != nil {
		return nil, "", fmt.Errorf("lookup user: %w", err)
	}
	if user == nil {
		return nil, "", ErrInvalidCredentials
	}

	if !user.IsActive {
		return nil, "", ErrUserInactive
	}

	if !user.IsAdmin {
		return nil, "", ErrNotAdmin
	}

	if !s.passwordSvc.VerifyPassword(user.PasswordHash, password) {
		return nil, "", ErrInvalidCredentials
	}

	if err := s.users.UpdateLastLogin(ctx, user.ID); err != nil {
		return nil, "", fmt.Errorf("update last login: %w", err)
	}

	session, rawToken, err := s.sessionSvc.Create(ctx, user.ID, nil, "", 0)
	if err != nil {
		return nil, "", fmt.Errorf("create session: %w", err)
	}

	return session, rawToken, nil
}

// Logout revokes the session identified by id.
func (s *Service) Logout(ctx context.Context, sessionID uuid.UUID) error {
	return s.sessionSvc.Revoke(ctx, sessionID)
}

// ValidateSession validates the raw session token and returns the associated
// Actor (authenticated principal).
func (s *Service) ValidateSession(ctx context.Context, rawToken string) (*domain.Actor, error) {
	session, err := s.sessionSvc.Validate(ctx, rawToken)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSessionRequired, err)
	}

	user, err := s.users.FindByID(ctx, session.UserID)
	if err != nil {
		return nil, fmt.Errorf("lookup session user: %w", err)
	}
	if user == nil {
		return nil, ErrSessionRequired
	}

	actor := &domain.Actor{
		UserID:    user.ID,
		SessionID: session.ID,
		IsAdmin:   user.IsAdmin,
		Scopes:    []string{},
		Kind:      domain.ActorKindSession,
		Origin:    domain.ActorOriginRemote,
	}

	return actor, nil
}

// GetCurrentUser returns the user by ID, or an error if not found.
func (s *Service) GetCurrentUser(ctx context.Context, userID uuid.UUID) (*domain.User, error) {
	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("lookup user: %w", err)
	}
	if user == nil {
		return nil, fmt.Errorf("%w: user %v not found", ErrSessionRequired, userID)
	}
	return user, nil
}
