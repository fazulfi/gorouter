package mutators

import (
	"context"
	"fmt"

	"gorouter/internal/domain/engine"
)

// Result carries optional metadata about a mutation for observability.
type Result struct {
	Applied bool                   `json:"applied"`
	Stats   map[string]interface{} `json:"stats,omitempty"`
}

// Log returns a compact log-line representation of the result.
func (r *Result) Log(name string) string {
	if r == nil || !r.Applied {
		return ""
	}
	return fmt.Sprintf("[%s] applied", name)
}

// Mutator transforms a request in-place. On any error the request MUST remain
// unchanged. Implementations must not access credentials, headers, or request
// metadata beyond what is documented in their contract.
type Mutator interface {
	// Name returns the mutator identifier for logging and metrics.
	Name() string

	// Mutate applies the transformation. A nil or non-applied Result together
	// with a nil error means the mutator chose not to act (e.g. disabled,
	// below threshold, unsupported format). A non-nil error must leave the
	// request unchanged (fail-open contract).
	Mutate(ctx context.Context, req *engine.Request) (*Result, error)
}

// MutatorFunc is an adapter that allows ordinary functions to implement Mutator.
type MutatorFunc func(ctx context.Context, req *engine.Request) (*Result, error)

func (f MutatorFunc) Name() string { return "anonymous" }

func (f MutatorFunc) Mutate(ctx context.Context, req *engine.Request) (*Result, error) {
	return f(ctx, req)
}

// namedFunc wraps a MutatorFunc with a fixed name.
type namedFunc struct {
	name string
	fn   MutatorFunc
}

func (n *namedFunc) Name() string { return n.name }

func (n *namedFunc) Mutate(ctx context.Context, req *engine.Request) (*Result, error) {
	return n.fn(ctx, req)
}

// NamedFunc wraps f with the given name so it satisfies Mutator.
func NamedFunc(name string, fn MutatorFunc) Mutator {
	return &namedFunc{name: name, fn: fn}
}

// ErrCancelled is returned when the pipeline is cancelled mid-flight.
var ErrCancelled = fmt.Errorf("mutator: pipeline cancelled")
