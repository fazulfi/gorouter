package combos

import (
	"context"
	"errors"

	"gorouter/internal/domain/combo"
)

// SequentialEngine runs combo members in configured order, moving to the next
// member only when the failure is fallback-eligible.
type SequentialEngine struct{}

// NewSequentialEngine creates a sequential combo engine.
func NewSequentialEngine() *SequentialEngine { return &SequentialEngine{} }

// Run invokes active members in order with the request body. It returns the
// first success. A failure is terminal when classify returns false or when the
// classifier is nil. Cancellation is propagated and returns context.Canceled
// immediately.
func (e *SequentialEngine) Run(ctx context.Context, members []combo.Member, request []byte, invoke Invoker, classify FailureClassifier) (*MemberResult, error) {
	active := combo.ActiveMembers(members)
	if len(active) == 0 {
		return nil, ErrNoEligibleMember
	}
	var lastErr error
	for _, m := range active {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		body, err := invoke(ctx, m, request)
		if err == nil {
			return &MemberResult{Member: m, Body: body}, nil
		}
		lastErr = err
		if classify == nil || !classify(err) {
			return nil, err
		}
	}
	return nil, lastErr
}

// DefaultFailureClassifier treats every failure as fallback-eligible except
// definitive failures (auth, policy, validation).
func DefaultFailureClassifier(err error) bool {
	return !errors.Is(err, ErrDefinitiveFailure)
}
