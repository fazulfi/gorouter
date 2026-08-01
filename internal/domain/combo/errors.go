// Package combo defines domain types and repository contracts for multi-provider
// routing combos (sequential fallback, sticky round-robin, capability auto-switch,
// and fusion with judge synthesis).
package combo

import "errors"

// Sentinel errors shared across the combo domain.
var (
	// ErrNotFound is returned when a combo definition does not exist.
	ErrNotFound = errors.New("combo: definition not found")

	// ErrInactive is returned when executing a combo that is not active.
	ErrInactive = errors.New("combo: definition is not active")

	// ErrUnknownStrategy is returned when a definition carries an unsupported strategy.
	ErrUnknownStrategy = errors.New("combo: unknown strategy")

	// ErrInvalidConfig is returned when a definition's JSONB config is malformed
	// or violates strategy constraints.
	ErrInvalidConfig = errors.New("combo: invalid config")

	// ErrNoMembers is returned when a combo has no members to execute.
	ErrNoMembers = errors.New("combo: definition has no members")

	// ErrInvalidMember is returned when a member lacks a provider or model reference.
	ErrInvalidMember = errors.New("combo: invalid member")

	// ErrJudgeModelUnresolved is returned when a fusion judge model reference
	// does not resolve to an actually configured model.
	ErrJudgeModelUnresolved = errors.New("combo: judge model unresolved")
)
