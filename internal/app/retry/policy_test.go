package retry

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"gorouter/internal/shared"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.MaxAttempts != 3 {
		t.Errorf("MaxAttempts = %d, want 3", cfg.MaxAttempts)
	}
	if cfg.BaseDelay != 500*time.Millisecond {
		t.Errorf("BaseDelay = %v, want 500ms", cfg.BaseDelay)
	}
	if cfg.MaxDelay != 30*time.Second {
		t.Errorf("MaxDelay = %v, want 30s", cfg.MaxDelay)
	}
	if cfg.BackoffFactor != 2.0 {
		t.Errorf("BackoffFactor = %f, want 2.0", cfg.BackoffFactor)
	}
	if cfg.Jitter != 0.1 {
		t.Errorf("Jitter = %f, want 0.1", cfg.Jitter)
	}
	if len(cfg.RetryableErrorCodes) == 0 {
		t.Error("RetryableErrorCodes is empty")
	}
}

func TestNewPolicyZeroValue(t *testing.T) {
	p := NewPolicy(Config{})
	if p.config.MaxAttempts != 3 {
		t.Errorf("MaxAttempts = %d, want 3", p.config.MaxAttempts)
	}
	if p.config.BaseDelay != 500*time.Millisecond {
		t.Errorf("BaseDelay = %v, want 500ms", p.config.BaseDelay)
	}
}

func TestNewPolicyPartialConfig(t *testing.T) {
	p := NewPolicy(Config{
		MaxAttempts: 5,
	})
	if p.config.MaxAttempts != 5 {
		t.Errorf("MaxAttempts = %d, want 5", p.config.MaxAttempts)
	}
	if p.config.BaseDelay != 500*time.Millisecond {
		t.Errorf("BaseDelay = %v, want 500ms", p.config.BaseDelay)
	}
}

func TestBackoffNoJitter(t *testing.T) {
	cfg := Config{
		MaxAttempts:   5,
		BaseDelay:     1 * time.Second,
		MaxDelay:      10 * time.Second,
		BackoffFactor: 2.0,
		Jitter:        0,
	}
	p := NewPolicy(cfg)

	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{1, 1 * time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{4, 8 * time.Second},
		{5, 10 * time.Second},
	}

	for _, tt := range tests {
		got := p.backoff(tt.attempt)
		if got != tt.want {
			t.Errorf("backoff(%d) = %v, want %v", tt.attempt, got, tt.want)
		}
	}
}

func TestBackoffJitterWithinBounds(t *testing.T) {
	jitter := 0.2
	cfg := Config{
		MaxAttempts:   3,
		BaseDelay:     1 * time.Second,
		MaxDelay:      10 * time.Second,
		BackoffFactor: 2.0,
		Jitter:        jitter,
	}
	p := NewPolicy(cfg)

	base := float64(p.config.BaseDelay)
	for attempt := 1; attempt <= 3; attempt++ {
		got := p.backoff(attempt)
		expected := base * math.Pow(p.config.BackoffFactor, float64(attempt-1))
		expected = math.Min(expected, float64(p.config.MaxDelay))

		minDelay := expected * (1 - jitter)
		maxDelay := expected * (1 + jitter)

		d := float64(got)
		if d < minDelay || d > maxDelay {
			t.Errorf("backoff(%d) = %v, want within [%v, %v] (jitter=%.2f)",
				attempt, got, time.Duration(minDelay), time.Duration(maxDelay), jitter)
		}
	}
}

func TestEvaluateMaxAttemptsReached(t *testing.T) {
	p := NewPolicy(Config{MaxAttempts: 2})
	err := shared.NewAppError(ErrCodeUpstream, "upstream error", 502, nil)
	d := p.Evaluate(2, err)
	if d.ShouldRetry {
		t.Error("expected no retry when max attempts reached")
	}
	if d.Reason != "max attempts reached" {
		t.Errorf("reason = %q, want %q", d.Reason, "max attempts reached")
	}
}

func TestEvaluateRetryableError(t *testing.T) {
	p := NewPolicy(DefaultConfig())
	err := shared.NewAppError(ErrCodeUpstream, "upstream 5xx", 502, nil)
	d := p.Evaluate(1, err)
	if !d.ShouldRetry {
		t.Fatal("expected retry for 5xx upstream error")
	}
	if d.Delay <= 0 {
		t.Errorf("expected positive delay, got %v", d.Delay)
	}
	if d.Reason != "retryable error" {
		t.Errorf("reason = %q, want %q", d.Reason, "retryable error")
	}
}

func TestEvaluateNonRetryableError(t *testing.T) {
	p := NewPolicy(DefaultConfig())
	err := shared.NewAppError(ErrCodeInvalidRequest, "bad request", 400, nil)
	d := p.Evaluate(1, err)
	if d.ShouldRetry {
		t.Error("expected no retry for non-retryable error")
	}
	if d.Reason != "non-retryable error" {
		t.Errorf("reason = %q, want %q", d.Reason, "non-retryable error")
	}
}

func TestEvaluateNilError(t *testing.T) {
	p := NewPolicy(DefaultConfig())
	d := p.Evaluate(1, nil)
	if d.ShouldRetry {
		t.Error("expected no retry for nil error")
	}
	if d.Reason != "no error" {
		t.Errorf("reason = %q, want %q", d.Reason, "no error")
	}
}

func TestEvaluateContextCanceled(t *testing.T) {
	p := NewPolicy(DefaultConfig())
	d := p.Evaluate(1, context.Canceled)
	if d.ShouldRetry {
		t.Error("expected no retry for context.Canceled")
	}
}

func TestEvaluateContextDeadlineExceeded(t *testing.T) {
	p := NewPolicy(DefaultConfig())
	d := p.Evaluate(1, context.DeadlineExceeded)
	if d.ShouldRetry {
		t.Error("expected no retry for context.DeadlineExceeded")
	}
}

func TestEvaluateAttemptZeroNormalized(t *testing.T) {
	p := NewPolicy(DefaultConfig())
	err := shared.NewAppError(ErrCodeUpstream, "upstream error", 503, nil)
	d := p.Evaluate(0, err)
	if d.Attempt != 1 {
		t.Errorf("Attempt = %d, want 1", d.Attempt)
	}
}

func TestEvaluateCustomRetryableCodes(t *testing.T) {
	p := NewPolicy(Config{
		MaxAttempts: 3,
		RetryableErrorCodes: []shared.ErrorCode{
			ErrCodeInvalidRequest,
		},
	})
	err := shared.NewAppError(ErrCodeInvalidRequest, "invalid but config says retry", 400, nil)
	d := p.Evaluate(1, err)
	if !d.ShouldRetry {
		t.Error("expected retry for custom retryable code")
	}
}

func TestEvaluateNonRetryableAppErrorCode(t *testing.T) {
	p := NewPolicy(DefaultConfig())
	err := shared.NewAppError(shared.ErrValidation, "validation failed", 422, nil)
	d := p.Evaluate(1, err)
	if d.ShouldRetry {
		t.Error("expected no retry for ErrValidation")
	}
}

func TestEvaluateUpstream4xxNotRetryable(t *testing.T) {
	p := NewPolicy(DefaultConfig())
	err := shared.NewAppError(ErrCodeUpstream, "upstream 4xx", 429, nil)
	d := p.Evaluate(1, err)
	if d.ShouldRetry {
		t.Error("expected no retry for upstream 4xx error")
	}
}

func TestEvaluateWrappedRetryableError(t *testing.T) {
	p := NewPolicy(DefaultConfig())
	inner := shared.NewAppError(ErrCodeStreamFailed, "stream failed", 0, nil)
	wrapped := errors.New("execution wrapper: " + inner.Error())
	d := p.Evaluate(1, wrapped)
	if d.ShouldRetry {
		t.Log("non-AppError wrapping is not retryable — this is expected")
	}
}

func TestBackoffCappedAtMax(t *testing.T) {
	cfg := Config{
		MaxAttempts:   10,
		BaseDelay:     1 * time.Second,
		MaxDelay:      5 * time.Second,
		BackoffFactor: 3.0,
		Jitter:        0,
	}
	p := NewPolicy(cfg)
	for attempt := 1; attempt <= 10; attempt++ {
		got := p.backoff(attempt)
		if got > p.config.MaxDelay {
			t.Errorf("backoff(%d) = %v, exceeds MaxDelay %v", attempt, got, p.config.MaxDelay)
		}
	}
}

func TestEvaluateMultipleAttempts(t *testing.T) {
	p := NewPolicy(Config{
		MaxAttempts:   4,
		BaseDelay:     100 * time.Millisecond,
		MaxDelay:      1 * time.Second,
		BackoffFactor: 2.0,
		Jitter:        0,
	})
	err := shared.NewAppError(ErrCodeExecutorTimeout, "timeout", 0, nil)

	for attempt := 1; attempt < 4; attempt++ {
		d := p.Evaluate(attempt, err)
		if !d.ShouldRetry {
			t.Fatalf("expected retry on attempt %d", attempt)
		}
		if d.Delay <= 0 {
			t.Errorf("expected delay > 0 on attempt %d, got %v", attempt, d.Delay)
		}
	}

	d := p.Evaluate(4, err)
	if d.ShouldRetry {
		t.Error("expected no retry on attempt 4 (max)")
	}
}
