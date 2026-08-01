package mutators

import (
	"context"
	"encoding/json"

	"gorouter/internal/domain/engine"
)

const ponytailPrompt = `You are a lazy senior developer. Lazy means efficient, not careless. Before writing code, stop at the first rung that holds. No unrequested abstractions. Deletion over addition. ACTIVE EVERY RESPONSE.`

type PonytailConfig struct {
	Enabled bool
	Level   string // "lite", "full", "ultra"
	Prompt  string
}

func DefaultPonytailConfig() PonytailConfig {
	return PonytailConfig{
		Enabled: false,
		Level:   "full",
		Prompt:  ponytailPrompt,
	}
}

type ponytailMutator struct {
	cfg PonytailConfig
}

func NewPonytail(cfg PonytailConfig) Mutator {
	return &ponytailMutator{cfg: cfg}
}

func (m *ponytailMutator) Name() string { return "ponytail" }

func (m *ponytailMutator) Mutate(_ context.Context, req *engine.Request) (*Result, error) {
	if !m.cfg.Enabled {
		return &Result{Applied: false}, nil
	}
	if req == nil || len(req.MappedBody) == 0 {
		return &Result{Applied: false}, nil
	}

	prompt := m.cfg.Prompt
	if prompt == "" {
		prompt = ponytailPrompt
	}

	body := make(map[string]json.RawMessage)
	if err := json.Unmarshal(req.MappedBody, &body); err != nil {
		return &Result{Applied: false}, nil
	}

	if injectSystemPromptToBody(body, prompt) {
		updated, err := json.Marshal(body)
		if err != nil {
			return &Result{Applied: false}, nil
		}
		req.MappedBody = updated
		return &Result{Applied: true}, nil
	}
	return &Result{Applied: false}, nil
}
