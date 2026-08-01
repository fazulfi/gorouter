// Package refresh provides domain types for credential refresh operations.
//
// It defines refresh policies, status codes, credential representation, and
// error categories used by the engine coordinator and app service layers.
package refresh

import "time"

// Reason classifies the trigger for a refresh operation.
type Reason int

const (
	ReasonProactive Reason = iota // Scheduled before credential expiry
	ReasonReactive                // Triggered by 401/403 response
)

func (r Reason) String() string {
	switch r {
	case ReasonProactive:
		return "proactive"
	case ReasonReactive:
		return "reactive"
	default:
		return "unknown"
	}
}

// Status represents the outcome of a refresh attempt.
type Status string

const (
	StatusSuccess    Status = "success"
	StatusDefinitive Status = "definitive"
	StatusTransient  Status = "transient"
)

func (s Status) IsTerminal() bool {
	return s == StatusSuccess || s == StatusDefinitive
}

func (s Status) TerminalLabel() string {
	switch s {
	case StatusSuccess:
		return "success"
	case StatusDefinitive:
		return "definitive"
	default:
		return ""
	}
}

// TokenCredential holds the sensitive credential material for a provider account.
type TokenCredential struct {
	AccessToken  string
	RefreshToken *string
	ExpiresAt    *time.Time
}

// Result carries the outcome of a single refresh attempt or sequence.
type Result struct {
	Status     Status
	Credential *TokenCredential
	Err        error
}

// RefreshPolicy configures the refresh coordinator behaviour.
type RefreshPolicy struct {
	MaxParallel      int
	DefaultLeadTime  time.Duration
	NoExpiryInterval time.Duration
	AttemptTimeout   time.Duration
	MaxAttempts      int
	RequestWait      time.Duration
	BackoffBase      time.Duration
	JitterMax        time.Duration
}

func DefaultPolicy() RefreshPolicy {
	return RefreshPolicy{
		MaxParallel:      4,
		DefaultLeadTime:  5 * time.Minute,
		NoExpiryInterval: 6 * time.Hour,
		AttemptTimeout:   30 * time.Second,
		MaxAttempts:      3,
		RequestWait:      10 * time.Second,
		BackoffBase:      1 * time.Second,
		JitterMax:        500 * time.Millisecond,
	}
}
