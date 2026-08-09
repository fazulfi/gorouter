package settings

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"gorouter/internal/app/tx"
	"gorouter/internal/domain/auth"

	"github.com/google/uuid"
)

type fakeAuditQueryRepo struct {
	entries []tx.AuditEntry
	listErr error
}

func (f *fakeAuditQueryRepo) List(_ context.Context, _ tx.AuditFilters, _ tx.AuditPage) ([]tx.AuditEntry, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.entries, nil
}

func (f *fakeAuditQueryRepo) Export(_ context.Context, _ tx.AuditFilters) ([]tx.AuditEntry, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.entries, nil
}

type fakeAuditQueryScope struct {
	repo *fakeAuditQueryRepo
}

func (s *fakeAuditQueryScope) AuditQuery() tx.AuditLogQueryRepository { return s.repo }
func (s *fakeAuditQueryScope) Commit(context.Context) error           { return nil }
func (s *fakeAuditQueryScope) Rollback(context.Context) error         { return nil }

var _ AuditQueryScope = (*fakeAuditQueryScope)(nil)

func TestAuditQueryService_List(t *testing.T) {
	t.Parallel()

	t.Run("actor required", func(t *testing.T) {
		svc := NewAuditQueryService(AuditQueryScopeBeginnerFunc(func(context.Context) (AuditQueryScope, error) {
			return &fakeAuditQueryScope{repo: &fakeAuditQueryRepo{}}, nil
		}))
		if _, err := svc.List(context.Background(), nil, tx.AuditFilters{}, tx.AuditPage{}); !errors.Is(err, ErrAuditActorRequired) {
			t.Errorf("err = %v, want ErrAuditActorRequired", err)
		}
	})

	t.Run("passes filters and page to the select-only repo", func(t *testing.T) {
		got := &fakeAuditQueryRepo{entries: []tx.AuditEntry{{ID: uuid.New(), Action: "settings.set", ActorKind: "user"}}}
		svc := NewAuditQueryService(AuditQueryScopeBeginnerFunc(func(context.Context) (AuditQueryScope, error) {
			return &fakeAuditQueryScope{repo: got}, nil
		}))
		actor := &auth.Actor{UserID: uuid.New(), IsAdmin: true}
		action := "backup.generate"
		entries, err := svc.List(context.Background(), actor, tx.AuditFilters{Action: &action}, tx.AuditPage{Number: 3, Size: 7})
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0].Action != "settings.set" {
			t.Fatalf("entries = %+v", entries)
		}
	})
}

func TestAuditQueryService_Export(t *testing.T) {
	t.Parallel()

	t.Run("actor required", func(t *testing.T) {
		svc := NewAuditQueryService(AuditQueryScopeBeginnerFunc(func(context.Context) (AuditQueryScope, error) {
			return &fakeAuditQueryScope{repo: &fakeAuditQueryRepo{}}, nil
		}))
		if _, err := svc.Export(context.Background(), nil, tx.AuditFilters{}); !errors.Is(err, ErrAuditActorRequired) {
			t.Errorf("err = %v, want ErrAuditActorRequired", err)
		}
	})

	t.Run("redacts credential-shaped details while keeping valid JSON", func(t *testing.T) {
		repo := &fakeAuditQueryRepo{entries: []tx.AuditEntry{
			{
				ID: uuid.New(), Action: "provider.update", ResourceType: "provider",
				Details:    []byte(`{"api_key_value":"SK-A1!","ok":true}`),
				OccurredAt: time.Now().UTC(),
			},
			{
				ID: uuid.New(), Action: "oauth.token", ResourceType: "oauth",
				Details:    []byte(`{"refresh_token":"YA-SYN!"}`),
				OccurredAt: time.Now().UTC(),
			},
			{
				ID: uuid.New(), Action: "backup.generate", ResourceType: "backup",
				Details:    []byte(`{"sha256":"aaaabbbbccccdddd"}`),
				OccurredAt: time.Now().UTC(),
			},
		}}
		svc := NewAuditQueryService(AuditQueryScopeBeginnerFunc(func(context.Context) (AuditQueryScope, error) {
			return &fakeAuditQueryScope{repo: repo}, nil
		}))
		entries, err := svc.Export(context.Background(), &auth.Actor{UserID: uuid.New(), IsAdmin: true}, tx.AuditFilters{})
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 3 {
			t.Fatalf("entries = %d, want 3", len(entries))
		}
		joined := strings.Join([]string{string(entries[0].Details), string(entries[1].Details), string(entries[2].Details)}, "\n")
		if strings.Contains(joined, "SK-A1!") || strings.Contains(joined, "YA-SYN!") {
			t.Errorf("raw credential leaked into export: %s", joined)
		}
		if !strings.Contains(joined, "[REDACTED]") {
			t.Errorf("export not redacted: %s", joined)
		}
		for _, e := range entries {
			if len(e.Details) > 0 && !validJSON(e.Details) {
				t.Errorf("exported details must stay valid JSON: %s", e.Details)
			}
		}
		// Non-secret details pass through unchanged.
		if string(entries[2].Details) != `{"sha256":"aaaabbbbccccdddd"}` {
			t.Errorf("non-secret details altered: %s", entries[2].Details)
		}
	})

	t.Run("does not mutate repository rows", func(t *testing.T) {
		raw := []byte(`{"api_key_value":"SK-A1!"}`)
		repo := &fakeAuditQueryRepo{entries: []tx.AuditEntry{{ID: uuid.New(), Action: "a", Details: raw}}}
		svc := NewAuditQueryService(AuditQueryScopeBeginnerFunc(func(context.Context) (AuditQueryScope, error) {
			return &fakeAuditQueryScope{repo: repo}, nil
		}))
		if _, err := svc.Export(context.Background(), &auth.Actor{UserID: uuid.New()}, tx.AuditFilters{}); err != nil {
			t.Fatal(err)
		}
		if string(repo.entries[0].Details) != string(raw) {
			t.Error("export mutated the repository row")
		}
	})
}

func TestAuditQueryService_BeginError(t *testing.T) {
	t.Parallel()
	beginErr := errors.New("begin down")
	svc := NewAuditQueryService(AuditQueryScopeBeginnerFunc(func(context.Context) (AuditQueryScope, error) {
		return nil, beginErr
	}))
	actor := &auth.Actor{UserID: uuid.New(), IsAdmin: true}
	for name, call := range map[string]func() error{
		"List": func() error {
			_, err := svc.List(context.Background(), actor, tx.AuditFilters{}, tx.AuditPage{})
			return err
		},
		"Export": func() error { _, err := svc.Export(context.Background(), actor, tx.AuditFilters{}); return err },
	} {
		t.Run(name, func(t *testing.T) {
			if err := call(); !errors.Is(err, beginErr) {
				t.Errorf("err = %v, want begin error", err)
			}
		})
	}
}

func validJSON(raw []byte) bool {
	var probe interface{}
	return json.Unmarshal(raw, &probe) == nil
}
