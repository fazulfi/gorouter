package modelref

import "strings"

// ModelRef represents a parsed model reference string. A model reference is a
// compact human-readable string that identifies an AI model, optionally
// qualified with a provider, account, and capability.
//
// Supported formats:
//
//	"provider:model"              → Provider=provider, Model=model
//	"provider:account:model"      → Provider=provider, Account=*account, Model=model
//	"provider:account:model:cap"  → Provider=provider, Account=*account, Model=model, Capability=*cap
//	"provider/model"              → Provider=provider, Model=model  (slash-delimited)
//	"unqualified"                 → Raw=unqualified, Provider="", Model=""
type ModelRef struct {
	// Raw is the original unmodified input string.
	Raw string

	// Provider is the named AI provider (e.g. "openai", "anthropic"). Empty
	// for unqualified references.
	Provider string

	// Account is an optional account selector (e.g. "acct-1", "prod", "8675309").
	// When nil, the resolver will select the highest-priority non-cooldown account.
	Account *string

	// Model is the model identifier (e.g. "gpt-4", "claude-3-opus"). For
	// unqualified references this is also empty and Raw carries the input.
	Model string

	// Capability is an optional capability selector (e.g. "chat", "embedding").
	Capability *string
}

// Parse parses a raw model reference string into a ModelRef. It recognizes
// the formats documented on the ModelRef struct.
//
// An empty string returns ErrInvalidModelRef. Strings that contain no
// provider delimiter (":" or "/") are treated as unqualified references.
func Parse(raw string) (ModelRef, error) {
	if raw == "" {
		return ModelRef{}, ErrInvalidModelRef
	}

	provider, remainder := parseProvider(raw)

	// No delimiter found — unqualified reference.
	if provider == "" {
		return ModelRef{Raw: raw}, nil
	}

	// Slash-delimited: provider/model
	if strings.Contains(raw, "/") {
		if remainder == "" {
			return ModelRef{}, ErrInvalidModelRef
		}
		return ModelRef{
			Raw:      raw,
			Provider: provider,
			Model:    remainder,
		}, nil
	}

	// Colon-delimited: the remainder after the provider segment may be
	// model                          → provider:model
	// account:model                  → provider:account:model
	// account:model:capability       → provider:account:model:capability
	parts := strings.Split(remainder, ":")
	if len(parts) == 0 || parts[0] == "" {
		return ModelRef{}, ErrInvalidModelRef
	}

	switch len(parts) {
	case 1:
		return ModelRef{
			Raw:      raw,
			Provider: provider,
			Model:    parts[0],
		}, nil
	case 2:
		return ModelRef{
			Raw:      raw,
			Provider: provider,
			Account:  &parts[0],
			Model:    parts[1],
		}, nil
	case 3:
		return ModelRef{
			Raw:        raw,
			Provider:   provider,
			Account:    &parts[0],
			Model:      parts[1],
			Capability: &parts[2],
		}, nil
	default:
		return ModelRef{}, ErrInvalidModelRef
	}
}

// String reconstructs the canonical colon-delimited form of the model
// reference. Unqualified references return their Raw string.
func (m ModelRef) String() string {
	if m.IsUnqualified() {
		return m.Raw
	}

	var b strings.Builder
	b.WriteString(m.Provider)
	if m.Account != nil {
		b.WriteByte(':')
		b.WriteString(*m.Account)
	}
	b.WriteByte(':')
	b.WriteString(m.Model)
	if m.Capability != nil {
		b.WriteByte(':')
		b.WriteString(*m.Capability)
	}
	return b.String()
}

// IsUnqualified returns true when the reference has no provider segment.
func (m ModelRef) IsUnqualified() bool {
	return m.Provider == ""
}
