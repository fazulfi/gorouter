// Package modelref provides model reference parsing and resolution for the AI
// gateway. The package defines the ModelRef grammar, parsing helpers, resolver
// interface, and domain error variables.
package modelref

import "errors"

var (
	// ErrInvalidModelRef is returned when the model reference string cannot be
	// parsed as any known format (empty string, too many segments, empty
	// segments, etc.).
	ErrInvalidModelRef = errors.New("invalid model reference syntax")

	// ErrProviderNotFound is returned when the provider named in a model
	// reference does not match any configured provider.
	ErrProviderNotFound = errors.New("provider not found for reference")

	// ErrModelNotFound is returned when the model named in a reference does
	// not match any known model for the resolved provider.
	ErrModelNotFound = errors.New("model not found for reference")

	// ErrCapNotSupported is returned when the requested capability is not
	// supported by the resolved model.
	ErrCapNotSupported = errors.New("capability not supported by model")

	// ErrNoAccountsAvail is returned when no active, non-cooldown accounts
	// are available for the resolved provider.
	ErrNoAccountsAvail = errors.New("no available accounts for provider")
)
