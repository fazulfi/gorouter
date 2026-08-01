package media

import (
	"strings"
	"testing"
)

// TestAllLimitsEveryEntryHasAuthority proves every limit entry carries a
// non-empty authority citation (phase-3-design.md §10.4 + audit checklist
// item at phase-3-design.md:866: no invented limits).
func TestAllLimitsEveryEntryHasAuthority(t *testing.T) {
	limits := AllLimits()
	if len(limits) == 0 {
		t.Fatal("AllLimits() empty")
	}
	for _, l := range limits {
		if strings.TrimSpace(l.Authority) == "" {
			t.Errorf("limit %q missing authority", l.Name)
		}
		if l.Name == "" {
			t.Errorf("limit with empty name")
		}
		if !l.Defined && !strings.Contains(l.Authority, "transport ceiling") {
			t.Errorf("limit %q: undefined limit must cite the transport-ceiling derivation, got %q", l.Name, l.Authority)
		}
	}
}

// TestLimitCoverageEveryModalityHasLimits proves the registry covers every
// modality.
func TestLimitCoverageEveryModalityHasLimits(t *testing.T) {
	for _, m := range AllModalities() {
		if len(Limits(m)) == 0 {
			t.Errorf("modality %q has no limit entries", m)
		}
	}
}

// TestBodyCeilingIsPlatformDerived pins the transport ceiling to the
// upstream platform body limit citation.
func TestBodyCeilingIsPlatformDerived(t *testing.T) {
	if BodyCeiling != 128<<20 {
		t.Errorf("BodyCeiling = %d, want %d", BodyCeiling, int64(128)<<20)
	}
	if !strings.Contains(BodyCeilingAuthority, "audit/00-upstream-audit.md:67") {
		t.Errorf("BodyCeilingAuthority = %q, want audit/00-upstream-audit.md:67", BodyCeilingAuthority)
	}
}

// TestKnownUpstreamLimits pins the concrete upstream-derived values that the
// executors rely on (regression guard against silent value drift).
func TestKnownUpstreamLimits(t *testing.T) {
	find := func(m Modality, name string) (Limit, bool) {
		for _, l := range Limits(m) {
			if l.Name == name {
				return l, true
			}
		}
		return Limit{}, false
	}
	cases := []struct {
		m    Modality
		name string
		want int64
	}{
		{ModalityWebSearch, "default_max_results", 5},
		{ModalityWebFetch, "default_timeout_ms", 15000},
		{ModalityWebFetch, "max_characters_firecrawl", 200000},
		{ModalityImageGeneration, "poll_interval_ms", 1500},
		{ModalityImageGeneration, "poll_timeout_ms", 120000},
		{ModalitySTT, "route_max_duration_s", 300},
		{ModalitySTT, "assemblyai_poll_timeout_ms", 120000},
		{ModalityTTS, "voices_ttl_ms", 86400000},
		{ModalityVideoGeneration, "round_trip_timeout_ms", 120000},
		{ModalityVideoStatus, "cli_poll_interval_ms", 5000},
		{ModalityVideoStatus, "cli_wait_timeout_ms", 600000},
	}
	for _, tc := range cases {
		l, ok := find(tc.m, tc.name)
		if !ok {
			t.Errorf("limit %s/%s not found", tc.m, tc.name)
			continue
		}
		if !l.Defined {
			t.Errorf("limit %s/%s expected Defined=true", tc.m, tc.name)
		}
		if l.Value != tc.want {
			t.Errorf("limit %s/%s = %d, want %d", tc.m, tc.name, l.Value, tc.want)
		}
	}
}
