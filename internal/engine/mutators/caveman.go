package mutators

import (
	"context"
	"encoding/json"

	"gorouter/internal/domain/engine"
)

const cavemanPrompt = `Respond tersely. Keep grammar and full sentences but drop filler, hedging and pleasantries. Pattern: state the thing, the action, the reason. Before writing code, stop at the first rung that holds. No unrequested abstractions. ACTIVE EVERY RESPONSE.`

type CavemanConfig struct {
	Enabled bool
	Level   string // "lite", "full", "ultra"
	Prompt  string
}

func DefaultCavemanConfig() CavemanConfig {
	return CavemanConfig{
		Enabled: false,
		Level:   "full",
		Prompt:  cavemanPrompt,
	}
}

type cavemanMutator struct {
	cfg CavemanConfig
}

func NewCaveman(cfg CavemanConfig) Mutator {
	return &cavemanMutator{cfg: cfg}
}

func (m *cavemanMutator) Name() string { return "caveman" }

func (m *cavemanMutator) Mutate(_ context.Context, req *engine.Request) (*Result, error) {
	if !m.cfg.Enabled {
		return &Result{Applied: false}, nil
	}
	if req == nil || len(req.MappedBody) == 0 {
		return &Result{Applied: false}, nil
	}

	prompt := m.cfg.Prompt
	if prompt == "" {
		prompt = cavemanPrompt
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
