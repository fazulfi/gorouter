package repositories

import (
	"context"
	"testing"
	"time"

	"gorouter/internal/domain/oauth"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var ctxbg = context.Background()

func TestOAuthRepo_CreateAndFind(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	pid := uuid.New()
	now := time.Now().UTC()
	state := uuid.New().String()

	tx := &mockTx{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
			return &mockRow{
				vals: []interface{}{
					id, pid, nil, strP(state), strP("verifier"), strP("http://localhost:0/cb"),
					string(oauth.FlowOpenAI), string(oauth.MechanismAuthCodePKCE), string(oauth.OAuthStatePending),
					nil, nil, nil, nil, nil, nil, nil,
					now.Add(10 * time.Minute), nil, now, now,
				},
			}
		},
		execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}

	repo := NewOAuthRepo(tx)
	s := &oauth.Session{
		ID:           id,
		ProviderID:   pid,
		FlowID:       oauth.FlowOpenAI,
		Mechanism:    oauth.MechanismAuthCodePKCE,
		State:        state,
		CodeVerifier: "verifier",
		Status:       oauth.OAuthStatePending,
		ExpiresAt:    now.Add(10 * time.Minute),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := repo.Create(ctxbg, s); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := repo.FindByID(ctxbg, id)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got == nil {
		t.Fatal("FindByID returned nil")
	}
	if got.FlowID != oauth.FlowOpenAI {
		t.Errorf("FlowID = %s, want %s", got.FlowID, oauth.FlowOpenAI)
	}
}

func TestOAuthRepo_FindByState(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	state := uuid.New().String()

	tx := &mockTx{
		queryRowFn: func(_ context.Context, _ string, args ...interface{}) pgx.Row {
			if len(args) > 0 && args[0] == state {
				return &mockRow{
					vals: []interface{}{
						id, uuid.New(), nil, strP(state), nil, nil,
						string(oauth.FlowCodex), string(oauth.MechanismAuthCodePKCE), string(oauth.OAuthStatePending),
						nil, nil, nil, nil, nil, nil, nil,
						time.Now().Add(5 * time.Minute), nil, time.Now(), time.Now(),
					},
				}
			}
			return &mockRow{err: pgx.ErrNoRows}
		},
	}

	repo := NewOAuthRepo(tx)
	got, err := repo.FindByState(ctxbg, state)
	if err != nil {
		t.Fatalf("FindByState: %v", err)
	}
	if got == nil {
		t.Fatal("FindByState returned nil")
	}
	if got.ID != id {
		t.Errorf("ID = %v, want %v", got.ID, id)
	}
}

func TestOAuthRepo_CancelPending(t *testing.T) {
	t.Parallel()
	tx := &mockTx{
		execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("UPDATE 2"), nil
		},
	}
	repo := NewOAuthRepo(tx)
	n, err := repo.CancelPending(ctxbg)
	if err != nil {
		t.Fatalf("CancelPending: %v", err)
	}
	if n != 2 {
		t.Errorf("got %d, want 2", n)
	}
}

func TestOAuthRepo_UpdateStatus(t *testing.T) {
	t.Parallel()
	id := uuid.New()

	tx := &mockTx{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
			return &mockRow{
				vals: []interface{}{
					id, uuid.New(), nil, strP("state"), nil, nil,
					string(oauth.FlowXAI), string(oauth.MechanismAuthCodePKCE), string(oauth.OAuthStateFailed),
					strP("err"), nil, nil, nil, nil, nil, nil,
					time.Now(), nil, time.Now(), time.Now(),
				},
			}
		},
		execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}

	repo := NewOAuthRepo(tx)
	if err := repo.UpdateStatus(ctxbg, id, oauth.OAuthStateFailed, strP("err")); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	got, err := repo.FindByID(ctxbg, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != oauth.OAuthStateFailed {
		t.Errorf("got %s, want %s", got.Status, oauth.OAuthStateFailed)
	}
}

func TestOAuthRepo_Complete(t *testing.T) {
	t.Parallel()
	id := uuid.New()

	tx := &mockTx{
		queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
			return &mockRow{
				vals: []interface{}{
					id, uuid.New(), nil, strP("state"), nil, nil,
					string(oauth.FlowGitHub), string(oauth.MechanismDeviceCode), string(oauth.OAuthStateCompleted),
					nil, strP("hash123"), strP("refresh456"), timePtr(time.Now().Add(1 * time.Hour)),
					nil, nil, nil,
					time.Now(), timePtr(time.Now()), time.Now(), time.Now(),
				},
			}
		},
		execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}

	repo := NewOAuthRepo(tx)
	if err := repo.Complete(ctxbg, id, "hash123", "refresh456", time.Now().Add(1*time.Hour)); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	got, err := repo.FindByID(ctxbg, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != oauth.OAuthStateCompleted {
		t.Errorf("got %s, want %s", got.Status, oauth.OAuthStateCompleted)
	}
}

func timePtr(t time.Time) *time.Time { return &t }
