package retry

import (
	"context"
	"crypto/rand"
	"math"
	"math/big"
	"time"

	"gorouter/internal/shared"
)

type Config struct {
	MaxAttempts         int
	BaseDelay           time.Duration
	MaxDelay            time.Duration
	BackoffFactor       float64
	Jitter              float64
	RetryableErrorCodes []shared.ErrorCode
}

// DefaultConfig returns a Config populated with sensible defaults.
func DefaultConfig() Config {
	return Config{
		MaxAttempts:   3,
		BaseDelay:     500 * time.Millisecond,
		MaxDelay:      30 * time.Second,
		BackoffFactor: 2.0,
		Jitter:        0.1,
		RetryableErrorCodes: []shared.ErrorCode{
			ErrCodeUpstream,
			ErrCodeStreamFailed,
			ErrCodeExecutorTimeout,
		},
	}
}

type Decision struct {
	ShouldRetry bool
	Delay       time.Duration
	Attempt     int
	Reason      string
}

// Policy evaluates whether a failed attempt should be retried.
type Policy struct {
	config Config
}

// NewPolicy creates a Policy with the given Config.
func NewPolicy(cfg Config) *Policy {
	if cfg.MaxAttempts == 0 {
		cfg.MaxAttempts = 3
	}
	if cfg.BaseDelay <= 0 {
		cfg.BaseDelay = 500 * time.Millisecond
	}
	if cfg.MaxDelay <= 0 {
		cfg.MaxDelay = 30 * time.Second
	}
	if cfg.BackoffFactor <= 0 {
		cfg.BackoffFactor = 2.0
	}
	if cfg.Jitter < 0 || cfg.Jitter > 1.0 {
		cfg.Jitter = 0.1
	}
	if cfg.RetryableErrorCodes == nil {
		cfg.RetryableErrorCodes = []shared.ErrorCode{
			ErrCodeUpstream,
			ErrCodeStreamFailed,
			ErrCodeExecutorTimeout,
		}
	}
	return &Policy{config: cfg}
}

// Evaluate returns a retry Decision based on attempt number and error.
func (p *Policy) Evaluate(attempt int, err error) *Decision {
	if attempt < 1 {
		attempt = 1
	}

	if err == nil {
		return &Decision{ShouldRetry: false, Attempt: attempt, Reason: "no error"}
	}

	d := &Decision{Attempt: attempt}

	if attempt >= p.config.MaxAttempts {
		d.Reason = "max attempts reached"
		return d
	}

	if err == context.Canceled || err == context.DeadlineExceeded {
		d.Reason = "context terminated: " + err.Error()
		return d
	}

	if !p.isRetryable(err) {
		d.Reason = "non-retryable error"
		return d
	}

	d.ShouldRetry = true
	d.Delay = p.backoff(attempt)
	d.Reason = "retryable error"
	return d
}

func (p *Policy) backoff(attempt int) time.Duration {
	exp := math.Pow(p.config.BackoffFactor, float64(attempt-1))
	delay := float64(p.config.BaseDelay) * exp
	delay = math.Min(delay, float64(p.config.MaxDelay))

	if p.config.Jitter > 0 {
		jitterRange := delay * p.config.Jitter
		offset := cryptoRandInt64(int64(jitterRange*2+1)) - int64(jitterRange)
		delay += float64(offset)
	}

	if delay < 0 {
		delay = 0
	}
	return time.Duration(delay)
}

// isRetryable checks whether err matches a retryable code in the policy config.
// For ErrCodeUpstream only 5xx status codes are considered retryable.
func (p *Policy) isRetryable(err error) bool {
	if err == nil {
		return false
	}
	appErr, ok := shared.AsAppError(err)
	if !ok {
		return false
	}
	for _, code := range p.config.RetryableErrorCodes {
		if appErr.Code == code {
			if code == ErrCodeUpstream {
				return appErr.HTTPStatus >= 500 && appErr.HTTPStatus < 600
			}
			return true
		}
	}
	return false
}

// cryptoRandInt64 returns a non-negative random int64 in [0, max).
func cryptoRandInt64(max int64) int64 {
	if max <= 0 {
		return 0
	}
	n, err := rand.Int(rand.Reader, big.NewInt(max))
	if err != nil {
		panic("retry: crypto/rand read failed: " + err.Error())
	}
	return n.Int64()
}
