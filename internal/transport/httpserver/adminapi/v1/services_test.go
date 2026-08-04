package v1

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	appproviders "gorouter/internal/app/providers"
	appsettings "gorouter/internal/app/settings"
	"gorouter/internal/app/tx"
	"gorouter/internal/domain/auth"
	"gorouter/internal/domain/backup"
	"gorouter/internal/domain/combo"
	"gorouter/internal/domain/console"
	"gorouter/internal/domain/keys"
	"gorouter/internal/domain/pricing"
	"gorouter/internal/domain/provider"
	"gorouter/internal/domain/quota"
	"gorouter/internal/domain/settings"
	enginerouting "gorouter/internal/engine/routing"

	"github.com/google/uuid"
)

var errBoom = errors.New("boom")

func okMap() any  { return map[string]any{"ok": true} }
func okList() any { return []any{map[string]any{"ok": true}} }

type fakeProviders struct{}

var _ ProvidersService = &fakeProviders{}

func (fakeProviders) List(ctx context.Context) ([]provider.Provider, error) {
	secret := "sk-provider-secret"
	return []provider.Provider{{ID: uuid.New(), Name: "p", Type: provider.ProviderOpenAI, APIKeyValue: &secret, IsEnabled: true}}, nil
}
func (fakeProviders) Get(ctx context.Context, id uuid.UUID) (*provider.Provider, error) {
	return &provider.Provider{ID: id, Name: "p", Type: provider.ProviderOpenAI, IsEnabled: true}, nil
}
func (fakeProviders) Create(ctx context.Context, in *provider.Provider) (*provider.Provider, error) {
	return in, nil
}
func (fakeProviders) Update(ctx context.Context, in *provider.Provider) (*provider.Provider, error) {
	return in, nil
}
func (fakeProviders) Delete(ctx context.Context, id uuid.UUID) error { return nil }
func (fakeProviders) Client(ctx context.Context) ([]provider.Provider, error) {
	return []provider.Provider{{ID: uuid.New(), Name: "p", Type: provider.ProviderOpenAI, IsEnabled: true}}, nil
}
func (fakeProviders) KiloFreeModels(ctx context.Context) ([]string, error) {
	return []string{"k1"}, nil
}
func (fakeProviders) SuggestedModels(ctx context.Context) ([]string, error) {
	return []string{"s1"}, nil
}
func (fakeProviders) TestBatch(ctx context.Context, ids []uuid.UUID, concurrencyLimit int) (appproviders.TestBatchResult, error) {
	return appproviders.TestBatchResult{Results: []appproviders.ProviderTestResult{{ProviderID: uuid.New(), Name: "p", OK: true}}}, nil
}
func (fakeProviders) Validate(ctx context.Context, id uuid.UUID) error { return nil }

type fakeAccounts struct{}

var _ AccountsService = &fakeAccounts{}

func (fakeAccounts) List(ctx context.Context, providerID string) ([]provider.Account, error) {
	return []provider.Account{{ID: uuid.New(), ProviderID: uuid.New(), Label: "a", AuthType: "api_key"}}, nil
}
func (fakeAccounts) Update(ctx context.Context, account *provider.Account) error { return nil }

type fakeNodes struct{}

var _ NodesService = &fakeNodes{}

func (fakeNodes) List(ctx context.Context) ([]enginerouting.ProviderNode, error) {
	return []enginerouting.ProviderNode{{ID: "n1", ProviderID: uuid.New(), Name: "n", BaseURL: "http://x", IsActive: true}}, nil
}
func (fakeNodes) Get(ctx context.Context, id string) (*enginerouting.ProviderNode, error) {
	return &enginerouting.ProviderNode{ID: id, Name: "n", IsActive: true}, nil
}
func (fakeNodes) Save(ctx context.Context, actor *auth.Actor, node enginerouting.ProviderNode) error {
	return nil
}
func (fakeNodes) Update(ctx context.Context, node enginerouting.ProviderNode) error { return nil }
func (fakeNodes) Delete(ctx context.Context, id string) error                       { return nil }
func (fakeNodes) Validate(ctx context.Context, id string) error                     { return nil }

type fakePools struct{}

var _ PoolsService = &fakePools{}

func (fakePools) List(ctx context.Context) ([]provider.ProxyPool, error) {
	return []provider.ProxyPool{{ID: uuid.New(), Name: "pool"}}, nil
}
func (fakePools) Get(ctx context.Context, id uuid.UUID) (*provider.ProxyPool, error) {
	return &provider.ProxyPool{ID: id, Name: "pool"}, nil
}
func (fakePools) Create(ctx context.Context, actor *auth.Actor, pool *provider.ProxyPool) (*provider.ProxyPool, error) {
	return pool, nil
}
func (fakePools) Update(ctx context.Context, actor *auth.Actor, pool *provider.ProxyPool) (*provider.ProxyPool, error) {
	return pool, nil
}
func (fakePools) Delete(ctx context.Context, actor *auth.Actor, id uuid.UUID) error { return nil }

type fakeCombos struct{}

var _ CombosService = &fakeCombos{}

func (fakeCombos) List(ctx context.Context) ([]combo.Definition, error) {
	return []combo.Definition{{ID: uuid.New(), Name: "c", Strategy: combo.StrategyRoundRobin, IsActive: true}}, nil
}
func (fakeCombos) Get(ctx context.Context, id uuid.UUID) (*combo.Definition, error) {
	return &combo.Definition{ID: id, Name: "c", Strategy: combo.StrategyRoundRobin, IsActive: true}, nil
}
func (fakeCombos) Create(ctx context.Context, actor *auth.Actor, name string, strategy combo.Strategy, config json.RawMessage, members []combo.Member) (*combo.Definition, error) {
	return &combo.Definition{ID: uuid.New(), Name: name, Strategy: strategy, IsActive: true}, nil
}
func (fakeCombos) Update(ctx context.Context, actor *auth.Actor, def *combo.Definition) error {
	return nil
}
func (fakeCombos) Delete(ctx context.Context, actor *auth.Actor, id uuid.UUID) error { return nil }

type fakeAliases struct{}

var _ AliasesService = &fakeAliases{}

func (fakeAliases) List(ctx context.Context) ([]enginerouting.Alias, error) {
	return []enginerouting.Alias{{ID: uuid.New(), Alias: "a", Target: "gpt-4", IsActive: true}}, nil
}
func (fakeAliases) Set(ctx context.Context, alias *enginerouting.Alias) error { return nil }
func (fakeAliases) Delete(ctx context.Context, id uuid.UUID) error            { return nil }

type fakeModels struct{}

var _ ModelsService = &fakeModels{}

func (fakeModels) List(ctx context.Context) (any, error)                   { return okList(), nil }
func (fakeModels) Update(ctx context.Context, in any) (any, error)         { return in, nil }
func (fakeModels) CustomList(ctx context.Context) (any, error)             { return okList(), nil }
func (fakeModels) CustomCreate(ctx context.Context, in any) (any, error)   { return in, nil }
func (fakeModels) CustomDelete(ctx context.Context, in any) error          { return nil }
func (fakeModels) DisabledList(ctx context.Context) (any, error)           { return okList(), nil }
func (fakeModels) DisabledCreate(ctx context.Context, in any) (any, error) { return in, nil }
func (fakeModels) DisabledDelete(ctx context.Context, in any) error        { return nil }
func (fakeModels) Test(ctx context.Context, in any) (any, error)           { return in, nil }
func (fakeModels) Availability(ctx context.Context, in any) (any, error)   { return in, nil }

type fakePricing struct{}

var _ PricingService = &fakePricing{}

func (fakePricing) ListOverrides(ctx context.Context) ([]pricing.PriceOverride, error) {
	return []pricing.PriceOverride{{ModelID: "m", ProviderID: "p", InputPrice: 1, OutputPrice: 2}}, nil
}
func (fakePricing) ApplyOverride(ctx context.Context, actor *auth.Actor, override pricing.PriceOverride) (pricing.PriceOverride, error) {
	return override, nil
}
func (fakePricing) Reset(ctx context.Context, actor *auth.Actor) error { return nil }

type fakeUsage struct{}

var _ UsageService = &fakeUsage{}

func (fakeUsage) Stats(ctx context.Context) (any, error)          { return okMap(), nil }
func (fakeUsage) History(ctx context.Context) (any, error)        { return okList(), nil }
func (fakeUsage) Chart(ctx context.Context) (any, error)          { return okList(), nil }
func (fakeUsage) Providers(ctx context.Context) (any, error)      { return okList(), nil }
func (fakeUsage) RequestDetails(ctx context.Context) (any, error) { return okList(), nil }
func (fakeUsage) RequestLogs(ctx context.Context) (any, error)    { return okList(), nil }
func (fakeUsage) Logs(ctx context.Context) (any, error)           { return okList(), nil }
func (fakeUsage) Connection(ctx context.Context, connectionID string) (any, error) {
	return map[string]any{"connectionId": connectionID}, nil
}

type fakeQuota struct{}

var _ QuotaService = &fakeQuota{}

func (fakeQuota) Status(ctx context.Context, actor *auth.Actor, providerID uuid.UUID) (*quota.QuotaStatus, error) {
	return &quota.QuotaStatus{ProviderID: providerID, WindowStart: time.Now()}, nil
}
func (fakeQuota) Unlock(ctx context.Context, actor *auth.Actor, providerID uuid.UUID) error {
	return nil
}
func (fakeQuota) Reset(ctx context.Context, actor *auth.Actor, providerID uuid.UUID) error {
	return nil
}

type fakeMutators struct{}

var _ MutatorsService = &fakeMutators{}

func (fakeMutators) List(ctx context.Context) (any, error)                    { return okList(), nil }
func (fakeMutators) Set(ctx context.Context, name string, enabled bool) error { return nil }

type fakeCLITools struct{}

var _ CLIToolsService = &fakeCLITools{}

func (fakeCLITools) AllStatuses(ctx context.Context) (any, error)          { return okMap(), nil }
func (fakeCLITools) ToolGet(ctx context.Context, tool string) (any, error) { return okMap(), nil }
func (fakeCLITools) ToolAction(ctx context.Context, tool string, in any) (any, error) {
	return in, nil
}
func (fakeCLITools) AntigravityAlias(ctx context.Context, in any) (any, error) { return in, nil }

type fakeMedia struct{}

var _ MediaService = &fakeMedia{}

func (fakeMedia) Voices(ctx context.Context, providerKind, providerID, lang string) (any, error) {
	return map[string]any{"provider": providerKind}, nil
}

type fakeSkills struct{}

var _ SkillsService = &fakeSkills{}

func (fakeSkills) Catalog(ctx context.Context) ([]Skill, error) {
	return []Skill{{ID: "s", Name: "skill"}}, nil
}

type fakeConsole struct{}

var _ ConsoleService = &fakeConsole{}

func (fakeConsole) ListAfter(ctx context.Context, seq int64, limit int) ([]console.ConsoleLog, error) {
	return []console.ConsoleLog{{ID: 1, Seq: 1, RedactedMessage: "line"}}, nil
}

type fakeTunnels struct{}

var _ TunnelsService = &fakeTunnels{}

func (fakeTunnels) Get(ctx context.Context) (any, error)            { return okMap(), nil }
func (fakeTunnels) Status(ctx context.Context) (any, error)         { return okMap(), nil }
func (fakeTunnels) TailscaleCheck(ctx context.Context) (any, error) { return okMap(), nil }
func (fakeTunnels) Enable(ctx context.Context) (any, error)         { return okMap(), nil }
func (fakeTunnels) Disable(ctx context.Context) (any, error)        { return okMap(), nil }
func (fakeTunnels) TailscaleInstall(ctx context.Context) (any, error) {
	return okMap(), nil
}
func (fakeTunnels) TailscaleEnable(ctx context.Context) (any, error) { return okMap(), nil }
func (fakeTunnels) TailscaleDisable(ctx context.Context) (any, error) {
	return okMap(), nil
}

type fakeSettings struct{}

var _ SettingsService = &fakeSettings{}

func (fakeSettings) Get(ctx context.Context, actor *auth.Actor, key string) (*settings.Setting, error) {
	return &settings.Setting{Key: key, Value: json.RawMessage(`"v"`)}, nil
}
func (fakeSettings) List(ctx context.Context, actor *auth.Actor) ([]settings.Setting, error) {
	return []settings.Setting{{Key: "k", Value: json.RawMessage(`"v"`)}}, nil
}
func (fakeSettings) Update(ctx context.Context, actor *auth.Actor, values map[string]json.RawMessage) error {
	return nil
}
func (fakeSettings) ProxyTest(ctx context.Context, actor *auth.Actor, in any) (any, error) {
	return okMap(), nil
}
func (fakeSettings) RequireLogin(ctx context.Context, actor *auth.Actor) (*settings.Setting, error) {
	return &settings.Setting{Key: "require_login", Value: json.RawMessage("true")}, nil
}
func (fakeSettings) SetRequireLogin(ctx context.Context, actor *auth.Actor, enabled bool) error {
	return nil
}

type fakeTransfer struct{}

var _ ConfigTransferService = &fakeTransfer{}

func (fakeTransfer) Export(ctx context.Context, actor *auth.Actor) (*appsettings.ConfigPayload, error) {
	return &appsettings.ConfigPayload{Settings: map[string]json.RawMessage{}}, nil
}
func (fakeTransfer) Import(ctx context.Context, actor *auth.Actor, payload appsettings.ConfigPayload, confirmation string) (*appsettings.ConfigPayload, error) {
	return &payload, nil
}

type fakeTranslator struct{}

var _ TranslatorService = &fakeTranslator{}

func (fakeTranslator) Translate(ctx context.Context, in any) (any, error) { return in, nil }
func (fakeTranslator) Load(ctx context.Context) (any, error)              { return okMap(), nil }
func (fakeTranslator) Save(ctx context.Context, in any) (any, error)      { return in, nil }
func (fakeTranslator) Send(ctx context.Context, in any) (any, error)      { return in, nil }
func (fakeTranslator) ConsoleLogs(ctx context.Context) (any, error)       { return okList(), nil }
func (fakeTranslator) ClearConsoleLogs(ctx context.Context) error         { return nil }

type fakeOAuth struct{}

var _ OAuthService = &fakeOAuth{}

func (fakeOAuth) Status(ctx context.Context, providerName string) (any, error) {
	return map[string]any{"provider": providerName}, nil
}
func (fakeOAuth) Connect(ctx context.Context, providerName string, in any) (any, error) {
	return okMap(), nil
}
func (fakeOAuth) Disconnect(ctx context.Context, providerName string) (any, error) {
	return okMap(), nil
}
func (fakeOAuth) Import(ctx context.Context, flow string, in any) (any, error) {
	return map[string]any{"flow": flow}, nil
}

type fakeHostOps struct{}

var _ HostOpsService = &fakeHostOps{}

func (fakeHostOps) Action(ctx context.Context, feature, action string, in any) (any, error) {
	return map[string]any{"feature": feature, "action": action}, nil
}
func (fakeHostOps) Version(ctx context.Context) (any, error) {
	return map[string]any{"version": "1.0"}, nil
}

type fakeBackups struct{}

var _ BackupsService = &fakeBackups{}

func (fakeBackups) List(ctx context.Context, actor *auth.Actor) ([]backup.Backup, error) {
	return []backup.Backup{{ID: uuid.New(), Path: "/tmp/x", SHA256: "abc", Bytes: 3}}, nil
}
func (fakeBackups) Download(ctx context.Context, actor *auth.Actor, ip net.IP, id uuid.UUID) (*BackupDownload, error) {
	f, err := os.CreateTemp("", "backup-dl-*")
	if err != nil {
		return nil, err
	}
	_, _ = f.WriteString("abc")
	_ = f.Close()
	return &BackupDownload{Path: f.Name(), Bytes: 3}, nil
}
func (fakeBackups) Verify(ctx context.Context, actor *auth.Actor, id uuid.UUID) error { return nil }

type fakeAudit struct{}

var _ AuditService = &fakeAudit{}

func (fakeAudit) List(ctx context.Context, actor *auth.Actor, filters tx.AuditFilters, page tx.AuditPage) ([]tx.AuditEntry, error) {
	return []tx.AuditEntry{secretAuditEntry()}, nil
}
func (fakeAudit) Export(ctx context.Context, actor *auth.Actor, filters tx.AuditFilters) ([]tx.AuditEntry, error) {
	return []tx.AuditEntry{secretAuditEntry()}, nil
}
func (fakeAudit) Get(ctx context.Context, actor *auth.Actor, id uuid.UUID) (*tx.AuditEntry, error) {
	e := secretAuditEntry()
	return &e, nil
}

func secretAuditEntry() tx.AuditEntry {
	actorID := uuid.New()
	return tx.AuditEntry{
		ID: uuid.New(), ActorID: &actorID, ActorKind: "user",
		Action: "keys.create", ResourceType: "key", ResourceID: &actorID,
		IPAddress:  net.ParseIP("10.0.0.1"),
		Details:    json.RawMessage(`{"api_key_value":"sk-audit-secret","ok":1}`),
		OccurredAt: time.Now(),
	}
}

// fullDeps wires every seam with a successful fake.
func fullDeps() Dependencies {
	return Dependencies{
		Keys: &fakeKeysService{}, PATs: &fakePATsService{}, Providers: &fakeProviders{},
		Accounts: &fakeAccounts{}, Nodes: &fakeNodes{}, Pools: &fakePools{},
		Combos: &fakeCombos{}, Aliases: &fakeAliases{}, Models: &fakeModels{},
		Pricing: &fakePricing{}, Usage: &fakeUsage{}, Quota: &fakeQuota{},
		Mutators: &fakeMutators{}, CLITools: &fakeCLITools{}, Media: &fakeMedia{},
		Skills: &fakeSkills{}, Console: &fakeConsole{}, Tunnels: &fakeTunnels{},
		Settings: &fakeSettings{}, ConfigTransfer: &fakeTransfer{}, Translator: &fakeTranslator{},
		OAuth: &fakeOAuth{}, Jobs: &fakeJobsService{}, HostOps: &fakeHostOps{},
		Backups: &fakeBackups{}, Audit: &fakeAudit{},
		ValidatePAT: fakePAT,
	}
}

// allHostFlags opens every host-operation feature for the route test.
func allHostFlags() map[string]bool {
	return map[string]bool{
		"deploy": true, "tunnel": true, "tailscale": true, "headroom": true,
		"pxpipe": true, "mcp": true, "shutdown": true, "updater": true,
	}
}

// TestEveryRouteResolvesWithServices exercises every registered route against
// the wired service fakes: host flags open, confirm echoed on host
// mutations, path parameters substituted. The only non-2xx routes are the
// placeholder stream/meta surfaces (503, wired by later atoms).
func TestEveryRouteResolvesWithServices(t *testing.T) {
	cfg := Config{Auth: authFake{}, OIDC: authFake{}, HostFlags: allHostFlags(), Resources: fullDeps()}
	h := testRouter(cfg)
	sess := []*http.Cookie{{Name: "gorouter_session", Value: "raw"}}
	csrf := []*http.Cookie{{Name: "gorouter_session", Value: "raw"}, {Name: "gorouter_csrf", Value: "tok"}}

	placeholder := map[string]bool{
		"GET /usage/stream": true, "GET /jobs/stream": true, "GET /console/stream": true,
		"GET /mcp/{plugin}/sse": true, "GET /translator/console-logs/stream": true,
		"POST /init": true, "GET /init": true, "POST /locale": true, "GET /locale": true,
		"GET /tags": true,
	}

	for _, rt := range RouteTable(New(cfg)) {
		path := expandPath(rt.Path)
		full := "/api/admin/v1" + path
		key := rt.Method + " " + rt.Path
		headers := map[string]string{}
		var cookies []*http.Cookie
		switch rt.Mode {
		case AuthPublic:
		case AuthSession:
			cookies = sess
			if rt.Method != http.MethodGet && rt.Method != http.MethodHead {
				cookies = csrf
				headers["X-CSRF-Token"] = "tok"
			}
		case AuthSessionPAT:
			headers["Authorization"] = "Bearer pat-1"
		}
		rr := doJSON(t, h, rt.Method, full, cookies, headers, bodyFor(rt))
		if placeholder[key] {
			if rr.Code != http.StatusServiceUnavailable {
				t.Errorf("placeholder route %s = %d, want 503", key, rr.Code)
			}
			continue
		}
		if rr.Code < 200 || rr.Code >= 300 {
			t.Errorf("route %s %s = %d (body %s), want 2xx with services wired", rt.Method, rt.Path, rr.Code, rr.Body.String())
		}
	}
}

// expandPath substitutes route path parameters so the sweep exercises real
// matching.
func expandPath(path string) string {
	repl := map[string]string{
		"{id}":           uuid.NewString(),
		"{type}":         "refresh",
		"{connectionId}": "c1",
		"{tool}":         "kilo-settings",
		"{provider}":     "anthropic",
		"{plugin}":       "mp",
		"{path}":         "probe",
	}
	for k, v := range repl {
		path = strings.ReplaceAll(path, k, v)
	}
	return path
}

// bodyFor returns a minimally-valid request body for a route so the
// handler reaches its service call and projection.
func bodyFor(rt Route) string {
	if rt.HostFeature != "" && rt.Method != http.MethodGet && rt.Method != http.MethodHead {
		return `{"confirm":true}`
	}
	switch rt.Method + " " + rt.Path {
	case "POST /keys", "PUT /keys/{id}":
		return `{"name":"k"}`
	case "POST /pats":
		return `{"description":"d"}`
	case "POST /providers", "PUT /providers/{id}":
		return `{"name":"p","type":"openai"}`
	case "POST /providers/test-batch":
		return `{"ids":[],"concurrency_limit":2}`
	case "POST /providers/validate":
		return `{"id":"` + uuid.NewString() + `"}`
	case "POST /provider-nodes", "PUT /provider-nodes/{id}":
		return `{"name":"n"}`
	case "POST /proxy-pools", "PUT /proxy-pools/{id}":
		return `{"name":"pool"}`
	case "POST /combos", "PUT /combos/{id}":
		return `{"name":"c","strategy":"roundrobin"}`
	case "PUT /models/alias":
		return `{"alias":"a","target":"gpt-4"}`
	case "PUT /models", "POST /models/custom", "POST /models/disabled":
		return `{"name":"m"}`
	case "PATCH /pricing":
		return `{"model_id":"m","provider_id":"p"}`
	case "PATCH /settings":
		return `{"theme":"dark"}`
	case "PUT /settings/require-login":
		return `{"value":true}`
	case "POST /settings/database":
		return `{"payload":{},"confirmation":"x"}`
	case "POST /translator/translate", "POST /translator/save", "POST /translator/send":
		return `{"text":"hi"}`
	case "POST /cli-tools/{tool}", "POST /cli-tools/antigravity-mitm/alias":
		return `{"settings":{}}`
	case "POST /oauth/{provider}",
		"POST /oauth/codex/import-token", "POST /oauth/codex/bulk-import",
		"POST /oauth/cursor/import", "POST /oauth/cursor/auto-import",
		"POST /oauth/gitlab/pat", "POST /oauth/iflow/cookie", "POST /oauth/kiro/import",
		"POST /oauth/kiro/auto-import", "POST /oauth/kiro/import-cli-proxy",
		"POST /oauth/kiro/api-key", "POST /oauth/kiro/social-authorize",
		"POST /oauth/kiro/social-exchange":
		return `{"token":"x"}`
	}
	return `{}`
}

// TestHandlerErrorPaths asserts the mapping error mapping: bad ids, invalid
// bodies and service errors map to 400/404/500 through writeError.
func TestHandlerErrorPaths(t *testing.T) {
	fail := &failingKeys{err: combo.ErrNotFound}
	deps := Dependencies{ValidatePAT: fakePAT, Keys: fail}
	h := testRouter(Config{Auth: authFake{}, Resources: deps})
	headers := bearer("pat-1")

	if rr := doJSON(t, h, http.MethodGet, "/api/admin/v1/keys/not-a-uuid", nil, headers, ""); rr.Code != http.StatusBadRequest {
		t.Fatalf("bad uuid = %d, want 400", rr.Code)
	}
	if rr := doJSON(t, h, http.MethodPost, "/api/admin/v1/keys", nil, headers, `{"name":""}`); rr.Code != http.StatusBadRequest {
		t.Fatalf("empty name = %d, want 400", rr.Code)
	}
	if rr := doJSON(t, h, http.MethodPost, "/api/admin/v1/keys", nil, headers, `{invalid`); rr.Code != http.StatusBadRequest {
		t.Fatalf("malformed body = %d, want 400", rr.Code)
	}
	if rr := doJSON(t, h, http.MethodGet, "/api/admin/v1/keys/"+uuid.New().String(), nil, headers, ""); rr.Code != http.StatusNotFound {
		t.Fatalf("service not-found = %d, want 404", rr.Code)
	}
	fail.err = errBoom
	if rr := doJSON(t, h, http.MethodGet, "/api/admin/v1/keys/"+uuid.New().String(), nil, headers, ""); rr.Code != http.StatusInternalServerError {
		t.Fatalf("service error = %d, want 500", rr.Code)
	}
}

// failingKeys returns per-test errors for the keys seam.
type failingKeys struct {
	err error
}

var _ KeysService = &failingKeys{}

func (f *failingKeys) List(ctx context.Context, userID uuid.UUID) ([]keys.APIKey, error) {
	return nil, f.err
}
func (f *failingKeys) Get(ctx context.Context, id uuid.UUID) (*keys.APIKey, error) { return nil, f.err }
func (f *failingKeys) Create(ctx context.Context, userID uuid.UUID, name string, expiresAt *time.Time) (*keys.APIKey, string, error) {
	return nil, "", f.err
}
func (f *failingKeys) Update(ctx context.Context, id uuid.UUID, name string, expiresAt *time.Time) (*keys.APIKey, error) {
	return nil, f.err
}
func (f *failingKeys) Revoke(ctx context.Context, id uuid.UUID) error { return f.err }

// TestAuditProjectionRedacts asserts the audit projection redacts
// credential-shaped remnants and preserves the provenance fields.
func TestAuditProjectionRedacts(t *testing.T) {
	deps := Dependencies{ValidatePAT: fakePAT, Audit: &fakeAudit{}}
	h := testRouter(Config{Auth: authFake{}, Resources: deps})
	rr := doJSON(t, h, http.MethodGet, "/api/admin/v1/audit", nil, bearer("pat-1"), "")
	if rr.Code != http.StatusOK {
		t.Fatalf("audit list = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if strings.Contains(body, "sk-audit-secret") {
		t.Fatalf("audit projection leaked a credential: %s", body)
	}
	if !strings.Contains(body, `"actor_kind":"user"`) || !strings.Contains(body, `"ip_address":"10.0.0.1"`) {
		t.Fatalf("audit projection missing provenance: %s", body)
	}
}

// TestHostOpsVersionRead asserts the non-gated version read resolves.
func TestHostOpsVersionRead(t *testing.T) {
	deps := Dependencies{ValidatePAT: fakePAT, HostOps: &fakeHostOps{}}
	h := testRouter(Config{Auth: authFake{}, Resources: deps})
	rr := doJSON(t, h, http.MethodGet, "/api/admin/v1/version", nil, bearer("pat-1"), "")
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "1.0") {
		t.Fatalf("version = %d %s, want 200 with version", rr.Code, rr.Body.String())
	}
}

// TestProjectionsDirect covers the remaining projection helpers.
func TestProjectionsDirect(t *testing.T) {
	secret := "sk-x"
	p := provider.Provider{ID: uuid.New(), Name: "p", APIKeyValue: &secret, IsEnabled: true}
	if v := projectProvider(&p); !v.HasCredentials || v.Name != "p" {
		t.Fatalf("projectProvider = %+v", v)
	}
	noSecret := provider.Provider{ID: uuid.New(), Name: "p"}
	if v := projectProvider(&noSecret); v.HasCredentials {
		t.Fatal("projectProvider must not claim credentials when none are configured")
	}
	pat := keys.PAT{ID: uuid.New(), TokenHash: "0123456789abcdef"}
	if v := projectPAT(&pat); v.TokenPrefix != "01234567" {
		t.Fatalf("projectPAT prefix = %q", v.TokenPrefix)
	}
	qs := quota.QuotaStatus{ProviderID: uuid.New(), WindowStart: time.Now(), ErrorKind: quota.ErrorKindTemporary}
	if v := projectQuotaStatus(&qs); v.ProviderID != qs.ProviderID || v.ErrorKind == "" {
		t.Fatalf("projectQuotaStatus = %+v", v)
	}
	acc := provider.Account{ID: uuid.New(), ProviderID: uuid.New(), Label: "a", CredentialRef: "ref"}
	if v := projectAccount(&acc); v.Label != "a" {
		t.Fatalf("projectAccount = %+v", v)
	}
	ov := pricing.PriceOverride{ModelID: "m", ProviderID: "p", InputPrice: 1}
	if v := projectOverride(ov); v.ModelID != "m" || v.InputPrice != 1 {
		t.Fatalf("projectOverride = %+v", v)
	}
	key := keys.APIKey{ID: uuid.New(), Name: "k", KeyPrefix: "sk-"}
	if v := projectAPIKey(&key); v.KeyPrefix != "sk-" {
		t.Fatalf("projectAPIKey = %+v", v)
	}
	cd := combo.Definition{ID: uuid.New(), Name: "c", Strategy: combo.StrategyRoundRobin}
	if v := projectCombo(&cd); v.Name != "c" || v.Strategy != string(combo.StrategyRoundRobin) {
		t.Fatalf("projectCombo = %+v", v)
	}
	if v := projectAuditEntry(secretAuditEntry()); v.ActorKind != "user" || v.IPAddress != "10.0.0.1" {
		t.Fatalf("projectAuditEntry = %+v", v)
	}
}
