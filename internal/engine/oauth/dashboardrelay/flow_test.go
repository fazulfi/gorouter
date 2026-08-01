package dashboardrelay

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	domainoauth "gorouter/internal/domain/oauth"

	"github.com/google/uuid"
)

type updateCall struct {
	id     uuid.UUID
	status domainoauth.OAuthState
	detail *string
	called bool
}

type fakeRepo struct {
	sessions        map[string]*domainoauth.Session
	updateStatusErr error
	updates         []updateCall
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{sessions: make(map[string]*domainoauth.Session)}
}

func (f *fakeRepo) Create(_ context.Context, s *domainoauth.Session) error {
	f.sessions[s.State] = s
	return nil
}
func (f *fakeRepo) FindByID(_ context.Context, _ uuid.UUID) (*domainoauth.Session, error) {
	return nil, nil
}
func (f *fakeRepo) FindByState(_ context.Context, state string) (*domainoauth.Session, error) {
	s, ok := f.sessions[state]
	if !ok {
		return nil, nil
	}
	return s, nil
}
func (f *fakeRepo) FindPendingByProvider(_ context.Context, _ uuid.UUID) ([]domainoauth.Session, error) {
	return nil, nil
}
func (f *fakeRepo) UpdateStatus(_ context.Context, id uuid.UUID, status domainoauth.OAuthState, detail *string) error {
	f.updates = append(f.updates, updateCall{id: id, status: status, detail: detail, called: true})
	return f.updateStatusErr
}
func (f *fakeRepo) Complete(_ context.Context, _ uuid.UUID, _, _ string, _ time.Time) error {
	return nil
}
func (f *fakeRepo) CancelPending(_ context.Context) (int64, error) { return 0, nil }
func (f *fakeRepo) DeleteExpired(_ context.Context) (int64, error) { return 0, nil }
func (f *fakeRepo) Cleanup(_ context.Context) (int64, error)       { return 0, nil }

func pendingSession(state string, expiresIn time.Duration) *domainoauth.Session {
	now := time.Now()
	return &domainoauth.Session{
		ID:         uuid.New(),
		ProviderID: uuid.New(),
		FlowID:     domainoauth.FlowClaude,
		State:      state,
		Status:     domainoauth.OAuthStatePending,
		ExpiresAt:  now.Add(expiresIn),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

func TestCreateSession(t *testing.T) {
	repo := newFakeRepo()
	f := NewFlow(repo)
	ctx := context.Background()
	s, err := f.CreateSession(ctx, uuid.New(), domainoauth.FlowClaude, "state-1")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil session")
	}
	if s.Status != domainoauth.OAuthStatePending {
		t.Errorf("Status = %s, want pending", s.Status)
	}
	if s.State != "state-1" {
		t.Errorf("State = %s, want state-1", s.State)
	}
	if repo.sessions["state-1"] == nil {
		t.Error("session should be persisted")
	}
}

func TestHandleRelay_Success(t *testing.T) {
	repo := newFakeRepo()
	f := NewFlow(repo)
	ctx := context.Background()
	session := pendingSession("state-ok", 10*time.Minute)
	repo.sessions["state-ok"] = session

	got, err := f.HandleRelay(ctx, &RelayMessage{State: "state-ok", Code: "code-1"})
	if err != nil {
		t.Fatalf("HandleRelay: %v", err)
	}
	if got == nil {
		t.Fatal("expected session on success")
	}
	if got.Status != domainoauth.OAuthStateCompleted {
		t.Errorf("Status = %s, want completed", got.Status)
	}
	if len(repo.updates) != 0 {
		t.Errorf("no status update expected on success, got %d", len(repo.updates))
	}
}

func TestHandleRelay_Expired(t *testing.T) {
	repo := newFakeRepo()
	f := NewFlow(repo)
	ctx := context.Background()
	session := pendingSession("state-expired", -time.Minute)
	repo.sessions["state-expired"] = session

	_, err := f.HandleRelay(ctx, &RelayMessage{State: "state-expired", Code: "code-1"})
	if err == nil || !strings.Contains(err.Error(), "session expired") {
		t.Fatalf("HandleRelay expired = %v, want 'session expired'", err)
	}
	if len(repo.updates) != 1 || repo.updates[0].status != domainoauth.OAuthStateExpired {
		t.Fatalf("expected one UpdateStatus(expired), got %+v", repo.updates)
	}
}

func TestHandleRelay_ExpiredUpdateErrorObserved(t *testing.T) {
	repo := newFakeRepo()
	repo.updateStatusErr = errors.New("db down")
	f := NewFlow(repo)
	ctx := context.Background()
	session := pendingSession("state-expired2", -time.Minute)
	repo.sessions["state-expired2"] = session

	_, err := f.HandleRelay(ctx, &RelayMessage{State: "state-expired2", Code: "code-1"})
	if err == nil {
		t.Fatal("expected error for expired session")
	}
	if !strings.HasPrefix(err.Error(), "session expired") {
		t.Errorf("primary result overridden: %v", err)
	}
	if !strings.Contains(err.Error(), "db down") {
		t.Errorf("cleanup update error not observed: %v", err)
	}
}

func TestHandleRelay_RelayError(t *testing.T) {
	repo := newFakeRepo()
	f := NewFlow(repo)
	ctx := context.Background()
	session := pendingSession("state-err", 10*time.Minute)
	repo.sessions["state-err"] = session

	_, err := f.HandleRelay(ctx, &RelayMessage{State: "state-err", Error: "access_denied", ErrorDesc: "denied"})
	if err == nil || !strings.Contains(err.Error(), "relay error: access_denied") {
		t.Fatalf("HandleRelay error = %v, want 'relay error: access_denied'", err)
	}
	if len(repo.updates) != 1 || repo.updates[0].status != domainoauth.OAuthStateFailed {
		t.Fatalf("expected one UpdateStatus(failed), got %+v", repo.updates)
	}
	if repo.updates[0].detail == nil || *repo.updates[0].detail != "access_denied: denied" {
		t.Errorf("failure detail = %v, want 'access_denied: denied'", repo.updates[0].detail)
	}
}

func TestHandleRelay_RelayErrorUpdateErrorObserved(t *testing.T) {
	repo := newFakeRepo()
	repo.updateStatusErr = errors.New("db down")
	f := NewFlow(repo)
	ctx := context.Background()
	session := pendingSession("state-err2", 10*time.Minute)
	repo.sessions["state-err2"] = session

	_, err := f.HandleRelay(ctx, &RelayMessage{State: "state-err2", Error: "denied"})
	if err == nil {
		t.Fatal("expected error for relay error message")
	}
	if !strings.HasPrefix(err.Error(), "relay error: denied") {
		t.Errorf("primary result overridden: %v", err)
	}
	if !strings.Contains(err.Error(), "db down") {
		t.Errorf("cleanup update error not observed: %v", err)
	}
}

func TestHandleRelay_SessionNotFound(t *testing.T) {
	repo := newFakeRepo()
	f := NewFlow(repo)
	_, err := f.HandleRelay(context.Background(), &RelayMessage{State: "nope"})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("HandleRelay = %v, want session not found", err)
	}
}

func TestHandleRelay_TerminalSession(t *testing.T) {
	repo := newFakeRepo()
	f := NewFlow(repo)
	ctx := context.Background()
	session := pendingSession("state-terminal", 10*time.Minute)
	session.Status = domainoauth.OAuthStateCompleted
	repo.sessions["state-terminal"] = session

	_, err := f.HandleRelay(ctx, &RelayMessage{State: "state-terminal", Code: "code-1"})
	if err == nil || !strings.Contains(err.Error(), "terminal") {
		t.Fatalf("HandleRelay = %v, want terminal state error", err)
	}
}
