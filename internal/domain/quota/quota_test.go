package quota

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestErrorKindValidity freezes the two-kind contract: only temporary and
// definitive are valid; anything else (including the zero value) is rejected.
func TestErrorKindValidity(t *testing.T) {
	if !ErrorKindTemporary.IsValid() {
		t.Errorf("temporary must be a valid kind")
	}
	if !ErrorKindDefinitive.IsValid() {
		t.Errorf("definitive must be a valid kind")
	}
	for _, invalid := range []ErrorKind{"", "transient", "permanent", "fatal", "TEMP", "temporary "} {
		if invalid.IsValid() {
			t.Errorf("kind %q must be rejected", invalid)
		}
	}
}

// TestFrozenConstants freezes the auto-ping window shape observed from the
// pinned upstream (audit/11 section 7d): 5h windows, 5s ping lead, 5min
// refresh ahead, 15min failure cooldown.
func TestFrozenConstants(t *testing.T) {
	if WindowDuration != 5*time.Hour {
		t.Errorf("WindowDuration = %s, want 5h", WindowDuration)
	}
	if DefaultPingLead != 5*time.Second {
		t.Errorf("DefaultPingLead = %s, want 5s", DefaultPingLead)
	}
	if DefaultRefreshAhead != 5*time.Minute {
		t.Errorf("DefaultRefreshAhead = %s, want 5m", DefaultRefreshAhead)
	}
	if DefaultFailureCooldown != 15*time.Minute {
		t.Errorf("DefaultFailureCooldown = %s, want 15m", DefaultFailureCooldown)
	}
}

// TestQuotaStatusZeroValue freezes the zero-status semantics: a zero
// QuotaStatus carries no provider identity, no window, no cooldown and no
// error — the exact shape the service returns for a never-observed provider.
func TestQuotaStatusZeroValue(t *testing.T) {
	var s QuotaStatus
	if s.ProviderID != uuid.Nil {
		t.Errorf("zero status must carry a nil provider ID")
	}
	if !s.WindowStart.IsZero() {
		t.Errorf("zero status must have no window start")
	}
	if !s.CooldownUntil.IsZero() {
		t.Errorf("zero status must have no cooldown")
	}
	if s.LastError != "" || s.ErrorKind != "" {
		t.Errorf("zero status must carry no error state")
	}
	if s.PingLead != 0 || s.RefreshAhead != 0 {
		t.Errorf("zero status must carry zero durations")
	}
}
