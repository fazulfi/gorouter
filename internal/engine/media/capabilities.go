package media

import (
	"gorouter/internal/domain/provider"
)

// Capability records whether a provider is proven to serve a modality. The
// provider sets are extracted verbatim from the pinned upstream provider
// registry serviceKinds fields (decolua/9router @
// 79918c7830695bbca4a45c9fea4a42c3e9fd73d1, open-sse/providers/registry/*.js).
// A provider missing from the table is UNPROVEN and fails closed.
type Capability struct {
	Modality  Modality
	Provider  provider.ProviderType
	Proven    bool
	Authority string
	Note      string
}

// capabilityAuthority is the shared citation prefix for all registry-derived
// capability entries.
const capabilityAuthority = "upstream open-sse/providers/registry/ (decolua/9router 79918c7830695bbca4a45c9fea4a42c3e9fd73d1)"

func capability(m Modality, pt provider.ProviderType, fileLine string, note string) Capability {
	return Capability{
		Modality:  m,
		Provider:  pt,
		Proven:    true,
		Authority: capabilityAuthority + " " + fileLine,
		Note:      note,
	}
}

// table is the exhaustive proven mapping: modality → providers. Each entry's
// Authority carries the exact upstream serviceKinds line.
var table = map[Modality][]Capability{
	ModalityEmbeddings: {
		capability(ModalityEmbeddings, "openai", "openai.js:64", "serviceKinds includes embedding"),
		capability(ModalityEmbeddings, "openrouter", "openrouter.js:42", "serviceKinds includes embedding"),
		capability(ModalityEmbeddings, "mistral", "mistral.js:29", "serviceKinds includes embedding"),
		capability(ModalityEmbeddings, "together", "together.js:29", "serviceKinds includes embedding"),
		capability(ModalityEmbeddings, "voyage-ai", "voyage-ai.js:28", "serviceKinds [embedding]"),
		capability(ModalityEmbeddings, "jina-ai", "jina-ai.js:17", "serviceKinds [embedding]"),
	},
	ModalityImageGeneration: {
		capability(ModalityImageGeneration, "openai", "openai.js:64", "serviceKinds includes image"),
		capability(ModalityImageGeneration, "fal-ai", "fal-ai.js:32", "serviceKinds [image]"),
		capability(ModalityImageGeneration, "black-forest-labs", "black-forest-labs.js:30", "serviceKinds [image]"),
		capability(ModalityImageGeneration, "stability-ai", "stability-ai.js:29", "serviceKinds [image]"),
		capability(ModalityImageGeneration, "recraft", "recraft.js:22", "serviceKinds [image]"),
		capability(ModalityImageGeneration, "runwayml", "runwayml.js:28", "serviceKinds [image]"),
		capability(ModalityImageGeneration, "sdwebui", "sdwebui.js:18", "serviceKinds [image]"),
		capability(ModalityImageGeneration, "comfyui", "comfyui.js:18", "serviceKinds [image]"),
		capability(ModalityImageGeneration, "topaz", "topaz.js:16", "serviceKinds [image]"),
		capability(ModalityImageGeneration, "minimax", "minimax.js:71", "serviceKinds includes image"),
		capability(ModalityImageGeneration, "xai", "xai.js:37", "serviceKinds includes image"),
	},
	ModalityImageToText: {
		capability(ModalityImageToText, "openai", "openai.js:64", "serviceKinds includes imageToText"),
		capability(ModalityImageToText, "openrouter", "openrouter.js:42", "serviceKinds includes imageToText"),
		capability(ModalityImageToText, "xai", "xai.js:37", "serviceKinds includes imageToText"),
		capability(ModalityImageToText, "minimax", "minimax.js:71", "serviceKinds includes imageToText"),
		capability(ModalityImageToText, "mistral", "mistral.js:29", "serviceKinds includes imageToText"),
		capability(ModalityImageToText, "groq", "groq.js:30", "serviceKinds includes imageToText"),
	},
	ModalityTTS: {
		capability(ModalityTTS, "openai", "openai.js:64", "serviceKinds includes tts"),
		capability(ModalityTTS, "openrouter", "openrouter.js:42", "serviceKinds includes tts"),
		capability(ModalityTTS, "minimax", "minimax.js:71", "serviceKinds includes tts"),
		capability(ModalityTTS, "minimax-cn", "minimax-cn.js:70", "serviceKinds includes tts"),
		capability(ModalityTTS, "edge-tts", "edge-tts.js:12", "serviceKinds [tts], mediaPriority 5 (:15)"),
		capability(ModalityTTS, "google-tts", "google-tts.js:12", "serviceKinds [tts], mediaPriority 5 (:15)"),
		capability(ModalityTTS, "elevenlabs", "elevenlabs.js:16", "serviceKinds [tts]"),
		capability(ModalityTTS, "aws-polly", "aws-polly.js:17", "serviceKinds [tts]"),
		capability(ModalityTTS, "cartesia", "cartesia.js:16", "serviceKinds [tts]"),
		capability(ModalityTTS, "playht", "playht.js:16", "serviceKinds [tts]"),
		capability(ModalityTTS, "inworld", "inworld.js:17", "serviceKinds [tts]"),
		capability(ModalityTTS, "tortoise", "tortoise.js:13", "serviceKinds [tts], localhost:5000"),
		capability(ModalityTTS, "coqui", "coqui.js:13", "serviceKinds [tts], localhost:5002"),
	},
	ModalitySTT: {
		capability(ModalitySTT, "openai", "openai.js:64", "serviceKinds includes stt"),
		capability(ModalitySTT, "assemblyai", "assemblyai.js:31", "serviceKinds [stt]"),
		capability(ModalitySTT, "deepgram", "deepgram.js:31", "serviceKinds [stt]"),
		capability(ModalitySTT, "groq", "groq.js:30", "serviceKinds includes stt"),
	},
	ModalityVoices: {
		capability(ModalityVoices, "elevenlabs", "audit/01-http-contracts.md:499", "GET /api/media-providers/tts/elevenlabs/voices"),
		capability(ModalityVoices, "deepgram", "audit/01-http-contracts.md:500", "GET /api/media-providers/tts/deepgram/voices"),
		capability(ModalityVoices, "inworld", "audit/01-http-contracts.md:501", "GET /api/media-providers/tts/inworld/voices"),
		capability(ModalityVoices, "minimax", "audit/01-http-contracts.md:502", "GET /api/media-providers/tts/minimax/voices"),
		capability(ModalityVoices, "edge-tts", "upstream src/app/api/v1/audio/voices/route.js:4-10", "voices route supports edge-tts via provider query"),
		capability(ModalityVoices, "local-device", "upstream src/app/api/v1/audio/voices/route.js:4-10", "voices route supports local-device via provider query"),
	},
	ModalityWebSearch: {
		capability(ModalityWebSearch, "openai", "openai.js:64", "serviceKinds includes webSearch"),
		capability(ModalityWebSearch, "xai", "xai.js:37", "serviceKinds includes webSearch"),
		capability(ModalityWebSearch, "minimax", "minimax.js:71", "serviceKinds includes webSearch"),
		capability(ModalityWebSearch, "brave-search", "brave-search.js:16", "serviceKinds [webSearch]"),
		capability(ModalityWebSearch, "google-pse", "google-pse.js:16", "serviceKinds [webSearch]"),
		capability(ModalityWebSearch, "linkup", "linkup.js:16", "serviceKinds [webSearch]"),
		capability(ModalityWebSearch, "searchapi", "searchapi.js:16", "serviceKinds [webSearch]"),
		capability(ModalityWebSearch, "searxng", "searxng.js:15", "serviceKinds [webSearch]"),
		capability(ModalityWebSearch, "serper", "serper.js:16", "serviceKinds [webSearch]"),
		capability(ModalityWebSearch, "youcom", "youcom.js:16", "serviceKinds [webSearch]"),
		capability(ModalityWebSearch, "exa", "exa.js:16", "serviceKinds [webSearch]"),
		capability(ModalityWebSearch, "tavily", "tavily.js:16", "serviceKinds [webSearch]"),
		capability(ModalityWebSearch, "perplexity", "perplexity.js:29", "serviceKinds includes webSearch"),
		capability(ModalityWebSearch, "perplexity-agent", "perplexity-agent.js:41", "serviceKinds includes webSearch"),
	},
	ModalityWebFetch: {
		capability(ModalityWebFetch, "exa", "exa.js:16", "serviceKinds [webSearch,webFetch]"),
		capability(ModalityWebFetch, "tavily", "tavily.js:16", "serviceKinds [webSearch,webFetch]"),
		capability(ModalityWebFetch, "firecrawl", "firecrawl.js:16", "serviceKinds [webFetch]"),
		capability(ModalityWebFetch, "jina-reader", "jina-reader.js:16", "serviceKinds [webFetch]"),
	},
	ModalityVideoGeneration: {
		capability(ModalityVideoGeneration, "xai", "xai.js:37 + xai.js:35", "serviceKinds includes video; grok-imagine-video model"),
	},
	ModalityVideoEdit: {
		capability(ModalityVideoEdit, "xai", "videoCore.js:14", "VIDEO_ACTIONS includes edits"),
	},
	ModalityVideoExtension: {
		capability(ModalityVideoExtension, "xai", "videoCore.js:14", "VIDEO_ACTIONS includes extensions"),
	},
	ModalityVideoStatus: {
		capability(ModalityVideoStatus, "xai", "videoCore.js:33-36", "GET /videos/{requestId} status proxy"),
	},
}

// Supports performs the fail-closed capability check: true only when the
// provider is proven for the modality by an upstream serviceKinds citation.
func Supports(m Modality, pt provider.ProviderType) (bool, Capability) {
	if !Valid(m) {
		return false, Capability{Modality: m, Provider: pt, Proven: false,
			Authority: "media.Valid rejects unknown modalities (fail closed)"}
	}
	for _, c := range table[m] {
		if c.Provider == pt {
			return true, c
		}
	}
	return false, Capability{Modality: m, Provider: pt, Proven: false,
		Authority: capabilityAuthority + " — no serviceKinds entry for provider"}
}

// SupportedProviders returns the proven provider set for a modality in
// canonical table order (empty for unproven modalities).
func SupportedProviders(m Modality) []provider.ProviderType {
	entries := table[m]
	out := make([]provider.ProviderType, 0, len(entries))
	for _, c := range entries {
		out = append(out, c.Provider)
	}
	return out
}

// AllCapabilities returns every proven capability entry across all
// modalities (exhaustive; used by the coverage test).
func AllCapabilities() []Capability {
	var out []Capability
	for _, m := range allModalities {
		out = append(out, table[m]...)
	}
	return out
}
