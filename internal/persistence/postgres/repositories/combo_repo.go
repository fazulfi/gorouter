package repositories

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorouter/internal/domain/combo"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// comboRepo implements combo.Repository against the P3-T06 migration tables
// gorouter_combo_definitions and gorouter_combo_members.
type comboRepo struct {
	tx pgx.Tx
}

// NewComboRepo creates a combo.Repository backed by the given transaction.
func NewComboRepo(tx pgx.Tx) combo.Repository {
	return &comboRepo{tx: tx}
}

const comboDefColumns = `id, name, strategy, config, is_active, created_at, updated_at`

func scanComboDefinition(row pgx.Row) (*combo.Definition, error) {
	d := &combo.Definition{}
	var cfgBytes []byte
	err := row.Scan(&d.ID, &d.Name, &d.Strategy, &cfgBytes, &d.IsActive, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	d.Config = json.RawMessage(cfgBytes)
	return d, nil
}

func (r *comboRepo) Create(ctx context.Context, d *combo.Definition) error {
	_, err := r.tx.Exec(ctx, `
		INSERT INTO gorouter_combo_definitions
			(id, name, strategy, config, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		d.ID, d.Name, string(d.Strategy), []byte(d.Config), d.IsActive, d.CreatedAt, d.UpdatedAt)
	return err
}

func (r *comboRepo) FindByID(ctx context.Context, id uuid.UUID) (*combo.Definition, error) {
	row := r.tx.QueryRow(ctx, `
		SELECT `+comboDefColumns+`
		FROM gorouter_combo_definitions WHERE id = $1`, id)
	return scanComboDefinition(row)
}

func (r *comboRepo) FindByName(ctx context.Context, name string) (*combo.Definition, error) {
	row := r.tx.QueryRow(ctx, `
		SELECT `+comboDefColumns+`
		FROM gorouter_combo_definitions WHERE name = $1`, name)
	return scanComboDefinition(row)
}

func (r *comboRepo) List(ctx context.Context) ([]combo.Definition, error) {
	rows, err := r.tx.Query(ctx, `
		SELECT `+comboDefColumns+`
		FROM gorouter_combo_definitions ORDER BY created_at ASC, name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []combo.Definition
	for rows.Next() {
		d, err := scanComboDefinition(rows)
		if err != nil {
			return nil, err
		}
		if d != nil {
			out = append(out, *d)
		}
	}
	return out, rows.Err()
}

func (r *comboRepo) Update(ctx context.Context, d *combo.Definition) error {
	_, err := r.tx.Exec(ctx, `
		UPDATE gorouter_combo_definitions
		SET name = $1, strategy = $2, config = $3, is_active = $4, updated_at = $5
		WHERE id = $6`,
		d.Name, string(d.Strategy), []byte(d.Config), d.IsActive, d.UpdatedAt, d.ID)
	return err
}

func (r *comboRepo) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.tx.Exec(ctx, `DELETE FROM gorouter_combo_definitions WHERE id = $1`, id)
	return err
}

func (r *comboRepo) SetActive(ctx context.Context, id uuid.UUID, active bool) error {
	_, err := r.tx.Exec(ctx, `
		UPDATE gorouter_combo_definitions
		SET is_active = $1, updated_at = NOW()
		WHERE id = $2`, active, id)
	return err
}

const comboMemberColumns = `id, combo_id, provider_id, account_id, model_ref, priority, weight, is_active, created_at, updated_at`

func scanComboMember(row pgx.Row) (*combo.Member, error) {
	m := &combo.Member{}
	err := row.Scan(&m.ID, &m.ComboID, &m.ProviderID, &m.AccountID, &m.ModelRef,
		&m.Priority, &m.Weight, &m.IsActive, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return m, nil
}

func (r *comboRepo) ListMembers(ctx context.Context, comboID uuid.UUID) ([]combo.Member, error) {
	rows, err := r.tx.Query(ctx, `
		SELECT `+comboMemberColumns+`
		FROM gorouter_combo_members
		WHERE combo_id = $1
		ORDER BY priority ASC, weight DESC, created_at ASC`, comboID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []combo.Member
	for rows.Next() {
		m, err := scanComboMember(rows)
		if err != nil {
			return nil, err
		}
		if m != nil {
			out = append(out, *m)
		}
	}
	return out, rows.Err()
}

// UpsertMembers replaces the member set of a combo: members not present in the
// given set are deleted, present members are inserted or updated by ID.
func (r *comboRepo) UpsertMembers(ctx context.Context, comboID uuid.UUID, members []combo.Member) error {
	if len(members) == 0 {
		_, err := r.tx.Exec(ctx, `DELETE FROM gorouter_combo_members WHERE combo_id = $1`, comboID)
		return err
	}
	ids := make([]uuid.UUID, len(members))
	for i, m := range members {
		ids[i] = m.ID
	}
	if _, err := r.tx.Exec(ctx, `
		DELETE FROM gorouter_combo_members
		WHERE combo_id = $1 AND NOT (id = ANY($2::uuid[]))`, comboID, ids); err != nil {
		return err
	}
	for _, m := range members {
		if _, err := r.tx.Exec(ctx, `
			INSERT INTO gorouter_combo_members
				(id, combo_id, provider_id, account_id, model_ref, priority, weight, is_active, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT (id) DO UPDATE SET
				combo_id = $2, provider_id = $3, account_id = $4, model_ref = $5,
				priority = $6, weight = $7, is_active = $8, updated_at = $10`,
			m.ID, m.ComboID, m.ProviderID, m.AccountID, m.ModelRef,
			m.Priority, m.Weight, m.IsActive, m.CreatedAt, m.UpdatedAt); err != nil {
			return err
		}
	}
	return nil
}

func (r *comboRepo) DeleteMember(ctx context.Context, memberID uuid.UUID) error {
	_, err := r.tx.Exec(ctx, `DELETE FROM gorouter_combo_members WHERE id = $1`, memberID)
	return err
}

// runtimeStateRepo implements combo.RuntimeState against the approved
// gorouter_runtime_state table (P3-T06). No new tables are introduced.
type runtimeStateRepo struct {
	tx pgx.Tx
}

// NewRuntimeStateRepo creates a combo.RuntimeState backed by the given
// transaction.
func NewRuntimeStateRepo(tx pgx.Tx) combo.RuntimeState {
	return &runtimeStateRepo{tx: tx}
}

func (r *runtimeStateRepo) Get(ctx context.Context, key string) (json.RawMessage, error) {
	var value []byte
	err := r.tx.QueryRow(ctx, `
		SELECT value FROM gorouter_runtime_state
		WHERE key = $1 AND (ttl IS NULL OR ttl > clock_timestamp())`, key).Scan(&value)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return json.RawMessage(value), nil
}

func (r *runtimeStateRepo) Set(ctx context.Context, key string, value json.RawMessage, ttl time.Duration) error {
	var expiry *time.Time
	if ttl > 0 {
		t := time.Now().UTC().Add(ttl)
		expiry = &t
	}
	_, err := r.tx.Exec(ctx, `
		INSERT INTO gorouter_runtime_state (key, value, ttl, created_at, updated_at)
		VALUES ($1, $2, $3, NOW(), NOW())
		ON CONFLICT (key) DO UPDATE SET value = $2, ttl = $3, updated_at = NOW()`,
		key, []byte(value), expiry)
	return err
}

// Compile-time interface checks.
var (
	_ combo.Repository   = (*comboRepo)(nil)
	_ combo.RuntimeState = (*runtimeStateRepo)(nil)
)
