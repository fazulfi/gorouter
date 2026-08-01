package refresh_test

import (
	"testing"
	"time"

	"gorouter/internal/domain/refresh"
)

func TestDefaultPolicy(t *testing.T) {
	t.Parallel()
	p := refresh.DefaultPolicy()
	if p.MaxParallel != 4 {
		t.Errorf("MaxParallel = %d; want 4", p.MaxParallel)
	}
	if p.DefaultLeadTime != 5*time.Minute {
		t.Errorf("DefaultLeadTime = %v; want 5m", p.DefaultLeadTime)
	}
	if p.NoExpiryInterval != 6*time.Hour {
		t.Errorf("NoExpiryInterval = %v; want 6h", p.NoExpiryInterval)
	}
	if p.AttemptTimeout != 30*time.Second {
		t.Errorf("AttemptTimeout = %v; want 30s", p.AttemptTimeout)
	}
	if p.MaxAttempts != 3 {
		t.Errorf("MaxAttempts = %d; want 3", p.MaxAttempts)
	}
	if p.RequestWait != 10*time.Second {
		t.Errorf("RequestWait = %v; want 10s", p.RequestWait)
	}
	if p.BackoffBase <= 0 {
		t.Error("BackoffBase must be positive")
	}
	if p.JitterMax <= 0 {
		t.Error("JitterMax must be positive")
	}
}

func TestReasonString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		r    refresh.Reason
		want string
	}{
		{refresh.ReasonProactive, "proactive"},
		{refresh.ReasonReactive, "reactive"},
	}
	for _, tc := range tests {
		got := tc.r.String()
		if got != tc.want {
			t.Errorf("Reason.String() = %q; want %q", got, tc.want)
		}
	}
}

func TestStatusTerminal(t *testing.T) {
	t.Parallel()
	if !refresh.StatusSuccess.IsTerminal() {
		t.Error("StatusSuccess should be terminal")
	}
	if !refresh.StatusDefinitive.IsTerminal() {
		t.Error("StatusDefinitive should be terminal")
	}
	if refresh.StatusTransient.IsTerminal() {
		t.Error("StatusTransient should not be terminal")
	}
}

func TestTokenCredential(t *testing.T) {
	t.Parallel()
	cred := refresh.TokenCredential{
		AccessToken:  "test",
		RefreshToken: strPtr("rtoken"),
		ExpiresAt:    timePtr(time.Now().Add(1 * time.Hour)),
	}
	if cred.AccessToken != "test" {
		t.Errorf("got %q", cred.AccessToken)
	}
	if cred.RefreshToken == nil || *cred.RefreshToken != "rtoken" {
		t.Error("RefreshToken mismatch")
	}
	if cred.ExpiresAt == nil || cred.ExpiresAt.Before(time.Now()) {
		t.Error("ExpiresAt should be future")
	}
}

func TestResult(t *testing.T) {
	t.Parallel()
	cred := &refresh.TokenCredential{AccessToken: "new"}
	r := refresh.Result{Status: refresh.StatusSuccess, Credential: cred}
	if r.Status != refresh.StatusSuccess {
		t.Errorf("Status = %v", r.Status)
	}
	if r.Err != nil {
		t.Errorf("Err = %v", r.Err)
	}

	err := refresh.ErrDefinitive("revoked")
	r2 := refresh.Result{Status: refresh.StatusDefinitive, Err: err}
	if r2.Status != refresh.StatusDefinitive {
		t.Errorf("Status = %v", r2.Status)
	}
	if r2.Err == nil {
		t.Error("Err should be non-nil")
	}
	if r2.Credential != nil {
		t.Error("Credential should be nil")
	}
}

func strPtr(s string) *string        { return &s }
func timePtr(t time.Time) *time.Time { return &t }
