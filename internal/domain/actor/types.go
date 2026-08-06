// Package actor defines pure actor vocabulary and capability helpers.
package actor

import "gorouter/internal/domain/auth"

// Kind and Origin are aliases of the canonical auth contract.
type Kind = auth.ActorKind
type Origin = auth.ActorOrigin

const (
	KindUser     = auth.ActorKindUser
	KindSession  = auth.ActorKindSession
	KindPAT      = auth.ActorKindPAT
	KindCLI      = auth.ActorKindCLI
	KindJob      = auth.ActorKindJob
	OriginLocal  = auth.ActorOriginLocal
	OriginRemote = auth.ActorOriginRemote
)

// Capability identifies a narrowly scoped actor permission.
type Capability string

const (
	CapabilityStatusRead Capability = "status-read"
	CapabilityAuditWrite Capability = "audit-write"
)

// CapabilitiesForJob returns the fixed, least-privilege job capability set.
func CapabilitiesForJob() []Capability {
	return []Capability{CapabilityStatusRead, CapabilityAuditWrite}
}

// CopyCapabilities returns an independent copy, preventing caller mutation.
func CopyCapabilities(in []Capability) []Capability {
	return append([]Capability(nil), in...)
}

// IsLocalCLI classifies only the explicit local CLI identity.
func IsLocalCLI(a auth.Actor) bool {
	return a.Kind == KindCLI && a.Origin == OriginLocal
}

// IsJob classifies only the explicit job actor kind.
func IsJob(a auth.Actor) bool { return a.Kind == KindJob }
