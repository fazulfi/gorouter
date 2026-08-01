// Command upstreammap generates and validates the authoritative provider, format,
// and OAuth manifests from the pinned upstream audit.
//
// Usage:
//
//	go run ./tools/upstreammap/           # validate manifests against source data
//	go run ./tools/upstreammap/ --write   # regenerate YAML manifests
//
// Authority baseline: decolua/9router commit 79918c7830695bbca4a45c9fea4a42c3e9fd73d1
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Manifest type definitions — must match tests/parity/matrix_test.go
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Authority data — extracted from pinned upstream SHA 79918c78
// ---------------------------------------------------------------------------

// providerRegistry is the complete 100-entry provider list from upstream
// open-sse/providers/registry/index.js (auto-generated, p0..p99).
// Categories: 64 apikey, 16 oauth, 13 freeTier, 5 free, 2 webCookie = 100
// Source: audit/02-provider-matrix.md
var providerRegistry = []providerEntry{
	// --- apikey (64) - default registry category ---
	{ProviderID: "alicode", DisplayName: "Ali Code", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/alicode.js"},
	{ProviderID: "alicode-intl", DisplayName: "Ali Code (Intl)", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/alicode-intl.js"},
	{ProviderID: "alims-intl", DisplayName: "Ali MS (Intl)", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/alims-intl.js"},
	{ProviderID: "anthropic", DisplayName: "Anthropic", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatAnthropic"}, SourceCitation: "registry/anthropic.js"},
	{ProviderID: "assemblyai", DisplayName: "AssemblyAI", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/assemblyai.js"},
	{ProviderID: "aws-polly", DisplayName: "AWS Polly", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/aws-polly.js"},
	{ProviderID: "azure", DisplayName: "Azure OpenAI", Category: "apikey", AuthType: "apikey", ExecutorPath: "specialized/azure_openai", SupportedFormats: []string{"FormatOpenAIChat", "FormatOpenAICompat"}, SourceCitation: "registry/azure.js, executors/azure.js"},
	{ProviderID: "blackbox", DisplayName: "Blackbox", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/blackbox.js"},
	{ProviderID: "black-forest-labs", DisplayName: "Black Forest Labs", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/black-forest-labs.js"},
	{ProviderID: "brave-search", DisplayName: "Brave Search", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/brave-search.js"},
	{ProviderID: "cartesia", DisplayName: "Cartesia", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/cartesia.js"},
	{ProviderID: "cerebras", DisplayName: "Cerebras", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/cerebras.js"},
	{ProviderID: "chutes", DisplayName: "Chutes", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/chutes.js"},
	{ProviderID: "cohere", DisplayName: "Cohere", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/cohere.js"},
	{ProviderID: "comfyui", DisplayName: "ComfyUI", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/comfyui.js"},
	{ProviderID: "commandcode", DisplayName: "Command Code", Category: "apikey", AuthType: "apikey", ExecutorPath: "specialized/gemini", SupportedFormats: []string{"FormatGemini"}, SourceCitation: "registry/commandcode.js, executors/commandcode.js"},
	{ProviderID: "deepgram", DisplayName: "Deepgram", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/deepgram.js"},
	{ProviderID: "deepseek", DisplayName: "DeepSeek", Category: "apikey", AuthType: "apikey", ExecutorPath: "specialized/deepseek", SupportedFormats: []string{"FormatOpenAIChat", "FormatOpenAICompat"}, SourceCitation: "registry/deepseek.js"},
	{ProviderID: "elevenlabs", DisplayName: "ElevenLabs", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/elevenlabs.js"},
	{ProviderID: "exa", DisplayName: "Exa", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/exa.js"},
	{ProviderID: "fal-ai", DisplayName: "Fal AI", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/fal-ai.js"},
	{ProviderID: "featherless", DisplayName: "Featherless", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/featherless.js"},
	{ProviderID: "firecrawl", DisplayName: "Firecrawl", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/firecrawl.js"},
	{ProviderID: "fireworks", DisplayName: "Fireworks AI", Category: "apikey", AuthType: "apikey", ExecutorPath: "specialized/fireworks", SupportedFormats: []string{"FormatOpenAIChat", "FormatOpenAICompat"}, SourceCitation: "registry/fireworks.js"},
	{ProviderID: "glm", DisplayName: "GLM", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/glm.js"},
	{ProviderID: "glm-cn", DisplayName: "GLM (China)", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/glm-cn.js"},
	{ProviderID: "google-pse", DisplayName: "Google PSE", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/google-pse.js"},
	{ProviderID: "groq", DisplayName: "Groq", Category: "apikey", AuthType: "apikey", ExecutorPath: "specialized/groq", SupportedFormats: []string{"FormatOpenAIChat", "FormatOpenAICompat"}, SourceCitation: "registry/groq.js"},
	{ProviderID: "huggingface", DisplayName: "Hugging Face", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/huggingface.js"},
	{ProviderID: "hyperbolic", DisplayName: "Hyperbolic", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/hyperbolic.js"},
	{ProviderID: "inworld", DisplayName: "Inworld", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/inworld.js"},
	{ProviderID: "jina-ai", DisplayName: "Jina AI", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/jina-ai.js"},
	{ProviderID: "jina-reader", DisplayName: "Jina Reader", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/jina-reader.js"},
	{ProviderID: "linkup", DisplayName: "Linkup", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/linkup.js"},
	{ProviderID: "minimax", DisplayName: "MiniMax", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/minimax.js"},
	{ProviderID: "minimax-cn", DisplayName: "MiniMax (China)", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/minimax-cn.js"},
	{ProviderID: "mistral", DisplayName: "Mistral", Category: "apikey", AuthType: "apikey", ExecutorPath: "specialized/mistral", SupportedFormats: []string{"FormatOpenAIChat", "FormatOpenAICompat"}, SourceCitation: "registry/mistral.js"},
	{ProviderID: "mmf", DisplayName: "MMF", Category: "apikey", AuthType: "none", ExecutorPath: "specialized/gemini", SupportedFormats: []string{"FormatGemini"}, SourceCitation: "registry/mmf.js (category=apikey, noAuth=true, double-entry with mimo-free.js)"},
	{ProviderID: "nanobanana", DisplayName: "Nanobanana", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/nanobanana.js"},
	{ProviderID: "nebius", DisplayName: "Nebius", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/nebius.js"},
	{ProviderID: "ollama-local", DisplayName: "Ollama Local", Category: "apikey", AuthType: "apikey", ExecutorPath: "specialized/gemini", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/ollama-local.js, executors/ollama-local.js"},
	{ProviderID: "openai", DisplayName: "OpenAI", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAIChat", "FormatOpenAICompat"}, SourceCitation: "registry/openai.js"},
	{ProviderID: "opencode-go", DisplayName: "OpenCode Go", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/opencode-go.js"},
	{ProviderID: "perplexity", DisplayName: "Perplexity", Category: "apikey", AuthType: "apikey", ExecutorPath: "specialized/perplexity", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/perplexity.js"},
	{ProviderID: "perplexity-agent", DisplayName: "Perplexity Agent", Category: "apikey", AuthType: "apikey", ExecutorPath: "specialized/perplexity", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/perplexity-agent.js"},
	{ProviderID: "playht", DisplayName: "PlayHT", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/playht.js"},
	{ProviderID: "recraft", DisplayName: "Recraft", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/recraft.js"},
	{ProviderID: "runwayml", DisplayName: "RunwayML", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/runwayml.js"},
	{ProviderID: "sdwebui", DisplayName: "SD Web UI", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/sdwebui.js"},
	{ProviderID: "searchapi", DisplayName: "Search API", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/searchapi.js"},
	{ProviderID: "serper", DisplayName: "Serper", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/serper.js"},
	{ProviderID: "siliconflow", DisplayName: "SiliconFlow", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/siliconflow.js"},
	{ProviderID: "stability-ai", DisplayName: "Stability AI", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/stability-ai.js"},
	{ProviderID: "tavily", DisplayName: "Tavily", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/tavily.js"},
	{ProviderID: "together", DisplayName: "Together AI", Category: "apikey", AuthType: "apikey", ExecutorPath: "specialized/together", SupportedFormats: []string{"FormatOpenAIChat", "FormatOpenAICompat"}, SourceCitation: "registry/together.js"},
	{ProviderID: "topaz", DisplayName: "Topaz", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/topaz.js"},
	{ProviderID: "venice", DisplayName: "Venice", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/venice.js"},
	{ProviderID: "vercel-ai-gateway", DisplayName: "Vercel AI Gateway", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/vercel-ai-gateway.js"},
	{ProviderID: "vertex-partner", DisplayName: "Vertex AI Partner", Category: "apikey", AuthType: "apikey", ExecutorPath: "specialized/gcp_vertex", SupportedFormats: []string{"FormatGemini"}, SourceCitation: "registry/vertex-partner.js, executors/vertex.js"},
	{ProviderID: "volcengine-ark", DisplayName: "Volcengine Ark", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/volcengine-ark.js"},
	{ProviderID: "voyage-ai", DisplayName: "Voyage AI", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/voyage-ai.js"},
	{ProviderID: "xiaomi-mimo", DisplayName: "Xiaomi Mimo", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/xiaomi-mimo.js"},
	{ProviderID: "xiaomi-tokenplan", DisplayName: "Xiaomi Tokenplan", Category: "apikey", AuthType: "apikey", ExecutorPath: "specialized/gemini", SupportedFormats: []string{"FormatGemini"}, SourceCitation: "registry/xiaomi-tokenplan.js, executors/xiaomi-tokenplan.js"},
	{ProviderID: "youcom", DisplayName: "You.com", Category: "apikey", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/youcom.js"},

	// --- oauth (16) ---
	{ProviderID: "antigravity", DisplayName: "Antigravity", Category: "oauth", AuthType: "oauth", ExecutorPath: "specialized/gemini", SupportedFormats: []string{"FormatGemini"}, SourceCitation: "registry/antigravity.js, executors/antigravity.js"},
	{ProviderID: "claude", DisplayName: "Claude", Category: "oauth", AuthType: "oauth", ExecutorPath: "generic", SupportedFormats: []string{"FormatAnthropic"}, SourceCitation: "registry/claude.js"},
	{ProviderID: "cline", DisplayName: "Cline", Category: "oauth", AuthType: "oauth", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/cline.js"},
	{ProviderID: "clinepass", DisplayName: "Cline Pass", Category: "oauth", AuthType: "oauth", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/clinepass.js"},
	{ProviderID: "codebuddy-cn", DisplayName: "CodeBuddy CN", Category: "oauth", AuthType: "oauth", ExecutorPath: "specialized/gemini", SupportedFormats: []string{"FormatGemini"}, SourceCitation: "registry/codebuddy-cn.js, executors/codebuddy-cn.js"},
	{ProviderID: "codex", DisplayName: "OpenAI Codex", Category: "oauth", AuthType: "oauth", ExecutorPath: "specialized/codex", SupportedFormats: []string{"FormatCodexResponses"}, SourceCitation: "registry/codex.js"},
	{ProviderID: "cursor", DisplayName: "Cursor", Category: "oauth", AuthType: "pat", ExecutorPath: "specialized/cursor", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/cursor.js, executors/cursor.js"},
	{ProviderID: "github", DisplayName: "GitHub", Category: "oauth", AuthType: "oauth", ExecutorPath: "specialized/github_models", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/github.js, executors/github.js"},
	{ProviderID: "gitlab", DisplayName: "GitLab", Category: "oauth", AuthType: "oauth", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/gitlab.js"},
	{ProviderID: "grok-cli", DisplayName: "Grok CLI", Category: "oauth", AuthType: "oauth", ExecutorPath: "specialized/gemini", SupportedFormats: []string{"FormatGemini"}, SourceCitation: "registry/grok-cli.js, executors/grok-cli.js"},
	{ProviderID: "iflow", DisplayName: "IFlow", Category: "oauth", AuthType: "oauth", ExecutorPath: "specialized/gemini", SupportedFormats: []string{"FormatGemini"}, SourceCitation: "registry/iflow.js, executors/iflow.js"},
	{ProviderID: "kilocode", DisplayName: "Kilocode", Category: "oauth", AuthType: "oauth", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/kilocode.js"},
	{ProviderID: "kimchi", DisplayName: "Kimchi", Category: "oauth", AuthType: "cookie", ExecutorPath: "specialized/gemini", SupportedFormats: []string{"FormatGemini"}, SourceCitation: "registry/kimchi.js, executors/kimchi.js"},
	{ProviderID: "kimi", DisplayName: "Kimi", Category: "oauth", AuthType: "oauth", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/kimi.js"},
	{ProviderID: "qwen", DisplayName: "Qwen", Category: "oauth", AuthType: "oauth", ExecutorPath: "specialized/gemini", SupportedFormats: []string{"FormatGemini"}, SourceCitation: "registry/qwen.js, executors/qwen.js"},
	{ProviderID: "xai", DisplayName: "xAI", Category: "oauth", AuthType: "oauth", ExecutorPath: "specialized/xai", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/xai.js"},

	// --- freeTier (13) ---
	{ProviderID: "byteplus", DisplayName: "BytePlus", Category: "freeTier", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/byteplus.js"},
	{ProviderID: "cloudflare-ai", DisplayName: "Cloudflare AI", Category: "freeTier", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/cloudflare-ai.js"},
	{ProviderID: "coqui", DisplayName: "Coqui", Category: "freeTier", AuthType: "none", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/coqui.js (authType=none, noAuth=true)"},
	{ProviderID: "edge-tts", DisplayName: "Edge TTS", Category: "freeTier", AuthType: "none", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/edge-tts.js (authType=none, noAuth=true)"},
	{ProviderID: "gemini", DisplayName: "Gemini", Category: "freeTier", AuthType: "apikey", ExecutorPath: "specialized/gemini", SupportedFormats: []string{"FormatGemini"}, SourceCitation: "registry/gemini.js"},
	{ProviderID: "google-tts", DisplayName: "Google TTS", Category: "freeTier", AuthType: "none", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/google-tts.js (authType=none, noAuth=true)"},
	{ProviderID: "local-device", DisplayName: "Local Device", Category: "freeTier", AuthType: "none", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/local-device.js"},
	{ProviderID: "nvidia", DisplayName: "NVIDIA", Category: "freeTier", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/nvidia.js"},
	{ProviderID: "ollama", DisplayName: "Ollama", Category: "freeTier", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/ollama.js"},
	{ProviderID: "openrouter", DisplayName: "OpenRouter", Category: "freeTier", AuthType: "apikey", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAIChat", "FormatOpenAICompat"}, SourceCitation: "registry/openrouter.js"},
	{ProviderID: "searxng", DisplayName: "SearXNG", Category: "freeTier", AuthType: "none", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/searxng.js (authType=none, noAuth=true)"},
	{ProviderID: "tortoise", DisplayName: "Tortoise TTS", Category: "freeTier", AuthType: "none", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/tortoise.js (authType=none, noAuth=true)"},
	{ProviderID: "vertex", DisplayName: "Vertex AI", Category: "freeTier", AuthType: "apikey", ExecutorPath: "specialized/gcp_vertex", SupportedFormats: []string{"FormatGemini"}, SourceCitation: "registry/vertex.js, executors/vertex.js"},

	// --- free (5) ---
	{ProviderID: "gemini-cli", DisplayName: "Gemini CLI", Category: "free", AuthType: "none", ExecutorPath: "specialized/gemini", SupportedFormats: []string{"FormatGemini"}, SourceCitation: "registry/gemini-cli.js, executors/gemini-cli.js"},
	{ProviderID: "kiro", DisplayName: "Kiro", Category: "free", AuthType: "none", ExecutorPath: "specialized/kiro", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/kiro.js, executors/kiro.js"},
	{ProviderID: "mimo-free", DisplayName: "Mimo Free", Category: "free", AuthType: "none", ExecutorPath: "specialized/gemini", SupportedFormats: []string{"FormatGemini"}, SourceCitation: "registry/mimo-free.js, executors/mimo-free.js"},
	{ProviderID: "opencode", DisplayName: "OpenCode", Category: "free", AuthType: "none", ExecutorPath: "generic", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/opencode.js"},
	{ProviderID: "qoder", DisplayName: "Qoder", Category: "free", AuthType: "none", ExecutorPath: "specialized/gemini", SupportedFormats: []string{"FormatGemini"}, SourceCitation: "registry/qoder.js, executors/qoder.js"},

	// --- webCookie (2) ---
	{ProviderID: "grok-web", DisplayName: "Grok Web", Category: "webCookie", AuthType: "cookie", ExecutorPath: "specialized/gemini", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/grok-web.js, executors/grok-web.js"},
	{ProviderID: "perplexity-web", DisplayName: "Perplexity Web", Category: "webCookie", AuthType: "cookie", ExecutorPath: "specialized/gemini", SupportedFormats: []string{"FormatOpenAICompat"}, SourceCitation: "registry/perplexity-web.js, executors/perplexity-web.js"},
}

// formatRegistry is the 5-entry format matrix.
// Sources: internal/domain/engine/types.go, audit/05-streaming-translation.md
var formatRegistry = []formatEntry{
	{FormatConstant: "FormatOpenAIChat", CodecPackage: "engine/formats/openai/", Detection: "Endpoint /v1/chat/completions or body has messages + model", TerminalEvent: "data: [DONE]\\n\\n", SourceCitation: "internal/domain/engine/types.go:14, audit/05-streaming-translation.md:4.5"},
	{FormatConstant: "FormatOpenAICompat", CodecPackage: "engine/formats/openai/", Detection: "Audited OpenAI-compatible endpoint/body rules from provider transport config", TerminalEvent: "data: [DONE]\\n\\n (or provider-specific variant)", SourceCitation: "internal/domain/engine/types.go:15, audit/02-provider-matrix.md:97"},
	{FormatConstant: "FormatCodexResponses", CodecPackage: "engine/formats/responses/", Detection: "Endpoint /v1/responses plus audited Codex/Responses discrimination", TerminalEvent: "Response-typed completion/failure terminal from pinned source (event: done / response.failed)", SourceCitation: "internal/domain/engine/types.go:16, registry/codex.js transport.format=openai-responses, audit/05-streaming-translation.md:4.5"},
	{FormatConstant: "FormatAnthropic", CodecPackage: "engine/formats/claude/", Detection: "Endpoint /v1/messages or anthropic-version header", TerminalEvent: "event: message_stop\\ndata: {}\\n\\n", SourceCitation: "internal/domain/engine/types.go:17, audit/05-streaming-translation.md:4.5"},
	{FormatConstant: "FormatGemini", CodecPackage: "engine/formats/gemini/", Detection: "Endpoint /v1beta/models/* or x-goog-api-key header", TerminalEvent: "finishReason in candidates; no [DONE] sentinel", SourceCitation: "internal/domain/engine/types.go:18, audit/05-streaming-translation.md:4.5"},
}

// oauthRegistry is the 20-entry OAuth flow matrix.
// Source: audit/03-oauth-token-lifecycle.md:70-92, src/lib/oauth/providers.js
var oauthRegistry = []oauthEntry{
	{FlowID: "claude", Mechanism: "Authorization code with PKCE (standard callback)", PortBehavior: "find_free", PKCE: "S256", Providers: []string{"claude"}, RestartPolicy: "cancel_all_pending", SourceCitation: "audit/03-oauth-token-lifecycle.md:72, src/lib/oauth/providers.js PROVIDERS"},
	{FlowID: "codex", Mechanism: "Authorization code with PKCE plus fixed-port proxy (port 1455)", PortBehavior: "fixed (1455)", PKCE: "S256", Providers: []string{"codex"}, RestartPolicy: "cancel_all_pending", SourceCitation: "audit/03-oauth-token-lifecycle.md:73, registry/codex.js oauth.fixedPort:1455, src/lib/oauth/utils/server.js"},
	{FlowID: "openai", Mechanism: "Authorization code with PKCE (standard callback)", PortBehavior: "find_free", PKCE: "S256", Providers: []string{"openai"}, RestartPolicy: "cancel_all_pending", SourceCitation: "audit/03-oauth-token-lifecycle.md:74, src/lib/oauth/providers.js PROVIDERS"},
	{FlowID: "gemini-cli", Mechanism: "Authorization code (no PKCE, standard callback)", PortBehavior: "find_free", PKCE: "none", Providers: []string{"gemini-cli"}, RestartPolicy: "cancel_all_pending", SourceCitation: "audit/03-oauth-token-lifecycle.md:75, src/lib/oauth/providers.js PROVIDERS"},
	{FlowID: "antigravity", Mechanism: "Authorization code (no PKCE, standard callback)", PortBehavior: "find_free", PKCE: "none", Providers: []string{"antigravity"}, RestartPolicy: "cancel_all_pending", SourceCitation: "audit/03-oauth-token-lifecycle.md:76, src/lib/oauth/providers.js PROVIDERS"},
	{FlowID: "iflow", Mechanism: "Authorization code (no PKCE, standard callback)", PortBehavior: "find_free", PKCE: "none", Providers: []string{"iflow"}, RestartPolicy: "cancel_all_pending", SourceCitation: "audit/03-oauth-token-lifecycle.md:77, src/lib/oauth/providers.js PROVIDERS"},
	{FlowID: "qwen", Mechanism: "Device code flow with PKCE", PortBehavior: "none", PKCE: "S256", Providers: []string{"qwen"}, RestartPolicy: "cancel_all_pending", SourceCitation: "audit/03-oauth-token-lifecycle.md:78, src/lib/oauth/providers.js PROVIDERS"},
	{FlowID: "qoder", Mechanism: "Device code flow (custom) with PKCE", PortBehavior: "none", PKCE: "S256", Providers: []string{"qoder"}, RestartPolicy: "cancel_all_pending", SourceCitation: "audit/03-oauth-token-lifecycle.md:79, src/lib/oauth/providers.js PROVIDERS"},
	{FlowID: "github", Mechanism: "Device code flow (no PKCE)", PortBehavior: "none", PKCE: "none", Providers: []string{"github"}, RestartPolicy: "cancel_all_pending", SourceCitation: "audit/03-oauth-token-lifecycle.md:80, src/lib/oauth/providers.js PROVIDERS"},
	{FlowID: "kiro", Mechanism: "Device code flow (AWS SSO OIDC, no PKCE)", PortBehavior: "none", PKCE: "none", Providers: []string{"kiro"}, RestartPolicy: "cancel_all_pending", SourceCitation: "audit/03-oauth-token-lifecycle.md:81, src/lib/oauth/providers.js PROVIDERS"},
	{FlowID: "kimi", Mechanism: "Device code flow (no PKCE)", PortBehavior: "none", PKCE: "none", Providers: []string{"kimi"}, RestartPolicy: "cancel_all_pending", SourceCitation: "audit/03-oauth-token-lifecycle.md:82, src/lib/oauth/providers.js PROVIDERS"},
	{FlowID: "kilocode", Mechanism: "Device code flow (custom, no PKCE)", PortBehavior: "none", PKCE: "none", Providers: []string{"kilocode"}, RestartPolicy: "cancel_all_pending", SourceCitation: "audit/03-oauth-token-lifecycle.md:83, src/lib/oauth/providers.js PROVIDERS"},
	{FlowID: "codebuddy-cn", Mechanism: "Device code flow (custom, no PKCE)", PortBehavior: "none", PKCE: "none", Providers: []string{"codebuddy-cn"}, RestartPolicy: "cancel_all_pending", SourceCitation: "audit/03-oauth-token-lifecycle.md:84, src/lib/oauth/providers.js PROVIDERS"},
	{FlowID: "grok-cli", Mechanism: "Device code flow (no PKCE)", PortBehavior: "none", PKCE: "none", Providers: []string{"grok-cli"}, RestartPolicy: "cancel_all_pending", SourceCitation: "audit/03-oauth-token-lifecycle.md:85, src/lib/oauth/providers.js PROVIDERS"},
	{FlowID: "gitlab", Mechanism: "Authorization code with PKCE (standard callback)", PortBehavior: "find_free", PKCE: "S256", Providers: []string{"gitlab"}, RestartPolicy: "cancel_all_pending", SourceCitation: "audit/03-oauth-token-lifecycle.md:86, src/lib/oauth/providers.js PROVIDERS"},
	{FlowID: "cline", Mechanism: "Authorization code (no PKCE)", PortBehavior: "find_free", PKCE: "none", Providers: []string{"cline"}, RestartPolicy: "cancel_all_pending", SourceCitation: "audit/03-oauth-token-lifecycle.md:87, src/lib/oauth/providers.js PROVIDERS"},
	{FlowID: "clinepass", Mechanism: "Authorization code (no PKCE, pass-through)", PortBehavior: "find_free", PKCE: "none", Providers: []string{"clinepass"}, RestartPolicy: "cancel_all_pending", SourceCitation: "audit/03-oauth-token-lifecycle.md:88, src/lib/oauth/providers.js PROVIDERS"},
	{FlowID: "kimchi", Mechanism: "Browser token import (cookie-based)", PortBehavior: "none", PKCE: "none", Providers: []string{"kimchi"}, RestartPolicy: "cancel_all_pending", SourceCitation: "audit/03-oauth-token-lifecycle.md:89, src/lib/oauth/providers.js PROVIDERS"},
	{FlowID: "cursor", Mechanism: "IDE token import (local credential discovery)", PortBehavior: "none", PKCE: "none", Providers: []string{"cursor"}, RestartPolicy: "cancel_all_pending", SourceCitation: "audit/03-oauth-token-lifecycle.md:90, src/lib/oauth/providers.js PROVIDERS"},
	{FlowID: "xai", Mechanism: "Authorization code with PKCE plus fixed-port proxy (port 56121)", PortBehavior: "fixed (56121)", PKCE: "S256", Providers: []string{"xai"}, RestartPolicy: "cancel_all_pending", SourceCitation: "audit/03-oauth-token-lifecycle.md:91, registry/xai.js oauth.fixedPort:56121, src/lib/oauth/utils/server.js"},
}

// ---------------------------------------------------------------------------
// Main entry point
// ---------------------------------------------------------------------------

func main() {
	writeMode := false
	stdoutMode := false
	stdoutFormat := false
	stdoutOAuth := false
	for _, a := range os.Args[1:] {
		if a == "--write" {
			writeMode = true
		}
		if a == "--stdout" {
			stdoutMode = true
		}
		if a == "--stdout-format" {
			stdoutFormat = true
		}
		if a == "--stdout-oauth" {
			stdoutOAuth = true
		}
	}

	repoRoot := findRepoRoot()
	docDir := filepath.Join(repoRoot, "docs", "implementation")

	if stdoutMode {
		gen := buildProviderManifestData()
		enc := yaml.NewEncoder(os.Stdout)
		enc.SetIndent(2)
		enc.Encode(gen)
		enc.Close()
		return
	}
	if stdoutFormat {
		gen := buildFormatManifestData()
		enc := yaml.NewEncoder(os.Stdout)
		enc.SetIndent(2)
		enc.Encode(gen)
		enc.Close()
		return
	}
	if stdoutOAuth {
		gen := buildOAuthManifestData()
		enc := yaml.NewEncoder(os.Stdout)
		enc.SetIndent(2)
		enc.Encode(gen)
		enc.Close()
		return
	}

	if writeMode {
		writeProviderManifest(docDir)
		writeFormatManifest(docDir)
		writeOAuthManifest(docDir)
		fmt.Println("Manifests written successfully.")
	} else {
		validateManifests(docDir)
	}
}

// buildProviderManifestData returns the provider manifest data without writing.
func buildProviderManifestData() map[string]interface{} {
	entries := deduplicateProviders(providerRegistry)
	for i := range entries {
		entries[i].ProviderType = providerTypeForID(entries[i].ProviderID)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].ProviderID < entries[j].ProviderID
	})
	return map[string]interface{}{
		"metadata": map[string]interface{}{
			"title":       "Provider Matrix",
			"description": "Exhaustive provider registry derived from decolua/9router commit 79918c7830695bbca4a45c9fea4a42c3e9fd73d1",
			"source":      "audit/02-provider-matrix.md, upstream open-sse/providers/registry/index.js",
			"total":       len(entries),
		},
		"providers": entries,
	}
}

func buildFormatManifestData() map[string]interface{} {
	entries := formatRegistry
	return map[string]interface{}{
		"metadata": map[string]interface{}{
			"title":       "Format Matrix",
			"description": "Wire protocol format constants derived from decolua/9router commit 79918c7830695bbca4a45c9fea4a42c3e9fd73d1",
			"source":      "internal/domain/engine/types.go, audit/05-streaming-translation.md",
			"total":       len(entries),
		},
		"formats": entries,
	}
}

func buildOAuthManifestData() map[string]interface{} {
	entries := oauthRegistry
	return map[string]interface{}{
		"metadata": map[string]interface{}{
			"title":       "OAuth Flow Matrix",
			"description": "OAuth flow definitions derived from decolua/9router commit 79918c7830695bbca4a45c9fea4a42c3e9fd73d1",
			"source":      "audit/03-oauth-token-lifecycle.md, src/lib/oauth/providers.js",
			"total":       len(entries),
		},
		"oauth_flows": entries,
	}
}

func findRepoRoot() string {
	// Walk up from tools/upstreammap to find go.mod
	wd, _ := os.Getwd()
	dir := wd
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	// Fallback: assume CWD is somewhere under repo root
	return wd
}

func writeProviderManifest(dir string) {
	entries := deduplicateProviders(providerRegistry)
	// Fill provider_type from provider_id (with known overrides)
	for i := range entries {
		entries[i].ProviderType = providerTypeForID(entries[i].ProviderID)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].ProviderID < entries[j].ProviderID
	})
	writeYAML(filepath.Join(dir, "provider-matrix.yaml"), map[string]interface{}{
		"metadata": map[string]interface{}{
			"title":       "Provider Matrix",
			"description": "Exhaustive provider registry derived from decolua/9router commit 79918c7830695bbca4a45c9fea4a42c3e9fd73d1",
			"source":      "audit/02-provider-matrix.md, upstream open-sse/providers/registry/index.js",
			"total":       len(entries),
		},
		"providers": entries,
	})
	fmt.Printf("Wrote %d provider entries.\n", len(entries))
}

func writeFormatManifest(dir string) {
	writeYAML(filepath.Join(dir, "format-matrix.yaml"), map[string]interface{}{
		"metadata": map[string]interface{}{
			"title":       "Format Matrix",
			"description": "Wire protocol format constants derived from decolua/9router commit 79918c7830695bbca4a45c9fea4a42c3e9fd73d1",
			"source":      "internal/domain/engine/types.go, audit/05-streaming-translation.md",
			"total":       len(formatRegistry),
		},
		"formats": formatRegistry,
	})
	fmt.Printf("Wrote %d format entries.\n", len(formatRegistry))
}

func writeOAuthManifest(dir string) {
	writeYAML(filepath.Join(dir, "oauth-matrix.yaml"), map[string]interface{}{
		"metadata": map[string]interface{}{
			"title":       "OAuth Flow Matrix",
			"description": "OAuth flow definitions derived from decolua/9router commit 79918c7830695bbca4a45c9fea4a42c3e9fd73d1",
			"source":      "audit/03-oauth-token-lifecycle.md, src/lib/oauth/providers.js",
			"total":       len(oauthRegistry),
		},
		"oauth_flows": oauthRegistry,
	})
	fmt.Printf("Wrote %d OAuth flow entries.\n", len(oauthRegistry))
}

func writeYAML(path string, data interface{}) {
	f, err := os.Create(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR creating %s: %v\n", path, err)
		os.Exit(1)
	}
	defer f.Close()

	enc := yaml.NewEncoder(f)
	enc.SetIndent(2)
	if err := enc.Encode(data); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR encoding YAML to %s: %v\n", path, err)
		os.Exit(1)
	}
	enc.Close()
}

func validateManifests(dir string) {
	exitCode := 0

	// Validate provider manifest
	exitCode += validateYAMLFile(filepath.Join(dir, "provider-matrix.yaml"), "provider")
	// Validate format manifest
	exitCode += validateYAMLFile(filepath.Join(dir, "format-matrix.yaml"), "format")
	// Validate OAuth manifest
	exitCode += validateYAMLFile(filepath.Join(dir, "oauth-matrix.yaml"), "oauth")

	if exitCode > 0 {
		fmt.Printf("\nValidation FAILED with %d errors.\n", exitCode)
		os.Exit(1)
	}
	fmt.Println("All manifests validated successfully.")
}

func validateYAMLFile(path string, kind string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR reading %s: %v\n", path, err)
		return 1
	}

	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR unmarshalling %s: %v\n", path, err)
		return 1
	}

	fmt.Printf("OK: %s is valid YAML (%d bytes)\n", path, len(data))
	return 0
}

func providerTypeForID(id string) string {
	switch id {
	case "claude":
		return "anthropic"
	case "black-forest-labs":
		return "black_forest_labs"
	case "brave-search":
		return "brave_search"
	case "cloudflare-ai":
		return "cloudflare_ai"
	case "codebuddy-cn":
		return "codebuddy_cn"
	case "edge-tts":
		return "edge_tts"
	case "fal-ai":
		return "fal_ai"
	case "gemini-cli":
		return "gemini_cli"
	case "github":
		return "github_models"
	case "glm-cn":
		return "glm_cn"
	case "google-pse":
		return "google_pse"
	case "google-tts":
		return "google_tts"
	case "grok-cli":
		return "grok_cli"
	case "grok-web":
		return "grok_web"
	case "jina-ai":
		return "jina_ai"
	case "jina-reader":
		return "jina_reader"
	case "local-device":
		return "local_device"
	case "mimo-free":
		return "mimo_free"
	case "minimax-cn":
		return "minimax_cn"
	case "ollama-local":
		return "ollama_local"
	case "opencode-go":
		return "opencode_go"
	case "perplexity-agent":
		return "perplexity_agent"
	case "perplexity-web":
		return "perplexity_web"
	case "stability-ai":
		return "stability_ai"
	case "vercel-ai-gateway":
		return "vercel_ai_gateway"
	case "vertex-partner":
		return "vertex_partner"
	case "voyage-ai":
		return "voyage_ai"
	case "volcengine-ark":
		return "volcengine_ark"
	default:
		return id
	}
}

func deduplicateProviders(entries []providerEntry) []providerEntry {
	seen := map[string]int{}
	result := make([]providerEntry, 0, len(entries))
	for _, e := range entries {
		if _, ok := seen[e.ProviderID]; ok {
			fmt.Fprintf(os.Stderr, "WARN: duplicate provider_id %q skipped (category %s)\n", e.ProviderID, e.Category)
			continue
		}
		seen[e.ProviderID] = 1
		result = append(result, e)
	}
	return result
}
