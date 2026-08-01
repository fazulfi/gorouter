package mutators

import (
	"context"
	"encoding/json"

	"gorouter/internal/domain/engine"
)

type PxpipeConfig struct {
	Enabled   bool
	MinChars  int
	TimeoutMs int
	Format    string // only "claude" supported upstream
}

type pxpipeTransformFunc func(body []byte) ([]byte, error)

type pxpipeMutator struct {
	cfg       PxpipeConfig
	transform pxpipeTransformFunc
}

func NewPxpipe(cfg PxpipeConfig) Mutator {
	return &pxpipeMutator{cfg: cfg}
}

func (m *pxpipeMutator) Name() string { return "pxpipe" }

func (m *pxpipeMutator) SetTransform(fn pxpipeTransformFunc) {
	m.transform = fn
}

func (m *pxpipeMutator) Mutate(_ context.Context, req *engine.Request) (*Result, error) {
	if !m.cfg.Enabled {
		return &Result{Applied: false}, nil
	}
	if req == nil || len(req.MappedBody) == 0 {
		return &Result{Applied: false}, nil
	}
	if m.transform == nil {
		return &Result{Applied: false}, nil
	}

	minChars := m.cfg.MinChars
	if minChars <= 0 {
		minChars = 25000
	}

	if len(req.MappedBody) < minChars {
		return &Result{Applied: false}, nil
	}

	result, err := m.transform(req.MappedBody)
	if err != nil {
		return &Result{Applied: false}, nil
	}
	if result == nil || len(result) == 0 {
		return &Result{Applied: false}, nil
	}

	// Verify it's valid JSON
	if !json.Valid(result) {
		return &Result{Applied: false}, nil
	}

	beforeBytes := len(req.MappedBody)
	req.MappedBody = result
	return &Result{
		Applied: true,
		Stats: map[string]interface{}{
			"before_bytes": beforeBytes,
			"after_bytes":  len(result),
		},
	}, nil
}
