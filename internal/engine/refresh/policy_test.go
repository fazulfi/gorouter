package refresh

import (
	"testing"
	"time"

	domainrefresh "gorouter/internal/domain/refresh"
)

func TestNewDefaultPolicy(t *testing.T) {
	t.Parallel()
	p := NewDefaultPolicy()
	if p.MaxParallel() != 4 {
		t.Errorf("MaxParallel = %d; want 4", p.MaxParallel())
	}
	if p.DefaultLeadTime() != 5*time.Minute {
		t.Errorf("DefaultLeadTime = %v; want 5m", p.DefaultLeadTime())
	}
	if p.NoExpiryInterval() != 6*time.Hour {
		t.Errorf("NoExpiryInterval = %v; want 6h", p.NoExpiryInterval())
	}
	if p.AttemptTimeout() != 30*time.Second {
		t.Errorf("AttemptTimeout = %v; want 30s", p.AttemptTimeout())
	}
	if p.MaxAttempts() != 3 {
		t.Errorf("MaxAttempts = %d; want 3", p.MaxAttempts())
	}
	if p.RequestWait() != 10*time.Second {
		t.Errorf("RequestWait = %v; want 10s", p.RequestWait())
	}
	if p.BackoffBase() <= 0 {
		t.Error("BackoffBase must be positive")
	}
	if p.JitterMax() <= 0 {
		t.Error("JitterMax must be positive")
	}
}

func TestNewPolicyFromDomain(t *testing.T) {
	t.Parallel()
	dp := domainrefresh.DefaultPolicy()
	dp.MaxParallel = 2
	p := NewPolicyFromDomain(dp)
	if p.MaxParallel() != 2 {
		t.Errorf("MaxParallel = %d; want 2", p.MaxParallel())
	}
}

func TestWithProviderOverride(t *testing.T) {
	t.Parallel()
	p := NewDefaultPolicy()

	lt := 1 * time.Minute
	o := &ProviderOverride{LeadTime: &lt}
	p2 := p.WithProviderOverride(o)
	if p2.DefaultLeadTime() != 1*time.Minute {
		t.Errorf("LeadTime = %v; want 1m", p2.DefaultLeadTime())
	}
	// Original unchanged
	if p.DefaultLeadTime() != 5*time.Minute {
		t.Errorf("Original LeadTime changed to %v", p.DefaultLeadTime())
	}
}

func TestWithProviderOverrideNil(t *testing.T) {
	t.Parallel()
	p := NewDefaultPolicy()
	p2 := p.WithProviderOverride(nil)
	if p2.DefaultLeadTime() != p.DefaultLeadTime() {
		t.Error("nil override should not change policy")
	}
}
