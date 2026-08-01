// Package refresh implements the unified proactive/reactive credential refresh
// coordinator, per-account singleflight, and policy configuration.
package refresh

import (
	"time"

	domainrefresh "gorouter/internal/domain/refresh"
)

// Policy wraps domainrefresh.RefreshPolicy with engine-level defaults.
// It is a value type safe for concurrent use.
type Policy struct {
	inner domainrefresh.RefreshPolicy
}

// NewDefaultPolicy returns a Policy initialised with domain defaults
// including MaxParallel=4, DefaultLeadTime=5m, NoExpiryInterval=6h,
// AttemptTimeout=30s, MaxAttempts=3, RequestWait=10s, BackoffBase=1s,
// and JitterMax=500ms.
func NewDefaultPolicy() Policy {
	return Policy{inner: domainrefresh.DefaultPolicy()}
}

// NewPolicyFromDomain wraps an already-configured domain policy.
func NewPolicyFromDomain(p domainrefresh.RefreshPolicy) Policy {
	return Policy{inner: p}
}

// MaxParallel returns the global maximum concurrent refresh operations.
func (p Policy) MaxParallel() int { return p.inner.MaxParallel }

// DefaultLeadTime returns the duration before expiry for proactive refresh.
func (p Policy) DefaultLeadTime() time.Duration { return p.inner.DefaultLeadTime }

// NoExpiryInterval returns the interval for credentials without reliable expiry.
func (p Policy) NoExpiryInterval() time.Duration { return p.inner.NoExpiryInterval }

// AttemptTimeout returns the per-attempt timeout.
func (p Policy) AttemptTimeout() time.Duration { return p.inner.AttemptTimeout }

// MaxAttempts returns the maximum number of attempts per sequence.
func (p Policy) MaxAttempts() int { return p.inner.MaxAttempts }

// RequestWait returns how long a request waits for an in-flight refresh.
func (p Policy) RequestWait() time.Duration { return p.inner.RequestWait }

// BackoffBase returns the base backoff duration.
func (p Policy) BackoffBase() time.Duration { return p.inner.BackoffBase }

// JitterMax returns the maximum random jitter added to backoff.
func (p Policy) JitterMax() time.Duration { return p.inner.JitterMax }

// WithProviderOverride returns a copy of the policy with provider-specific
// overrides applied. Nil overrides are silently ignored.
func (p Policy) WithProviderOverride(o *ProviderOverride) Policy {
	if o == nil {
		return p
	}
	cp := p.inner
	if o.LeadTime != nil {
		cp.DefaultLeadTime = *o.LeadTime
	}
	if o.NoExpiryInterval != nil {
		cp.NoExpiryInterval = *o.NoExpiryInterval
	}
	if o.AttemptTimeout != nil {
		cp.AttemptTimeout = *o.AttemptTimeout
	}
	if o.MaxAttempts != nil {
		cp.MaxAttempts = *o.MaxAttempts
	}
	return Policy{inner: cp}
}

// ProviderOverride allows provider-specific refresh tuning backed by
// source audit. Nil fields defer to the policy default.
type ProviderOverride struct {
	LeadTime         *time.Duration
	NoExpiryInterval *time.Duration
	AttemptTimeout   *time.Duration
	MaxAttempts      *int
}
