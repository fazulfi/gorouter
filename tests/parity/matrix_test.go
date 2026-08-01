package parity_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func matrixRoot(elem ...string) string {
	return filepath.Join(append([]string{"..", ".."}, elem...)...)
}

func docPath(name string) string {
	return matrixRoot("docs", "implementation", name)
}

type providerEntry struct {
	ProviderID       string   `yaml:"provider_id"`
	DisplayName      string   `yaml:"display_name"`
	ProviderType     string   `yaml:"provider_type"`
	Category         string   `yaml:"category"`
	AuthType         string   `yaml:"auth_type"`
	ExecutorPath     string   `yaml:"executor_path"`
	SupportedFormats []string `yaml:"supported_formats"`
	SourceCitation   string   `yaml:"source_citation"`
}

type formatEntry struct {
	FormatConstant string `yaml:"format_constant"`
	CodecPackage   string `yaml:"codec_package"`
	Detection      string `yaml:"detection"`
	TerminalEvent  string `yaml:"terminal_event"`
	SourceCitation string `yaml:"source_citation"`
}

type oauthEntry struct {
	FlowID         string   `yaml:"flow_id"`
	Mechanism      string   `yaml:"mechanism"`
	PortBehavior   string   `yaml:"port_behavior"`
	PKCE           string   `yaml:"pkce"`
	Providers      []string `yaml:"providers"`
	RestartPolicy  string   `yaml:"restart_policy"`
	SourceCitation string   `yaml:"source_citation"`
}

type providerManifest struct {
	Entries []providerEntry `yaml:"providers"`
}
type formatManifest struct {
	Entries []formatEntry `yaml:"formats"`
}
type oauthManifest struct {
	Entries []oauthEntry `yaml:"oauth_flows"`
}

type expectedProviderRow struct {
	displayName                                    string
	category, authType, providerType, executorPath string
	formats                                        []string
	citationKey                                    string
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

type expectedOAuthRow struct {
	mechanism, pkce, portBehavior string
	providers                     []string
}

type expectedFormatRow struct {
	codecPackage, detection, terminalEvent, citationKey string
}

const (
	expectedProviderCount = 100
	expectedFormatCount   = 5
	expectedOAuthCount    = 20
)

var shorthandPatterns = []string{
	"all audited", "per registry", "SOURCE NEEDED", "TODO", "TBD", "...", "to be determined",
}

var expectedProviders = map[string]expectedProviderRow{
	"alicode":           {"Ali Code", "apikey", "apikey", "alicode", "generic", []string{"FormatOpenAICompat"}, "registry/alicode.js"},
	"alicode-intl":      {"Ali Code (Intl)", "apikey", "apikey", "alicode-intl", "generic", []string{"FormatOpenAICompat"}, "registry/alicode-intl.js"},
	"alims-intl":        {"Ali MS (Intl)", "apikey", "apikey", "alims-intl", "generic", []string{"FormatOpenAICompat"}, "registry/alims-intl.js"},
	"anthropic":         {"Anthropic", "apikey", "apikey", "anthropic", "generic", []string{"FormatAnthropic"}, "registry/anthropic.js"},
	"assemblyai":        {"AssemblyAI", "apikey", "apikey", "assemblyai", "generic", []string{"FormatOpenAICompat"}, "registry/assemblyai.js"},
	"aws-polly":         {"AWS Polly", "apikey", "apikey", "aws-polly", "generic", []string{"FormatOpenAICompat"}, "registry/aws-polly.js"},
	"azure":             {"Azure OpenAI", "apikey", "apikey", "azure", "specialized/azure_openai", []string{"FormatOpenAIChat", "FormatOpenAICompat"}, "registry/azure.js"},
	"black-forest-labs": {"Black Forest Labs", "apikey", "apikey", "black_forest_labs", "generic", []string{"FormatOpenAICompat"}, "registry/black-forest-labs.js"},
	"blackbox":          {"Blackbox", "apikey", "apikey", "blackbox", "generic", []string{"FormatOpenAICompat"}, "registry/blackbox.js"},
	"brave-search":      {"Brave Search", "apikey", "apikey", "brave_search", "generic", []string{"FormatOpenAICompat"}, "registry/brave-search.js"},
	"cartesia":          {"Cartesia", "apikey", "apikey", "cartesia", "generic", []string{"FormatOpenAICompat"}, "registry/cartesia.js"},
	"cerebras":          {"Cerebras", "apikey", "apikey", "cerebras", "generic", []string{"FormatOpenAICompat"}, "registry/cerebras.js"},
	"chutes":            {"Chutes", "apikey", "apikey", "chutes", "generic", []string{"FormatOpenAICompat"}, "registry/chutes.js"},
	"cohere":            {"Cohere", "apikey", "apikey", "cohere", "generic", []string{"FormatOpenAICompat"}, "registry/cohere.js"},
	"comfyui":           {"ComfyUI", "apikey", "apikey", "comfyui", "generic", []string{"FormatOpenAICompat"}, "registry/comfyui.js"},
	"commandcode":       {"Command Code", "apikey", "apikey", "commandcode", "specialized/gemini", []string{"FormatGemini"}, "registry/commandcode.js"},
	"deepgram":          {"Deepgram", "apikey", "apikey", "deepgram", "generic", []string{"FormatOpenAICompat"}, "registry/deepgram.js"},
	"deepseek":          {"DeepSeek", "apikey", "apikey", "deepseek", "specialized/deepseek", []string{"FormatOpenAIChat", "FormatOpenAICompat"}, "registry/deepseek.js"},
	"elevenlabs":        {"ElevenLabs", "apikey", "apikey", "elevenlabs", "generic", []string{"FormatOpenAICompat"}, "registry/elevenlabs.js"},
	"exa":               {"Exa", "apikey", "apikey", "exa", "generic", []string{"FormatOpenAICompat"}, "registry/exa.js"},
	"fal-ai":            {"Fal AI", "apikey", "apikey", "fal_ai", "generic", []string{"FormatOpenAICompat"}, "registry/fal-ai.js"},
	"featherless":       {"Featherless", "apikey", "apikey", "featherless", "generic", []string{"FormatOpenAICompat"}, "registry/featherless.js"},
	"firecrawl":         {"Firecrawl", "apikey", "apikey", "firecrawl", "generic", []string{"FormatOpenAICompat"}, "registry/firecrawl.js"},
	"fireworks":         {"Fireworks AI", "apikey", "apikey", "fireworks", "specialized/fireworks", []string{"FormatOpenAIChat", "FormatOpenAICompat"}, "registry/fireworks.js"},
	"glm":               {"GLM", "apikey", "apikey", "glm", "generic", []string{"FormatOpenAICompat"}, "registry/glm.js"},
	"glm-cn":            {"GLM (China)", "apikey", "apikey", "glm_cn", "generic", []string{"FormatOpenAICompat"}, "registry/glm-cn.js"},
	"google-pse":        {"Google PSE", "apikey", "apikey", "google_pse", "generic", []string{"FormatOpenAICompat"}, "registry/google-pse.js"},
	"groq":              {"Groq", "apikey", "apikey", "groq", "specialized/groq", []string{"FormatOpenAIChat", "FormatOpenAICompat"}, "registry/groq.js"},
	"huggingface":       {"Hugging Face", "apikey", "apikey", "huggingface", "generic", []string{"FormatOpenAICompat"}, "registry/huggingface.js"},
	"hyperbolic":        {"Hyperbolic", "apikey", "apikey", "hyperbolic", "generic", []string{"FormatOpenAICompat"}, "registry/hyperbolic.js"},
	"inworld":           {"Inworld", "apikey", "apikey", "inworld", "generic", []string{"FormatOpenAICompat"}, "registry/inworld.js"},
	"jina-ai":           {"Jina AI", "apikey", "apikey", "jina_ai", "generic", []string{"FormatOpenAICompat"}, "registry/jina-ai.js"},
	"jina-reader":       {"Jina Reader", "apikey", "apikey", "jina_reader", "generic", []string{"FormatOpenAICompat"}, "registry/jina-reader.js"},
	"linkup":            {"Linkup", "apikey", "apikey", "linkup", "generic", []string{"FormatOpenAICompat"}, "registry/linkup.js"},
	"minimax":           {"MiniMax", "apikey", "apikey", "minimax", "generic", []string{"FormatOpenAICompat"}, "registry/minimax.js"},
	"minimax-cn":        {"MiniMax (China)", "apikey", "apikey", "minimax_cn", "generic", []string{"FormatOpenAICompat"}, "registry/minimax-cn.js"},
	"mistral":           {"Mistral", "apikey", "apikey", "mistral", "specialized/mistral", []string{"FormatOpenAIChat", "FormatOpenAICompat"}, "registry/mistral.js"},
	"mmf":               {"MMF", "apikey", "none", "mmf", "specialized/gemini", []string{"FormatGemini"}, "registry/mmf.js"},
	"nanobanana":        {"Nanobanana", "apikey", "apikey", "nanobanana", "generic", []string{"FormatOpenAICompat"}, "registry/nanobanana.js"},
	"nebius":            {"Nebius", "apikey", "apikey", "nebius", "generic", []string{"FormatOpenAICompat"}, "registry/nebius.js"},
	"ollama-local":      {"Ollama Local", "apikey", "apikey", "ollama_local", "specialized/gemini", []string{"FormatOpenAICompat"}, "registry/ollama-local.js"},
	"openai":            {"OpenAI", "apikey", "apikey", "openai", "generic", []string{"FormatOpenAIChat", "FormatOpenAICompat"}, "registry/openai.js"},
	"opencode-go":       {"OpenCode Go", "apikey", "apikey", "opencode_go", "generic", []string{"FormatOpenAICompat"}, "registry/opencode-go.js"},
	"perplexity":        {"Perplexity", "apikey", "apikey", "perplexity", "specialized/perplexity", []string{"FormatOpenAICompat"}, "registry/perplexity.js"},
	"perplexity-agent":  {"Perplexity Agent", "apikey", "apikey", "perplexity_agent", "specialized/perplexity", []string{"FormatOpenAICompat"}, "registry/perplexity-agent.js"},
	"playht":            {"PlayHT", "apikey", "apikey", "playht", "generic", []string{"FormatOpenAICompat"}, "registry/playht.js"},
	"recraft":           {"Recraft", "apikey", "apikey", "recraft", "generic", []string{"FormatOpenAICompat"}, "registry/recraft.js"},
	"runwayml":          {"RunwayML", "apikey", "apikey", "runwayml", "generic", []string{"FormatOpenAICompat"}, "registry/runwayml.js"},
	"sdwebui":           {"SD Web UI", "apikey", "apikey", "sdwebui", "generic", []string{"FormatOpenAICompat"}, "registry/sdwebui.js"},
	"searchapi":         {"Search API", "apikey", "apikey", "searchapi", "generic", []string{"FormatOpenAICompat"}, "registry/searchapi.js"},
	"serper":            {"Serper", "apikey", "apikey", "serper", "generic", []string{"FormatOpenAICompat"}, "registry/serper.js"},
	"siliconflow":       {"SiliconFlow", "apikey", "apikey", "siliconflow", "generic", []string{"FormatOpenAICompat"}, "registry/siliconflow.js"},
	"stability-ai":      {"Stability AI", "apikey", "apikey", "stability_ai", "generic", []string{"FormatOpenAICompat"}, "registry/stability-ai.js"},
	"tavily":            {"Tavily", "apikey", "apikey", "tavily", "generic", []string{"FormatOpenAICompat"}, "registry/tavily.js"},
	"together":          {"Together AI", "apikey", "apikey", "together", "specialized/together", []string{"FormatOpenAIChat", "FormatOpenAICompat"}, "registry/together.js"},
	"topaz":             {"Topaz", "apikey", "apikey", "topaz", "generic", []string{"FormatOpenAICompat"}, "registry/topaz.js"},
	"venice":            {"Venice", "apikey", "apikey", "venice", "generic", []string{"FormatOpenAICompat"}, "registry/venice.js"},
	"vercel-ai-gateway": {"Vercel AI Gateway", "apikey", "apikey", "vercel_ai_gateway", "generic", []string{"FormatOpenAICompat"}, "registry/vercel-ai-gateway.js"},
	"vertex-partner":    {"Vertex AI Partner", "apikey", "apikey", "vertex_partner", "specialized/gcp_vertex", []string{"FormatGemini"}, "registry/vertex-partner.js"},
	"volcengine-ark":    {"Volcengine Ark", "apikey", "apikey", "volcengine_ark", "generic", []string{"FormatOpenAICompat"}, "registry/volcengine-ark.js"},
	"voyage-ai":         {"Voyage AI", "apikey", "apikey", "voyage_ai", "generic", []string{"FormatOpenAICompat"}, "registry/voyage-ai.js"},
	"xiaomi-mimo":       {"Xiaomi Mimo", "apikey", "apikey", "xiaomi-mimo", "generic", []string{"FormatOpenAICompat"}, "registry/xiaomi-mimo.js"},
	"xiaomi-tokenplan":  {"Xiaomi Tokenplan", "apikey", "apikey", "xiaomi-tokenplan", "specialized/gemini", []string{"FormatGemini"}, "registry/xiaomi-tokenplan.js"},
	"youcom":            {"You.com", "apikey", "apikey", "youcom", "generic", []string{"FormatOpenAICompat"}, "registry/youcom.js"},
	"antigravity":       {"Antigravity", "oauth", "oauth", "antigravity", "specialized/gemini", []string{"FormatGemini"}, "registry/antigravity.js"},
	"claude":            {"Claude", "oauth", "oauth", "anthropic", "generic", []string{"FormatAnthropic"}, "registry/claude.js"},
	"cline":             {"Cline", "oauth", "oauth", "cline", "generic", []string{"FormatOpenAICompat"}, "registry/cline.js"},
	"clinepass":         {"Cline Pass", "oauth", "oauth", "clinepass", "generic", []string{"FormatOpenAICompat"}, "registry/clinepass.js"},
	"codebuddy-cn":      {"CodeBuddy CN", "oauth", "oauth", "codebuddy_cn", "specialized/gemini", []string{"FormatGemini"}, "registry/codebuddy-cn.js"},
	"codex":             {"OpenAI Codex", "oauth", "oauth", "codex", "specialized/codex", []string{"FormatCodexResponses"}, "registry/codex.js"},
	"cursor":            {"Cursor", "oauth", "pat", "cursor", "specialized/cursor", []string{"FormatOpenAICompat"}, "registry/cursor.js"},
	"github":            {"GitHub", "oauth", "oauth", "github_models", "specialized/github_models", []string{"FormatOpenAICompat"}, "registry/github.js"},
	"gitlab":            {"GitLab", "oauth", "oauth", "gitlab", "generic", []string{"FormatOpenAICompat"}, "registry/gitlab.js"},
	"grok-cli":          {"Grok CLI", "oauth", "oauth", "grok_cli", "specialized/gemini", []string{"FormatGemini"}, "registry/grok-cli.js"},
	"iflow":             {"IFlow", "oauth", "oauth", "iflow", "specialized/gemini", []string{"FormatGemini"}, "registry/iflow.js"},
	"kilocode":          {"Kilocode", "oauth", "oauth", "kilocode", "generic", []string{"FormatOpenAICompat"}, "registry/kilocode.js"},
	"kimchi":            {"Kimchi", "oauth", "cookie", "kimchi", "specialized/gemini", []string{"FormatGemini"}, "registry/kimchi.js"},
	"kimi":              {"Kimi", "oauth", "oauth", "kimi", "generic", []string{"FormatOpenAICompat"}, "registry/kimi.js"},
	"qwen":              {"Qwen", "oauth", "oauth", "qwen", "specialized/gemini", []string{"FormatGemini"}, "registry/qwen.js"},
	"xai":               {"xAI", "oauth", "oauth", "xai", "specialized/xai", []string{"FormatOpenAICompat"}, "registry/xai.js"},
	"byteplus":          {"BytePlus", "freeTier", "apikey", "byteplus", "generic", []string{"FormatOpenAICompat"}, "registry/byteplus.js"},
	"cloudflare-ai":     {"Cloudflare AI", "freeTier", "apikey", "cloudflare_ai", "generic", []string{"FormatOpenAICompat"}, "registry/cloudflare-ai.js"},
	"coqui":             {"Coqui", "freeTier", "none", "coqui", "generic", []string{"FormatOpenAICompat"}, "registry/coqui.js"},
	"edge-tts":          {"Edge TTS", "freeTier", "none", "edge_tts", "generic", []string{"FormatOpenAICompat"}, "registry/edge-tts.js"},
	"gemini":            {"Gemini", "freeTier", "apikey", "gemini", "specialized/gemini", []string{"FormatGemini"}, "registry/gemini.js"},
	"google-tts":        {"Google TTS", "freeTier", "none", "google_tts", "generic", []string{"FormatOpenAICompat"}, "registry/google-tts.js"},
	"local-device":      {"Local Device", "freeTier", "none", "local_device", "generic", []string{"FormatOpenAICompat"}, "registry/local-device.js"},
	"nvidia":            {"NVIDIA", "freeTier", "apikey", "nvidia", "generic", []string{"FormatOpenAICompat"}, "registry/nvidia.js"},
	"ollama":            {"Ollama", "freeTier", "apikey", "ollama", "generic", []string{"FormatOpenAICompat"}, "registry/ollama.js"},
	"openrouter":        {"OpenRouter", "freeTier", "apikey", "openrouter", "generic", []string{"FormatOpenAIChat", "FormatOpenAICompat"}, "registry/openrouter.js"},
	"searxng":           {"SearXNG", "freeTier", "none", "searxng", "generic", []string{"FormatOpenAICompat"}, "registry/searxng.js"},
	"tortoise":          {"Tortoise TTS", "freeTier", "none", "tortoise", "generic", []string{"FormatOpenAICompat"}, "registry/tortoise.js"},
	"vertex":            {"Vertex AI", "freeTier", "apikey", "vertex", "specialized/gcp_vertex", []string{"FormatGemini"}, "registry/vertex.js"},
	"gemini-cli":        {"Gemini CLI", "free", "none", "gemini_cli", "specialized/gemini", []string{"FormatGemini"}, "registry/gemini-cli.js"},
	"kiro":              {"Kiro", "free", "none", "kiro", "specialized/kiro", []string{"FormatOpenAICompat"}, "registry/kiro.js"},
	"mimo-free":         {"Mimo Free", "free", "none", "mimo_free", "specialized/gemini", []string{"FormatGemini"}, "registry/mimo-free.js"},
	"opencode":          {"OpenCode", "free", "none", "opencode", "generic", []string{"FormatOpenAICompat"}, "registry/opencode.js"},
	"qoder":             {"Qoder", "free", "none", "qoder", "specialized/gemini", []string{"FormatGemini"}, "registry/qoder.js"},
	"grok-web":          {"Grok Web", "webCookie", "cookie", "grok_web", "specialized/gemini", []string{"FormatOpenAICompat"}, "registry/grok-web.js"},
	"perplexity-web":    {"Perplexity Web", "webCookie", "cookie", "perplexity_web", "specialized/gemini", []string{"FormatOpenAICompat"}, "registry/perplexity-web.js"},
}

var expectedOAuthFlows = map[string]expectedOAuthRow{
	"claude":       {"Authorization code with PKCE (standard callback)", "S256", "find_free", []string{"claude"}},
	"codex":        {"Authorization code with PKCE plus fixed-port proxy (port 1455)", "S256", "fixed (1455)", []string{"codex"}},
	"openai":       {"Authorization code with PKCE (standard callback)", "S256", "find_free", []string{"openai"}},
	"gemini-cli":   {"Authorization code (no PKCE, standard callback)", "none", "find_free", []string{"gemini-cli"}},
	"antigravity":  {"Authorization code (no PKCE, standard callback)", "none", "find_free", []string{"antigravity"}},
	"iflow":        {"Authorization code (no PKCE, standard callback)", "none", "find_free", []string{"iflow"}},
	"qwen":         {"Device code flow with PKCE", "S256", "none", []string{"qwen"}},
	"qoder":        {"Device code flow (custom) with PKCE", "S256", "none", []string{"qoder"}},
	"github":       {"Device code flow (no PKCE)", "none", "none", []string{"github"}},
	"kiro":         {"Device code flow (AWS SSO OIDC, no PKCE)", "none", "none", []string{"kiro"}},
	"kimi":         {"Device code flow (no PKCE)", "none", "none", []string{"kimi"}},
	"kilocode":     {"Device code flow (custom, no PKCE)", "none", "none", []string{"kilocode"}},
	"codebuddy-cn": {"Device code flow (custom, no PKCE)", "none", "none", []string{"codebuddy-cn"}},
	"grok-cli":     {"Device code flow (no PKCE)", "none", "none", []string{"grok-cli"}},
	"gitlab":       {"Authorization code with PKCE (standard callback)", "S256", "find_free", []string{"gitlab"}},
	"cline":        {"Authorization code (no PKCE)", "none", "find_free", []string{"cline"}},
	"clinepass":    {"Authorization code (no PKCE, pass-through)", "none", "find_free", []string{"clinepass"}},
	"kimchi":       {"Browser token import (cookie-based)", "none", "none", []string{"kimchi"}},
	"cursor":       {"IDE token import (local credential discovery)", "none", "none", []string{"cursor"}},
	"xai":          {"Authorization code with PKCE plus fixed-port proxy (port 56121)", "S256", "fixed (56121)", []string{"xai"}},
}

var expectedFormats = map[string]expectedFormatRow{
	"FormatOpenAIChat":     {"engine/formats/openai/", "Endpoint /v1/chat/completions or body has messages + model", "data: [DONE]\\n\\n", "types.go:14"},
	"FormatOpenAICompat":   {"engine/formats/openai/", "Audited OpenAI-compatible endpoint/body rules from provider transport config", "data: [DONE]\\n\\n (or provider-specific variant)", "types.go:15"},
	"FormatCodexResponses": {"engine/formats/responses/", "Endpoint /v1/responses plus audited Codex/Responses discrimination", "Response-typed completion/failure terminal from pinned source (event: done / response.failed)", "types.go:16"},
	"FormatAnthropic":      {"engine/formats/claude/", "Endpoint /v1/messages or anthropic-version header", "event: message_stop\\ndata: {}\\n\\n", "types.go:17"},
	"FormatGemini":         {"engine/formats/gemini/", "Endpoint /v1beta/models/* or x-goog-api-key header", "finishReason in candidates; no [DONE] sentinel", "types.go:18"},
}

func TestProviderMatrixExists(t *testing.T) {
	if _, err := os.Stat(docPath("provider-matrix.yaml")); os.IsNotExist(err) {
		t.Fatal("provider-matrix.yaml does not exist")
	}
}

func TestProviderMatrixCount(t *testing.T) {
	if len(mustLoadProviderManifest(t)) != expectedProviderCount {
		t.Error("count mismatch")
	}
}

func TestProviderMatrixSourceCitations(t *testing.T) {
	for _, e := range mustLoadProviderManifest(t) {
		if strings.TrimSpace(e.SourceCitation) == "" {
			t.Errorf("provider %q empty source_citation", e.ProviderID)
		}
	}
}

func TestProviderMatrixNoShorthand(t *testing.T) {
	for _, e := range mustLoadProviderManifest(t) {
		for _, pat := range shorthandPatterns {
			if containsProviderShorthand(e, pat) {
				t.Errorf("provider %q contains %q", e.ProviderID, pat)
			}
		}
	}
}

func TestProviderMatrixRequiredFields(t *testing.T) {
	for _, e := range mustLoadProviderManifest(t) {
		if e.ProviderID == "" {
			t.Error("empty provider_id")
		}
		if e.DisplayName == "" {
			t.Errorf("provider %q empty display_name", e.ProviderID)
		}
		if e.ProviderType == "" {
			t.Errorf("provider %q empty provider_type", e.ProviderID)
		}
		if e.Category == "" {
			t.Errorf("provider %q empty category", e.ProviderID)
		}
		if e.AuthType == "" {
			t.Errorf("provider %q empty auth_type", e.ProviderID)
		}
		if e.ExecutorPath == "" {
			t.Errorf("provider %q empty executor_path", e.ProviderID)
		}
		if len(e.SupportedFormats) == 0 {
			t.Errorf("provider %q empty formats", e.ProviderID)
		}
	}
}

func TestFormatMatrixExists(t *testing.T) {
	if _, err := os.Stat(docPath("format-matrix.yaml")); os.IsNotExist(err) {
		t.Fatal("missing")
	}
}

func TestFormatMatrixCount(t *testing.T) {
	if len(mustLoadFormatManifest(t)) != expectedFormatCount {
		t.Error("count mismatch")
	}
}

func TestFormatMatrixRequiredFields(t *testing.T) {
	for _, e := range mustLoadFormatManifest(t) {
		if e.FormatConstant == "" {
			t.Error("empty format_constant")
		}
		if e.CodecPackage == "" {
			t.Errorf("format %q empty codec_package", e.FormatConstant)
		}
		if e.Detection == "" {
			t.Errorf("format %q empty detection", e.FormatConstant)
		}
		if e.TerminalEvent == "" {
			t.Errorf("format %q empty terminal_event", e.FormatConstant)
		}
		if e.SourceCitation == "" {
			t.Errorf("format %q empty source_citation", e.FormatConstant)
		}
	}
}

func TestFormatMatrixNoShorthand(t *testing.T) {
	for _, e := range mustLoadFormatManifest(t) {
		for _, pat := range shorthandPatterns {
			if containsFormatShorthand(e, pat) {
				t.Errorf("format %q contains %q", e.FormatConstant, pat)
			}
		}
	}
}

func TestOAuthMatrixExists(t *testing.T) {
	if _, err := os.Stat(docPath("oauth-matrix.yaml")); os.IsNotExist(err) {
		t.Fatal("missing")
	}
}

func TestOAuthMatrixCount(t *testing.T) {
	if len(mustLoadOAuthManifest(t)) != expectedOAuthCount {
		t.Error("count mismatch")
	}
}

func TestOAuthMatrixRequiredFields(t *testing.T) {
	for _, e := range mustLoadOAuthManifest(t) {
		if e.FlowID == "" {
			t.Error("empty flow_id")
		}
		if e.Mechanism == "" {
			t.Errorf("flow %q empty mechanism", e.FlowID)
		}
		if e.PortBehavior == "" {
			t.Errorf("flow %q empty port_behavior", e.FlowID)
		}
		if e.RestartPolicy == "" {
			t.Errorf("flow %q empty restart_policy", e.FlowID)
		}
		if e.SourceCitation == "" {
			t.Errorf("flow %q empty source_citation", e.FlowID)
		}
		if len(e.Providers) == 0 {
			t.Errorf("flow %q empty providers", e.FlowID)
		}
	}
}

func TestOAuthMatrixRestartPolicy(t *testing.T) {
	for _, e := range mustLoadOAuthManifest(t) {
		if e.RestartPolicy != "cancel_all_pending" {
			t.Errorf("flow %q restart=%q", e.FlowID, e.RestartPolicy)
		}
	}
}

func TestOAuthMatrixNoShorthand(t *testing.T) {
	for _, e := range mustLoadOAuthManifest(t) {
		for _, pat := range shorthandPatterns {
			if containsOAuthShorthand(e, pat) {
				t.Errorf("flow %q contains %q", e.FlowID, pat)
			}
		}
	}
}

func TestFormatConstantsMatchDomain(t *testing.T) {
	entries := mustLoadFormatManifest(t)
	seen := map[string]int{}
	for _, e := range entries {
		seen[e.FormatConstant]++
	}
	for _, e := range entries {
		if seen[e.FormatConstant] != 1 {
			t.Errorf("format %q appears %d times", e.FormatConstant, seen[e.FormatConstant])
		}
	}
	baseline := map[string]bool{"FormatOpenAIChat": false, "FormatOpenAICompat": false, "FormatCodexResponses": false, "FormatAnthropic": false, "FormatGemini": false}
	for _, e := range entries {
		if _, ok := baseline[e.FormatConstant]; ok {
			baseline[e.FormatConstant] = true
		}
	}
	for name, found := range baseline {
		if !found {
			t.Errorf("baseline format %q missing", name)
		}
	}
}

func TestProviderFormatsCrossReference(t *testing.T) {
	formatSet := map[string]bool{}
	for _, f := range mustLoadFormatManifest(t) {
		formatSet[f.FormatConstant] = true
	}
	for _, p := range mustLoadProviderManifest(t) {
		for _, f := range p.SupportedFormats {
			if !formatSet[f] {
				t.Errorf("provider %q unknown format %q", p.ProviderID, f)
			}
		}
	}
}

func TestProviderExhaustiveSemanticParity(t *testing.T) {
	entries := mustLoadProviderManifest(t)
	if len(entries) != len(expectedProviders) {
		t.Fatalf("manifest %d != expected %d", len(entries), len(expectedProviders))
	}
	byID := map[string]providerEntry{}
	for _, e := range entries {
		byID[e.ProviderID] = e
	}
	for pid := range expectedProviders {
		if _, ok := byID[pid]; !ok {
			t.Errorf("expected %q missing from manifest", pid)
		}
	}
	for _, e := range entries {
		if _, ok := expectedProviders[e.ProviderID]; !ok {
			t.Errorf("unexpected provider %q", e.ProviderID)
		}
	}
	for pid, exp := range expectedProviders {
		e, ok := byID[pid]
		if !ok {
			continue
		}
		if e.DisplayName != exp.displayName {
			t.Errorf("%s display_name=%q want %q", pid, e.DisplayName, exp.displayName)
		}
		if e.Category != exp.category {
			t.Errorf("%s category=%q want %q", pid, e.Category, exp.category)
		}
		if e.AuthType != exp.authType {
			t.Errorf("%s auth_type=%q want %q", pid, e.AuthType, exp.authType)
		}
		if e.ProviderType != exp.providerType {
			t.Errorf("%s provider_type=%q want %q", pid, e.ProviderType, exp.providerType)
		}
		if e.ExecutorPath != exp.executorPath {
			t.Errorf("%s executor_path=%q want %q", pid, e.ExecutorPath, exp.executorPath)
		}
		if !equalStringSlice(e.SupportedFormats, exp.formats) {
			t.Errorf("%s formats=%v want %v", pid, e.SupportedFormats, exp.formats)
		}
		if !strings.Contains(e.SourceCitation, exp.citationKey) {
			t.Errorf("%s citation missing %q: %q", pid, exp.citationKey, e.SourceCitation)
		}
	}
}

func TestOAuthExhaustiveSemanticParity(t *testing.T) {
	entries := mustLoadOAuthManifest(t)
	if len(entries) != len(expectedOAuthFlows) {
		t.Fatalf("manifest %d != expected %d", len(entries), len(expectedOAuthFlows))
	}
	byID := map[string]oauthEntry{}
	for _, e := range entries {
		byID[e.FlowID] = e
	}
	for fid := range expectedOAuthFlows {
		if _, ok := byID[fid]; !ok {
			t.Errorf("expected flow %q missing", fid)
		}
	}
	for _, e := range entries {
		if _, ok := expectedOAuthFlows[e.FlowID]; !ok {
			t.Errorf("unexpected flow %q", e.FlowID)
		}
	}
	for fid, exp := range expectedOAuthFlows {
		e, ok := byID[fid]
		if !ok {
			continue
		}
		if e.Mechanism != exp.mechanism {
			t.Errorf("flow %s mechanism=%q want %q", fid, e.Mechanism, exp.mechanism)
		}
		if e.PKCE != exp.pkce {
			t.Errorf("flow %s pkce=%q want %q", fid, e.PKCE, exp.pkce)
		}
		if e.PortBehavior != exp.portBehavior {
			t.Errorf("flow %s port=%q want %q", fid, e.PortBehavior, exp.portBehavior)
		}
		if e.RestartPolicy != "cancel_all_pending" {
			t.Errorf("flow %s restart=%q", fid, e.RestartPolicy)
		}
		if !equalStringSlice(e.Providers, exp.providers) {
			t.Errorf("flow %s providers=%v want %v", fid, e.Providers, exp.providers)
		}
		if strings.TrimSpace(e.SourceCitation) == "" {
			t.Errorf("flow %s empty citation", fid)
		}
	}
}

func TestFormatExhaustiveSemanticParity(t *testing.T) {
	entries := mustLoadFormatManifest(t)
	if len(entries) != len(expectedFormats) {
		t.Fatalf("manifest %d != expected %d", len(entries), len(expectedFormats))
	}
	byID := map[string]formatEntry{}
	for _, e := range entries {
		byID[e.FormatConstant] = e
	}
	for fid := range expectedFormats {
		if _, ok := byID[fid]; !ok {
			t.Errorf("expected format %q missing", fid)
		}
	}
	for _, e := range entries {
		if _, ok := expectedFormats[e.FormatConstant]; !ok {
			t.Errorf("unexpected format %q", e.FormatConstant)
		}
	}
	for fid, exp := range expectedFormats {
		e, ok := byID[fid]
		if !ok {
			continue
		}
		if e.CodecPackage != exp.codecPackage {
			t.Errorf("format %s codec=%q want %q", fid, e.CodecPackage, exp.codecPackage)
		}
		if e.Detection != exp.detection {
			t.Errorf("format %s detect=%q want %q", fid, e.Detection, exp.detection)
		}
		if e.TerminalEvent != exp.terminalEvent {
			t.Errorf("format %s term=%q want %q", fid, e.TerminalEvent, exp.terminalEvent)
		}
		if !strings.Contains(e.SourceCitation, exp.citationKey) {
			t.Errorf("format %s citation=%q should contain %q", fid, e.SourceCitation, exp.citationKey)
		}
	}
}

func TestAuthTypeNoneProviders(t *testing.T) {
	byID := map[string]providerEntry{}
	for _, e := range mustLoadProviderManifest(t) {
		byID[e.ProviderID] = e
	}
	for pid, exp := range expectedProviders {
		if exp.authType != "none" {
			continue
		}
		e, ok := byID[pid]
		if !ok {
			t.Errorf("provider %q not found", pid)
			continue
		}
		if e.AuthType != "none" {
			t.Errorf("%q auth=%q want none", pid, e.AuthType)
		}
	}
}

func TestMMFCategoryAndAuth(t *testing.T) {
	for _, e := range mustLoadProviderManifest(t) {
		if e.ProviderID != "mmf" {
			continue
		}
		if e.Category != "apikey" {
			t.Errorf("mmf category=%q", e.Category)
		}
		if e.AuthType != "none" {
			t.Errorf("mmf auth=%q", e.AuthType)
		}
		if !strings.Contains(e.SourceCitation, "noAuth=true") {
			t.Errorf("mmf citation: %q", e.SourceCitation)
		}
		return
	}
	t.Error("mmf not found")
}

func TestVoyageAIProviderType(t *testing.T) {
	for _, e := range mustLoadProviderManifest(t) {
		if e.ProviderID != "voyage-ai" {
			continue
		}
		if e.ProviderType != "voyage_ai" {
			t.Errorf("voyage-ai type=%q", e.ProviderType)
		}
		return
	}
	t.Error("voyage-ai not found")
}

func TestGeneratorByteMatch(t *testing.T) {
	type mc struct{ flag, file string }
	for _, c := range []mc{
		{"--stdout", docPath("provider-matrix.yaml")},
		{"--stdout-format", docPath("format-matrix.yaml")},
		{"--stdout-oauth", docPath("oauth-matrix.yaml")},
	} {
		cmd := exec.Command("go", "run", "./tools/upstreammap/", c.flag)
		cmd.Dir = matrixRoot()
		genOut, err := cmd.Output()
		if err != nil {
			t.Fatalf("generator %s: %v", c.flag, err)
		}
		fileBytes, err := os.ReadFile(c.file)
		if err != nil {
			t.Fatalf("read %s: %v", c.file, err)
		}
		if !bytes.Equal(genOut, fileBytes) {
			t.Errorf("byte mismatch %s: gen=%d file=%d", c.file, len(genOut), len(fileBytes))
			minL := len(genOut)
			if len(fileBytes) < minL {
				minL = len(fileBytes)
			}
			for i := 0; i < minL; i++ {
				if genOut[i] != fileBytes[i] {
					s := i - 20
					if s < 0 {
						s = 0
					}
					e := i + 40
					if e > minL {
						e = minL
					}
					t.Logf("diff at byte %d: gen=%q file=%q", i, string(genOut[s:e]), string(fileBytes[s:e]))
					break
				}
			}
		}
	}
}

func mustLoadProviderManifest(t *testing.T) []providerEntry {
	t.Helper()
	data, err := os.ReadFile(docPath("provider-matrix.yaml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var m providerManifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		t.Fatalf("yaml: %v", err)
	}
	return m.Entries
}

func mustLoadFormatManifest(t *testing.T) []formatEntry {
	t.Helper()
	data, err := os.ReadFile(docPath("format-matrix.yaml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var m formatManifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		t.Fatalf("yaml: %v", err)
	}
	return m.Entries
}

func mustLoadOAuthManifest(t *testing.T) []oauthEntry {
	t.Helper()
	data, err := os.ReadFile(docPath("oauth-matrix.yaml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var m oauthManifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		t.Fatalf("yaml: %v", err)
	}
	return m.Entries
}

func containsProviderShorthand(e providerEntry, pat string) bool {
	return strings.Contains(strings.ToLower(e.ProviderID), pat) ||
		strings.Contains(strings.ToLower(e.DisplayName), pat) ||
		strings.Contains(strings.ToLower(e.ProviderType), pat) ||
		strings.Contains(strings.ToLower(e.Category), pat) ||
		strings.Contains(strings.ToLower(e.AuthType), pat) ||
		strings.Contains(strings.ToLower(e.ExecutorPath), pat) ||
		strings.Contains(strings.ToLower(e.SourceCitation), pat)
}

func containsFormatShorthand(e formatEntry, pat string) bool {
	return strings.Contains(strings.ToLower(e.FormatConstant), pat) ||
		strings.Contains(strings.ToLower(e.CodecPackage), pat) ||
		strings.Contains(strings.ToLower(e.Detection), pat) ||
		strings.Contains(strings.ToLower(e.TerminalEvent), pat) ||
		strings.Contains(strings.ToLower(e.SourceCitation), pat)
}

func containsOAuthShorthand(e oauthEntry, pat string) bool {
	if strings.Contains(strings.ToLower(e.FlowID), pat) ||
		strings.Contains(strings.ToLower(e.Mechanism), pat) ||
		strings.Contains(strings.ToLower(e.PortBehavior), pat) ||
		strings.Contains(strings.ToLower(e.PKCE), pat) ||
		strings.Contains(strings.ToLower(e.RestartPolicy), pat) ||
		strings.Contains(strings.ToLower(e.SourceCitation), pat) {
		return true
	}
	for _, p := range e.Providers {
		if strings.Contains(strings.ToLower(p), pat) {
			return true
		}
	}
	return false
}
