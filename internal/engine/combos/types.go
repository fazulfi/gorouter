package combos

import (
	"context"
	"time"

	"github.com/google/uuid"

	"gorouter/internal/domain/combo"
)

// Invoker executes one request against a combo member and returns the raw
// response body. Invokers MUST respect context cancellation promptly; engines
// rely on that contract for leak-free termination.
type Invoker func(ctx context.Context, member combo.Member, request []byte) ([]byte, error)

// MemberResult is a successful invocation outcome tied to its member.
type MemberResult struct {
	Member combo.Member
	Body   []byte
}

// PanelResult is the outcome of one fusion panel invocation.
type PanelResult struct {
	Member combo.Member
	Body   []byte
	Err    error
	// Synthesized marks a result produced by judge synthesis rather than a
	// single panel; the Member field is then unattributed.
	Synthesized bool
}

// Judge synthesizes the final fusion answer from the quorum of successful
// panels. The successful panels are passed in deterministic member order.
type Judge func(ctx context.Context, panels []PanelResult) ([]byte, error)

// FailureClassifier reports whether a failure is fallback-eligible.
type FailureClassifier func(err error) bool

// Rotation is the persisted sticky round-robin checkpoint.
type Rotation struct {
	Index      int       `json:"index"`
	Stickiness int       `json:"stickiness"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// StickyStore persists rotation checkpoints through the approved runtime state
// storage so round-robin stickiness survives restart.
type StickyStore interface {
	GetRotation(ctx context.Context, comboID uuid.UUID) (*Rotation, error)
	SaveRotation(ctx context.Context, comboID uuid.UUID, r Rotation) error
}

// CapabilityCatalog reports whether a member's model satisfies a set of
// required capabilities.
type CapabilityCatalog interface {
	Supports(ctx context.Context, member combo.Member, capabilities []string) (bool, error)
}
