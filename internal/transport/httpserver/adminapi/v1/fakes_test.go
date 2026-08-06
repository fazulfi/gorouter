package v1

import (
	"context"
	"encoding/json"
	"time"

	"gorouter/internal/domain/auth"
	"gorouter/internal/domain/jobs"
	"gorouter/internal/domain/keys"

	"github.com/google/uuid"
)

// recordingAuditor records audit calls for host-gate and projection tests.
type recordingAuditor struct {
	entries []auditRecord
}

type auditRecord struct {
	Actor  *auth.Actor
	Action string
	Target string
}

func (r *recordingAuditor) Audit(ctx context.Context, actor *auth.Actor, action, target string, before, after json.RawMessage) error {
	if r != nil {
		r.entries = append(r.entries, auditRecord{Actor: actor, Action: action, Target: target})
	}
	return nil
}

var _ Auditor = &recordingAuditor{}

// fakeKeysService satisfies KeysService for the projection tests.
type fakeKeysService struct{}

var _ KeysService = &fakeKeysService{}

func (fakeKeysService) List(ctx context.Context, userID uuid.UUID) ([]keys.APIKey, error) {
	return []keys.APIKey{{
		ID: uuid.New(), UserID: userID, Name: "alpha",
		KeyPrefix: "sk-", KeyHash: "deadbeef-hash-that-must-not-leak",
		CreatedAt: time.Now(),
	}}, nil
}

func (fakeKeysService) Get(ctx context.Context, id uuid.UUID) (*keys.APIKey, error) {
	return &keys.APIKey{ID: id, Name: "alpha", KeyPrefix: "sk-", CreatedAt: time.Now()}, nil
}

func (fakeKeysService) Create(ctx context.Context, userID uuid.UUID, name string, expiresAt *time.Time) (*keys.APIKey, string, error) {
	return &keys.APIKey{ID: uuid.New(), UserID: userID, Name: name, KeyPrefix: "sk-", CreatedAt: time.Now()}, "sk-test-secret", nil
}

func (fakeKeysService) Update(ctx context.Context, id uuid.UUID, name string, expiresAt *time.Time) (*keys.APIKey, error) {
	return &keys.APIKey{ID: id, Name: name, KeyPrefix: "sk-", CreatedAt: time.Now()}, nil
}

func (fakeKeysService) Revoke(ctx context.Context, id uuid.UUID) error { return nil }

// fakePATsService satisfies PATsService for the projection tests.
type fakePATsService struct{}

var _ PATsService = &fakePATsService{}

func (fakePATsService) List(ctx context.Context, userID uuid.UUID) ([]keys.PAT, error) {
	return []keys.PAT{{
		ID: uuid.New(), UserID: userID, TokenHash: "feedface-hash-that-must-not-leak",
		CreatedAt: time.Now(),
	}}, nil
}

func (fakePATsService) Get(ctx context.Context, id uuid.UUID) (*keys.PAT, error) {
	return &keys.PAT{ID: id, CreatedAt: time.Now()}, nil
}

func (fakePATsService) Create(ctx context.Context, userID uuid.UUID, description *string, expiresAt *time.Time) (*keys.PAT, string, error) {
	return &keys.PAT{ID: uuid.New(), UserID: userID, Description: description, CreatedAt: time.Now()}, "pat-test-secret", nil
}

func (fakePATsService) Revoke(ctx context.Context, id uuid.UUID) error { return nil }

// fakeJobsService satisfies JobsService and records the run-now actor.
type fakeJobsService struct {
	runNowActor *auth.Actor
	runNowType  string
}

var _ JobsService = &fakeJobsService{}

func (fakeJobsService) List(ctx context.Context) ([]jobs.Job, error) { return nil, nil }

func (fakeJobsService) History(ctx context.Context, jobType string) ([]jobs.Job, error) {
	return nil, nil
}

func (f *fakeJobsService) RunNow(ctx context.Context, actor *auth.Actor, jobType string) error {
	f.runNowActor = actor
	f.runNowType = jobType
	return nil
}
