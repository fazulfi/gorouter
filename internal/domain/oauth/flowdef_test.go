package oauth

import (
	"testing"
)

func TestFlowDefinitions(t *testing.T) {
	fds := FlowDefinitions()
	if len(fds) != 20 {
		t.Fatalf("expected 20 flow definitions, got %d", len(fds))
	}

	// Verify mechanism distribution
	var pkceCount, authCodeCount, deviceCodeCount, importCount int
	for _, fd := range fds {
		switch fd.Mechanism {
		case MechanismAuthCodePKCE:
			pkceCount++
		case MechanismAuthCode:
			authCodeCount++
		case MechanismDevicePKCE:
			pkceCount++
			deviceCodeCount++
		case MechanismDeviceCode:
			deviceCodeCount++
		case MechanismCookieImport, MechanismTokenImport:
			importCount++
		}
	}
	if pkceCount != 7 {
		t.Errorf("expected 7 PKCE flows, got %d", pkceCount)
	}
	if authCodeCount != 5 {
		t.Errorf("expected 5 non-PKCE auth-code flows, got %d", authCodeCount)
	}
	if deviceCodeCount != 8 {
		t.Errorf("expected 8 device-code flows, got %d", deviceCodeCount)
	}
}

func TestLookupFlow(t *testing.T) {
	fd := LookupFlow(FlowCodex)
	if fd == nil {
		t.Fatal("expected codex flow definition")
	}
	if fd.FixedPort != 1455 {
		t.Errorf("expected fixed port 1455, got %d", fd.FixedPort)
	}
	if !fd.PKCE {
		t.Error("expected PKCE=true for codex")
	}

	fd = LookupFlow(FlowXAI)
	if fd == nil {
		t.Fatal("expected xai flow definition")
	}
	if fd.FixedPort != 56121 {
		t.Errorf("expected fixed port 56121, got %d", fd.FixedPort)
	}

	fd = LookupFlow("nonexistent")
	if fd != nil {
		t.Error("expected nil for nonexistent flow")
	}
}

func TestProviderFlow(t *testing.T) {
	fd := ProviderFlow("claude")
	if fd == nil {
		t.Fatal("expected flow for claude provider")
	}
	if fd.FlowID != FlowClaude {
		t.Errorf("expected flow claude, got %s", fd.FlowID)
	}

	fd = ProviderFlow("xai")
	if fd == nil {
		t.Fatal("expected flow for xai provider")
	}
	if fd.FlowID != FlowXAI {
		t.Errorf("expected flow xai, got %s", fd.FlowID)
	}

	fd = ProviderFlow("unknown-provider")
	if fd != nil {
		t.Error("expected nil for unknown provider")
	}
}

func TestSessionIsTerminal(t *testing.T) {
	tests := []struct {
		state    OAuthState
		terminal bool
	}{
		{OAuthStatePending, false},
		{OAuthStateCompleted, true},
		{OAuthStateFailed, true},
		{OAuthStateCancelled, true},
		{OAuthStateExpired, true},
	}
	for _, tt := range tests {
		s := &Session{Status: tt.state}
		if got := s.IsTerminal(); got != tt.terminal {
			t.Errorf("IsTerminal(%s) = %v, want %v", tt.state, got, tt.terminal)
		}
	}
}
