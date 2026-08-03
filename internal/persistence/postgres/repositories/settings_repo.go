package repositories

import (
	"context"
	"encoding/json"
	"errors"
	"sort"

	"gorouter/internal/domain/settings"

	"github.com/jackc/pgx/v5"
)

// settingsRepo implements settings.SettingsRepository against the existing
// gorouter_settings KV table. Key ownership is exclusive: the pricing
// override set (pricing:overrides) belongs to the pricing repository and is
// never read or written here.
type settingsRepo struct {
	tx pgx.Tx
}

// NewSettingsRepo creates a settings.SettingsRepository backed by the given
// transaction.
func NewSettingsRepo(tx pgx.Tx) settings.SettingsRepository {
	return &settingsRepo{tx: tx}
}

// reservedKeysParam returns the reserved keys in deterministic order for the
// <> ALL(...) exclusion clauses.
func reservedKeysParam() []string {
	keys := make([]string, 0, len(settings.ReservedKeys))
	for k := range settings.ReservedKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (r *settingsRepo) Get(ctx context.Context, key string) (*settings.Setting, error) {
	if _, reserved := settings.ReservedKeys[key]; reserved {
		return nil, settings.ErrReservedKey
	}
	var s settings.Setting
	var valueBytes []byte
	err := r.tx.QueryRow(ctx,
		`SELECT key, value, description, created_at, updated_at
		 FROM gorouter_settings WHERE key = $1`, key).
		Scan(&s.Key, &valueBytes, &s.Description, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, settings.ErrSettingNotFound
		}
		return nil, err
	}
	s.Value = append(json.RawMessage(nil), valueBytes...)
	return &s, nil
}

func (r *settingsRepo) Set(ctx context.Context, s *settings.Setting) error {
	if s == nil {
		return errors.New("settings: nil setting")
	}
	if _, reserved := settings.ReservedKeys[s.Key]; reserved {
		return settings.ErrReservedKey
	}
	valueBytes := []byte(s.Value)
	if s.Value == nil {
		valueBytes = []byte("null")
	}
	_, err := r.tx.Exec(ctx,
		`INSERT INTO gorouter_settings (key, value, description, created_at, updated_at)
		 VALUES ($1, $2, $3, NOW(), NOW())
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, description = EXCLUDED.description, updated_at = NOW()`,
		s.Key, valueBytes, s.Description)
	return err
}

func (r *settingsRepo) List(ctx context.Context) ([]settings.Setting, error) {
	rows, err := r.tx.Query(ctx,
		`SELECT key, value, description, created_at, updated_at
		 FROM gorouter_settings WHERE key <> ALL($1::varchar[])
		 ORDER BY key`, reservedKeysParam())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]settings.Setting, 0)
	for rows.Next() {
		var s settings.Setting
		var valueBytes []byte
		if err := rows.Scan(&s.Key, &valueBytes, &s.Description, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		s.Value = append(json.RawMessage(nil), valueBytes...)
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *settingsRepo) Wipe(ctx context.Context) error {
	_, err := r.tx.Exec(ctx,
		`DELETE FROM gorouter_settings WHERE key <> ALL($1::varchar[])`,
		reservedKeysParam())
	return err
}

// Compile-time interface check.
var _ settings.SettingsRepository = (*settingsRepo)(nil)
