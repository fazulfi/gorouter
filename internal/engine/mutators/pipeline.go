package mutators

import (
	"context"
	"fmt"
	"strings"

	"gorouter/internal/domain/engine"
)

// Pipeline runs a sequence of Mutators in fixed order. Each mutator is
// independent — a failure in one does not affect the others (fail-open).
type Pipeline struct {
	mutators []Mutator
}

// New creates a Pipeline that runs the given mutators in order.
func New(mutators ...Mutator) *Pipeline {
	return &Pipeline{mutators: mutators}
}

// Append adds one or more mutators to the end of the pipeline.
func (p *Pipeline) Append(mutators ...Mutator) {
	p.mutators = append(p.mutators, mutators...)
}

// Mutators returns a copy of the registered mutator list.
func (p *Pipeline) Mutators() []Mutator {
	out := make([]Mutator, len(p.mutators))
	copy(out, p.mutators)
	return out
}

// Run executes every registered mutator in order. Each mutator failure is
// logged via the optional logger but does NOT abort the pipeline — the next
// mutator still runs. The request is never replaced; mutators modify it
// in-place. Run returns a combined report of all results.
func (p *Pipeline) Run(ctx context.Context, req *engine.Request, logger func(format string, args ...interface{})) []RunReport {
	if req == nil {
		return nil
	}
	reports := make([]RunReport, 0, len(p.mutators))
	for _, m := range p.mutators {
		select {
		case <-ctx.Done():
			reports = append(reports, RunReport{
				Name:   m.Name(),
				Result: nil,
				Err:    fmt.Errorf("%w: %w", ErrCancelled, ctx.Err()),
			})
			if logger != nil {
				logger("[mutators] %s skipped \u2014 pipeline cancelled", m.Name())
			}
			return reports
		default:
		}

		result, err := m.Mutate(ctx, req)
		reports = append(reports, RunReport{Name: m.Name(), Result: result, Err: err})
		if err != nil {
			if logger != nil {
				safe := sanitizeError(err)
				logger("[mutators] %s failed-open: %v", m.Name(), safe)
			}
		} else if result != nil && result.Applied && logger != nil {
			logger("[mutators] %s %s", m.Name(), result.Log(m.Name()))
		}
	}
	return reports
}

// RunReport captures the outcome of a single mutator invocation.
type RunReport struct {
	Name   string
	Result *Result
	Err    error
}

// AppliedCount returns the number of mutators that reported a successful
// application.
func AppliedCount(reports []RunReport) int {
	n := 0
	for _, r := range reports {
		if r.Result != nil && r.Result.Applied && r.Err == nil {
			n++
		}
	}
	return n
}

// ErrorCount returns the number of mutators that returned an error.
func ErrorCount(reports []RunReport) int {
	n := 0
	for _, r := range reports {
		if r.Err != nil {
			n++
		}
	}
	return n
}

// sanitizeError strips sensitive-looking substrings (credentials, tokens)
// from error messages before surfacing them in logs.
func sanitizeError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	// Redact common credential patterns
	redactions := []struct {
		old, new string
	}{
		{"sk-proj-", "sk-proj-***"},
		{"sk-ant-", "sk-ant-***"},
		{"Bearer ", "Bearer ***"},
		{"Authorization", "Authorization***"},
		{"x-api-key", "x-api-key***"},
	}
	for _, r := range redactions {
		if strings.Contains(msg, r.old) {
			msg = strings.ReplaceAll(msg, r.old, r.new)
		}
	}
	return fmt.Errorf("%s", msg)
}

// DefaultPipeline returns the standard upstream-ordered pipeline with all
// mutators enabled at their default settings.
func DefaultPipeline() *Pipeline {
	return New(
		NewRTK(RTKConfig{}),
		NewHeadroom(HeadroomConfig{}),
		NewCaveman(CavemanConfig{}),
		NewPonytail(PonytailConfig{}),
		NewPxpipe(PxpipeConfig{}),
	)
}
