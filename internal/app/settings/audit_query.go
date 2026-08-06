package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"gorouter/internal/app/tx"
	"gorouter/internal/domain/auth"
	"gorouter/internal/shared/redact"
)

// ErrAuditActorRequired reports an audit query without an actor. Audit data
// is only readable by authenticated principals; job actors get status-only
// access elsewhere and never query the audit log.
var ErrAuditActorRequired = errors.New("settings: actor is required to query the audit log")

// AuditQueryScope is the transaction-scoped read surface used by
// AuditQueryService. *tx.TxScope satisfies it.
type AuditQueryScope interface {
	AuditQuery() tx.AuditLogQueryRepository
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

var _ AuditQueryScope = (*tx.TxScope)(nil)

// AuditQueryScopeBeginner begins an audit query transaction scope.
type AuditQueryScopeBeginner interface {
	Begin(ctx context.Context) (AuditQueryScope, error)
}

// AuditQueryScopeBeginnerFunc adapts a function to AuditQueryScopeBeginner.
type AuditQueryScopeBeginnerFunc func(ctx context.Context) (AuditQueryScope, error)

// Begin implements AuditQueryScopeBeginner.
func (f AuditQueryScopeBeginnerFunc) Begin(ctx context.Context) (AuditQueryScope, error) {
	return f(ctx)
}

// AuditQueryService reads the append-only audit log through the SELECT-only
// repository surface: deterministic filtered listing with offset pagination
// and full exports with a redaction pass. It never mutates or deletes audit
// rows; the runtime role holds INSERT and SELECT only.
type AuditQueryService struct {
	beginner AuditQueryScopeBeginner
}

// NewAuditQueryService creates an AuditQueryService over the given scope
// beginner.
func NewAuditQueryService(beginner AuditQueryScopeBeginner) *AuditQueryService {
	return &AuditQueryService{beginner: beginner}
}

// List returns one page of matching entries, newest first, with the id as
// the deterministic tiebreaker.
func (s *AuditQueryService) List(ctx context.Context, actor *auth.Actor, filters tx.AuditFilters, page tx.AuditPage) ([]tx.AuditEntry, error) {
	if actor == nil {
		return nil, ErrAuditActorRequired
	}
	scope, err := s.beginner.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = scope.Rollback(ctx) }()
	entries, err := scope.AuditQuery().List(ctx, filters, page)
	if err != nil {
		return nil, err
	}
	return entries, nil
}

// Export returns every matching entry with a redaction pass over the details
// payloads. The JSON structure is preserved; credential-shaped segments are
// masked. The returned slice is a copy; the repository rows are never
// touched.
func (s *AuditQueryService) Export(ctx context.Context, actor *auth.Actor, filters tx.AuditFilters) ([]tx.AuditEntry, error) {
	if actor == nil {
		return nil, ErrAuditActorRequired
	}
	scope, err := s.beginner.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = scope.Rollback(ctx) }()
	entries, err := scope.AuditQuery().Export(ctx, filters)
	if err != nil {
		return nil, err
	}
	out := make([]tx.AuditEntry, len(entries))
	for i, e := range entries {
		cp := e
		cp.Details = sanitizeJSON(e.Details)
		out[i] = cp
	}
	return out, nil
}

// sanitizeJSON returns a copy of raw with credential-shaped segments masked
// while keeping the result valid JSON. When redaction would break JSON
// validity, the redacted text is stored as a JSON string instead.
func sanitizeJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	masked := redact.Redact(string(raw))
	if json.Valid([]byte(masked)) {
		return json.RawMessage(masked)
	}
	quoted, err := json.Marshal(masked)
	if err != nil {
		return json.RawMessage(`"[REDACTED]"`)
	}
	return json.RawMessage(quoted)
}

// sanitizeSettingValue applies the JSON-safe redaction to a settings value.
func sanitizeSettingValue(raw json.RawMessage) json.RawMessage {
	return sanitizeJSON(raw)
}
