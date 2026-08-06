// Package tx provides a transaction manager that scopes domain repository access
// within database transactions.
package tx

import (
	"context"
	"encoding/json"
	"net"
	"time"

	"gorouter/internal/domain/auth"
	"gorouter/internal/domain/backup"
	"gorouter/internal/domain/combo"
	"gorouter/internal/domain/console"
	"gorouter/internal/domain/jobs"
	"gorouter/internal/domain/keys"
	"gorouter/internal/domain/oauth"
	"gorouter/internal/domain/passwordreset"
	"gorouter/internal/domain/pricing"
	"gorouter/internal/domain/provider"
	"gorouter/internal/domain/settings"
	"gorouter/internal/domain/usage"
	enginerouting "gorouter/internal/engine/routing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AuditLogEntry represents a single audit log record.
type AuditLogEntry struct {
	ID           uuid.UUID       `json:"id"`
	ActorID      *uuid.UUID      `json:"actor_id,omitempty"`
	Action       string          `json:"action"`
	ResourceType string          `json:"resource_type"`
	ResourceID   *uuid.UUID      `json:"resource_id,omitempty"`
	Details      json.RawMessage `json:"details,omitempty"`
	IPAddress    net.IP          `json:"ip_address,omitempty"`
	OccurredAt   time.Time       `json:"occurred_at"`
}

// AuditLogRepository defines persistence operations for the audit log.
type AuditLogRepository interface {
	Create(ctx context.Context, entry *AuditLogEntry) error
}

// AuditEntry is one full audit log record as returned by the read surface.
// Audit rows are append-only forever; the query surface deliberately exposes
// no update or delete path (migration 000009 revokes them at the role level).
type AuditEntry struct {
	ID           uuid.UUID
	ActorID      *uuid.UUID
	Action       string
	ResourceType string
	ResourceID   *uuid.UUID
	Details      json.RawMessage
	IPAddress    net.IP
	JobID        *uuid.UUID
	ActorKind    string
	OccurredAt   time.Time
}

// AuditFilters narrows an audit query. Nil fields are not filtered.
type AuditFilters struct {
	ActorID      *uuid.UUID
	ResourceType *string
	ResourceID   *uuid.UUID
	Action       *string
	ActorKind    *string
	JobID        *uuid.UUID
	Since        *time.Time
	Until        *time.Time
}

// AuditPage is deterministic offset pagination for audit listing. Zero values
// mean page 1 of 50; the page size is capped at MaxAuditPageSize.
type AuditPage struct {
	Number int
	Size   int
}

// Audit pagination bounds.
const (
	DefaultAuditPageNumber = 1
	DefaultAuditPageSize   = 50
	MaxAuditPageSize       = 200
)

// Normalized returns the offset and limit for the page, applying the
// defaults and the size cap.
func (p AuditPage) Normalized() (offset, limit int) {
	limit = p.Size
	if limit <= 0 {
		limit = DefaultAuditPageSize
	}
	if limit > MaxAuditPageSize {
		limit = MaxAuditPageSize
	}
	offset = (p.Number - 1) * limit
	if offset < 0 {
		offset = 0
	}
	return offset, limit
}

// AuditLogQueryRepository is the SELECT-only read surface of the append-only
// audit log. It exposes no mutation methods by construction; the runtime role
// holds only INSERT and SELECT on gorouter_audit_log.
type AuditLogQueryRepository interface {
	// List returns the page of matching entries, newest first, with the id
	// as the deterministic tiebreaker.
	List(ctx context.Context, filters AuditFilters, page AuditPage) ([]AuditEntry, error)
	// Export returns every matching entry, newest first, with the id as the
	// deterministic tiebreaker. The caller is responsible for redaction.
	Export(ctx context.Context, filters AuditFilters) ([]AuditEntry, error)
}

// ProviderModel represents a model entry in the provider_models catalog.
type ProviderModel struct {
	ID           uuid.UUID
	ProviderID   uuid.UUID
	ModelName    string
	Capabilities []string
	MaxTokens    int
	CreatedAt    time.Time
}

// ModelRepository defines query operations for provider models.
type ModelRepository interface {
	GetModelsByProvider(ctx context.Context, providerID uuid.UUID) ([]ProviderModel, error)
	GetModelByRef(ctx context.Context, providerID uuid.UUID, modelName string) (*ProviderModel, error)
	ListByCapability(ctx context.Context, capability string) ([]ProviderModel, error)
}

// TxScope scopes all domain repository access within a single database transaction.
// Obtain one via TransactionManager.Begin, then call Commit or Rollback when done.
type TxScope struct {
	tx             pgx.Tx
	users          auth.UserRepository
	sessions       auth.SessionRepository
	apiKeys        keys.APIKeyRepository
	pats           keys.PATRepository
	providers      provider.ProviderRepository
	jobs           jobs.JobRepository
	auditLog       AuditLogRepository
	accounts       provider.AccountRepository
	proxies        provider.ProxyRepository
	models         ModelRepository
	aliases        enginerouting.AliasRepository
	combos         combo.Repository
	oauth          oauth.Repository
	pools          provider.PoolRepository
	nodes          enginerouting.NodeStore
	usage          usage.UsageRepository
	console        console.ConsoleLogRepository
	passwordResets passwordreset.PasswordResetRepository
	backups        backup.BackupRepository
	pricing        pricing.PricingRepository
	settings       settings.SettingsRepository
	auditQuery     AuditLogQueryRepository
}

// NewTxScope creates a TxScope with the given transaction and repositories.
// This is primarily used by the repository factory to wire concrete implementations.
func NewTxScope(tx pgx.Tx, users auth.UserRepository, sessions auth.SessionRepository,
	apiKeys keys.APIKeyRepository, pats keys.PATRepository,
	providers provider.ProviderRepository, jrs jobs.JobRepository,
	auditLog AuditLogRepository,
	accounts provider.AccountRepository, proxies provider.ProxyRepository,
	models ModelRepository, aliases enginerouting.AliasRepository,
	combos combo.Repository, oauth oauth.Repository,
	pools provider.PoolRepository, nodes enginerouting.NodeStore,
	usage usage.UsageRepository, console console.ConsoleLogRepository,
	passwordResets passwordreset.PasswordResetRepository,
	backups backup.BackupRepository,
	pricing pricing.PricingRepository,
	settings settings.SettingsRepository,
	auditQuery AuditLogQueryRepository) *TxScope {
	return &TxScope{
		tx:             tx,
		users:          users,
		sessions:       sessions,
		apiKeys:        apiKeys,
		pats:           pats,
		providers:      providers,
		jobs:           jrs,
		auditLog:       auditLog,
		accounts:       accounts,
		proxies:        proxies,
		models:         models,
		aliases:        aliases,
		combos:         combos,
		oauth:          oauth,
		pools:          pools,
		nodes:          nodes,
		usage:          usage,
		console:        console,
		passwordResets: passwordResets,
		backups:        backups,
		pricing:        pricing,
		settings:       settings,
		auditQuery:     auditQuery,
	}
}

// Commit commits the underlying transaction.
func (s *TxScope) Commit(ctx context.Context) error {
	return s.tx.Commit(ctx)
}

// Rollback rolls back the underlying transaction.
func (s *TxScope) Rollback(ctx context.Context) error {
	return s.tx.Rollback(ctx)
}

// Users returns the scoped UserRepository.
func (s *TxScope) Users() auth.UserRepository { return s.users }

// Sessions returns the scoped SessionRepository.
func (s *TxScope) Sessions() auth.SessionRepository { return s.sessions }

// APIKeys returns the scoped APIKeyRepository.
func (s *TxScope) APIKeys() keys.APIKeyRepository { return s.apiKeys }

// PATs returns the scoped PATRepository.
func (s *TxScope) PATs() keys.PATRepository { return s.pats }

// Providers returns the scoped ProviderRepository.
func (s *TxScope) Providers() provider.ProviderRepository { return s.providers }

// Jobs returns the scoped JobRepository.
func (s *TxScope) Jobs() jobs.JobRepository { return s.jobs }

// AuditLog returns the scoped AuditLogRepository.
func (s *TxScope) AuditLog() AuditLogRepository { return s.auditLog }

// Accounts returns the scoped AccountRepository.
func (s *TxScope) Accounts() provider.AccountRepository { return s.accounts }

// Proxies returns the scoped ProxyRepository.
func (s *TxScope) Proxies() provider.ProxyRepository { return s.proxies }

// Models returns the scoped ModelRepository.
func (s *TxScope) Models() ModelRepository { return s.models }

// Aliases returns the scoped AliasRepository.
func (s *TxScope) Aliases() enginerouting.AliasRepository { return s.aliases }

// Combos returns the scoped combo.Repository.
func (s *TxScope) Combos() combo.Repository { return s.combos }

// OAuth returns the scoped oauth.Repository.
func (s *TxScope) OAuth() oauth.Repository { return s.oauth }

// Pools returns the scoped PoolRepository.
func (s *TxScope) Pools() provider.PoolRepository { return s.pools }

// Nodes returns the scoped routing NodeStore.
func (s *TxScope) Nodes() enginerouting.NodeStore { return s.nodes }

// Usage returns the scoped UsageRepository.
func (s *TxScope) Usage() usage.UsageRepository { return s.usage }

// ConsoleLogs returns the scoped ConsoleLogRepository.
func (s *TxScope) ConsoleLogs() console.ConsoleLogRepository { return s.console }

// PasswordResets returns the scoped PasswordResetRepository.
func (s *TxScope) PasswordResets() passwordreset.PasswordResetRepository { return s.passwordResets }

// Backups returns the scoped BackupRepository.
func (s *TxScope) Backups() backup.BackupRepository { return s.backups }

// Pricing returns the scoped pricing.PricingRepository.
func (s *TxScope) Pricing() pricing.PricingRepository { return s.pricing }

// Settings returns the scoped settings.SettingsRepository.
func (s *TxScope) Settings() settings.SettingsRepository { return s.settings }

// AuditQuery returns the scoped SELECT-only audit log query surface.
func (s *TxScope) AuditQuery() AuditLogQueryRepository { return s.auditQuery }

// TxScopeFactory is a function type that creates a fully-wired TxScope from a
// pgx transaction. It is injected at bootstrap time to break the import cycle
// between the tx package (domain agnostic) and the repositories package
// (concrete SQL implementations).
type TxScopeFactory func(pgx.Tx) *TxScope

// TransactionManager manages database transactions and provides scoped repository
// access through TxScope. It depends on a pgx connection pool injected via New.
// The ScopeFactory must be set before Begin is called, typically at bootstrap.
type TransactionManager struct {
	pool         *pgxpool.Pool
	scopeFactory TxScopeFactory
}

// NewTransactionManager creates a TransactionManager backed by the given pool.
func NewTransactionManager(pool *pgxpool.Pool) *TransactionManager {
	return &TransactionManager{pool: pool}
}

// SetScopeFactory injects the TxScopeFactory function. Must be called before
// Begin, typically during bootstrap wiring.
func (tm *TransactionManager) SetScopeFactory(fn TxScopeFactory) {
	tm.scopeFactory = fn
}

// Begin opens a new database transaction and returns a TxScope that provides
// scoped access to domain repositories within that transaction. The scope is
// fully wired when a scope factory has been set via SetScopeFactory.
func (tm *TransactionManager) Begin(ctx context.Context) (*TxScope, error) {
	tx, err := tm.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if tm.scopeFactory != nil {
		return tm.scopeFactory(tx), nil
	}
	// Fallback: return an unwired scope (repositories will be nil). This
	// path is taken in tests that don't call SetScopeFactory.
	return &TxScope{tx: tx}, nil
}
