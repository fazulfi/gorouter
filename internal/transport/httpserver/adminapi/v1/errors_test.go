package v1

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
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
	"gorouter/internal/domain/jobs"
	"gorouter/internal/domain/keys"
	"gorouter/internal/domain/pricing"
	"gorouter/internal/domain/provider"
	"gorouter/internal/domain/quota"
	"gorouter/internal/domain/settings"
	enginerouting "gorouter/internal/engine/routing"

	"github.com/google/uuid"
)

// errKeys returns errBoom from every keys seam method.
type errKeys struct{}

var _ KeysService = &errKeys{}

func (errKeys) List(ctx context.Context, userID uuid.UUID) ([]keys.APIKey, error) { return nil, errBoom }
func (errKeys) Get(ctx context.Context, id uuid.UUID) (*keys.APIKey, error)        { return nil, errBoom }
func (errKeys) Create(ctx context.Context, userID uuid.UUID, name string, expiresAt *time.Time) (*keys.APIKey, string, error) {
	return nil, "", errBoom
}
func (errKeys) Update(ctx context.Context, id uuid.UUID, name string, expiresAt *time.Time) (*keys.APIKey, error) {
	return nil, errBoom
}
func (errKeys) Revoke(ctx context.Context, id uuid.UUID) error { return errBoom }

type errPats struct{}

var _ PATsService = &errPats{}

func (errPats) List(ctx context.Context, userID uuid.UUID) ([]keys.PAT, error) { return nil, errBoom }
func (errPats) Get(ctx context.Context, id uuid.UUID) (*keys.PAT, error)        { return nil, errBoom }
func (errPats) Create(ctx context.Context, userID uuid.UUID, description *string, expiresAt *time.Time) (*keys.PAT, string, error) {
	return nil, "", errBoom
}
func (errPats) Revoke(ctx context.Context, id uuid.UUID) error { return errBoom }

type errProviders struct{}

var _ ProvidersService = &errProviders{}

func (errProviders) List(ctx context.Context) ([]provider.Provider, error)          { return nil, errBoom }
func (errProviders) Get(ctx context.Context, id uuid.UUID) (*provider.Provider, error) {
	return nil, errBoom
}
func (errProviders) Create(ctx context.Context, in *provider.Provider) (*provider.Provider, error) {
	return nil, errBoom
}
func (errProviders) Update(ctx context.Context, in *provider.Provider) (*provider.Provider, error) {
	return nil, errBoom
}
func (errProviders) Delete(ctx context.Context, id uuid.UUID) error { return errBoom }
func (errProviders) Client(ctx context.Context) ([]provider.Provider, error) {
	return nil, errBoom
}
func (errProviders) KiloFreeModels(ctx context.Context) ([]string, error)  { return nil, errBoom }
func (errProviders) SuggestedModels(ctx context.Context) ([]string, error) { return nil, errBoom }
func (errProviders) TestBatch(ctx context.Context, ids []uuid.UUID, concurrencyLimit int) (appproviders.TestBatchResult, error) {
	return appproviders.TestBatchResult{}, errBoom
}
func (errProviders) Validate(ctx context.Context, id uuid.UUID) error { return errBoom }

type errAccounts struct{}

var _ AccountsService = &errAccounts{}

func (errAccounts) List(ctx context.Context, providerID string) ([]provider.Account, error) {
	return nil, errBoom
}
func (errAccounts) Update(ctx context.Context, account *provider.Account) error { return errBoom }

type errNodes struct{}

var _ NodesService = &errNodes{}

func (errNodes) List(ctx context.Context) ([]enginerouting.ProviderNode, error) {
	return nil, errBoom
}
func (errNodes) Get(ctx context.Context, id string) (*enginerouting.ProviderNode, error) {
	return nil, errBoom
}
func (errNodes) Save(ctx context.Context, actor *auth.Actor, node enginerouting.ProviderNode) error {
	return errBoom
}
func (errNodes) Update(ctx context.Context, node enginerouting.ProviderNode) error { return errBoom }
func (errNodes) Delete(ctx context.Context, id string) error                       { return errBoom }
func (errNodes) Validate(ctx context.Context, id string) error                     { return errBoom }

type errPools struct{}

var _ PoolsService = &errPools{}

func (errPools) List(ctx context.Context) ([]provider.ProxyPool, error) { return nil, errBoom }
func (errPools) Get(ctx context.Context, id uuid.UUID) (*provider.ProxyPool, error) {
	return nil, errBoom
}
func (errPools) Create(ctx context.Context, actor *auth.Actor, pool *provider.ProxyPool) (*provider.ProxyPool, error) {
	return nil, errBoom
}
func (errPools) Update(ctx context.Context, actor *auth.Actor, pool *provider.ProxyPool) (*provider.ProxyPool, error) {
	return nil, errBoom
}
func (errPools) Delete(ctx context.Context, actor *auth.Actor, id uuid.UUID) error { return errBoom }

type errCombos struct{}

var _ CombosService = &errCombos{}

func (errCombos) List(ctx context.Context) ([]combo.Definition, error) { return nil, errBoom }
func (errCombos) Get(ctx context.Context, id uuid.UUID) (*combo.Definition, error) {
	return nil, errBoom
}
func (errCombos) Create(ctx context.Context, actor *auth.Actor, name string, strategy combo.Strategy, config json.RawMessage, members []combo.Member) (*combo.Definition, error) {
	return nil, errBoom
}
func (errCombos) Update(ctx context.Context, actor *auth.Actor, def *combo.Definition) error {
	return errBoom
}
func (errCombos) Delete(ctx context.Context, actor *auth.Actor, id uuid.UUID) error { return errBoom }

type errAliases struct{}

var _ AliasesService = &errAliases{}

func (errAliases) List(ctx context.Context) ([]enginerouting.Alias, error) { return nil, errBoom }
func (errAliases) Set(ctx context.Context, alias *enginerouting.Alias) error {
	return errBoom
}
func (errAliases) Delete(ctx context.Context, id uuid.UUID) error { return errBoom }

type errModels struct{}

var _ ModelsService = &errModels{}

func (errModels) List(ctx context.Context) (any, error)                   { return nil, errBoom }
func (errModels) Update(ctx context.Context, in any) (any, error)         { return nil, errBoom }
func (errModels) CustomList(ctx context.Context) (any, error)             { return nil, errBoom }
func (errModels) CustomCreate(ctx context.Context, in any) (any, error)   { return nil, errBoom }
func (errModels) CustomDelete(ctx context.Context, in any) error          { return errBoom }
func (errModels) DisabledList(ctx context.Context) (any, error)           { return nil, errBoom }
func (errModels) DisabledCreate(ctx context.Context, in any) (any, error) { return nil, errBoom }
func (errModels) DisabledDelete(ctx context.Context, in any) error        { return errBoom }
func (errModels) Test(ctx context.Context, in any) (any, error)           { return nil, errBoom }
func (errModels) Availability(ctx context.Context, in any) (any, error)   { return nil, errBoom }

type errPricing struct{}

var _ PricingService = &errPricing{}

func (errPricing) ListOverrides(ctx context.Context) ([]pricing.PriceOverride, error) {
	return nil, errBoom
}
func (errPricing) ApplyOverride(ctx context.Context, actor *auth.Actor, override pricing.PriceOverride) (pricing.PriceOverride, error) {
	return pricing.PriceOverride{}, errBoom
}
func (errPricing) Reset(ctx context.Context, actor *auth.Actor) error { return errBoom }

type errUsage struct{}

var _ UsageService = &errUsage{}

func (errUsage) Stats(ctx context.Context) (any, error)                   { return nil, errBoom }
func (errUsage) History(ctx context.Context) (any, error)                 { return nil, errBoom }
func (errUsage) Chart(ctx context.Context) (any, error)                   { return nil, errBoom }
func (errUsage) Providers(ctx context.Context) (any, error)               { return nil, errBoom }
func (errUsage) RequestDetails(ctx context.Context) (any, error)          { return nil, errBoom }
func (errUsage) RequestLogs(ctx context.Context) (any, error)             { return nil, errBoom }
func (errUsage) Logs(ctx context.Context) (any, error)                    { return nil, errBoom }
func (errUsage) Connection(ctx context.Context, connectionID string) (any, error) {
	return nil, errBoom
}

type errQuota struct{}

var _ QuotaService = &errQuota{}

func (errQuota) Status(ctx context.Context, actor *auth.Actor, providerID uuid.UUID) (*quota.QuotaStatus, error) {
	return nil, errBoom
}
func (errQuota) Unlock(ctx context.Context, actor *auth.Actor, providerID uuid.UUID) error {
	return errBoom
}
func (errQuota) Reset(ctx context.Context, actor *auth.Actor, providerID uuid.UUID) error { return errBoom }

type errMutators struct{}

var _ MutatorsService = &errMutators{}

func (errMutators) List(ctx context.Context) (any, error)                    { return nil, errBoom }
func (errMutators) Set(ctx context.Context, name string, enabled bool) error { return errBoom }

type errCLITools struct{}

var _ CLIToolsService = &errCLITools{}

func (errCLITools) AllStatuses(ctx context.Context) (any, error) { return nil, errBoom }
func (errCLITools) ToolGet(ctx context.Context, tool string) (any, error) {
	return nil, errBoom
}
func (errCLITools) ToolAction(ctx context.Context, tool string, in any) (any, error) {
	return nil, errBoom
}
func (errCLITools) AntigravityAlias(ctx context.Context, in any) (any, error) { return nil, errBoom }

type errMedia struct{}

var _ MediaService = &errMedia{}

func (errMedia) Voices(ctx context.Context, providerKind, providerID, lang string) (any, error) {
	return nil, errBoom
}

type errSkills struct{}

var _ SkillsService = &errSkills{}

func (errSkills) Catalog(ctx context.Context) ([]Skill, error) { return nil, errBoom }

type errConsole struct{}

var _ ConsoleService = &errConsole{}

func (errConsole) ListAfter(ctx context.Context, seq int64, limit int) ([]console.ConsoleLog, error) {
	return nil, errBoom
}

type errTunnels struct{}

var _ TunnelsService = &errTunnels{}

func (errTunnels) Get(ctx context.Context) (any, error)            { return nil, errBoom }
func (errTunnels) Status(ctx context.Context) (any, error)         { return nil, errBoom }
func (errTunnels) TailscaleCheck(ctx context.Context) (any, error) { return nil, errBoom }
func (errTunnels) Enable(ctx context.Context) (any, error)         { return nil, errBoom }
func (errTunnels) Disable(ctx context.Context) (any, error)        { return nil, errBoom }
func (errTunnels) TailscaleInstall(ctx context.Context) (any, error) {
	return nil, errBoom
}
func (errTunnels) TailscaleEnable(ctx context.Context) (any, error) { return nil, errBoom }
func (errTunnels) TailscaleDisable(ctx context.Context) (any, error) {
	return nil, errBoom
}

type errSettings struct{}

var _ SettingsService = &errSettings{}

func (errSettings) Get(ctx context.Context, actor *auth.Actor, key string) (*settings.Setting, error) {
	return nil, errBoom
}
func (errSettings) List(ctx context.Context, actor *auth.Actor) ([]settings.Setting, error) {
	return nil, errBoom
}
func (errSettings) Update(ctx context.Context, actor *auth.Actor, values map[string]json.RawMessage) error {
	return errBoom
}
func (errSettings) ProxyTest(ctx context.Context, actor *auth.Actor, in any) (any, error) {
	return nil, errBoom
}
func (errSettings) RequireLogin(ctx context.Context, actor *auth.Actor) (*settings.Setting, error) {
	return nil, errBoom
}
func (errSettings) SetRequireLogin(ctx context.Context, actor *auth.Actor, enabled bool) error {
	return errBoom
}

type errTransfer struct{}

var _ ConfigTransferService = &errTransfer{}

func (errTransfer) Export(ctx context.Context, actor *auth.Actor) (*appsettings.ConfigPayload, error) {
	return nil, errBoom
}
func (errTransfer) Import(ctx context.Context, actor *auth.Actor, payload appsettings.ConfigPayload, confirmation string) (*appsettings.ConfigPayload, error) {
	return nil, errBoom
}

type errTranslator struct{}

var _ TranslatorService = &errTranslator{}

func (errTranslator) Translate(ctx context.Context, in any) (any, error) { return nil, errBoom }
func (errTranslator) Load(ctx context.Context) (any, error)              { return nil, errBoom }
func (errTranslator) Save(ctx context.Context, in any) (any, error)      { return nil, errBoom }
func (errTranslator) Send(ctx context.Context, in any) (any, error)      { return nil, errBoom }
func (errTranslator) ConsoleLogs(ctx context.Context) (any, error)       { return nil, errBoom }
func (errTranslator) ClearConsoleLogs(ctx context.Context) error         { return errBoom }

type errOAuth struct{}

var _ OAuthService = &errOAuth{}

func (errOAuth) Status(ctx context.Context, providerName string) (any, error) {
	return nil, errBoom
}
func (errOAuth) Connect(ctx context.Context, providerName string, in any) (any, error) {
	return nil, errBoom
}
func (errOAuth) Disconnect(ctx context.Context, providerName string) (any, error) {
	return nil, errBoom
}
func (errOAuth) Import(ctx context.Context, flow string, in any) (any, error) { return nil, errBoom }

type errJobs struct{}

var _ JobsService = &errJobs{}

func (errJobs) List(ctx context.Context) ([]jobs.Job, error) { return nil, errBoom }
func (errJobs) History(ctx context.Context, jobType string) ([]jobs.Job, error) {
	return nil, errBoom
}
func (errJobs) RunNow(ctx context.Context, actor *auth.Actor, jobType string) error { return errBoom }

type errHostOps struct{}

var _ HostOpsService = &errHostOps{}

func (errHostOps) Action(ctx context.Context, feature, action string, in any) (any, error) {
	return nil, errBoom
}
func (errHostOps) Version(ctx context.Context) (any, error) { return nil, errBoom }

type errBackups struct{}

var _ BackupsService = &errBackups{}

func (errBackups) List(ctx context.Context, actor *auth.Actor) ([]backup.Backup, error) {
	return nil, errBoom
}
func (errBackups) Download(ctx context.Context, actor *auth.Actor, ip net.IP, id uuid.UUID) (*BackupDownload, error) {
	return nil, errBoom
}
func (errBackups) Verify(ctx context.Context, actor *auth.Actor, id uuid.UUID) error { return errBoom }

type errAudit struct{}

var _ AuditService = &errAudit{}

func (errAudit) List(ctx context.Context, actor *auth.Actor, filters tx.AuditFilters, page tx.AuditPage) ([]tx.AuditEntry, error) {
	return nil, errBoom
}
func (errAudit) Export(ctx context.Context, actor *auth.Actor, filters tx.AuditFilters) ([]tx.AuditEntry, error) {
	return nil, errBoom
}
func (errAudit) Get(ctx context.Context, actor *auth.Actor, id uuid.UUID) (*tx.AuditEntry, error) {
	return nil, errBoom
}

// errorDeps wires every seam with a failing fake.
func errorDeps() Dependencies {
	return Dependencies{
		Keys: &errKeys{}, PATs: &errPats{}, Providers: &errProviders{}, Accounts: &errAccounts{},
		Nodes: &errNodes{}, Pools: &errPools{}, Combos: &errCombos{}, Aliases: &errAliases{},
		Models: &errModels{}, Pricing: &errPricing{}, Usage: &errUsage{}, Quota: &errQuota{},
		Mutators: &errMutators{}, CLITools: &errCLITools{}, Media: &errMedia{}, Skills: &errSkills{},
		Console: &errConsole{}, Tunnels: &errTunnels{}, Settings: &errSettings{},
		ConfigTransfer: &errTransfer{}, Translator: &errTranslator{}, OAuth: &errOAuth{},
		Jobs: &errJobs{}, HostOps: &errHostOps{}, Backups: &errBackups{}, Audit: &errAudit{},
		ValidatePAT: fakePAT,
	}
}

// TestEveryRouteServiceError asserts every service-backed route maps its
// service error through writeError (500 INTERNAL_ERROR for an unknown error)
// instead of leaking or 404ing.
func TestEveryRouteServiceError(t *testing.T) {
	cfg := Config{Auth: authFake{}, OIDC: authFake{}, HostFlags: allHostFlags(), Resources: errorDeps()}
	h := testRouter(cfg)

	placeholder := map[string]bool{
		"GET /usage/stream": true, "GET /jobs/stream": true, "GET /console/stream": true,
		"GET /mcp/{plugin}/sse": true, "GET /translator/console-logs/stream": true,
		"POST /init": true, "GET /init": true, "POST /locale": true, "GET /locale": true,
		"GET /tags": true,
	}

	for _, rt := range RouteTable(New(cfg)) {
		if rt.Mode != AuthSessionPAT || placeholder[rt.Method+" "+rt.Path] {
			continue
		}
		// Routes whose handler does not consult a service fake: the detailed
		// health build-info surface, the auth surface, and the proxy-pool
		// deploy helpers (which call the pure config generators directly).
		if strings.HasPrefix(rt.Path, "/health") || strings.HasPrefix(rt.Path, "/auth") ||
			strings.HasSuffix(rt.Path, "-deploy") {
			continue
		}
		full := "/api/admin/v1" + expandPath(rt.Path)
		headers := map[string]string{"Authorization": "Bearer pat-1"}
		rr := doJSON(t, h, rt.Method, full, nil, headers, bodyFor(rt))
		if rr.Code != http.StatusInternalServerError {
			t.Errorf("route %s %s with failing service = %d, want 500 (body %s)", rt.Method, rt.Path, rr.Code, rr.Body.String())
		}
	}
}

// TestWriteErrorMapping covers every statusFor/writeError branch directly.
func TestWriteErrorMapping(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/admin/v1/x", nil)
	cases := []struct {
		err  error
		code string
	}{
		{combo.ErrNotFound, "NOT_FOUND"},
		{combo.ErrInvalidConfig, "INVALID_REQUEST"},
		{errConflict, "CONFLICT"},
		{errForbidden, "FORBIDDEN"},
		{errUnauthorized, "UNAUTHORIZED"},
		{errBoom, "INTERNAL_ERROR"},
	}
	for _, c := range cases {
		rr := httptest.NewRecorder()
		writeError(rr, req, c.err)
		var body map[string]any
		_ = json.Unmarshal(rr.Body.Bytes(), &body)
		if body["code"] != c.code {
			t.Errorf("writeError(%v) code = %v, want %s", c.err, body["code"], c.code)
		}
	}
	if statusFor(nil) != http.StatusOK {
		t.Fatal("statusFor(nil) must be 200")
	}
}
