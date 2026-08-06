// Package settings defines pure domain contracts for the configuration key-value
// store (gorouter_settings) and the typed keys that live there: the RTK toggle
// and the host feature flags. The repository contract is deliberately small
// (Get/Set/List/Wipe) and never touches keys owned by other repositories, such
// as the pricing override set.
package settings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"
)

// ErrSettingNotFound reports that no setting exists with the key.
var ErrSettingNotFound = errors.New("settings: not found")

// ErrReservedKey reports a write to a key owned by another repository (for
// example the pricing override set). Key ownership is exclusive: the settings
// repository must never read or write reserved keys.
var ErrReservedKey = errors.New("settings: key is owned by another repository")

// ReservedKeys is the set of gorouter_settings keys owned by other
// repositories. The settings repository and the settings service refuse to
// read or write them; the owning repository is the only writer.
var ReservedKeys = map[string]struct{}{
	"pricing:overrides": {},
}

// KeyRTKEnabled is the response-toolkit compression toggle. The name is the
// pinned upstream settings key (Token Saver persists rtkEnabled through
// /api/settings).
const KeyRTKEnabled = "rtkEnabled"

// HostFlagKeys enumerates the host feature flag keys. They are surfaced in the
// UI/API exactly as listed and default to disabled (design §11); every host
// operation additionally requires policy and audit at the call site.
var HostFlagKeys = []string{
	"enable_tunnel",
	"enable_tailscale",
	"enable_mitm",
	"enable_headroom",
	"enable_pxpipe",
	"enable_mcp",
	"enable_updater",
	"enable_shutdown_remote",
}

// IsHostFlagKey reports whether key is one of the host feature flags.
func IsHostFlagKey(key string) bool {
	for _, k := range HostFlagKeys {
		if key == k {
			return true
		}
	}
	return false
}

// HostFlagDefault returns the default value for a host feature flag: every
// flag defaults to disabled.
func HostFlagDefault(key string) bool {
	return false
}

// IsTypedKey reports whether key is a settings key with a typed value
// contract (the RTK toggle or a host feature flag).
func IsTypedKey(key string) bool {
	return key == KeyRTKEnabled || IsHostFlagKey(key)
}

// ValidateTypedValue enforces the typed value contract for the known keys:
// every typed key holds a JSON boolean. Unknown keys accept any JSON value.
func ValidateTypedValue(key string, raw json.RawMessage) error {
	if !IsTypedKey(key) {
		return nil
	}
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return errors.New("settings: value for " + key + " must be a JSON boolean")
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err != nil {
		return errors.New("settings: value for " + key + " must be a JSON boolean")
	}
	return nil
}

// Setting is one configuration entry. Value holds the JSONB payload; the
// description is optional. The key is immutable after creation.
type Setting struct {
	Key         string
	Value       json.RawMessage
	Description *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// SettingsRepository defines persistence operations for the configuration
// store. Wipe removes every setting except keys owned by other repositories
// (ReservedKeys); it is used by the manual configuration transfer, whose
// destructive semantics replace the current configuration.
type SettingsRepository interface {
	// Get returns the setting with the given key, or ErrSettingNotFound.
	Get(ctx context.Context, key string) (*Setting, error)
	// Set upserts the setting. The caller's Setting is never mutated.
	Set(ctx context.Context, s *Setting) error
	// List returns all settings in deterministic key order. Reserved keys
	// owned by other repositories are excluded.
	List(ctx context.Context) ([]Setting, error)
	// Wipe removes every setting except reserved keys owned by other
	// repositories. It is transactional through the enclosing scope.
	Wipe(ctx context.Context) error
}
