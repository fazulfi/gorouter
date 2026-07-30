package auth

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	domain "gorouter/internal/domain/auth"

	"github.com/google/uuid"
)

// mockUserRepo implements domain.UserRepository in-memory for testing.
type mockUserRepo struct {
	mu      sync.RWMutex
	users   map[uuid.UUID]*domain.User
	byEmail map[string]*domain.User
}

func newMockUserRepo() *mockUserRepo {
	return &mockUserRepo{
		users:   make(map[uuid.UUID]*domain.User),
		byEmail: make(map[string]*domain.User),
	}
}

func (r *mockUserRepo) FindByID(_ context.Context, id uuid.UUID) (*domain.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	u, ok := r.users[id]
	if !ok {
		return nil, nil
	}
	return u, nil
}

func (r *mockUserRepo) FindByEmail(_ context.Context, email string) (*domain.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	u, ok := r.byEmail[email]
	if !ok {
		return nil, nil
	}
	return u, nil
}

func (r *mockUserRepo) Create(_ context.Context, u *domain.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.users[u.ID] = u
	r.byEmail[u.Email] = u
	return nil
}

func (r *mockUserRepo) Update(_ context.Context, u *domain.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.users[u.ID] = u
	r.byEmail[u.Email] = u
	return nil
}

func (r *mockUserRepo) UpdateLastLogin(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.users[id]
	if !ok {
		return nil
	}
	now := time.Now()
	u.LastLoginAt = &now
	return nil
}

// mockSessionRepo implements domain.SessionRepository in-memory for testing.
type mockSessionRepo struct {
	mu       sync.RWMutex
	sessions map[uuid.UUID]*domain.Session
	byHash   map[string]*domain.Session
}

func newMockSessionRepo() *mockSessionRepo {
	return &mockSessionRepo{
		sessions: make(map[uuid.UUID]*domain.Session),
		byHash:   make(map[string]*domain.Session),
	}
}

func (r *mockSessionRepo) FindByID(_ context.Context, id uuid.UUID) (*domain.Session, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sessions[id]
	if !ok {
		return nil, nil
	}
	return s, nil
}

func (r *mockSessionRepo) FindByTokenHash(_ context.Context, hash string) (*domain.Session, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.byHash[hash]
	if !ok {
		return nil, nil
	}
	return s, nil
}

func (r *mockSessionRepo) Create(_ context.Context, s *domain.Session) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[s.ID] = s
	r.byHash[s.TokenHash] = s
	return nil
}

func (r *mockSessionRepo) Revoke(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[id]
	if !ok {
		return nil
	}
	now := time.Now()
	s.RevokedAt = &now
	return nil
}

func (r *mockSessionRepo) UpdateExpiry(_ context.Context, id uuid.UUID, expiresAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[id]
	if !ok {
		return nil
	}
	s.ExpiresAt = expiresAt
	return nil
}

func (r *mockSessionRepo) DeleteExpired(_ context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	for id, s := range r.sessions {
		if s.ExpiresAt.Before(now) && s.RevokedAt == nil {
			delete(r.sessions, id)
			delete(r.byHash, s.TokenHash)
		}
	}
	return nil
}

func seededUser(t *testing.T, repo *mockUserRepo) *domain.User {
	t.Helper()
	hashed, err := domain.NewPasswordService().HashPassword("password123")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	u := &domain.User{
		ID:           uuid.New(),
		Email:        "test@example.com",
		PasswordHash: hashed,
		IsActive:     true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := repo.Create(context.Background(), u); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return u
}

func seededAdmin(t *testing.T, repo *mockUserRepo) *domain.User {
	t.Helper()
	hashed, err := domain.NewPasswordService().HashPassword("adminpass")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	u := &domain.User{
		ID:           uuid.New(),
		Email:        "admin@example.com",
		PasswordHash: hashed,
		IsAdmin:      true,
		IsActive:     true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := repo.Create(context.Background(), u); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	return u
}

func TestAuthService_Login_Success(t *testing.T) {
	userRepo := newMockUserRepo()
	sessionRepo := newMockSessionRepo()
	passwordSvc := domain.NewPasswordService()
	sessionSvc := domain.NewSessionService(sessionRepo, 24*time.Hour)
	authSvc := NewAuthService(passwordSvc, sessionSvc, userRepo, sessionRepo)

	seededUser(t, userRepo)

	session, rawToken, err := authSvc.Login(context.Background(), "test@example.com", "password123")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if session == nil {
		t.Fatal("expected non-nil session")
	}
	if rawToken == "" {
		t.Fatal("expected non-empty raw token")
	}
}

func TestAuthService_Login_WrongPassword(t *testing.T) {
	userRepo := newMockUserRepo()
	sessionRepo := newMockSessionRepo()
	passwordSvc := domain.NewPasswordService()
	sessionSvc := domain.NewSessionService(sessionRepo, 24*time.Hour)
	authSvc := NewAuthService(passwordSvc, sessionSvc, userRepo, sessionRepo)

	seededUser(t, userRepo)

	_, _, err := authSvc.Login(context.Background(), "test@example.com", "wrongpassword")
	if err == nil {
		t.Fatal("expected error for wrong password, got nil")
	}
}

func TestAuthService_Login_UserNotFound(t *testing.T) {
	userRepo := newMockUserRepo()
	sessionRepo := newMockSessionRepo()
	passwordSvc := domain.NewPasswordService()
	sessionSvc := domain.NewSessionService(sessionRepo, 24*time.Hour)
	authSvc := NewAuthService(passwordSvc, sessionSvc, userRepo, sessionRepo)

	_, _, err := authSvc.Login(context.Background(), "nonexistent@example.com", "password123")
	if err == nil {
		t.Fatal("expected error for nonexistent user, got nil")
	}
}

func TestAuthService_Login_InactiveUser(t *testing.T) {
	userRepo := newMockUserRepo()
	sessionRepo := newMockSessionRepo()
	passwordSvc := domain.NewPasswordService()
	sessionSvc := domain.NewSessionService(sessionRepo, 24*time.Hour)
	authSvc := NewAuthService(passwordSvc, sessionSvc, userRepo, sessionRepo)

	u := seededUser(t, userRepo)
	u.IsActive = false
	userRepo.Update(context.Background(), u)

	_, _, err := authSvc.Login(context.Background(), "test@example.com", "password123")
	if err == nil {
		t.Fatal("expected error for inactive user, got nil")
	}
}

func TestAuthService_LoginAdmin_Success(t *testing.T) {
	userRepo := newMockUserRepo()
	sessionRepo := newMockSessionRepo()
	passwordSvc := domain.NewPasswordService()
	sessionSvc := domain.NewSessionService(sessionRepo, 24*time.Hour)
	authSvc := NewAuthService(passwordSvc, sessionSvc, userRepo, sessionRepo)

	seededAdmin(t, userRepo)

	session, rawToken, err := authSvc.LoginAdmin(context.Background(), "admin@example.com", "adminpass")
	if err != nil {
		t.Fatalf("LoginAdmin failed: %v", err)
	}
	if session == nil {
		t.Fatal("expected non-nil session")
	}
	if rawToken == "" {
		t.Fatal("expected non-empty raw token")
	}
}

func TestAuthService_LoginAdmin_NotAdmin(t *testing.T) {
	userRepo := newMockUserRepo()
	sessionRepo := newMockSessionRepo()
	passwordSvc := domain.NewPasswordService()
	sessionSvc := domain.NewSessionService(sessionRepo, 24*time.Hour)
	authSvc := NewAuthService(passwordSvc, sessionSvc, userRepo, sessionRepo)

	seededUser(t, userRepo)

	_, _, err := authSvc.LoginAdmin(context.Background(), "test@example.com", "password123")
	if err == nil {
		t.Fatal("expected error for non-admin user, got nil")
	}
}

func TestAuthService_Logout(t *testing.T) {
	userRepo := newMockUserRepo()
	sessionRepo := newMockSessionRepo()
	passwordSvc := domain.NewPasswordService()
	sessionSvc := domain.NewSessionService(sessionRepo, 24*time.Hour)
	authSvc := NewAuthService(passwordSvc, sessionSvc, userRepo, sessionRepo)

	u := seededUser(t, userRepo)
	session, _, err := authSvc.Login(context.Background(), u.Email, "password123")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	if err := authSvc.Logout(context.Background(), session.ID); err != nil {
		t.Fatalf("Logout failed: %v", err)
	}

	// Verify session is revoked
	stored, _ := sessionRepo.FindByID(context.Background(), session.ID)
	if stored.RevokedAt == nil {
		t.Fatal("expected session to be revoked after logout")
	}
}

func TestAuthService_ValidateSession(t *testing.T) {
	userRepo := newMockUserRepo()
	sessionRepo := newMockSessionRepo()
	passwordSvc := domain.NewPasswordService()
	sessionSvc := domain.NewSessionService(sessionRepo, 24*time.Hour)
	authSvc := NewAuthService(passwordSvc, sessionSvc, userRepo, sessionRepo)

	u := seededUser(t, userRepo)
	_, rawToken, err := authSvc.Login(context.Background(), u.Email, "password123")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	actor, err := authSvc.ValidateSession(context.Background(), rawToken)
	if err != nil {
		t.Fatalf("ValidateSession failed: %v", err)
	}
	if actor == nil {
		t.Fatal("expected non-nil actor")
	}
	if actor.UserID != u.ID {
		t.Errorf("expected UserID %v, got %v", u.ID, actor.UserID)
	}
	if actor.IsAdmin {
		t.Error("expected non-admin actor")
	}
}

func TestAuthService_ValidateSession_Admin(t *testing.T) {
	userRepo := newMockUserRepo()
	sessionRepo := newMockSessionRepo()
	passwordSvc := domain.NewPasswordService()
	sessionSvc := domain.NewSessionService(sessionRepo, 24*time.Hour)
	authSvc := NewAuthService(passwordSvc, sessionSvc, userRepo, sessionRepo)

	u := seededAdmin(t, userRepo)
	_, rawToken, err := authSvc.LoginAdmin(context.Background(), u.Email, "adminpass")
	if err != nil {
		t.Fatalf("LoginAdmin failed: %v", err)
	}

	actor, err := authSvc.ValidateSession(context.Background(), rawToken)
	if err != nil {
		t.Fatalf("ValidateSession failed: %v", err)
	}
	if actor == nil {
		t.Fatal("expected non-nil actor")
	}
	if !actor.IsAdmin {
		t.Error("expected admin actor")
	}
}

func TestAuthService_ValidateSession_Invalid(t *testing.T) {
	userRepo := newMockUserRepo()
	sessionRepo := newMockSessionRepo()
	passwordSvc := domain.NewPasswordService()
	sessionSvc := domain.NewSessionService(sessionRepo, 24*time.Hour)
	authSvc := NewAuthService(passwordSvc, sessionSvc, userRepo, sessionRepo)

	_, err := authSvc.ValidateSession(context.Background(), "invalid-token")
	if err == nil {
		t.Fatal("expected error for invalid session, got nil")
	}
}

func TestAuthService_GetCurrentUser(t *testing.T) {
	userRepo := newMockUserRepo()
	sessionRepo := newMockSessionRepo()
	passwordSvc := domain.NewPasswordService()
	sessionSvc := domain.NewSessionService(sessionRepo, 24*time.Hour)
	authSvc := NewAuthService(passwordSvc, sessionSvc, userRepo, sessionRepo)

	u := seededUser(t, userRepo)

	got, err := authSvc.GetCurrentUser(context.Background(), u.ID)
	if err != nil {
		t.Fatalf("GetCurrentUser failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil user")
	}
	if got.Email != u.Email {
		t.Errorf("expected Email %q, got %q", u.Email, got.Email)
	}
}

func TestAuthService_GetCurrentUser_NotFound(t *testing.T) {
	userRepo := newMockUserRepo()
	sessionRepo := newMockSessionRepo()
	passwordSvc := domain.NewPasswordService()
	sessionSvc := domain.NewSessionService(sessionRepo, 24*time.Hour)
	authSvc := NewAuthService(passwordSvc, sessionSvc, userRepo, sessionRepo)

	_, err := authSvc.GetCurrentUser(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected error for nonexistent user, got nil")
	}
}

func TestAuthService_ErrorTypes(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"ErrInvalidCredentials", ErrInvalidCredentials},
		{"ErrUserInactive", ErrUserInactive},
		{"ErrNotAdmin", ErrNotAdmin},
		{"ErrSessionRequired", ErrSessionRequired},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err == nil {
				t.Fatal("expected non-nil error")
			}
		})
	}
	// Verify they are distinct
	if errors.Is(ErrInvalidCredentials, ErrUserInactive) {
		t.Error("sentinel errors should not match each other")
	}
}
