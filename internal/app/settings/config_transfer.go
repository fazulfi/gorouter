package settings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"gorouter/internal/app/tx"
	"gorouter/internal/domain/auth"
	"gorouter/internal/domain/pricing"
	"gorouter/internal/domain/provider"
	"gorouter/internal/domain/settings"
	enginerouting "gorouter/internal/engine/routing"

	"github.com/google/uuid"
)

// ConfigImportConfirmation is the exact typed confirmation required before a
// destructive configuration import is applied (DECISIONS #378: the original
// partial/destructive semantics with explicit risk confirmation).
const ConfigImportConfirmation = "I understand this import replaces the current configuration"

// ErrImportDeclined reports an import attempt whose typed confirmation did
// not match. The decline is audited; nothing is written.
var ErrImportDeclined = errors.New("settings: config import declined")

// ErrUnsupportedDomain reports a payload carrying a domain the local
// installation cannot faithfully transfer. The import fails closed instead of
// silently dropping data.
var ErrUnsupportedDomain = errors.New("settings: config import contains an unsupported domain")

// ConfigTransferScope is the transaction-scoped persistence surface used by
// ConfigTransferService. *tx.TxScope satisfies it.
type ConfigTransferScope interface {
	Settings() settings.SettingsRepository
	Pricing() pricing.PricingRepository
	Providers() provider.ProviderRepository
	Nodes() enginerouting.NodeStore
	Pools() provider.PoolRepository
	Aliases() enginerouting.AliasRepository
	AuditLog() tx.AuditLogRepository
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

var _ ConfigTransferScope = (*tx.TxScope)(nil)

// ConfigTransferScopeBeginner begins a config transfer transaction scope.
type ConfigTransferScopeBeginner interface {
	Begin(ctx context.Context) (ConfigTransferScope, error)
}

// ConfigTransferScopeBeginnerFunc adapts a function to
// ConfigTransferScopeBeginner.
type ConfigTransferScopeBeginnerFunc func(ctx context.Context) (ConfigTransferScope, error)

// Begin implements ConfigTransferScopeBeginner.
func (f ConfigTransferScopeBeginnerFunc) Begin(ctx context.Context) (ConfigTransferScope, error) {
	return f(ctx)
}

// ProviderConnectionTransfer is one provider row in a config payload. The
// field names mirror the local gorouter_providers row; the domain name is the
// pinned upstream one (providerConnections).
type ProviderConnectionTransfer struct {
	ID          uuid.UUID       `json:"id"`
	Name        string          `json:"name"`
	Type        string          `json:"type"`
	BaseURL     string          `json:"base_url"`
	APIKeyValue *string         `json:"api_key_value,omitempty"`
	Config      json.RawMessage `json:"config"`
	IsEnabled   bool            `json:"is_enabled"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// ProviderNodeTransfer is one provider node row in a config payload.
type ProviderNodeTransfer struct {
	ID         string            `json:"id"`
	ProviderID uuid.UUID         `json:"provider_id"`
	Name       string            `json:"name"`
	BaseURL    string            `json:"base_url"`
	Region     string            `json:"region"`
	Priority   int               `json:"priority"`
	IsActive   bool              `json:"is_active"`
	Metadata   map[string]string `json:"metadata"`
}

// ProxyPoolTransfer is one proxy pool row in a config payload. Pool members
// are not carried: they reference gorouter_proxy_configs, which are not part
// of the transfer contract.
type ProxyPoolTransfer struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ModelAliasTransfer is one model alias row in a config payload, keyed by
// the alias name exactly as the pinned upstream modelAliases payload is.
type ModelAliasTransfer struct {
	ID         uuid.UUID  `json:"id"`
	Target     string     `json:"target"`
	ProviderID *uuid.UUID `json:"provider_id"`
	IsActive   bool       `json:"is_active"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// PriceTransfer is one pricing override in a config payload, keyed by
// provider id then model id exactly as the pinned upstream pricing payload is.
type PriceTransfer struct {
	InputPrice  float64   `json:"input_price"`
	OutputPrice float64   `json:"output_price"`
	UpdatedBy   uuid.UUID `json:"updated_by"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ConfigPayload is the configuration transfer payload. The domain keys are
// the pinned upstream ones; usage, console logs, sessions, and audit data are
// always excluded. The apiKeys, combos, customModels, and mitmAlias domains
// are recognized but unsupported locally: a payload carrying non-empty values
// for them is rejected instead of silently losing data.
type ConfigPayload struct {
	Settings            map[string]json.RawMessage          `json:"settings"`
	ProviderConnections []ProviderConnectionTransfer        `json:"providerConnections"`
	ProviderNodes       []ProviderNodeTransfer              `json:"providerNodes"`
	ProxyPools          []ProxyPoolTransfer                 `json:"proxyPools"`
	ModelAliases        map[string]ModelAliasTransfer       `json:"modelAliases"`
	Pricing             map[string]map[string]PriceTransfer `json:"pricing"`
	APIKeys             json.RawMessage                     `json:"apiKeys"`
	Combos              json.RawMessage                     `json:"combos"`
	CustomModels        json.RawMessage                     `json:"customModels"`
	MitmAlias           json.RawMessage                     `json:"mitmAlias"`
}

// rejectedDomainFields is the recognized-but-unsupported domain set.
var rejectedDomainFields = []struct {
	name string
	raw  func(p *ConfigPayload) json.RawMessage
}{
	{"apiKeys", func(p *ConfigPayload) json.RawMessage { return p.APIKeys }},
	{"combos", func(p *ConfigPayload) json.RawMessage { return p.Combos }},
	{"customModels", func(p *ConfigPayload) json.RawMessage { return p.CustomModels }},
	{"mitmAlias", func(p *ConfigPayload) json.RawMessage { return p.MitmAlias }},
}

// Validate checks every carried domain and rejects recognized-but-unsupported
// domains that carry data. Unknown top-level keys are ignored, matching the
// pinned upstream import behavior.
func (p *ConfigPayload) Validate() error {
	if p == nil {
		return errors.New("settings: config payload must be an object")
	}
	for _, d := range rejectedDomainFields {
		raw := d.raw(p)
		if raw == nil || len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
			continue
		}
		var probe any
		if err := json.Unmarshal(raw, &probe); err != nil {
			continue
		}
		if probe == nil {
			continue
		}
		if arr, ok := probe.([]any); ok && len(arr) == 0 {
			continue
		}
		if obj, ok := probe.(map[string]any); ok && len(obj) == 0 {
			continue
		}
		return fmt.Errorf("%w: %s", ErrUnsupportedDomain, d.name)
	}

	for key, raw := range p.Settings {
		if key == "" {
			return errors.New("settings: config payload has an empty settings key")
		}
		if !json.Valid(raw) {
			return fmt.Errorf("settings: config payload settings key %q is not valid JSON", key)
		}
		if err := settings.ValidateTypedValue(key, raw); err != nil {
			return err
		}
	}
	for i, c := range p.ProviderConnections {
		if c.ID == uuid.Nil || c.Name == "" || c.Type == "" || c.BaseURL == "" {
			return fmt.Errorf("settings: providerConnections[%d] requires id, name, type and base_url", i)
		}
		if c.Config != nil && !json.Valid(c.Config) {
			return fmt.Errorf("settings: providerConnections[%d] config is not valid JSON", i)
		}
	}
	for i, n := range p.ProviderNodes {
		if n.ID == "" || n.ProviderID == uuid.Nil {
			return fmt.Errorf("settings: providerNodes[%d] requires id and provider_id", i)
		}
	}
	for i, p := range p.ProxyPools {
		if p.ID == uuid.Nil || p.Name == "" {
			return fmt.Errorf("settings: proxyPools[%d] requires id and name", i)
		}
	}
	for alias, a := range p.ModelAliases {
		if alias == "" || a.Target == "" {
			return fmt.Errorf("settings: modelAliases entry %q requires a target", alias)
		}
	}
	for providerID, models := range p.Pricing {
		if providerID == "" {
			return errors.New("settings: config payload has an empty pricing provider key")
		}
		for model, price := range models {
			if model == "" {
				return fmt.Errorf("settings: pricing provider %q has an empty model key", providerID)
			}
			if math.IsNaN(price.InputPrice) || math.IsInf(price.InputPrice, 0) ||
				math.IsNaN(price.OutputPrice) || math.IsInf(price.OutputPrice, 0) {
				return fmt.Errorf("settings: pricing %s/%s contains a non-finite price", providerID, model)
			}
		}
	}
	return nil
}

// ConfigTransferService exports and imports the manual configuration payload.
// The contract is frozen from the pinned upstream settings/database route and
// backup implementation (DECISIONS #378): the same domains, the same missing-
// key wipe replacement, transactionality, validation, and confirmation. It is
// deliberately distinct from disaster-recovery backup/restore.
type ConfigTransferService struct {
	beginner ConfigTransferScopeBeginner
}

// NewConfigTransferService creates a ConfigTransferService over the given
// scope beginner.
func NewConfigTransferService(beginner ConfigTransferScopeBeginner) *ConfigTransferService {
	return &ConfigTransferService{beginner: beginner}
}

// Export builds the transfer payload from the carried domains. Usage,
// console logs, sessions, and audit data are never included.
func (s *ConfigTransferService) Export(ctx context.Context, actor *auth.Actor) (*ConfigPayload, error) {
	if actor == nil {
		return nil, ErrActorRequired
	}
	scope, err := s.beginner.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = scope.Rollback(ctx) }()

	payload, err := exportFromScope(ctx, scope)
	if err != nil {
		return nil, err
	}
	if err := writeTransferAudit(ctx, scope.AuditLog(), actor, "config.transfer.export", map[string]any{
		"result": "exported",
	}); err != nil {
		return nil, fmt.Errorf("audit config export: %w", err)
	}
	if err := scope.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return payload, nil
}

// Import applies the payload with the pinned upstream wipe semantics: every
// carried domain is wiped inside the single transaction (missing keys are
// wiped too) and the payload rows are re-inserted. The typed confirmation is
// required before anything is written; both accept and decline are audited.
func (s *ConfigTransferService) Import(ctx context.Context, actor *auth.Actor, payload ConfigPayload, confirmation string) (*ConfigPayload, error) {
	if actor == nil {
		return nil, ErrActorRequired
	}
	if strings.TrimSpace(confirmation) != ConfigImportConfirmation {
		scope, err := s.beginner.Begin(ctx)
		if err != nil {
			return nil, fmt.Errorf("begin tx: %w", err)
		}
		defer func() { _ = scope.Rollback(ctx) }()
		if err := writeTransferAudit(ctx, scope.AuditLog(), actor, "config.transfer.import.decline", map[string]any{
			"result": "declined",
			"reason": "typed confirmation did not match",
		}); err != nil {
			return nil, fmt.Errorf("audit config import decline: %w", err)
		}
		if err := scope.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit tx: %w", err)
		}
		return nil, ErrImportDeclined
	}

	if err := payload.Validate(); err != nil {
		reason := err.Error()
		rejectScope, beginErr := s.beginner.Begin(ctx)
		if beginErr != nil {
			return nil, fmt.Errorf("begin tx: %w", beginErr)
		}
		defer func() { _ = rejectScope.Rollback(ctx) }()
		if auditErr := writeTransferAudit(ctx, rejectScope.AuditLog(), actor, "config.transfer.import.rejected", map[string]any{
			"result": "rejected",
			"reason": reason,
		}); auditErr != nil {
			return nil, fmt.Errorf("audit config import rejection: %w", auditErr)
		}
		if commitErr := rejectScope.Commit(ctx); commitErr != nil {
			return nil, fmt.Errorf("commit tx: %w", commitErr)
		}
		return nil, err
	}

	scope, err := s.beginner.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = scope.Rollback(ctx) }()

	counts, err := wipeCarriedDomains(ctx, scope)
	if err != nil {
		return nil, fmt.Errorf("wipe carried domains: %w", err)
	}
	if err := insertCarriedDomains(ctx, scope, payload); err != nil {
		return nil, fmt.Errorf("insert carried domains: %w", err)
	}
	counts = countPayload(payload, counts)

	if err := writeTransferAudit(ctx, scope.AuditLog(), actor, "config.transfer.import.accept", map[string]any{
		"result": "applied",
		"counts": counts,
	}); err != nil {
		return nil, fmt.Errorf("audit config import: %w", err)
	}
	exported, err := exportFromScope(ctx, scope)
	if err != nil {
		return nil, fmt.Errorf("re-export after import: %w", err)
	}
	if err := scope.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return exported, nil
}

// exportFromScope builds the payload from the current scope state.
func exportFromScope(ctx context.Context, scope ConfigTransferScope) (*ConfigPayload, error) {
	payload := &ConfigPayload{}

	settingRows, err := scope.Settings().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list settings: %w", err)
	}
	payload.Settings = make(map[string]json.RawMessage, len(settingRows))
	for _, s := range settingRows {
		payload.Settings[s.Key] = append(json.RawMessage(nil), s.Value...)
	}

	providers, err := scope.Providers().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list providers: %w", err)
	}
	payload.ProviderConnections = make([]ProviderConnectionTransfer, 0, len(providers))
	for _, p := range providers {
		row := ProviderConnectionTransfer{
			ID: p.ID, Name: p.Name, Type: string(p.Type), BaseURL: p.BaseURL,
			APIKeyValue: p.APIKeyValue,
			Config:      append(json.RawMessage(nil), p.Config...),
			IsEnabled:   p.IsEnabled, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
		}
		if len(row.Config) == 0 {
			row.Config = json.RawMessage(`{}`)
		}
		payload.ProviderConnections = append(payload.ProviderConnections, row)
	}

	nodes, err := scope.Nodes().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list provider nodes: %w", err)
	}
	payload.ProviderNodes = make([]ProviderNodeTransfer, 0, len(nodes))
	for _, n := range nodes {
		payload.ProviderNodes = append(payload.ProviderNodes, ProviderNodeTransfer{
			ID: n.ID, ProviderID: n.ProviderID, Name: n.Name, BaseURL: n.BaseURL,
			Region: n.Region, Priority: n.Priority, IsActive: n.IsActive,
			Metadata: copyStringMap(n.Metadata),
		})
	}

	pools, err := scope.Pools().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list proxy pools: %w", err)
	}
	payload.ProxyPools = make([]ProxyPoolTransfer, 0, len(pools))
	for _, p := range pools {
		payload.ProxyPools = append(payload.ProxyPools, ProxyPoolTransfer{
			ID: p.ID, Name: p.Name, Description: p.Description,
			CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
		})
	}

	aliases, err := scope.Aliases().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list model aliases: %w", err)
	}
	payload.ModelAliases = make(map[string]ModelAliasTransfer, len(aliases))
	for _, a := range aliases {
		payload.ModelAliases[a.Alias] = ModelAliasTransfer{
			ID: a.ID, Target: a.Target, ProviderID: a.ProviderID,
			IsActive: a.IsActive, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt,
		}
	}

	overrides, err := scope.Pricing().LoadOverrides(ctx)
	if err != nil {
		return nil, fmt.Errorf("load pricing overrides: %w", err)
	}
	payload.Pricing = make(map[string]map[string]PriceTransfer)
	for _, o := range overrides {
		models, ok := payload.Pricing[o.ProviderID]
		if !ok {
			models = make(map[string]PriceTransfer)
			payload.Pricing[o.ProviderID] = models
		}
		models[o.ModelID] = PriceTransfer{
			InputPrice: o.InputPrice, OutputPrice: o.OutputPrice,
			UpdatedBy: o.UpdatedBy, UpdatedAt: o.UpdatedAt,
		}
	}
	return payload, nil
}

// wipeCarriedDomains deletes every carried domain inside the transaction.
// Missing payload keys are wiped too (missing-key replacement), matching the
// pinned upstream importDb.
func wipeCarriedDomains(ctx context.Context, scope ConfigTransferScope) (map[string]int, error) {
	counts := map[string]int{}

	if err := scope.Settings().Wipe(ctx); err != nil {
		return nil, err
	}
	nodes, err := scope.Nodes().List(ctx)
	if err != nil {
		return nil, err
	}
	for _, n := range nodes {
		if err := scope.Nodes().Delete(ctx, n.ID); err != nil {
			return nil, err
		}
	}
	counts["providerNodes"] = len(nodes)

	aliases, err := scope.Aliases().List(ctx)
	if err != nil {
		return nil, err
	}
	for _, a := range aliases {
		if err := scope.Aliases().Delete(ctx, a.ID); err != nil {
			return nil, err
		}
	}
	counts["modelAliases"] = len(aliases)

	pools, err := scope.Pools().List(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range pools {
		if err := scope.Pools().Delete(ctx, p.ID); err != nil {
			return nil, err
		}
	}
	counts["proxyPools"] = len(pools)

	providers, err := scope.Providers().List(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range providers {
		if err := scope.Providers().Delete(ctx, p.ID); err != nil {
			return nil, err
		}
	}
	counts["providerConnections"] = len(providers)
	return counts, nil
}

// insertCarriedDomains re-inserts the payload rows. Providers land first so
// the referencing domains satisfy their foreign keys.
func insertCarriedDomains(ctx context.Context, scope ConfigTransferScope, payload ConfigPayload) error {
	for _, c := range payload.ProviderConnections {
		config := c.Config
		if len(config) == 0 {
			config = json.RawMessage(`{}`)
		}
		p := &provider.Provider{
			ID: c.ID, Name: c.Name, Type: provider.ProviderType(c.Type),
			BaseURL: c.BaseURL, APIKeyValue: c.APIKeyValue,
			Config:    append(json.RawMessage(nil), config...),
			IsEnabled: c.IsEnabled, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
		}
		if err := scope.Providers().Create(ctx, p); err != nil {
			return err
		}
	}
	for _, n := range payload.ProviderNodes {
		node := enginerouting.ProviderNode{
			ID: n.ID, ProviderID: n.ProviderID, Name: n.Name, BaseURL: n.BaseURL,
			Region: n.Region, Priority: n.Priority, IsActive: n.IsActive,
			Metadata: copyStringMap(n.Metadata),
		}
		if err := scope.Nodes().Save(ctx, node); err != nil {
			return err
		}
	}
	aliases := make([]string, 0, len(payload.ModelAliases))
	for alias := range payload.ModelAliases {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	for _, alias := range aliases {
		a := payload.ModelAliases[alias]
		if err := scope.Aliases().Create(ctx, &enginerouting.Alias{
			ID: a.ID, Alias: alias, Target: a.Target, ProviderID: a.ProviderID,
			IsActive: a.IsActive, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt,
		}); err != nil {
			return err
		}
	}
	for _, p := range payload.ProxyPools {
		if err := scope.Pools().Create(ctx, &provider.ProxyPool{
			ID: p.ID, Name: p.Name, Description: p.Description,
			CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
		}); err != nil {
			return err
		}
	}
	overrides := make([]pricing.PriceOverride, 0, len(payload.Pricing))
	providerIDs := make([]string, 0, len(payload.Pricing))
	for providerID := range payload.Pricing {
		providerIDs = append(providerIDs, providerID)
	}
	sort.Strings(providerIDs)
	for _, providerID := range providerIDs {
		models := payload.Pricing[providerID]
		modelIDs := make([]string, 0, len(models))
		for modelID := range models {
			modelIDs = append(modelIDs, modelID)
		}
		sort.Strings(modelIDs)
		for _, modelID := range modelIDs {
			price := models[modelID]
			overrides = append(overrides, pricing.PriceOverride{
				ModelID: modelID, ProviderID: providerID,
				InputPrice: price.InputPrice, OutputPrice: price.OutputPrice,
				UpdatedBy: price.UpdatedBy, UpdatedAt: price.UpdatedAt,
			})
		}
	}
	if err := scope.Pricing().SaveOverrides(ctx, overrides); err != nil {
		return err
	}
	keys := make([]string, 0, len(payload.Settings))
	for key := range payload.Settings {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := scope.Settings().Set(ctx, &settings.Setting{Key: key, Value: payload.Settings[key]}); err != nil {
			return err
		}
	}
	return nil
}

// countPayload counts the payload rows for the audit record.
func countPayload(payload ConfigPayload, counts map[string]int) map[string]int {
	counts["settings"] = len(payload.Settings)
	counts["providerConnections"] = len(payload.ProviderConnections)
	counts["providerNodes"] = len(payload.ProviderNodes)
	counts["proxyPools"] = len(payload.ProxyPools)
	counts["modelAliases"] = len(payload.ModelAliases)
	priceCount := 0
	for _, models := range payload.Pricing {
		priceCount += len(models)
	}
	counts["pricing"] = priceCount
	return counts
}

func copyStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// writeTransferAudit appends the immutable audit entry for a transfer event.
func writeTransferAudit(ctx context.Context, log tx.AuditLogRepository, actor *auth.Actor, action string, details map[string]any) error {
	raw, err := json.Marshal(details)
	if err != nil {
		return err
	}
	actorID := actor.UserID
	entry := &tx.AuditLogEntry{
		ID:           uuid.New(),
		ActorID:      &actorID,
		Action:       action,
		ResourceType: "config",
		Details:      raw,
		OccurredAt:   time.Now().UTC(),
	}
	return log.Create(ctx, entry)
}
