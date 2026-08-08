// Package settings provides application services over the configuration
// key-value store: the settings service (audited Get/Set), the SELECT-only
// audit query service, and the manual configuration transfer service whose
// partial/destructive semantics follow the pinned upstream contract.
package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorouter/internal/app/tx"
	"gorouter/internal/domain/auth"
	"gorouter/internal/domain/settings"

	"github.com/google/uuid"
)

// Setting is an alias for domain settings.Setting.
type Setting = settings.Setting

// ErrActorRequired reports a settings operation without an actor. Every
// application service entry point accepts the actor and passes it to audit.
var ErrActorRequired = errors.New("settings: actor is required")

// Scope is the transaction-scoped persistence surface used by SettingsService.
// *tx.TxScope satisfies it, so the service can be wired directly to
// TransactionManager.Begin output.
type Scope interface {
	Settings() settings.SettingsRepository
	AuditLog() tx.AuditLogRepository
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

var _ Scope = (*tx.TxScope)(nil)

// ScopeBeginner begins a settings transaction scope. It is an interface so
// tests can drive the service with recording scopes.
type ScopeBeginner interface {
	Begin(ctx context.Context) (Scope, error)
}

// ScopeBeginnerFunc adapts a function to ScopeBeginner.
type ScopeBeginnerFunc func(ctx context.Context) (Scope, error)

// Begin implements ScopeBeginner.
func (f ScopeBeginnerFunc) Begin(ctx context.Context) (Scope, error) { return f(ctx) }

// SettingsService reads and writes configuration keys. Every Set validates
// the typed key contract, writes the value, and appends a sanitized
// before/after audit diff in the same transaction.
type SettingsService struct {
	beginner ScopeBeginner
}

// NewSettingsService creates a SettingsService over the given scope beginner.
func NewSettingsService(beginner ScopeBeginner) *SettingsService {
	return &SettingsService{beginner: beginner}
}

// Get returns the setting with the given key. Reserved keys owned by other
// repositories are refused.
func (s *SettingsService) Get(ctx context.Context, actor *auth.Actor, key string) (*settings.Setting, error) {
	if actor == nil {
		return nil, ErrActorRequired
	}
	if err := validateKey(key); err != nil {
		return nil, err
	}
	scope, err := s.beginner.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = scope.Rollback(ctx) }()
	setting, err := scope.Settings().Get(ctx, key)
	if err != nil {
		return nil, err
	}
	return setting, nil
}

// List returns every non-reserved setting in deterministic key order.
func (s *SettingsService) List(ctx context.Context, actor *auth.Actor) ([]settings.Setting, error) {
	if actor == nil {
		return nil, ErrActorRequired
	}
	scope, err := s.beginner.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = scope.Rollback(ctx) }()
	rows, err := scope.Settings().List(ctx)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// Set validates and stores the value for the given key. The typed key
// contract is enforced (rtkEnabled and host flags hold JSON booleans), and
// the before/after diff is audited sanitized in the same transaction.
func (s *SettingsService) Set(ctx context.Context, actor *auth.Actor, key string, value json.RawMessage) (*settings.Setting, error) {
	if actor == nil {
		return nil, ErrActorRequired
	}
	if err := validateKey(key); err != nil {
		return nil, err
	}
	if len(value) == 0 || !json.Valid(value) {
		return nil, errors.New("settings: value must be valid JSON")
	}
	if err := settings.ValidateTypedValue(key, value); err != nil {
		return nil, err
	}

	scope, err := s.beginner.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = scope.Rollback(ctx) }()

	before, err := scope.Settings().Get(ctx, key)
	if err != nil && !errors.Is(err, settings.ErrSettingNotFound) {
		return nil, fmt.Errorf("read before value: %w", err)
	}
	if errors.Is(err, settings.ErrSettingNotFound) {
		before = nil
	}

	if err := scope.Settings().Set(ctx, &settings.Setting{Key: key, Value: value}); err != nil {
		return nil, fmt.Errorf("set setting: %w", err)
	}
	if err := writeSetAudit(ctx, scope.AuditLog(), actor, key, before, value); err != nil {
		return nil, fmt.Errorf("audit settings set: %w", err)
	}

	stored, err := scope.Settings().Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("read stored setting: %w", err)
	}
	if err := scope.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return stored, nil
}

func validateKey(key string) error {
	if key == "" {
		return errors.New("settings: key must not be empty")
	}
	if _, reserved := settings.ReservedKeys[key]; reserved {
		return settings.ErrReservedKey
	}
	return nil
}

// writeSetAudit appends the sanitized before/after diff for a settings write.
func writeSetAudit(ctx context.Context, log tx.AuditLogRepository, actor *auth.Actor, key string, before *settings.Setting, after json.RawMessage) error {
	details := map[string]any{
		"key":   key,
		"after": json.RawMessage(sanitizeSettingValue(after)),
	}
	if before != nil {
		details["before"] = json.RawMessage(sanitizeSettingValue(before.Value))
	} else {
		details["before"] = nil
	}
	raw, err := json.Marshal(details)
	if err != nil {
		return err
	}
	actorID := actor.UserID
	entry := &tx.AuditLogEntry{
		ID:           uuid.New(),
		ActorID:      &actorID,
		Action:       "settings.set",
		ResourceType: "setting",
		Details:      raw,
		OccurredAt:   time.Now().UTC(),
	}
	return log.Create(ctx, entry)
}
