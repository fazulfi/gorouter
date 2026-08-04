// Generated Admin API v1 client
// Source: api/admin-v1.openapi.yaml
// Do not edit manually; re-run the contract generator.

// Type definitions

export interface APIKey {
	created_at?: string;
	expires_at?: string;
	id?: string;
	key_prefix?: string; // Prefix of the API key for identification; hash-only storage after creation
	last_used_at?: string;
	name?: string;
	revoked_at?: string;
}
export interface AuditEntry {
	action?: string;
	actor_id?: string;
	actor_kind?: string;
	after?: unknown;
	before?: unknown;
	correlation_id?: string;
	id?: string;
	ip?: string;
	occurred_at?: string;
	origin?: string;
	target?: string;
}
export interface AuditExport {
	entries?: AuditEntry[];
	exported_at?: string;
	total?: number;
}
export interface Backup {
	bytes?: number;
	created_at?: string;
	id?: string;
	sha256?: string;
	status?: string;
	verified_at?: string;
}
export interface BackupVerifyResponse {
	id?: string;
	message?: string;
	sha256_match?: boolean;
	verified?: boolean;
	verified_at?: string;
}
export interface CliToolStatus {
	config_step?: string;
	installed?: boolean;
	message?: string;
	status?: string;
	tool?: string;
	version?: string;
}
export interface CliToolsStatus {
	checked_at?: string;
	tools?: CliToolStatus[];
}
export interface Combo {
	created_at?: string;
	id?: string;
	is_enabled?: boolean;
	models?: string[];
	name?: string;
	strategy?: string;
}
export interface ConsoleLog {
	at?: string;
	level?: string;
	line?: string;
}
export interface CreateAPIKeyRequest {
	expires_at?: string;
	name: string;
}
export interface CreateAPIKeyResponse {
	created_at?: string;
	expires_at?: string;
	id?: string;
	key?: string; // Full API key value, only shown once
	name?: string;
}
export interface CreateComboRequest {
	models: string[];
	name: string;
	strategy: string;
}
export interface CreatePATRequest {
	description: string;
	expires_at?: string;
}
export interface CreatePATResponse {
	created_at?: string;
	description?: string;
	expires_at?: string;
	id?: string;
	token?: string; // Full PAT value, only shown once
}
export interface CreateProviderNodeRequest {
	base_url: string;
	is_enabled?: boolean;
	name: string;
	provider_id: string;
	weight?: number;
}
export interface CreateProviderRequest {
	api_key?: string; // Provider credential (write-only, encrypted at rest)
	base_url: string;
	config?: unknown;
	name: string;
	type: string;
}
export interface CreateProxyPoolRequest {
	config?: unknown;
	kind: string;
	name: string;
	region?: string;
}
export interface CustomModel {
	api_key_hint?: string;
	base_url?: string;
	has_api_key?: boolean;
	id?: string;
	is_enabled?: boolean;
	name?: string;
}
export interface DatabaseInfo {
	connected?: boolean;
	driver?: string;
	retention_days?: number;
	size_bytes?: number;
	version?: string;
}
export interface DeployResult {
	message?: string;
	ok?: boolean;
	platform?: string;
	target?: string;
	url?: string;
}
export interface DetailedHealth {
	extends Health;
}
export interface DisabledModel {
	id?: string;
	name?: string;
	reason?: string;
}
export interface Error {
	code: string;
	details?: unknown;
	message: string;
	request_id?: string;
}
export interface HeadroomExtras {
	features?: string[];
	installed?: boolean;
	updated_at?: string;
	version?: string;
}
export interface HeadroomStatus {
	pid?: number;
	port?: number;
	running?: boolean;
	started_at?: string;
}
export interface Health {
	status?: string;
	timestamp?: string;
}
export interface InitRequest {
	email: string;
	password: string;
}
export interface InitResponse {
	initialized?: boolean;
	user?: User;
}
export interface JobEvent {
	at?: string;
	event?: string;
	status?: string;
	type?: string;
}
export interface JobHistory {
	entries?: JobRun[];
	type?: string;
}
export interface JobInfo {
	last_run_at?: string;
	last_run_status?: string;
	next_run_at?: string;
	schedule?: string;
	status?: string;
	type?: string;
}
export interface JobRun {
	error?: string;
	finished_at?: string;
	run_id?: string;
	started_at?: string;
	status?: string;
}
export interface LocaleCatalog {
	locale?: string;
	messages?: unknown;
}
export interface LoginRequest {
	email: string;
	password: string;
}
export interface LoginResponse {
	session_id?: string;
	user?: User;
}
export interface MCPEvent {
	data?: unknown;
	event?: string;
}
export interface MCPMessageRequest {
	id?: number;
	jsonrpc?: string;
	method?: string;
	params?: unknown;
}
export interface MCPMessageResponse {
	error?: unknown;
	id?: number;
	jsonrpc?: string;
	result?: unknown;
}
export interface MediaProvider {
	config?: unknown;
	id?: string;
	is_enabled?: boolean;
	kind?: string;
	name?: string;
}
export interface Model {
	id?: string;
	is_default?: boolean;
	is_enabled?: boolean;
	kind?: string;
	name?: string;
	provider?: string;
}
export interface ModelAlias {
	alias?: string;
	is_enabled?: boolean;
	target_model?: string;
}
export interface ModelAvailability {
	available?: boolean;
	cooldown_until?: string;
	model_id?: string;
	reason?: string;
}
export interface ModelConfigUpdate {
	defaults?: unknown;
	models?: Model[];
}
export interface ModelTestRequest {
	model_id: string;
	prompt?: string;
}
export interface ModelTestResult {
	error?: string;
	latency_ms?: number;
	model_id?: string;
	ok?: boolean;
	snippet?: string;
}
export interface NodeValidationResult {
	error?: string;
	latency_ms?: number;
	node_id?: string;
	ok?: boolean;
}
export interface OAuthAccount {
	connected?: boolean;
	display_name?: string;
	email?: string;
	expires_at?: string;
	granted_scopes?: string[];
	provider?: string;
}
export interface OAuthImportRequest {
	cli_proxy_url?: string;
	email?: string;
	token?: string; // Credential value (write-only)
}
export interface PAT {
	created_at?: string;
	description?: string;
	expires_at?: string;
	id?: string;
	last_used_at?: string;
	revoked_at?: string;
	token_prefix?: string; // First 8 characters of the PAT for identification
}
export interface PricingEntry {
	input_per_million?: number;
	model?: string;
	output_per_million?: number;
	provider?: string;
	updated_at?: string;
}
export interface Provider {
	api_key_hint?: string; // Last 4 characters of the stored credential for identification
	base_url?: string;
	config?: unknown;
	created_at?: string;
	has_api_key?: boolean; // Whether a credential is stored for this provider
	id?: string;
	is_enabled?: boolean;
	model?: string;
	name?: string;
	type?: string;
	updated_at?: string;
}
export interface ProviderNode {
	base_url?: string;
	id?: string;
	is_enabled?: boolean;
	last_checked_at?: string;
	name?: string;
	provider_id?: string;
	status?: string;
	weight?: number;
}
export interface ProviderTestBatchRequest {
	ids: string[];
}
export interface ProviderTestBatchResponse {
	results?: ProviderTestResult[];
	started_at?: string;
}
export interface ProviderTestResult {
	error?: string;
	latency_ms?: number;
	ok?: boolean;
	provider_id?: string;
	provider_name?: string;
}
export interface ProviderValidationResult {
	error?: string;
	latency_ms?: number;
	ok?: boolean;
	provider_id?: string;
}
export interface ProxyPool {
	config?: unknown;
	created_at?: string;
	endpoints?: string[];
	id?: string;
	kind?: string;
	name?: string;
	status?: string;
}
export interface ProxyTestRequest {
	headers?: unknown;
	method?: string;
	url: string;
}
export interface ProxyTestResult {
	error?: string;
	latency_ms?: number;
	ok?: boolean;
	status?: number;
}
export interface PxpipeLog {
	at?: string;
	level?: string;
	line?: string;
}
export interface PxpipeStats {
	errors?: number;
	last_log_at?: string;
	lines_total?: number;
}
export interface PxpipeStatus {
	logs_path?: string;
	pid?: number;
	port?: number;
	running?: boolean;
	version?: string;
}
export interface RecentRequest {
	completion_tokens?: number;
	connection_id?: string;
	latency_ms?: number;
	minute?: string;
	model?: string;
	prompt_tokens?: number;
	provider?: string;
	status?: string;
}
export interface RequireLoginStatus {
	require_login?: boolean;
}
export interface Session {
	created_at?: string;
	expires_at?: string;
	id?: string;
	user_id?: string;
}
export interface SessionStatus {
	authenticated?: boolean;
	csrf_token?: string; // CSRF token to echo on mutations (cookie actors)
	session_id?: string;
	user?: User;
}
export interface Settings {
	claude_auto_ping?: boolean;
	codex_auto_ping?: boolean;
	enable_translator?: boolean;
	oidc_enabled?: boolean;
	oidc_issuer?: string;
	require_login?: boolean;
	rtk_enabled?: boolean;
	updated_at?: string;
}
export interface StreamEvent {
	data?: unknown;
	event?: string;
	keepalive_seconds?: number;
}
export interface SuggestedModel {
	id?: string;
	kind?: string;
	name?: string;
	provider?: string;
}
export interface Tag {
	id?: string;
	kind?: string;
	name?: string;
}
export interface TailscaleStatus {
	funnel_enabled?: boolean;
	hostname?: string;
	installed?: boolean;
	running?: boolean;
	url?: string;
}
export interface TranslationRequest {
	from?: string;
	provider?: string;
	text: string;
	to: string;
}
export interface TranslationResponse {
	latency_ms?: number;
	provider?: string;
	translated?: string;
}
export interface TranslatorSaveRequest {
	config?: unknown;
	name: string;
	provider: string;
}
export interface TranslatorSendRequest {
	channel?: string;
	provider?: string;
	text: string;
}
export interface TranslatorState {
	default_provider?: string;
	enabled?: boolean;
	providers?: string[];
	updated_at?: string;
}
export interface TunnelStatus {
	enabled?: boolean;
	message?: string;
	provider?: string;
	started_at?: string;
	url?: string;
}
export interface UpdateStatus {
	available?: boolean;
	checking?: boolean;
	message?: string;
	version?: string;
}
export interface UsageChart {
	points?: UsageChartPoint[];
}
export interface UsageChartPoint {
	label?: string;
	ts?: string;
	value?: number;
}
export interface UsageHistory {
	entries?: UsageHistoryEntry[];
}
export interface UsageHistoryEntry {
	day?: string;
	errors?: number;
	requests?: number;
	tokens_in?: number;
	tokens_out?: number;
}
export interface UsageProviderStat {
	errors?: number;
	p95_latency_ms?: number;
	provider?: string;
	requests?: number;
	tokens_in?: number;
	tokens_out?: number;
}
export interface UsageProviders {
	providers?: UsageProviderStat[];
}
export interface UsageRequestDetail {
	connection_id?: string;
	error?: string;
	finished_at?: string;
	model?: string;
	provider?: string;
	started_at?: string;
	status?: string;
	tokens_in?: number;
	tokens_out?: number;
}
export interface UsageRequestLog {
	connection_id?: string;
	model?: string;
	provider?: string;
	started_at?: string;
	status?: string;
	tokens_in?: number;
	tokens_out?: number;
}
export interface UsageRequestLogs {
	entries?: UsageRequestLog[];
}
export interface UsageStats {
	active_requests?: number;
	error_rate?: number;
	period?: string;
	total_requests?: number;
	total_tokens_in?: number;
	total_tokens_out?: number;
}
export interface User {
	created_at?: string;
	display_name?: string;
	email?: string;
	id?: string;
	is_admin?: boolean;
}
export interface VersionInfo {
	build_date?: string;
	commit?: string;
	has_update?: boolean;
	version?: string;
}
export interface Voice {
	gender?: string;
	id?: string;
	language?: string;
	name?: string;
	provider?: string;
}

// Client

const BASE = "/api/admin/v1";

async function request<T>(path: string, init?: RequestInit): Promise<T> {
	const res = await fetch(BASE + path, {
		headers: {
			'Content-Type': 'application/json',
			...init?.headers,
		},
		...init,
	});
	if (!res.ok) {
		const body = await res.text();
		throw new Error(body);
	}
	return res.json();
}

export function ListAuditEntries(): Promise<AuditEntry[]> {
	return request<AuditEntry[]>("/audit", { method: "GET" });
}

export function ExportAudit(): Promise<AuditExport> {
	return request<AuditExport>("/audit/export", { method: "GET" });
}

export function GetAuditEntry(	id: string): Promise<AuditEntry> {
	return request<AuditEntry>(`/audit/${id}`, { method: "GET" });
}

export function Login(	body: LoginRequest): Promise<LoginResponse> {
	return request<LoginResponse>("/auth/login", {
		method: "POST",
		body: JSON.stringify(body),
	});
}

export function Logout(): Promise<void> {
	return request<void>("/auth/logout", { method: "POST" });
}

export function GetCurrentUser(): Promise<User> {
	return request<User>("/auth/me", { method: "GET" });
}

export function OidcCallback(): Promise<void> {
	return request<void>("/auth/oidc/callback", { method: "GET" });
}

export function OidcStart(): Promise<void> {
	return request<void>("/auth/oidc/start", { method: "GET" });
}

export function OidcTest(): Promise<void> {
	return request<void>("/auth/oidc/test", { method: "POST" });
}

export function GetAuthStatus(): Promise<SessionStatus> {
	return request<SessionStatus>("/auth/status", { method: "GET" });
}

export function ListBackups(): Promise<Backup[]> {
	return request<Backup[]>("/backups", { method: "GET" });
}

export function DownloadBackup(	id: string): Promise<string> {
	return request<string>(`/backups/${id}/download`, { method: "GET" });
}

export function VerifyBackup(	id: string): Promise<BackupVerifyResponse> {
	return request<BackupVerifyResponse>(`/backups/${id}/verify`, { method: "POST" });
}

export function GetCliToolsStatus(): Promise<CliToolsStatus> {
	return request<CliToolsStatus>("/cli-tools/all-statuses", { method: "GET" });
}

export function SetAntigravityMitmAlias(	body: unknown): Promise<void> {
	return request<void>("/cli-tools/antigravity-mitm/alias", {
		method: "POST",
		body: JSON.stringify(body),
	});
}

export function GetCliToolStatus(	tool: string): Promise<CliToolStatus> {
	return request<CliToolStatus>(`/cli-tools/${tool}`, { method: "GET" });
}

export function RunCliToolAction(	tool: string, body: unknown): Promise<CliToolStatus> {
	return request<CliToolStatus>(`/cli-tools/${tool}`, {
		method: "POST",
		body: JSON.stringify(body),
	});
}

export function ListCombos(): Promise<Combo[]> {
	return request<Combo[]>("/combos", { method: "GET" });
}

export function CreateCombo(	body: CreateComboRequest): Promise<Combo> {
	return request<Combo>("/combos", {
		method: "POST",
		body: JSON.stringify(body),
	});
}

export function DeleteCombo(	id: string): Promise<void> {
	return request<void>(`/combos/${id}`, { method: "DELETE" });
}

export function GetCombo(	id: string): Promise<Combo> {
	return request<Combo>(`/combos/${id}`, { method: "GET" });
}

export function UpdateCombo(	id: string, body: CreateComboRequest): Promise<Combo> {
	return request<Combo>(`/combos/${id}`, {
		method: "PUT",
		body: JSON.stringify(body),
	});
}

export function StreamConsole(): Promise<StreamEvent> {
	return request<StreamEvent>("/console/stream", { method: "GET" });
}

export function GetHeadroomExtras(): Promise<HeadroomExtras> {
	return request<HeadroomExtras>("/headroom/extras", { method: "GET" });
}

export function HeadroomProxyGet(	path: string): Promise<void> {
	return request<void>(`/headroom/proxy/${path}`, { method: "GET" });
}

export function HeadroomProxyPost(	path: string, body: unknown): Promise<void> {
	return request<void>(`/headroom/proxy/${path}`, {
		method: "POST",
		body: JSON.stringify(body),
	});
}

export function RestartHeadroom(): Promise<HeadroomStatus> {
	return request<HeadroomStatus>("/headroom/restart", { method: "POST" });
}

export function StartHeadroom(): Promise<HeadroomStatus> {
	return request<HeadroomStatus>("/headroom/start", { method: "POST" });
}

export function GetHeadroomStatus(): Promise<HeadroomStatus> {
	return request<HeadroomStatus>("/headroom/status", { method: "GET" });
}

export function StopHeadroom(): Promise<HeadroomStatus> {
	return request<HeadroomStatus>("/headroom/stop", { method: "POST" });
}

export function GetHealth(): Promise<Health> {
	return request<Health>("/health", { method: "GET" });
}

export function GetDetailedHealth(): Promise<DetailedHealth> {
	return request<DetailedHealth>("/health/detailed", { method: "GET" });
}

export function InitSystem(	body: InitRequest): Promise<InitResponse> {
	return request<InitResponse>("/init", {
		method: "POST",
		body: JSON.stringify(body),
	});
}

export function ListJobs(): Promise<JobInfo[]> {
	return request<JobInfo[]>("/jobs", { method: "GET" });
}

export function StreamJobs(): Promise<JobEvent> {
	return request<JobEvent>("/jobs/stream", { method: "GET" });
}

export function GetJobHistory(	type: string): Promise<JobHistory> {
	return request<JobHistory>(`/jobs/${type}/history`, { method: "GET" });
}

export function RunJobNow(	type: string): Promise<JobRun> {
	return request<JobRun>(`/jobs/${type}/run-now`, { method: "POST" });
}

export function ListAPIKeys(): Promise<APIKey[]> {
	return request<APIKey[]>("/keys", { method: "GET" });
}

export function CreateAPIKey(	body: CreateAPIKeyRequest): Promise<CreateAPIKeyResponse> {
	return request<CreateAPIKeyResponse>("/keys", {
		method: "POST",
		body: JSON.stringify(body),
	});
}

export function RevokeAPIKey(	id: string): Promise<void> {
	return request<void>(`/keys/${id}`, { method: "DELETE" });
}

export function GetAPIKey(	id: string): Promise<APIKey> {
	return request<APIKey>(`/keys/${id}`, { method: "GET" });
}

export function UpdateAPIKey(	id: string, body: CreateAPIKeyRequest): Promise<APIKey> {
	return request<APIKey>(`/keys/${id}`, {
		method: "PUT",
		body: JSON.stringify(body),
	});
}

export function GetLocale(): Promise<LocaleCatalog> {
	return request<LocaleCatalog>("/locale", { method: "GET" });
}

export function SendMCPMessage(	plugin: string, body: MCPMessageRequest): Promise<MCPMessageResponse> {
	return request<MCPMessageResponse>(`/mcp/${plugin}/message`, {
		method: "POST",
		body: JSON.stringify(body),
	});
}

export function StreamMCPEvents(	plugin: string): Promise<MCPEvent> {
	return request<MCPEvent>(`/mcp/${plugin}/sse`, { method: "GET" });
}

export function ListDeepgramVoices(): Promise<Voice[]> {
	return request<Voice[]>("/media-providers/tts/deepgram/voices", { method: "GET" });
}

export function ListElevenlabsVoices(): Promise<Voice[]> {
	return request<Voice[]>("/media-providers/tts/elevenlabs/voices", { method: "GET" });
}

export function ListInworldVoices(): Promise<Voice[]> {
	return request<Voice[]>("/media-providers/tts/inworld/voices", { method: "GET" });
}

export function ListMinimaxVoices(): Promise<Voice[]> {
	return request<Voice[]>("/media-providers/tts/minimax/voices", { method: "GET" });
}

export function ListTTSVoices(): Promise<Voice[]> {
	return request<Voice[]>("/media-providers/tts/voices", { method: "GET" });
}

export function ListModels(): Promise<Model[]> {
	return request<Model[]>("/models", { method: "GET" });
}

export function UpdateModels(	body: ModelConfigUpdate): Promise<Model[]> {
	return request<Model[]>("/models", {
		method: "PUT",
		body: JSON.stringify(body),
	});
}

export function DeleteModelAlias(	body: ModelAlias): Promise<void> {
	return request<void>("/models/alias", {
		method: "DELETE",
		body: JSON.stringify(body),
	});
}

export function ListModelAliases(): Promise<ModelAlias[]> {
	return request<ModelAlias[]>("/models/alias", { method: "GET" });
}

export function UpsertModelAlias(	body: ModelAlias): Promise<ModelAlias> {
	return request<ModelAlias>("/models/alias", {
		method: "PUT",
		body: JSON.stringify(body),
	});
}

export function GetModelAvailability(): Promise<ModelAvailability[]> {
	return request<ModelAvailability[]>("/models/availability", { method: "GET" });
}

export function DeleteCustomModel(	body: CustomModel): Promise<void> {
	return request<void>("/models/custom", {
		method: "DELETE",
		body: JSON.stringify(body),
	});
}

export function ListCustomModels(): Promise<CustomModel[]> {
	return request<CustomModel[]>("/models/custom", { method: "GET" });
}

export function CreateCustomModel(	body: CustomModel): Promise<CustomModel> {
	return request<CustomModel>("/models/custom", {
		method: "POST",
		body: JSON.stringify(body),
	});
}

export function ReenableModel(	body: DisabledModel): Promise<void> {
	return request<void>("/models/disabled", {
		method: "DELETE",
		body: JSON.stringify(body),
	});
}

export function ListDisabledModels(): Promise<DisabledModel[]> {
	return request<DisabledModel[]>("/models/disabled", { method: "GET" });
}

export function DisableModel(	body: DisabledModel): Promise<DisabledModel> {
	return request<DisabledModel>("/models/disabled", {
		method: "POST",
		body: JSON.stringify(body),
	});
}

export function TestModel(	body: ModelTestRequest): Promise<ModelTestResult> {
	return request<ModelTestResult>("/models/test", {
		method: "POST",
		body: JSON.stringify(body),
	});
}

export function BulkImportCodexTokens(	body: unknown): Promise<OAuthAccount[]> {
	return request<OAuthAccount[]>("/oauth/codex/bulk-import", {
		method: "POST",
		body: JSON.stringify(body),
	});
}

export function ImportCodexToken(	body: OAuthImportRequest): Promise<OAuthAccount> {
	return request<OAuthAccount>("/oauth/codex/import-token", {
		method: "POST",
		body: JSON.stringify(body),
	});
}

export function AutoImportCursor(): Promise<OAuthAccount> {
	return request<OAuthAccount>("/oauth/cursor/auto-import", { method: "POST" });
}

export function ImportCursorCredential(	body: OAuthImportRequest): Promise<OAuthAccount> {
	return request<OAuthAccount>("/oauth/cursor/import", {
		method: "POST",
		body: JSON.stringify(body),
	});
}

export function ImportGitlabPAT(	body: OAuthImportRequest): Promise<OAuthAccount> {
	return request<OAuthAccount>("/oauth/gitlab/pat", {
		method: "POST",
		body: JSON.stringify(body),
	});
}

export function ImportIflowCookie(	body: OAuthImportRequest): Promise<OAuthAccount> {
	return request<OAuthAccount>("/oauth/iflow/cookie", {
		method: "POST",
		body: JSON.stringify(body),
	});
}

export function ImportKiroApiKey(	body: OAuthImportRequest): Promise<OAuthAccount> {
	return request<OAuthAccount>("/oauth/kiro/api-key", {
		method: "POST",
		body: JSON.stringify(body),
	});
}

export function AutoImportKiro(): Promise<OAuthAccount> {
	return request<OAuthAccount>("/oauth/kiro/auto-import", { method: "POST" });
}

export function ImportKiroCredential(	body: OAuthImportRequest): Promise<OAuthAccount> {
	return request<OAuthAccount>("/oauth/kiro/import", {
		method: "POST",
		body: JSON.stringify(body),
	});
}

export function ImportKiroCliProxy(	body: OAuthImportRequest): Promise<OAuthAccount> {
	return request<OAuthAccount>("/oauth/kiro/import-cli-proxy", {
		method: "POST",
		body: JSON.stringify(body),
	});
}

export function AuthorizeKiroSocial(): Promise<unknown> {
	return request<unknown>("/oauth/kiro/social-authorize", { method: "POST" });
}

export function ExchangeKiroSocial(	body: unknown): Promise<OAuthAccount> {
	return request<OAuthAccount>("/oauth/kiro/social-exchange", {
		method: "POST",
		body: JSON.stringify(body),
	});
}

export function DisconnectOAuthProvider(	provider: string): Promise<void> {
	return request<void>(`/oauth/${provider}`, { method: "DELETE" });
}

export function GetOAuthProvider(	provider: string): Promise<OAuthAccount> {
	return request<OAuthAccount>(`/oauth/${provider}`, { method: "GET" });
}

export function ConnectOAuthProvider(	provider: string): Promise<unknown> {
	return request<unknown>(`/oauth/${provider}`, { method: "POST" });
}

export function ListPATs(): Promise<PAT[]> {
	return request<PAT[]>("/pats", { method: "GET" });
}

export function CreatePAT(	body: CreatePATRequest): Promise<CreatePATResponse> {
	return request<CreatePATResponse>("/pats", {
		method: "POST",
		body: JSON.stringify(body),
	});
}

export function RevokePAT(	id: string): Promise<void> {
	return request<void>(`/pats/${id}`, { method: "DELETE" });
}

export function ResetPricing(): Promise<void> {
	return request<void>("/pricing", { method: "DELETE" });
}

export function GetPricing(): Promise<PricingEntry[]> {
	return request<PricingEntry[]>("/pricing", { method: "GET" });
}

export function UpdatePricing(	body: PricingEntry[]): Promise<PricingEntry[]> {
	return request<PricingEntry[]>("/pricing", {
		method: "PATCH",
		body: JSON.stringify(body),
	});
}

export function ListProviderNodes(): Promise<ProviderNode[]> {
	return request<ProviderNode[]>("/provider-nodes", { method: "GET" });
}

export function CreateProviderNode(	body: CreateProviderNodeRequest): Promise<ProviderNode> {
	return request<ProviderNode>("/provider-nodes", {
		method: "POST",
		body: JSON.stringify(body),
	});
}

export function ValidateProviderNode(	body: CreateProviderNodeRequest): Promise<NodeValidationResult> {
	return request<NodeValidationResult>("/provider-nodes/validate", {
		method: "POST",
		body: JSON.stringify(body),
	});
}

export function DeleteProviderNode(	id: string): Promise<void> {
	return request<void>(`/provider-nodes/${id}`, { method: "DELETE" });
}

export function GetProviderNode(	id: string): Promise<ProviderNode> {
	return request<ProviderNode>(`/provider-nodes/${id}`, { method: "GET" });
}

export function UpdateProviderNode(	id: string, body: CreateProviderNodeRequest): Promise<ProviderNode> {
	return request<ProviderNode>(`/provider-nodes/${id}`, {
		method: "PUT",
		body: JSON.stringify(body),
	});
}

export function ListProviders(): Promise<Provider[]> {
	return request<Provider[]>("/providers", { method: "GET" });
}

export function CreateProvider(	body: CreateProviderRequest): Promise<Provider> {
	return request<Provider>("/providers", {
		method: "POST",
		body: JSON.stringify(body),
	});
}

export function GetProviderClient(): Promise<Provider[]> {
	return request<Provider[]>("/providers/client", { method: "GET" });
}

export function GetProviderKiloFreeModels(): Promise<SuggestedModel[]> {
	return request<SuggestedModel[]>("/providers/kilo/free-models", { method: "GET" });
}

export function StreamProviders(): Promise<StreamEvent> {
	return request<StreamEvent>("/providers/stream", { method: "GET" });
}

export function GetSuggestedModels(): Promise<SuggestedModel[]> {
	return request<SuggestedModel[]>("/providers/suggested-models", { method: "GET" });
}

export function TestProvidersBatch(	body: ProviderTestBatchRequest): Promise<ProviderTestBatchResponse> {
	return request<ProviderTestBatchResponse>("/providers/test-batch", {
		method: "POST",
		body: JSON.stringify(body),
	});
}

export function ValidateProvider(	body: CreateProviderRequest): Promise<ProviderValidationResult> {
	return request<ProviderValidationResult>("/providers/validate", {
		method: "POST",
		body: JSON.stringify(body),
	});
}

export function DeleteProvider(	id: string): Promise<void> {
	return request<void>(`/providers/${id}`, { method: "DELETE" });
}

export function GetProvider(	id: string): Promise<Provider> {
	return request<Provider>(`/providers/${id}`, { method: "GET" });
}

export function UpdateProvider(	id: string, body: CreateProviderRequest): Promise<Provider> {
	return request<Provider>(`/providers/${id}`, {
		method: "PUT",
		body: JSON.stringify(body),
	});
}

export function ListProxyPools(): Promise<ProxyPool[]> {
	return request<ProxyPool[]>("/proxy-pools", { method: "GET" });
}

export function CreateProxyPool(	body: CreateProxyPoolRequest): Promise<ProxyPool> {
	return request<ProxyPool>("/proxy-pools", {
		method: "POST",
		body: JSON.stringify(body),
	});
}

export function DeployProxyPoolCloudflare(	body: CreateProxyPoolRequest): Promise<DeployResult> {
	return request<DeployResult>("/proxy-pools/cloudflare-deploy", {
		method: "POST",
		body: JSON.stringify(body),
	});
}

export function DeployProxyPoolDeno(	body: CreateProxyPoolRequest): Promise<DeployResult> {
	return request<DeployResult>("/proxy-pools/deno-deploy", {
		method: "POST",
		body: JSON.stringify(body),
	});
}

export function DeployProxyPoolVercel(	body: CreateProxyPoolRequest): Promise<DeployResult> {
	return request<DeployResult>("/proxy-pools/vercel-deploy", {
		method: "POST",
		body: JSON.stringify(body),
	});
}

export function DeleteProxyPool(	id: string): Promise<void> {
	return request<void>(`/proxy-pools/${id}`, { method: "DELETE" });
}

export function GetProxyPool(	id: string): Promise<ProxyPool> {
	return request<ProxyPool>(`/proxy-pools/${id}`, { method: "GET" });
}

export function UpdateProxyPool(	id: string, body: CreateProxyPoolRequest): Promise<ProxyPool> {
	return request<ProxyPool>(`/proxy-pools/${id}`, {
		method: "PUT",
		body: JSON.stringify(body),
	});
}

export function GetPxpipeHealth(): Promise<void> {
	return request<void>("/pxpipe/health", { method: "GET" });
}

export function CheckPxpipeHealth(): Promise<void> {
	return request<void>("/pxpipe/health", { method: "POST" });
}

export function InstallPxpipe(): Promise<PxpipeStatus> {
	return request<PxpipeStatus>("/pxpipe/install", { method: "POST" });
}

export function GetPxpipeLogs(): Promise<PxpipeLog[]> {
	return request<PxpipeLog[]>("/pxpipe/logs", { method: "GET" });
}

export function RestartPxpipe(): Promise<PxpipeStatus> {
	return request<PxpipeStatus>("/pxpipe/restart", { method: "POST" });
}

export function StartPxpipe(): Promise<PxpipeStatus> {
	return request<PxpipeStatus>("/pxpipe/start", { method: "POST" });
}

export function GetPxpipeStats(): Promise<PxpipeStats> {
	return request<PxpipeStats>("/pxpipe/stats", { method: "GET" });
}

export function GetPxpipeStatus(): Promise<PxpipeStatus> {
	return request<PxpipeStatus>("/pxpipe/status", { method: "GET" });
}

export function StopPxpipe(): Promise<PxpipeStatus> {
	return request<PxpipeStatus>("/pxpipe/stop", { method: "POST" });
}

export function GetSettings(): Promise<Settings> {
	return request<Settings>("/settings", { method: "GET" });
}

export function UpdateSettings(	body: Settings): Promise<Settings> {
	return request<Settings>("/settings", {
		method: "PATCH",
		body: JSON.stringify(body),
	});
}

export function GetDatabaseInfo(): Promise<DatabaseInfo> {
	return request<DatabaseInfo>("/settings/database", { method: "GET" });
}

export function ImportConfiguration(	body: unknown): Promise<unknown> {
	return request<unknown>("/settings/database", {
		method: "POST",
		body: JSON.stringify(body),
	});
}

export function TestProxy(	body: ProxyTestRequest): Promise<ProxyTestResult> {
	return request<ProxyTestResult>("/settings/proxy-test", {
		method: "POST",
		body: JSON.stringify(body),
	});
}

export function GetRequireLogin(): Promise<RequireLoginStatus> {
	return request<RequireLoginStatus>("/settings/require-login", { method: "GET" });
}

export function SetRequireLogin(	body: RequireLoginStatus): Promise<RequireLoginStatus> {
	return request<RequireLoginStatus>("/settings/require-login", {
		method: "PUT",
		body: JSON.stringify(body),
	});
}

export function Shutdown(): Promise<void> {
	return request<void>("/shutdown", { method: "POST" });
}

export function GetTags(): Promise<Tag[]> {
	return request<Tag[]>("/tags", { method: "GET" });
}

export function ClearTranslatorConsoleLogs(): Promise<void> {
	return request<void>("/translator/console-logs", { method: "DELETE" });
}

export function GetTranslatorConsoleLogs(): Promise<ConsoleLog[]> {
	return request<ConsoleLog[]>("/translator/console-logs", { method: "GET" });
}

export function StreamTranslatorConsoleLogs(): Promise<StreamEvent> {
	return request<StreamEvent>("/translator/console-logs/stream", { method: "GET" });
}

export function LoadTranslator(): Promise<TranslatorState> {
	return request<TranslatorState>("/translator/load", { method: "GET" });
}

export function SaveTranslatorConfig(	body: TranslatorSaveRequest): Promise<TranslatorState> {
	return request<TranslatorState>("/translator/save", {
		method: "POST",
		body: JSON.stringify(body),
	});
}

export function SendTranslatorText(	body: TranslatorSendRequest): Promise<TranslationResponse> {
	return request<TranslationResponse>("/translator/send", {
		method: "POST",
		body: JSON.stringify(body),
	});
}

export function TranslateText(	body: TranslationRequest): Promise<TranslationResponse> {
	return request<TranslationResponse>("/translator/translate", {
		method: "POST",
		body: JSON.stringify(body),
	});
}

export function GetTunnel(): Promise<TunnelStatus> {
	return request<TunnelStatus>("/tunnel", { method: "GET" });
}

export function DisableTunnel(): Promise<TunnelStatus> {
	return request<TunnelStatus>("/tunnel/disable", { method: "POST" });
}

export function EnableTunnel(): Promise<TunnelStatus> {
	return request<TunnelStatus>("/tunnel/enable", { method: "POST" });
}

export function GetTunnelStatus(): Promise<TunnelStatus> {
	return request<TunnelStatus>("/tunnel/status", { method: "GET" });
}

export function CheckTailscale(): Promise<TailscaleStatus> {
	return request<TailscaleStatus>("/tunnel/tailscale-check", { method: "GET" });
}

export function DisableTailscale(): Promise<TailscaleStatus> {
	return request<TailscaleStatus>("/tunnel/tailscale-disable", { method: "POST" });
}

export function EnableTailscale(): Promise<TailscaleStatus> {
	return request<TailscaleStatus>("/tunnel/tailscale-enable", { method: "POST" });
}

export function InstallTailscale(): Promise<TailscaleStatus> {
	return request<TailscaleStatus>("/tunnel/tailscale-install", { method: "POST" });
}

export function GetUsageChart(): Promise<UsageChart> {
	return request<UsageChart>("/usage/chart", { method: "GET" });
}

export function GetUsageHistory(): Promise<UsageHistory> {
	return request<UsageHistory>("/usage/history", { method: "GET" });
}

export function GetUsageLogs(): Promise<UsageRequestLogs> {
	return request<UsageRequestLogs>("/usage/logs", { method: "GET" });
}

export function GetUsageProviders(): Promise<UsageProviders> {
	return request<UsageProviders>("/usage/providers", { method: "GET" });
}

export function GetUsageRequestDetails(): Promise<UsageRequestDetail[]> {
	return request<UsageRequestDetail[]>("/usage/request-details", { method: "GET" });
}

export function GetUsageRequestLogs(): Promise<UsageRequestLogs> {
	return request<UsageRequestLogs>("/usage/request-logs", { method: "GET" });
}

export function GetUsageStats(): Promise<UsageStats> {
	return request<UsageStats>("/usage/stats", { method: "GET" });
}

export function GetUsageStream(): Promise<StreamEvent> {
	return request<StreamEvent>("/usage/stream", { method: "GET" });
}

export function GetUsageConnection(	connectionId: string): Promise<UsageRequestDetail> {
	return request<UsageRequestDetail>(`/usage/${connectionId}`, { method: "GET" });
}

export function GetVersion(): Promise<VersionInfo> {
	return request<VersionInfo>("/version", { method: "GET" });
}

export function ShutdownForUpdate(): Promise<void> {
	return request<void>("/version/shutdown", { method: "POST" });
}

export function UpdateVersion(): Promise<UpdateStatus> {
	return request<UpdateStatus>("/version/update", { method: "POST" });
}
