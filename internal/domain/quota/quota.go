// Package quota defines pure domain contracts for the per-provider quota
// tracker: the quota status surface (P4-T08), the temporary/definitive error
// classification (DECISIONS #168, #353), and the frozen auto-ping window
// constants observed from the pinned upstream quota auto-ping scheduler
// (audit/11 section 7; upstream config tick 60 s, ping lead 5 s, refresh
// ahead 5 min, failure cooldown 15 min, 5 h quota windows).
//
// The package is deliberately dependency-free: only the standard library and
// uuid are imported, and the phase-4 domain purity guard
// (internal/domain/arch_test.go) enforces it. No persistence, transport or OS
// concerns live here.
package quota

import (
	"time"

	"github.com/google/uuid"
)

// ErrorKind distinguishes how a quota failure must be presented (DECISIONS
// #168, #353): a temporary kind means the provider is expected to recover on
// its own after the failure cooldown, while a definitive kind means the
// provider needs an explicit action (for example re-authentication) before
// quota pings can resume. Both kinds arm the same 15-minute failure cooldown;
// the kind only drives presentation, never the cooldown duration.
type ErrorKind string

const (
	// ErrorKindTemporary marks a transient failure (network, timeout, rate
	// limit, upstream 5xx): the auto-ping job retries after the cooldown.
	ErrorKindTemporary ErrorKind = "temporary"
	// ErrorKindDefinitive marks a terminal failure (authentication or
	// authorization problems, invalid requests): the provider surfaces as
	// needing re-auth rather than a plain cooldown countdown.
	ErrorKindDefinitive ErrorKind = "definitive"
)

// IsValid reports whether the kind is one of the frozen values.
func (k ErrorKind) IsValid() bool {
	switch k {
	case ErrorKindTemporary, ErrorKindDefinitive:
		return true
	default:
		return false
	}
}

// QuotaStatus is the exact status surface for one provider's quota tracker
// (plan BE-13 L153; DECISIONS #353: active/cooldown/re-auth, token expiry,
// last refresh, next retry and sanitized error). The fields are exactly:
// ProviderID, WindowStart, PingLead, RefreshAhead, CooldownUntil, LastError
// and ErrorKind.
//
// A zero PingLead/RefreshAhead means the frozen defaults apply; a zero
// WindowStart means no 5-hour window has been observed yet; a zero
// CooldownUntil means the provider is not on failure cooldown; an empty
// LastError/ErrorKind means no failure has been recorded. LastError is always
// the sanitized form of the underlying error — raw provider error strings,
// tokens or credentials never appear in the status surface.
type QuotaStatus struct {
	ProviderID    uuid.UUID
	WindowStart   time.Time // start of the current 5-hour quota window (UTC)
	PingLead      time.Duration
	RefreshAhead  time.Duration
	CooldownUntil time.Time // end of the failure cooldown (UTC); zero when not cooling down
	LastError     string    // sanitized last failure message; empty when none
	ErrorKind     ErrorKind
}

// Frozen auto-ping window constants (audit/11 section 7d; upstream
// QUOTA_AUTOPING_CONFIG). The scheduler tick itself is owned by the CH-08 job
// set and is deliberately NOT declared here; the service only freezes the
// window shape it stores and counts down from.
const (
	// WindowDuration is the length of one quota window ("warms 5h windows",
	// upstream quotaAutoPing.js L1).
	WindowDuration = 5 * time.Hour
	// DefaultPingLead is how long before the window reset the auto-ping job
	// sends its tiny request (upstream pingLeadMs = 5,000).
	DefaultPingLead = 5 * time.Second
	// DefaultRefreshAhead is how early before the reset the job may refresh
	// credentials and re-fetch usage (upstream refreshAheadMs = 300,000).
	DefaultRefreshAhead = 5 * time.Minute
	// DefaultFailureCooldown is how long a failed ping suppresses retries
	// (upstream failureCooldownMs = 900,000).
	DefaultFailureCooldown = 15 * time.Minute
)
