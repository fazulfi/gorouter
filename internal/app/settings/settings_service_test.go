package settings

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"gorouter/internal/app/tx"
	"gorouter/internal/domain/auth"
	"gorouter/internal/domain/settings"

	"github.com/google/uuid"
)

type fakeSettingsRepo struct {
	mu       sync.Mutex
	values   map[string]settings.Setting
	getErr   error
	setErr   error
	wipeErr  error
	wipes    int
	reserved map[string]bool
}

func newFakeSettingsRepo() *fakeSettingsRepo {
	return &fakeSettingsRepo{
		values:   map[string]settings.Setting{},
		reserved: map[string]bool{},
	}
}

func (f *fakeSettingsRepo) Get(_ context.Context, key string) (*settings.Setting, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.reserved[key] {
		return nil, settings.ErrReservedKey
	}
	s, ok := f.values[key]
	if !ok {
		return nil, settings.ErrSettingNotFound
	}
	cp := s
	cp.Value = append(json.RawMessage(nil), s.Value...)
	return &cp, nil
}

func (f *fakeSettingsRepo) Set(_ context.Context, s *settings.Setting) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.setErr != nil {
		return f.setErr
	}
	if f.reserved[s.Key] {
		return settings.ErrReservedKey
	}
	cp := *s
	cp.Value = append(json.RawMessage(nil), s.Value...)
	f.values[s.Key] = cp
	return nil
}

func (f *fakeSettingsRepo) List(_ context.Context) ([]settings.Setting, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]settings.Setting, 0, len(f.values))
	for _, s := range f.values {
		if !f.reserved[s.Key] {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakeSettingsRepo) Wipe(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.wipeErr != nil {
		return f.wipeErr
	}
	f.wipes++
	for k := range f.values {
		if !f.reserved[k] {
			delete(f.values, k)
		}
	}
	return nil
}

type fakeAuditLog struct {
	mu      sync.Mutex
	entries []tx.AuditLogEntry
	err     error
}

func (f *fakeAuditLog) Create(_ context.Context, entry *tx.AuditLogEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	cp := *entry
	cp.Details = append(json.RawMessage(nil), entry.Details...)
	f.entries = append(f.entries, cp)
	return nil
}

type fakeSettingsScope struct {
	backing    *fakeSettingsRepo
	audit      *fakeAuditLog
	mu         sync.Mutex
	commits    int
	rollbacks  int
	commitErr  error
	staged     map[string]settings.Setting
	dirty      bool
	wipeStaged bool
}

func newFakeSettingsScope() *fakeSettingsScope {
	return &fakeSettingsScope{
		backing: newFakeSettingsRepo(),
		audit:   &fakeAuditLog{},
		staged:  map[string]settings.Setting{},
	}
}

type stagedSettingsRepo struct{ scope *fakeSettingsScope }

func (r *stagedSettingsRepo) Get(ctx context.Context, key string) (*settings.Setting, error) {
	r.scope.mu.Lock()
	defer r.scope.mu.Unlock()
	if r.scope.dirty {
		if s, ok := r.scope.staged[key]; ok {
			cp := s
			cp.Value = append(json.RawMessage(nil), s.Value...)
			return &cp, nil
		}
	}
	return r.scope.backing.Get(ctx, key)
}

func (r *stagedSettingsRepo) Set(_ context.Context, s *settings.Setting) error {
	r.scope.mu.Lock()
	defer r.scope.mu.Unlock()
	if s.Key == "pricing:overrides" {
		return settings.ErrReservedKey
	}
	cp := *s
	cp.Value = append(json.RawMessage(nil), s.Value...)
	r.scope.staged[s.Key] = cp
	r.scope.dirty = true
	return nil
}

func (r *stagedSettingsRepo) List(ctx context.Context) ([]settings.Setting, error) {
	r.scope.mu.Lock()
	defer r.scope.mu.Unlock()
	merged := map[string]settings.Setting{}
	list, _ := r.scope.backing.List(ctx)
	for _, s := range list {
		merged[s.Key] = s
	}
	for k, s := range r.scope.staged {
		merged[k] = s
	}
	out := make([]settings.Setting, 0, len(merged))
	for _, s := range merged {
		out = append(out, s)
	}
	return out, nil
}

func (r *stagedSettingsRepo) Wipe(ctx context.Context) error {
	r.scope.mu.Lock()
	defer r.scope.mu.Unlock()
	r.scope.staged = map[string]settings.Setting{}
	r.scope.dirty = true
	r.scope.wipeStaged = true
	return nil
}

func (s *fakeSettingsScope) Settings() settings.SettingsRepository {
	return &stagedSettingsRepo{scope: s}
}
func (s *fakeSettingsScope) AuditLog() tx.AuditLogRepository { return s.audit }

func (s *fakeSettingsScope) Commit(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.commitErr != nil {
		return s.commitErr
	}
	if s.dirty {
		for k, v := range s.staged {
			_ = s.backing.Set(context.Background(), &v)
			_ = k
		}
		s.backing.reserved["pricing:overrides"] = true
		s.staged = map[string]settings.Setting{}
		s.dirty = false
		s.wipeStaged = false
	}
	s.commits++
	return nil
}

func (s *fakeSettingsScope) Rollback(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.staged = map[string]settings.Setting{}
	s.dirty = false
	s.wipeStaged = false
	s.rollbacks++
	return nil
}

var _ Scope = (*fakeSettingsScope)(nil)

func TestSettingsService_Get(t *testing.T) {
	t.Parallel()

	t.Run("actor required", func(t *testing.T) {
		svc := NewSettingsService(ScopeBeginnerFunc(func(context.Context) (Scope, error) {
			return newFakeSettingsScope(), nil
		}))
		if _, err := svc.Get(context.Background(), nil, "rtkEnabled"); !errors.Is(err, ErrActorRequired) {
			t.Errorf("err = %v, want ErrActorRequired", err)
		}
	})

	t.Run("returns stored setting", func(t *testing.T) {
		scope := newFakeSettingsScope()
		_ = scope.backing.Set(context.Background(), &settings.Setting{Key: "rtkEnabled", Value: json.RawMessage(`true`)})
		svc := NewSettingsService(ScopeBeginnerFunc(func(context.Context) (Scope, error) { return scope, nil }))
		actor := &auth.Actor{UserID: uuid.New(), IsAdmin: true}
		got, err := svc.Get(context.Background(), actor, "rtkEnabled")
		if err != nil {
			t.Fatal(err)
		}
		if string(got.Value) != "true" {
			t.Errorf("value = %s", got.Value)
		}
	})

	t.Run("empty key refused", func(t *testing.T) {
		svc := NewSettingsService(ScopeBeginnerFunc(func(context.Context) (Scope, error) {
			return newFakeSettingsScope(), nil
		}))
		if _, err := svc.Get(context.Background(), &auth.Actor{UserID: uuid.New()}, ""); err == nil {
			t.Error("empty key accepted")
		}
	})
}

func TestSettingsService_List(t *testing.T) {
	t.Parallel()

	t.Run("actor required", func(t *testing.T) {
		svc := NewSettingsService(ScopeBeginnerFunc(func(context.Context) (Scope, error) {
			return newFakeSettingsScope(), nil
		}))
		if _, err := svc.List(context.Background(), nil); !errors.Is(err, ErrActorRequired) {
			t.Errorf("err = %v, want ErrActorRequired", err)
		}
	})

	t.Run("returns all stored settings", func(t *testing.T) {
		scope := newFakeSettingsScope()
		for key, raw := range map[string]string{
			"rtkEnabled": "true",
			"enable_mcp": "false",
			"custom-key": `{"nested":1}`,
		} {
			_ = scope.backing.Set(context.Background(), &settings.Setting{Key: key, Value: json.RawMessage(raw)})
		}
		svc := NewSettingsService(ScopeBeginnerFunc(func(context.Context) (Scope, error) { return scope, nil }))
		rows, err := svc.List(context.Background(), &auth.Actor{UserID: uuid.New(), IsAdmin: true})
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		for _, s := range rows {
			got[s.Key] = string(s.Value)
		}
		if len(got) != 3 || got["rtkEnabled"] != "true" || got["enable_mcp"] != "false" || got["custom-key"] != `{"nested":1}` {
			t.Errorf("listed settings = %v", got)
		}
	})
}

func TestSettingsService_BeginError(t *testing.T) {
	t.Parallel()
	beginErr := errors.New("begin down")
	svc := NewSettingsService(ScopeBeginnerFunc(func(context.Context) (Scope, error) {
		return nil, beginErr
	}))
	actor := &auth.Actor{UserID: uuid.New(), IsAdmin: true}
	for name, call := range map[string]func() error{
		"Get":  func() error { _, err := svc.Get(context.Background(), actor, "k"); return err },
		"List": func() error { _, err := svc.List(context.Background(), actor); return err },
		"Set":  func() error { _, err := svc.Set(context.Background(), actor, "k", json.RawMessage(`1`)); return err },
	} {
		t.Run(name, func(t *testing.T) {
			if err := call(); !errors.Is(err, beginErr) {
				t.Errorf("err = %v, want begin error", err)
			}
		})
	}
}

func TestSettingsService_Set(t *testing.T) {
	t.Parallel()

	t.Run("actor required", func(t *testing.T) {
		svc := NewSettingsService(ScopeBeginnerFunc(func(context.Context) (Scope, error) {
			return newFakeSettingsScope(), nil
		}))
		if _, err := svc.Set(context.Background(), nil, "k", json.RawMessage(`1`)); !errors.Is(err, ErrActorRequired) {
			t.Errorf("err = %v, want ErrActorRequired", err)
		}
	})

	t.Run("typed key contract enforced", func(t *testing.T) {
		svc := NewSettingsService(ScopeBeginnerFunc(func(context.Context) (Scope, error) {
			return newFakeSettingsScope(), nil
		}))
		actor := &auth.Actor{UserID: uuid.New(), IsAdmin: true}
		if _, err := svc.Set(context.Background(), actor, settings.KeyRTKEnabled, json.RawMessage(`"yes"`)); err == nil {
			t.Error("non-boolean rtkEnabled accepted")
		}
		if _, err := svc.Set(context.Background(), actor, "enable_tunnel", json.RawMessage(`{"x":1}`)); err == nil {
			t.Error("non-boolean host flag accepted")
		}
	})

	t.Run("invalid JSON refused", func(t *testing.T) {
		svc := NewSettingsService(ScopeBeginnerFunc(func(context.Context) (Scope, error) {
			return newFakeSettingsScope(), nil
		}))
		if _, err := svc.Set(context.Background(), &auth.Actor{UserID: uuid.New()}, "k", json.RawMessage(`{not json`)); err == nil {
			t.Error("invalid JSON accepted")
		}
	})

	t.Run("reserved key refused", func(t *testing.T) {
		svc := NewSettingsService(ScopeBeginnerFunc(func(context.Context) (Scope, error) {
			return newFakeSettingsScope(), nil
		}))
		if _, err := svc.Set(context.Background(), &auth.Actor{UserID: uuid.New()}, "pricing:overrides", json.RawMessage(`[]`)); !errors.Is(err, settings.ErrReservedKey) {
			t.Errorf("err = %v, want ErrReservedKey", err)
		}
	})

	t.Run("audits sanitized before and after diff", func(t *testing.T) {
		scope := newFakeSettingsScope()
		_ = scope.backing.Set(context.Background(), &settings.Setting{
			Key: "provider-secret", Value: json.RawMessage(`{"api_key_value":"SK-A1!"}`),
		})
		svc := NewSettingsService(ScopeBeginnerFunc(func(context.Context) (Scope, error) { return scope, nil }))
		actor := &auth.Actor{UserID: uuid.New(), IsAdmin: true}
		if _, err := svc.Set(context.Background(), actor, "provider-secret",
			json.RawMessage(`{"api_key_value":"SK-B2!"}`)); err != nil {
			t.Fatal(err)
		}
		if len(scope.audit.entries) != 1 {
			t.Fatalf("audit entries = %d, want 1", len(scope.audit.entries))
		}
		entry := scope.audit.entries[0]
		if entry.Action != "settings.set" || entry.ResourceType != "setting" {
			t.Errorf("entry = %+v", entry)
		}
		if entry.ActorID == nil || *entry.ActorID != actor.UserID {
			t.Errorf("actor = %v", entry.ActorID)
		}
		details := string(entry.Details)
		if strings.Contains(details, "SK-A1!") || strings.Contains(details, "SK-B2!") {
			t.Errorf("raw credential leaked into audit details: %s", details)
		}
		if !strings.Contains(details, "[REDACTED]") {
			t.Errorf("audit details not redacted: %s", details)
		}
		var parsed map[string]any
		if err := json.Unmarshal(entry.Details, &parsed); err != nil {
			t.Fatalf("audit details must stay valid JSON: %v (%s)", err, details)
		}
		if parsed["key"] != "provider-secret" {
			t.Errorf("audit key = %v", parsed["key"])
		}
		if parsed["before"] == nil || parsed["after"] == nil {
			t.Errorf("before/after missing: %v", parsed)
		}
	})

	t.Run("first set audits nil before", func(t *testing.T) {
		scope := newFakeSettingsScope()
		svc := NewSettingsService(ScopeBeginnerFunc(func(context.Context) (Scope, error) { return scope, nil }))
		if _, err := svc.Set(context.Background(), &auth.Actor{UserID: uuid.New()}, "fresh-key", json.RawMessage(`1`)); err != nil {
			t.Fatal(err)
		}
		var parsed map[string]any
		if err := json.Unmarshal(scope.audit.entries[0].Details, &parsed); err != nil {
			t.Fatal(err)
		}
		if parsed["before"] != nil {
			t.Errorf("first set before = %v, want nil", parsed["before"])
		}
	})

	t.Run("audit failure rolls back the write", func(t *testing.T) {
		scope := newFakeSettingsScope()
		scope.audit.err = errors.New("audit down")
		svc := NewSettingsService(ScopeBeginnerFunc(func(context.Context) (Scope, error) { return scope, nil }))
		actor := &auth.Actor{UserID: uuid.New(), IsAdmin: true}
		if _, err := svc.Set(context.Background(), actor, "rollback-key", json.RawMessage(`1`)); err == nil {
			t.Fatal("expected audit failure")
		}
		if _, err := scope.backing.Get(context.Background(), "rollback-key"); !errors.Is(err, settings.ErrSettingNotFound) {
			t.Errorf("setting visible after failed tx: %v", err)
		}
		if scope.rollbacks == 0 {
			t.Error("rollback not recorded")
		}
	})

	t.Run("stored setting returned", func(t *testing.T) {
		scope := newFakeSettingsScope()
		svc := NewSettingsService(ScopeBeginnerFunc(func(context.Context) (Scope, error) { return scope, nil }))
		actor := &auth.Actor{UserID: uuid.New(), IsAdmin: true}
		stored, err := svc.Set(context.Background(), actor, "enable_pxpipe", json.RawMessage(`false`))
		if err != nil {
			t.Fatal(err)
		}
		if stored.Key != "enable_pxpipe" || string(stored.Value) != "false" {
			t.Errorf("stored = %+v", stored)
		}
	})
}
