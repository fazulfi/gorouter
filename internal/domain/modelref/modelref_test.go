package modelref

import (
	"testing"
)

func TestParse_ValidFormats(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantProv string
		wantMod  string
		wantUnq  bool
	}{
		{"colon provider:model", "openai:gpt-4", "openai", "gpt-4", false},
		{"slash provider/model", "anthropic/claude-3", "anthropic", "claude-3", false},
		{"with account", "openai:acct-1:gpt-4", "openai", "gpt-4", false},
		{"with account and cap", "openai:acct-1:gpt-4:chat", "openai", "gpt-4", false},
		{"unqualified", "gpt-4", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse(%q) unexpected error: %v", tt.input, err)
			}
			if ref.Raw != tt.input {
				t.Errorf("Raw = %q, want %q", ref.Raw, tt.input)
			}
			if ref.Provider != tt.wantProv {
				t.Errorf("Provider = %q, want %q", ref.Provider, tt.wantProv)
			}
			if ref.Model != tt.wantMod {
				t.Errorf("Model = %q, want %q", ref.Model, tt.wantMod)
			}
			if got := ref.IsUnqualified(); got != tt.wantUnq {
				t.Errorf("IsUnqualified() = %v, want %v", got, tt.wantUnq)
			}
		})
	}
}

func TestParse_InvalidInputs(t *testing.T) {
	inputs := []string{"", "openai:", "openai::", "a:b:c:d:e"}
	for _, in := range inputs {
		t.Run("invalid_"+in, func(t *testing.T) {
			_, err := Parse(in)
			if err == nil {
				t.Errorf("Parse(%q) expected error", in)
			}
		})
	}
}

func TestModelRef_String(t *testing.T) {
	ref, err := Parse("openai:acct-1:gpt-4:chat")
	if err != nil {
		t.Fatal(err)
	}
	want := "openai:acct-1:gpt-4:chat"
	if got := ref.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestModelRef_String_Unqualified(t *testing.T) {
	ref, err := Parse("gpt-4")
	if err != nil {
		t.Fatal(err)
	}
	if got := ref.String(); got != "gpt-4" {
		t.Errorf("String() = %q, want %q", got, "gpt-4")
	}
}

func TestParse_WithAccount_NoAccountOpt(t *testing.T) {
	ref, err := Parse("openai:gpt-4")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Account != nil {
		t.Errorf("expected nil Account, got %v", *ref.Account)
	}
}

func TestParse_WithAccount_HasAccount(t *testing.T) {
	ref, err := Parse("openai:acct-1:gpt-4")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Account == nil {
		t.Fatal("expected non-nil Account")
	}
	if *ref.Account != "acct-1" {
		t.Errorf("Account = %q, want %q", *ref.Account, "acct-1")
	}
}

func TestParse_Capability(t *testing.T) {
	ref, err := Parse("openai:acct-1:gpt-4:embedding")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Capability == nil {
		t.Fatal("expected non-nil Capability")
	}
	if *ref.Capability != "embedding" {
		t.Errorf("Capability = %q, want %q", *ref.Capability, "embedding")
	}
}

func TestErrorVariables(t *testing.T) {
	if ErrInvalidModelRef == nil {
		t.Error("ErrInvalidModelRef must not be nil")
	}
	if ErrProviderNotFound == nil {
		t.Error("ErrProviderNotFound must not be nil")
	}
	if ErrModelNotFound == nil {
		t.Error("ErrModelNotFound must not be nil")
	}
	if ErrCapNotSupported == nil {
		t.Error("ErrCapNotSupported must not be nil")
	}
	if ErrNoAccountsAvail == nil {
		t.Error("ErrNoAccountsAvail must not be nil")
	}
}
