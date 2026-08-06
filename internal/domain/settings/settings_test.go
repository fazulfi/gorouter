package settings

import (
	"encoding/json"
	"errors"
	"testing"
)

// TestTypedKeyContracts pins the exact typed settings keys: the RTK toggle and
// the eight host feature flags, all defaulting to disabled.
func TestTypedKeyContracts(t *testing.T) {
	if KeyRTKEnabled != "rtkEnabled" {
		t.Fatalf("KeyRTKEnabled = %q, want rtkEnabled", KeyRTKEnabled)
	}
	wantFlags := []string{
		"enable_tunnel",
		"enable_tailscale",
		"enable_mitm",
		"enable_headroom",
		"enable_pxpipe",
		"enable_mcp",
		"enable_updater",
		"enable_shutdown_remote",
	}
	if len(HostFlagKeys) != len(wantFlags) {
		t.Fatalf("HostFlagKeys has %d entries, want %d", len(HostFlagKeys), len(wantFlags))
	}
	for i, want := range wantFlags {
		if HostFlagKeys[i] != want {
			t.Errorf("HostFlagKeys[%d] = %q, want %q", i, HostFlagKeys[i], want)
		}
		if !IsHostFlagKey(want) {
			t.Errorf("IsHostFlagKey(%q) = false, want true", want)
		}
		if HostFlagDefault(want) {
			t.Errorf("HostFlagDefault(%q) = true, want false", want)
		}
	}
	for _, k := range []string{"enable_tunnel", "enable_pxpipe"} {
		if !IsTypedKey(k) {
			t.Errorf("IsTypedKey(%q) = false, want true", k)
		}
	}
	if IsHostFlagKey("enable_tunnels") || IsHostFlagKey("") {
		t.Error("unknown keys must not be host flag keys")
	}
	if IsTypedKey("arbitrary") {
		t.Error("unknown keys must not be typed keys")
	}
}

// TestValidateTypedValue enforces the boolean contract for typed keys and free
// JSON for unknown keys.
func TestValidateTypedValue(t *testing.T) {
	if err := ValidateTypedValue(KeyRTKEnabled, json.RawMessage(`true`)); err != nil {
		t.Errorf("rtkEnabled true: %v", err)
	}
	if err := ValidateTypedValue(KeyRTKEnabled, json.RawMessage(`false`)); err != nil {
		t.Errorf("rtkEnabled false: %v", err)
	}
	if err := ValidateTypedValue(KeyRTKEnabled, json.RawMessage(`"yes"`)); err == nil {
		t.Error("rtkEnabled string accepted, want error")
	}
	if err := ValidateTypedValue(KeyRTKEnabled, json.RawMessage(`1`)); err == nil {
		t.Error("rtkEnabled number accepted, want error")
	}
	if err := ValidateTypedValue("enable_tunnel", json.RawMessage(`false`)); err != nil {
		t.Errorf("host flag false: %v", err)
	}
	if err := ValidateTypedValue("enable_tunnel", json.RawMessage(`null`)); err == nil {
		t.Error("host flag null accepted, want error")
	}
	if err := ValidateTypedValue("arbitrary", json.RawMessage(`{"a":1}`)); err != nil {
		t.Errorf("unknown key object: %v", err)
	}
}

// TestReservedKeysPinsPricingOwnership: the pricing override key is owned by
// the pricing repository; the settings domain never lists it as a settings
// key and refuses settings writes to it.
func TestReservedKeysPinsPricingOwnership(t *testing.T) {
	if _, ok := ReservedKeys["pricing:overrides"]; !ok {
		t.Fatal("pricing:overrides must be reserved for the pricing repository")
	}
	if len(ReservedKeys) != 1 {
		t.Errorf("ReservedKeys = %v, want exactly pricing:overrides", ReservedKeys)
	}
}

// TestSentinelErrors pins the exported sentinel identity.
func TestSentinelErrors(t *testing.T) {
	if !errors.Is(ErrSettingNotFound, ErrSettingNotFound) {
		t.Fatal("ErrSettingNotFound must be a stable sentinel")
	}
	if !errors.Is(ErrReservedKey, ErrReservedKey) {
		t.Fatal("ErrReservedKey must be a stable sentinel")
	}
	if ErrSettingNotFound == ErrReservedKey {
		t.Fatal("sentinels must be distinct")
	}
}
