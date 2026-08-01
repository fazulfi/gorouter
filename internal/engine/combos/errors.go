// Package combos implements the four combo routing engines: sequential
// fallback, sticky round-robin, capability auto-switch, and fusion with
// quorum/grace/hard-timeout/judge synthesis.
package combos

import "errors"

var (
	// ErrNoEligibleMember is returned when a combo has no active, eligible member.
	ErrNoEligibleMember = errors.New("combos: no eligible member")

	// ErrFusionNoSuccess is returned when every fusion panel failed.
	ErrFusionNoSuccess = errors.New("combos: fusion produced no successful panel")

	// ErrFusionHardTimeout is returned when the fusion hard timeout expired
	// before a decision was reached.
	ErrFusionHardTimeout = errors.New("combos: fusion hard timeout exceeded")

	// ErrJudgeFailed is returned when judge synthesis failed after quorum.
	ErrJudgeFailed = errors.New("combos: judge synthesis failed")

	// ErrDefinitiveFailure marks a non-fallback-eligible failure (auth, policy,
	// validation). The default sequential classifier never falls back on it.
	ErrDefinitiveFailure = errors.New("combos: definitive failure")
)
