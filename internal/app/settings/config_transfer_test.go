package settings

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"gorouter/internal/app/tx"
	"gorouter/internal/domain/auth"
	"gorouter/internal/domain/pricing"
	"gorouter/internal/domain/provider"
	"gorouter/internal/domain/settings"
	enginerouting "gorouter/internal/engine/routing"

	"github.com/google/uuid"
)

// fakeTransferBacking holds the persisted state behind the transactional
// transfer scope: writes are staged and become visible only on Commit.
type fakeTransferBacking struct {
	mu        sync.Mutex
	settings  map[string]settings.Setting
	providers map[uuid.UUID]provider.Provider
	nodes     map[string]enginerouting.ProviderNode
	pools     map[uuid.UUID]provider.ProxyPool
	aliases   map[uuid.UUID]enginerouting.Alias
	overrides []pricing.PriceOverride
}

func newFakeTransferBacking() *fakeTransferBacking {
	return &fakeTransferBacking{
		settings:  map[string]settings.Setting{},
		providers: map[uuid.UUID]provider.Provider{},
		nodes:     map[string]enginerouting.ProviderNode{},
		pools:     map[uuid.UUID]provider.ProxyPool{},
		aliases:   map[uuid.UUID]enginerouting.Alias{},
	}
}

// fakeTransferScope stages all domain writes and applies them on Commit,
// modeling the single-transaction wipe-and-reinsert contract.
type fakeTransferScope struct {
	backing         *fakeTransferBacking
	audit           *fakeAuditLog
	mu              sync.Mutex
	stagedWipe      bool
	stagedP         []provider.Provider
	stagedN         []enginerouting.ProviderNode
	stagedPo        []provider.ProxyPool
	stagedA         []enginerouting.Alias
	stagedS         map[string]settings.Setting
	stagedO         []pricing.PriceOverride
	stagedDeletedP  []uuid.UUID
	stagedDeletedN  map[string]bool
	stagedDeletedPo map[uuid.UUID]bool
	stagedDeletedA  map[uuid.UUID]bool
	commits         int
	rollbacks       int
	commitErr       error
}

func newFakeTransferScope() *fakeTransferScope {
	return &fakeTransferScope{
		backing: newFakeTransferBacking(),
		audit:   &fakeAuditLog{},
		stagedS: map[string]settings.Setting{},
	}
}

func (s *fakeTransferScope) Settings() settings.SettingsRepository {
	return &fakeTransferSettingsRepo{scope: s}
}

type fakeTransferSettingsRepo struct{ scope *fakeTransferScope }

func (r *fakeTransferSettingsRepo) Get(_ context.Context, key string) (*settings.Setting, error) {
	r.scope.mu.Lock()
	defer r.scope.mu.Unlock()
	if _, reserved := settings.ReservedKeys[key]; reserved {
		return nil, settings.ErrReservedKey
	}
	if s, ok := r.scope.stagedS[key]; ok {
		cp := s
		return &cp, nil
	}
	s, ok := r.scope.backing.settings[key]
	if !ok {
		return nil, settings.ErrSettingNotFound
	}
	cp := s
	return &cp, nil
}

func (r *fakeTransferSettingsRepo) Set(_ context.Context, s *settings.Setting) error {
	r.scope.mu.Lock()
	defer r.scope.mu.Unlock()
	if s.Key == "pricing:overrides" {
		return settings.ErrReservedKey
	}
	cp := *s
	r.scope.stagedS[s.Key] = cp
	return nil
}

func (r *fakeTransferSettingsRepo) List(_ context.Context) ([]settings.Setting, error) {
	r.scope.mu.Lock()
	defer r.scope.mu.Unlock()
	out := make([]settings.Setting, 0)
	for k, v := range r.scope.backing.settings {
		if _, reserved := settings.ReservedKeys[k]; reserved {
			continue
		}
		if _, staged := r.scope.stagedS[k]; staged {
			continue
		}
		out = append(out, v)
	}
	for _, v := range r.scope.stagedS {
		if _, reserved := settings.ReservedKeys[v.Key]; reserved {
			continue
		}
		out = append(out, v)
	}
	return out, nil
}

func (r *fakeTransferSettingsRepo) Wipe(_ context.Context) error {
	r.scope.mu.Lock()
	defer r.scope.mu.Unlock()
	r.scope.stagedWipe = true
	r.scope.stagedS = map[string]settings.Setting{}
	return nil
}

func (s *fakeTransferScope) Pricing() pricing.PricingRepository {
	return &fakeTransferPricingRepo{scope: s}
}

type fakeTransferPricingRepo struct{ scope *fakeTransferScope }

func (r *fakeTransferPricingRepo) LoadOverrides(_ context.Context) ([]pricing.PriceOverride, error) {
	r.scope.mu.Lock()
	defer r.scope.mu.Unlock()
	out := append([]pricing.PriceOverride(nil), r.scope.backing.overrides...)
	if r.scope.stagedO != nil {
		out = append([]pricing.PriceOverride(nil), r.scope.stagedO...)
	}
	return out, nil
}

func (r *fakeTransferPricingRepo) SaveOverrides(_ context.Context, o []pricing.PriceOverride) error {
	r.scope.mu.Lock()
	defer r.scope.mu.Unlock()
	r.scope.stagedO = append([]pricing.PriceOverride(nil), o...)
	return nil
}

func (s *fakeTransferScope) Providers() provider.ProviderRepository {
	return &fakeTransferProviderRepo{scope: s}
}

type fakeTransferProviderRepo struct{ scope *fakeTransferScope }

func (r *fakeTransferProviderRepo) List(_ context.Context) ([]provider.Provider, error) {
	r.scope.mu.Lock()
	defer r.scope.mu.Unlock()
	out := make([]provider.Provider, 0, len(r.scope.backing.providers))
	for _, p := range r.scope.backing.providers {
		if !r.stagedDeleted(p.ID) {
			out = append(out, p)
		}
	}
	for _, p := range r.scope.stagedP {
		out = append(out, p)
	}
	return out, nil
}

func (r *fakeTransferProviderRepo) stagedDeleted(id uuid.UUID) bool {
	for _, d := range r.scope.stagedDeletedP {
		if d == id {
			return true
		}
	}
	return false
}

func (r *fakeTransferProviderRepo) Create(_ context.Context, p *provider.Provider) error {
	r.scope.mu.Lock()
	defer r.scope.mu.Unlock()
	r.scope.stagedP = append(r.scope.stagedP, *p)
	return nil
}

func (r *fakeTransferProviderRepo) Delete(_ context.Context, id uuid.UUID) error {
	r.scope.mu.Lock()
	defer r.scope.mu.Unlock()
	r.scope.stagedDeletedP = append(r.scope.stagedDeletedP, id)
	return nil
}

func (r *fakeTransferProviderRepo) FindByID(context.Context, uuid.UUID) (*provider.Provider, error) {
	return nil, errors.New("unexpected")
}
func (r *fakeTransferProviderRepo) FindByType(context.Context, provider.ProviderType) ([]provider.Provider, error) {
	return nil, errors.New("unexpected")
}
func (r *fakeTransferProviderRepo) Update(context.Context, *provider.Provider) error {
	return errors.New("unexpected")
}

func (s *fakeTransferScope) Nodes() enginerouting.NodeStore {
	return &fakeTransferNodeStore{scope: s}
}

type fakeTransferNodeStore struct{ scope *fakeTransferScope }

func (r *fakeTransferNodeStore) List(_ context.Context) ([]enginerouting.ProviderNode, error) {
	r.scope.mu.Lock()
	defer r.scope.mu.Unlock()
	out := make([]enginerouting.ProviderNode, 0)
	for _, n := range r.scope.backing.nodes {
		if _, del := r.scope.stagedDeletedN[n.ID]; del {
			continue
		}
		out = append(out, n)
	}
	out = append(out, r.scope.stagedN...)
	return out, nil
}

func (r *fakeTransferNodeStore) Save(_ context.Context, n enginerouting.ProviderNode) error {
	r.scope.mu.Lock()
	defer r.scope.mu.Unlock()
	r.scope.stagedN = append(r.scope.stagedN, n)
	return nil
}

func (r *fakeTransferNodeStore) Delete(_ context.Context, id string) error {
	r.scope.mu.Lock()
	defer r.scope.mu.Unlock()
	if r.scope.stagedDeletedN == nil {
		r.scope.stagedDeletedN = map[string]bool{}
	}
	r.scope.stagedDeletedN[id] = true
	return nil
}

func (s *fakeTransferScope) Pools() provider.PoolRepository {
	return &fakeTransferPoolRepo{scope: s}
}

type fakeTransferPoolRepo struct{ scope *fakeTransferScope }

func (r *fakeTransferPoolRepo) List(_ context.Context) ([]provider.ProxyPool, error) {
	r.scope.mu.Lock()
	defer r.scope.mu.Unlock()
	out := make([]provider.ProxyPool, 0)
	for _, p := range r.scope.backing.pools {
		if _, del := r.scope.stagedDeletedPo[p.ID]; del {
			continue
		}
		out = append(out, p)
	}
	out = append(out, r.scope.stagedPo...)
	return out, nil
}

func (r *fakeTransferPoolRepo) Create(_ context.Context, p *provider.ProxyPool) error {
	r.scope.mu.Lock()
	defer r.scope.mu.Unlock()
	r.scope.stagedPo = append(r.scope.stagedPo, *p)
	return nil
}

func (r *fakeTransferPoolRepo) Delete(_ context.Context, id uuid.UUID) error {
	r.scope.mu.Lock()
	defer r.scope.mu.Unlock()
	if r.scope.stagedDeletedPo == nil {
		r.scope.stagedDeletedPo = map[uuid.UUID]bool{}
	}
	r.scope.stagedDeletedPo[id] = true
	return nil
}

func (r *fakeTransferPoolRepo) Update(context.Context, *provider.ProxyPool) error {
	return errors.New("unexpected")
}
func (r *fakeTransferPoolRepo) Members(context.Context, uuid.UUID) ([]provider.PoolMember, error) {
	return nil, errors.New("unexpected")
}
func (r *fakeTransferPoolRepo) SetMembers(context.Context, uuid.UUID, []provider.PoolMember) error {
	return errors.New("unexpected")
}

func (s *fakeTransferScope) Aliases() enginerouting.AliasRepository {
	return &fakeTransferAliasRepo{scope: s}
}

type fakeTransferAliasRepo struct{ scope *fakeTransferScope }

func (r *fakeTransferAliasRepo) List(_ context.Context) ([]enginerouting.Alias, error) {
	r.scope.mu.Lock()
	defer r.scope.mu.Unlock()
	out := make([]enginerouting.Alias, 0)
	for _, a := range r.scope.backing.aliases {
		if _, del := r.scope.stagedDeletedA[a.ID]; del {
			continue
		}
		out = append(out, a)
	}
	out = append(out, r.scope.stagedA...)
	return out, nil
}

func (r *fakeTransferAliasRepo) Create(_ context.Context, a *enginerouting.Alias) error {
	r.scope.mu.Lock()
	defer r.scope.mu.Unlock()
	r.scope.stagedA = append(r.scope.stagedA, *a)
	return nil
}

func (r *fakeTransferAliasRepo) Delete(_ context.Context, id uuid.UUID) error {
	r.scope.mu.Lock()
	defer r.scope.mu.Unlock()
	if r.scope.stagedDeletedA == nil {
		r.scope.stagedDeletedA = map[uuid.UUID]bool{}
	}
	r.scope.stagedDeletedA[id] = true
	return nil
}

func (r *fakeTransferAliasRepo) Update(context.Context, *enginerouting.Alias) error {
	return errors.New("unexpected")
}
func (r *fakeTransferAliasRepo) FindByAlias(context.Context, string) (*enginerouting.Alias, error) {
	return nil, errors.New("unexpected")
}
func (r *fakeTransferAliasRepo) FindByID(context.Context, uuid.UUID) (*enginerouting.Alias, error) {
	return nil, errors.New("unexpected")
}
func (r *fakeTransferAliasRepo) ListActive(context.Context) ([]enginerouting.Alias, error) {
	return nil, errors.New("unexpected")
}
func (r *fakeTransferAliasRepo) SetActive(context.Context, uuid.UUID, bool) error {
	return errors.New("unexpected")
}

func (s *fakeTransferScope) AuditLog() tx.AuditLogRepository { return s.audit }

func (s *fakeTransferScope) Commit(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.commitErr != nil {
		return s.commitErr
	}
	b := s.backing
	if s.stagedWipe {
		b.settings = map[string]settings.Setting{}
		b.providers = map[uuid.UUID]provider.Provider{}
		b.nodes = map[string]enginerouting.ProviderNode{}
		b.pools = map[uuid.UUID]provider.ProxyPool{}
		b.aliases = map[uuid.UUID]enginerouting.Alias{}
	}
	for _, id := range s.stagedDeletedP {
		delete(b.providers, id)
	}
	for id := range s.stagedDeletedN {
		delete(b.nodes, id)
	}
	for id := range s.stagedDeletedPo {
		delete(b.pools, id)
	}
	for id := range s.stagedDeletedA {
		delete(b.aliases, id)
	}
	for _, p := range s.stagedP {
		b.providers[p.ID] = p
	}
	for _, n := range s.stagedN {
		b.nodes[n.ID] = n
	}
	for _, p := range s.stagedPo {
		b.pools[p.ID] = p
	}
	for _, a := range s.stagedA {
		b.aliases[a.ID] = a
	}
	for k, v := range s.stagedS {
		b.settings[k] = v
	}
	if s.stagedO != nil {
		b.overrides = append([]pricing.PriceOverride(nil), s.stagedO...)
	}
	s.commits++
	return nil
}

func (s *fakeTransferScope) Rollback(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rollbacks++
	return nil
}

var _ ConfigTransferScope = (*fakeTransferScope)(nil)

// seedTransferBacking populates the backing store so export and wipe tests
// start from a non-empty configuration.
func seedTransferBacking(b *fakeTransferBacking, providerID uuid.UUID) {
	b.settings["rtkEnabled"] = settings.Setting{Key: "rtkEnabled", Value: json.RawMessage(`true`)}
	b.settings["pricing:overrides"] = settings.Setting{Key: "pricing:overrides", Value: json.RawMessage(`[]`)}
	b.providers[providerID] = provider.Provider{
		ID: providerID, Name: "openai", Type: "openai", BaseURL: "https://api.openai.com/v1",
		IsEnabled: true, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	b.nodes["node-1"] = enginerouting.ProviderNode{ID: "node-1", ProviderID: providerID, Name: "n1",
		Metadata: map[string]string{"region": "eu"}}
	b.pools[uuid.New()] = provider.ProxyPool{ID: uuid.New(), Name: "pool-1"}
	aliasID := uuid.New()
	b.aliases[aliasID] = enginerouting.Alias{ID: aliasID, Alias: "fast", Target: "gpt-4o", ProviderID: &providerID}
}

func TestConfigTransferService_Export(t *testing.T) {
	t.Parallel()

	t.Run("actor required", func(t *testing.T) {
		svc := NewConfigTransferService(ConfigTransferScopeBeginnerFunc(func(context.Context) (ConfigTransferScope, error) {
			return newFakeTransferScope(), nil
		}))
		if _, err := svc.Export(context.Background(), nil); !errors.Is(err, ErrActorRequired) {
			t.Errorf("err = %v, want ErrActorRequired", err)
		}
	})

	t.Run("export carries the transfer domains and no usage/log/session/audit domain", func(t *testing.T) {
		scope := newFakeTransferScope()
		providerID := uuid.New()
		seedTransferBacking(scope.backing, providerID)
		scope.backing.overrides = []pricing.PriceOverride{
			{ModelID: "gpt-4o", ProviderID: providerID.String(), InputPrice: 1, OutputPrice: 2},
		}
		svc := NewConfigTransferService(ConfigTransferScopeBeginnerFunc(func(context.Context) (ConfigTransferScope, error) {
			return scope, nil
		}))
		actor := &auth.Actor{UserID: uuid.New(), IsAdmin: true}
		payload, err := svc.Export(context.Background(), actor)
		if err != nil {
			t.Fatal(err)
		}
		if payload == nil {
			t.Fatal("nil payload")
		}
		if len(payload.ProviderConnections) != 1 || len(payload.ProviderNodes) != 1 ||
			len(payload.ProxyPools) != 1 || len(payload.ModelAliases) != 1 ||
			len(payload.Pricing) != 1 || payload.Settings["rtkEnabled"] == nil {
			t.Errorf("exported domains incomplete: %+v", payload)
		}
		if _, ok := payload.Settings["pricing:overrides"]; ok {
			t.Error("settings export must not carry the pricing override key")
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		for _, banned := range []string{"usageHistory", "usageDaily", "requestDetails", "gorouter_console_logs",
			"gorouter_sessions", "gorouter_audit_log", "sessions", "auditLog", "consoleLogs"} {
			if strings.Contains(string(raw), banned) {
				t.Errorf("export leaks excluded domain %q: %s", banned, raw)
			}
		}
		if len(scope.audit.entries) != 1 || scope.audit.entries[0].Action != "config.transfer.export" {
			t.Errorf("export audit = %+v", scope.audit.entries)
		}
	})

	t.Run("export audit failure", func(t *testing.T) {
		scope := newFakeTransferScope()
		seedTransferBacking(scope.backing, uuid.New())
		scope.audit.err = errors.New("audit down")
		svc := NewConfigTransferService(ConfigTransferScopeBeginnerFunc(func(context.Context) (ConfigTransferScope, error) {
			return scope, nil
		}))
		if _, err := svc.Export(context.Background(), &auth.Actor{UserID: uuid.New()}); err == nil {
			t.Error("export audit failure swallowed")
		}
	})

	t.Run("export commit failure", func(t *testing.T) {
		scope := newFakeTransferScope()
		seedTransferBacking(scope.backing, uuid.New())
		scope.commitErr = errors.New("commit down")
		svc := NewConfigTransferService(ConfigTransferScopeBeginnerFunc(func(context.Context) (ConfigTransferScope, error) {
			return scope, nil
		}))
		if _, err := svc.Export(context.Background(), &auth.Actor{UserID: uuid.New()}); err == nil {
			t.Error("export commit failure swallowed")
		}
	})
}

// TestConfigTransferExcludesAudit is the atom's mandated one-behavior test:
// an export contains no usage, console-log, session, or audit domain, and an
// import applies the missing-key wipe semantics transactionally after typed
// confirmation.
func TestConfigTransferExcludesAudit(t *testing.T) {
	t.Parallel()
	scope := newFakeTransferScope()
	providerID := uuid.New()
	seedTransferBacking(scope.backing, providerID)
	svc := NewConfigTransferService(ConfigTransferScopeBeginnerFunc(func(context.Context) (ConfigTransferScope, error) {
		return scope, nil
	}))
	actor := &auth.Actor{UserID: uuid.New(), IsAdmin: true}

	exported, err := svc.Export(context.Background(), actor)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(exported)
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"usage", "console", "session", "audit", "requestDetail"} {
		if strings.Contains(strings.ToLower(string(raw)), banned) {
			t.Errorf("export contains excluded domain %q: %s", banned, raw)
		}
	}

	// Partial import: domains absent from the payload are still wiped, and
	// the wipe is transactional with the re-insertion.
	now := time.Now().UTC()
	replacement := uuid.New()
	partial := ConfigPayload{
		Settings: map[string]json.RawMessage{"rtkEnabled": json.RawMessage(`true`)},
		ProviderConnections: []ProviderConnectionTransfer{
			{ID: replacement, Name: "replacement", Type: "openai",
				BaseURL: "https://api.replacement.example/v1", Config: json.RawMessage(`{}`),
				IsEnabled: true, CreatedAt: now, UpdatedAt: now},
		},
	}
	if _, err := svc.Import(context.Background(), actor, partial, ConfigImportConfirmation); err != nil {
		t.Fatal(err)
	}
	if _, ok := scope.backing.settings["rtkEnabled"]; !ok {
		t.Error("imported settings missing")
	}
	if len(scope.backing.nodes) != 0 || len(scope.backing.aliases) != 0 || len(scope.backing.pools) != 0 {
		t.Errorf("missing-key wipe did not apply: nodes=%d aliases=%d pools=%d",
			len(scope.backing.nodes), len(scope.backing.aliases), len(scope.backing.pools))
	}
	if len(scope.backing.providers) != 1 || scope.backing.providers[replacement].Name != "replacement" {
		t.Errorf("providers = %+v", scope.backing.providers)
	}
}

func TestConfigTransferService_Import(t *testing.T) {
	t.Parallel()

	providerID := uuid.New()
	now := time.Now().UTC()

	basePayload := ConfigPayload{
		Settings: map[string]json.RawMessage{
			"rtkEnabled": json.RawMessage(`false`),
		},
		ProviderConnections: []ProviderConnectionTransfer{
			{ID: providerID, Name: "replacement", Type: "openai", BaseURL: "https://x.example/v1",
				Config: json.RawMessage(`{}`), IsEnabled: true, CreatedAt: now, UpdatedAt: now},
		},
		ProviderNodes: []ProviderNodeTransfer{
			{ID: "node-new", ProviderID: providerID, Name: "n", BaseURL: "https://n.example"},
		},
		ProxyPools: []ProxyPoolTransfer{
			{ID: uuid.New(), Name: "new-pool", CreatedAt: now, UpdatedAt: now},
		},
		ModelAliases: map[string]ModelAliasTransfer{
			"fast": {ID: uuid.New(), Target: "gpt-4o", ProviderID: &providerID, CreatedAt: now, UpdatedAt: now},
		},
		Pricing: map[string]map[string]PriceTransfer{
			providerID.String(): {"gpt-4o": {InputPrice: 2.5, OutputPrice: 10, UpdatedAt: now}},
		},
	}

	t.Run("actor required", func(t *testing.T) {
		svc := NewConfigTransferService(ConfigTransferScopeBeginnerFunc(func(context.Context) (ConfigTransferScope, error) {
			return newFakeTransferScope(), nil
		}))
		if _, err := svc.Import(context.Background(), nil, basePayload, ConfigImportConfirmation); !errors.Is(err, ErrActorRequired) {
			t.Errorf("err = %v, want ErrActorRequired", err)
		}
	})

	t.Run("confirmation mismatch declines and audits, writes nothing", func(t *testing.T) {
		scope := newFakeTransferScope()
		seedTransferBacking(scope.backing, providerID)
		before := scope.backing.settings["rtkEnabled"]
		svc := NewConfigTransferService(ConfigTransferScopeBeginnerFunc(func(context.Context) (ConfigTransferScope, error) {
			return scope, nil
		}))
		actor := &auth.Actor{UserID: uuid.New(), IsAdmin: true}
		if _, err := svc.Import(context.Background(), actor, basePayload, "wrong confirmation"); !errors.Is(err, ErrImportDeclined) {
			t.Fatalf("err = %v, want ErrImportDeclined", err)
		}
		if len(scope.audit.entries) != 1 || scope.audit.entries[0].Action != "config.transfer.import.decline" {
			t.Fatalf("decline audit = %+v", scope.audit.entries)
		}
		if scope.backing.settings["rtkEnabled"].Key != before.Key ||
			string(scope.backing.settings["rtkEnabled"].Value) != string(before.Value) {
			t.Error("declined import modified settings")
		}
	})

	t.Run("unsupported domains rejected with audit", func(t *testing.T) {
		scope := newFakeTransferScope()
		seedTransferBacking(scope.backing, providerID)
		svc := NewConfigTransferService(ConfigTransferScopeBeginnerFunc(func(context.Context) (ConfigTransferScope, error) {
			return scope, nil
		}))
		actor := &auth.Actor{UserID: uuid.New(), IsAdmin: true}
		payload := basePayload
		payload.APIKeys = json.RawMessage(`[{"id":"x"}]`)
		if _, err := svc.Import(context.Background(), actor, payload, ConfigImportConfirmation); !errors.Is(err, ErrUnsupportedDomain) {
			t.Fatalf("err = %v, want ErrUnsupportedDomain", err)
		}
		if len(scope.audit.entries) != 1 || scope.audit.entries[0].Action != "config.transfer.import.rejected" {
			t.Errorf("rejection audit = %+v", scope.audit.entries)
		}
		if len(scope.backing.providers) == 0 {
			t.Error("rejected import must not touch the backing store")
		}
	})

	t.Run("missing-key wipe replacement applies transactionally", func(t *testing.T) {
		scope := newFakeTransferScope()
		seedTransferBacking(scope.backing, providerID)
		svc := NewConfigTransferService(ConfigTransferScopeBeginnerFunc(func(context.Context) (ConfigTransferScope, error) {
			return scope, nil
		}))
		actor := &auth.Actor{UserID: uuid.New(), IsAdmin: true}

		// Partial payload: no providerNodes, no proxyPools, no modelAliases,
		// no pricing. Those domains must still be wiped (missing-key
		// replacement), matching the pinned upstream importDb.
		partial := ConfigPayload{
			Settings:            map[string]json.RawMessage{"rtkEnabled": json.RawMessage(`true`)},
			ProviderConnections: basePayload.ProviderConnections,
		}
		exported, err := svc.Import(context.Background(), actor, partial, ConfigImportConfirmation)
		if err != nil {
			t.Fatal(err)
		}
		if scope.backing.settings["rtkEnabled"].Key != "rtkEnabled" {
			t.Error("settings not replaced")
		}
		if len(scope.backing.providers) != 1 || scope.backing.providers[providerID].Name != "replacement" {
			t.Errorf("providers after import = %+v", scope.backing.providers)
		}
		if len(scope.backing.nodes) != 0 || len(scope.backing.pools) != 0 || len(scope.backing.aliases) != 0 {
			t.Errorf("missing-key wipe did not apply: nodes=%d pools=%d aliases=%d",
				len(scope.backing.nodes), len(scope.backing.pools), len(scope.backing.aliases))
		}
		if len(scope.audit.entries) != 1 || scope.audit.entries[0].Action != "config.transfer.import.accept" {
			t.Fatalf("accept audit = %+v", scope.audit.entries)
		}
		details := string(scope.audit.entries[0].Details)
		for _, want := range []string{"\"applied\"", "\"counts\"", "\"providerConnections\":1"} {
			if !strings.Contains(details, want) {
				t.Errorf("accept audit details missing %s: %s", want, details)
			}
		}
		// Upstream returns the re-exported payload after import.
		if exported == nil || len(exported.ProviderConnections) != 1 || exported.ProviderConnections[0].Name != "replacement" {
			t.Errorf("re-exported payload = %+v", exported)
		}
	})

	t.Run("validation failure precedes any write", func(t *testing.T) {
		scope := newFakeTransferScope()
		seedTransferBacking(scope.backing, providerID)
		svc := NewConfigTransferService(ConfigTransferScopeBeginnerFunc(func(context.Context) (ConfigTransferScope, error) {
			return scope, nil
		}))
		actor := &auth.Actor{UserID: uuid.New(), IsAdmin: true}
		bad := ConfigPayload{
			Settings: map[string]json.RawMessage{"rtkEnabled": json.RawMessage(`true`)},
			Pricing:  map[string]map[string]PriceTransfer{"": {"m": {InputPrice: 1, OutputPrice: 1}}},
		}
		if _, err := svc.Import(context.Background(), actor, bad, ConfigImportConfirmation); err == nil {
			t.Fatal("invalid payload accepted")
		}
		if _, ok := scope.backing.settings["rtkEnabled"]; !ok {
			t.Error("validation failure must not touch the backing store")
		}
		if len(scope.backing.providers) == 0 {
			t.Error("validation failure must not touch the backing store")
		}
	})

	t.Run("commit failure rolls back", func(t *testing.T) {
		scope := newFakeTransferScope()
		seedTransferBacking(scope.backing, providerID)
		scope.commitErr = errors.New("commit down")
		svc := NewConfigTransferService(ConfigTransferScopeBeginnerFunc(func(context.Context) (ConfigTransferScope, error) {
			return scope, nil
		}))
		actor := &auth.Actor{UserID: uuid.New(), IsAdmin: true}
		if _, err := svc.Import(context.Background(), actor, basePayload, ConfigImportConfirmation); err == nil {
			t.Fatal("commit failure swallowed")
		}
		if len(scope.backing.providers) != 1 || scope.backing.providers[providerID].Name == "replacement" {
			t.Error("commit failure left partial state in the backing store")
		}
	})

	t.Run("decline audit failure", func(t *testing.T) {
		scope := newFakeTransferScope()
		seedTransferBacking(scope.backing, providerID)
		scope.audit.err = errors.New("audit down")
		svc := NewConfigTransferService(ConfigTransferScopeBeginnerFunc(func(context.Context) (ConfigTransferScope, error) {
			return scope, nil
		}))
		if _, err := svc.Import(context.Background(), &auth.Actor{UserID: uuid.New()}, basePayload, "wrong"); err == nil {
			t.Error("decline audit failure swallowed")
		}
	})

	t.Run("decline commit failure", func(t *testing.T) {
		scope := newFakeTransferScope()
		seedTransferBacking(scope.backing, providerID)
		scope.commitErr = errors.New("commit down")
		svc := NewConfigTransferService(ConfigTransferScopeBeginnerFunc(func(context.Context) (ConfigTransferScope, error) {
			return scope, nil
		}))
		if _, err := svc.Import(context.Background(), &auth.Actor{UserID: uuid.New()}, basePayload, "wrong"); err == nil {
			t.Error("decline commit failure swallowed")
		}
	})

	t.Run("rejection audit failure", func(t *testing.T) {
		scope := newFakeTransferScope()
		seedTransferBacking(scope.backing, providerID)
		scope.audit.err = errors.New("audit down")
		svc := NewConfigTransferService(ConfigTransferScopeBeginnerFunc(func(context.Context) (ConfigTransferScope, error) {
			return scope, nil
		}))
		rejected := ConfigPayload{APIKeys: json.RawMessage(`[{"id":"x"}]`)}
		if _, err := svc.Import(context.Background(), &auth.Actor{UserID: uuid.New()}, rejected, ConfigImportConfirmation); err == nil {
			t.Error("rejection audit failure swallowed")
		}
	})

	t.Run("rejection commit failure", func(t *testing.T) {
		scope := newFakeTransferScope()
		seedTransferBacking(scope.backing, providerID)
		scope.commitErr = errors.New("commit down")
		svc := NewConfigTransferService(ConfigTransferScopeBeginnerFunc(func(context.Context) (ConfigTransferScope, error) {
			return scope, nil
		}))
		rejected := ConfigPayload{APIKeys: json.RawMessage(`[{"id":"x"}]`)}
		if _, err := svc.Import(context.Background(), &auth.Actor{UserID: uuid.New()}, rejected, ConfigImportConfirmation); err == nil {
			t.Error("rejection commit failure swallowed")
		}
	})

	t.Run("accept audit failure writes nothing", func(t *testing.T) {
		scope := newFakeTransferScope()
		seedTransferBacking(scope.backing, providerID)
		scope.audit.err = errors.New("audit down")
		svc := NewConfigTransferService(ConfigTransferScopeBeginnerFunc(func(context.Context) (ConfigTransferScope, error) {
			return scope, nil
		}))
		if _, err := svc.Import(context.Background(), &auth.Actor{UserID: uuid.New()}, basePayload, ConfigImportConfirmation); err == nil {
			t.Fatal("accept audit failure swallowed")
		}
		if len(scope.backing.providers) != 1 {
			t.Error("accept audit failure applied staged writes to the backing store")
		}
	})
}

func TestConfigTransferService_BeginError(t *testing.T) {
	t.Parallel()
	beginErr := errors.New("begin down")
	svc := NewConfigTransferService(ConfigTransferScopeBeginnerFunc(func(context.Context) (ConfigTransferScope, error) {
		return nil, beginErr
	}))
	actor := &auth.Actor{UserID: uuid.New(), IsAdmin: true}
	valid := ConfigPayload{Settings: map[string]json.RawMessage{"rtkEnabled": json.RawMessage(`true`)}}
	for name, call := range map[string]func() error{
		"Export": func() error { _, err := svc.Export(context.Background(), actor); return err },
		"ImportDecline": func() error {
			_, err := svc.Import(context.Background(), actor, valid, "wrong")
			return err
		},
		"ImportReject": func() error {
			bad := ConfigPayload{APIKeys: json.RawMessage(`[{"id":"x"}]`)}
			_, err := svc.Import(context.Background(), actor, bad, ConfigImportConfirmation)
			return err
		},
		"ImportAccept": func() error {
			_, err := svc.Import(context.Background(), actor, valid, ConfigImportConfirmation)
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := call(); !errors.Is(err, beginErr) {
				t.Errorf("err = %v, want begin error", err)
			}
		})
	}
}

func TestConfigPayloadValidate(t *testing.T) {
	t.Parallel()

	valid := ConfigPayload{Settings: map[string]json.RawMessage{"rtkEnabled": json.RawMessage(`true`)}}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid payload rejected: %v", err)
	}

	t.Run("nil payload refused", func(t *testing.T) {
		var p *ConfigPayload
		if err := p.Validate(); err == nil {
			t.Error("nil payload accepted")
		}
	})

	t.Run("empty rejected domains tolerated", func(t *testing.T) {
		p := valid
		p.APIKeys = json.RawMessage(`null`)
		p.Combos = json.RawMessage(`not json`)
		p.CustomModels = json.RawMessage(`[]`)
		p.MitmAlias = json.RawMessage(`{}`)
		if err := p.Validate(); err != nil {
			t.Fatalf("empty rejected domains must be tolerated: %v", err)
		}
	})

	t.Run("carried rejected domain refused", func(t *testing.T) {
		p := valid
		p.MitmAlias = json.RawMessage(`[1]`)
		if err := p.Validate(); !errors.Is(err, ErrUnsupportedDomain) {
			t.Errorf("err = %v, want ErrUnsupportedDomain", err)
		}
	})
}

func TestConfigPayloadValidateErrors(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	goodConn := ProviderConnectionTransfer{ID: uuid.New(), Name: "n", Type: "openai",
		BaseURL: "https://x.example", Config: json.RawMessage(`{}`), CreatedAt: now, UpdatedAt: now}
	goodNode := ProviderNodeTransfer{ID: "node", ProviderID: uuid.New(), Name: "n"}
	goodPool := ProxyPoolTransfer{ID: uuid.New(), Name: "pool", CreatedAt: now, UpdatedAt: now}
	goodAlias := ModelAliasTransfer{ID: uuid.New(), Target: "gpt-4o", CreatedAt: now, UpdatedAt: now}
	goodPrice := PriceTransfer{InputPrice: 1, OutputPrice: 2, UpdatedAt: now}
	base := ConfigPayload{
		ProviderConnections: []ProviderConnectionTransfer{goodConn},
		ProviderNodes:       []ProviderNodeTransfer{goodNode},
		ProxyPools:          []ProxyPoolTransfer{goodPool},
		ModelAliases:        map[string]ModelAliasTransfer{"fast": goodAlias},
		Pricing:             map[string]map[string]PriceTransfer{"prov": {"gpt-4o": goodPrice}},
	}
	clone := func() ConfigPayload {
		return ConfigPayload{
			Settings:            map[string]json.RawMessage{"rtkEnabled": json.RawMessage(`true`)},
			ProviderConnections: append([]ProviderConnectionTransfer(nil), base.ProviderConnections...),
			ProviderNodes:       append([]ProviderNodeTransfer(nil), base.ProviderNodes...),
			ProxyPools:          append([]ProxyPoolTransfer(nil), base.ProxyPools...),
			ModelAliases:        map[string]ModelAliasTransfer{"fast": goodAlias},
			Pricing:             map[string]map[string]PriceTransfer{"prov": {"gpt-4o": goodPrice}},
		}
	}
	cases := []struct {
		name   string
		mutate func(*ConfigPayload)
	}{
		{"empty settings key", func(p *ConfigPayload) { p.Settings = map[string]json.RawMessage{"": json.RawMessage(`true`)} }},
		{"settings value not JSON", func(p *ConfigPayload) { p.Settings = map[string]json.RawMessage{"k": json.RawMessage(`{bad`)} }},
		{"typed settings value rejected", func(p *ConfigPayload) {
			p.Settings = map[string]json.RawMessage{settings.KeyRTKEnabled: json.RawMessage(`"yes"`)}
		}},
		{"provider connection missing id", func(p *ConfigPayload) { p.ProviderConnections[0].ID = uuid.Nil }},
		{"provider connection missing name", func(p *ConfigPayload) { p.ProviderConnections[0].Name = "" }},
		{"provider connection config not JSON", func(p *ConfigPayload) {
			p.ProviderConnections[0].Config = json.RawMessage(`{bad`)
		}},
		{"provider node missing id", func(p *ConfigPayload) { p.ProviderNodes[0].ID = "" }},
		{"provider node missing provider", func(p *ConfigPayload) { p.ProviderNodes[0].ProviderID = uuid.Nil }},
		{"proxy pool missing id", func(p *ConfigPayload) { p.ProxyPools[0].ID = uuid.Nil }},
		{"proxy pool missing name", func(p *ConfigPayload) { p.ProxyPools[0].Name = "" }},
		{"model alias empty key", func(p *ConfigPayload) {
			p.ModelAliases = map[string]ModelAliasTransfer{"": goodAlias}
		}},
		{"model alias missing target", func(p *ConfigPayload) {
			p.ModelAliases = map[string]ModelAliasTransfer{"fast": {ID: uuid.New(), CreatedAt: now}}
		}},
		{"pricing empty provider", func(p *ConfigPayload) {
			p.Pricing = map[string]map[string]PriceTransfer{"": {"m": goodPrice}}
		}},
		{"pricing empty model", func(p *ConfigPayload) {
			p.Pricing = map[string]map[string]PriceTransfer{"prov": {"": goodPrice}}
		}},
		{"pricing non-finite price", func(p *ConfigPayload) {
			p.Pricing = map[string]map[string]PriceTransfer{"prov": {"m": {InputPrice: math.NaN(), OutputPrice: 1}}}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := clone()
			tc.mutate(&p)
			if err := p.Validate(); err == nil {
				t.Error("invalid payload accepted")
			}
		})
	}
}
