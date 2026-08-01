package media

import (
	"strings"
	"testing"

	"gorouter/internal/domain/provider"
)

// TestCapabilityCoverage pins the proven provider sets to the upstream
// serviceKinds extraction (decolua/9router @ 79918c7830695bbca4a45c9fea4a42c3e9fd73d1,
// open-sse/providers/registry/*.js). Any change requires a citation change.
func TestCapabilityCoverage(t *testing.T) {
	want := map[Modality][]provider.ProviderType{
		ModalityEmbeddings:      {"openai", "openrouter", "mistral", "together", "voyage-ai", "jina-ai"},
		ModalityImageGeneration: {"openai", "fal-ai", "black-forest-labs", "stability-ai", "recraft", "runwayml", "sdwebui", "comfyui", "topaz", "minimax", "xai"},
		ModalityImageToText:     {"openai", "openrouter", "xai", "minimax", "mistral", "groq"},
		ModalityTTS:             {"openai", "openrouter", "minimax", "minimax-cn", "edge-tts", "google-tts", "elevenlabs", "aws-polly", "cartesia", "playht", "inworld", "tortoise", "coqui"},
		ModalitySTT:             {"openai", "assemblyai", "deepgram", "groq"},
		ModalityVoices:          {"elevenlabs", "deepgram", "inworld", "minimax", "edge-tts", "local-device"},
		ModalityWebSearch:       {"openai", "xai", "minimax", "brave-search", "google-pse", "linkup", "searchapi", "searxng", "serper", "youcom", "exa", "tavily", "perplexity", "perplexity-agent"},
		ModalityWebFetch:        {"exa", "tavily", "firecrawl", "jina-reader"},
		ModalityVideoGeneration: {"xai"},
		ModalityVideoEdit:       {"xai"},
		ModalityVideoExtension:  {"xai"},
		ModalityVideoStatus:     {"xai"},
	}
	for m, wantProviders := range want {
		got := SupportedProviders(m)
		if len(got) != len(wantProviders) {
			t.Errorf("SupportedProviders(%s) len = %d, want %d", m, len(got), len(wantProviders))
			continue
		}
		for i := range wantProviders {
			if got[i] != wantProviders[i] {
				t.Errorf("SupportedProviders(%s)[%d] = %q, want %q", m, i, got[i], wantProviders[i])
			}
		}
	}
	for _, m := range allModalities {
		if _, ok := want[m]; !ok {
			t.Errorf("modality %q has no pinned capability coverage", m)
		}
	}
}

// TestSupportsFailClosed proves every provider NOT in the proven set is
// rejected deterministically, and every proven provider is accepted.
func TestSupportsFailClosed(t *testing.T) {
	for _, m := range allModalities {
		proven := map[provider.ProviderType]bool{}
		for _, pt := range SupportedProviders(m) {
			proven[pt] = true
		}
		if ok, cap := Supports(m, provider.ProviderType("unproven-provider")); ok {
			t.Errorf("Supports(%s, unproven) = true, want false (fail closed)", m)
		} else if cap.Proven {
			t.Errorf("Supports(%s, unproven) cap.Proven = true", m)
		} else if strings.TrimSpace(cap.Authority) == "" {
			t.Errorf("Supports(%s, unproven) missing fail-closed authority", m)
		}
		for pt := range proven {
			if ok, cap := Supports(m, pt); !ok || !cap.Proven {
				t.Errorf("Supports(%s, %s) = %v/%v, want true/proven", m, pt, ok, cap.Proven)
			} else if strings.TrimSpace(cap.Authority) == "" {
				t.Errorf("Supports(%s, %s) missing authority", m, pt)
			}
		}
	}
}

// TestSupportsUnknownModalityFailClosed proves unknown modalities are never
// supported.
func TestSupportsUnknownModalityFailClosed(t *testing.T) {
	if ok, _ := Supports(Modality("chat"), provider.ProviderType("openai")); ok {
		t.Errorf("Supports(chat, openai) = true, want false")
	}
	if ok, _ := Supports("", "openai"); ok {
		t.Errorf("Supports(\"\", openai) = true, want false")
	}
}

// TestAllCapabilitiesEveryEntryHasAuthority proves no capability entry lacks
// a citation (audit checklist: no invented capabilities).
func TestAllCapabilitiesEveryEntryHasAuthority(t *testing.T) {
	for _, c := range AllCapabilities() {
		if !c.Proven {
			t.Errorf("capability %s/%s not proven", c.Modality, c.Provider)
		}
		if strings.TrimSpace(c.Authority) == "" {
			t.Errorf("capability %s/%s missing authority", c.Modality, c.Provider)
		}
	}
}
