package translators

import (
	"fmt"

	"gorouter/internal/domain/engine"
	"gorouter/internal/engine/translators/direct"
	"gorouter/internal/engine/translators/safeir"
)

type Registry struct {
	translators map[string]Translator
}

func NewRegistry() *Registry {
	r := &Registry{
		translators: make(map[string]Translator),
	}

	// Register direct translators (lossless, preferred path).
	r.Register(direct.NewOpenAIToClaude())
	r.Register(direct.NewClaudeToOpenAI())
	r.Register(direct.NewOpenAIToGemini())
	r.Register(direct.NewGeminiToOpenAI())
	r.Register(direct.NewCodexToOpenAI())
	r.Register(direct.NewOpenAIToCompat())
	r.Register(direct.NewCompatToOpenAI())

	// Register SafeIR fallback for all formats not covered by direct translators.
	for _, src := range allFormats {
		for _, dst := range allFormats {
			if src == dst {
				// Same-format handled by noop.
				continue
			}
			key := pairKey(src, dst)
			if _, exists := r.translators[key]; exists {
				// Direct translator already registered; skip SafeIR fallback.
				continue
			}
			r.translators[key] = safeir.NewSafeIRTranslator(src, dst)
		}
	}

	return r
}

var allFormats = []engine.RequestFormat{
	engine.FormatOpenAIChat,
	engine.FormatOpenAICompat,
	engine.FormatCodexResponses,
	engine.FormatAnthropic,
	engine.FormatGemini,
}

func pairKey(source, target engine.RequestFormat) string {
	return string(source) + "->" + string(target)
}

func (r *Registry) Register(t Translator) {
	key := pairKey(t.Source(), t.Target())
	r.translators[key] = t
}

func (r *Registry) GetTranslator(source, target engine.RequestFormat) (Translator, error) {
	// Same-format passthrough.
	if source == target {
		return &noopTranslator{fmt: source}, nil
	}

	key := pairKey(source, target)
	t, ok := r.translators[key]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrTranslatorNotFound, key)
	}
	return t, nil
}

func (r *Registry) SupportsPair(source, target engine.RequestFormat) bool {
	if source == target {
		return true
	}
	key := pairKey(source, target)
	_, ok := r.translators[key]
	return ok
}
