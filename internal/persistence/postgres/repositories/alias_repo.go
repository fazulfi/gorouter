package repositories

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	enginerouting "gorouter/internal/engine/routing"
)

// aliasRepo implements enginerouting.AliasRepository against the
// gorouter_model_aliases table (000004_engine_matrix.up.sql).
type aliasRepo struct {
	tx pgx.Tx
}

// NewAliasRepo creates an enginerouting.AliasRepository backed by the given
// transaction.
func NewAliasRepo(tx pgx.Tx) enginerouting.AliasRepository {
	return &aliasRepo{tx: tx}
}

const aliasColumns = "id, alias, target, provider_id, is_active, created_at, updated_at"

func scanAlias(row pgx.Row) (*enginerouting.Alias, error) {
	a := &enginerouting.Alias{}
	err := row.Scan(&a.ID, &a.Alias, &a.Target, &a.ProviderID, &a.IsActive,
		&a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return a, nil
}

func (r *aliasRepo) Create(ctx context.Context, a *enginerouting.Alias) error {
	_, err := r.tx.Exec(ctx, `
		INSERT INTO gorouter_model_aliases
			(id, alias, target, provider_id, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		a.ID, a.Alias, a.Target, a.ProviderID, a.IsActive, a.CreatedAt, a.UpdatedAt)
	if err != nil {
		return mapUniqueViolation(err)
	}
	return nil
}

func (r *aliasRepo) Update(ctx context.Context, a *enginerouting.Alias) error {
	_, err := r.tx.Exec(ctx, `
		UPDATE gorouter_model_aliases
		SET alias = $1, target = $2, provider_id = $3, is_active = $4, updated_at = $5
		WHERE id = $6`,
		a.Alias, a.Target, a.ProviderID, a.IsActive, a.UpdatedAt, a.ID)
	if err != nil {
		return mapUniqueViolation(err)
	}
	return nil
}

func (r *aliasRepo) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.tx.Exec(ctx, `DELETE FROM gorouter_model_aliases WHERE id = $1`, id)
	return err
}

func (r *aliasRepo) FindByAlias(ctx context.Context, alias string) (*enginerouting.Alias, error) {
	row := r.tx.QueryRow(ctx,
		`SELECT `+aliasColumns+` FROM gorouter_model_aliases WHERE alias = $1`, alias)
	return scanAlias(row)
}

func (r *aliasRepo) FindByID(ctx context.Context, id uuid.UUID) (*enginerouting.Alias, error) {
	row := r.tx.QueryRow(ctx,
		`SELECT `+aliasColumns+` FROM gorouter_model_aliases WHERE id = $1`, id)
	return scanAlias(row)
}

func (r *aliasRepo) List(ctx context.Context) ([]enginerouting.Alias, error) {
	rows, err := r.tx.Query(ctx,
		`SELECT `+aliasColumns+` FROM gorouter_model_aliases
		 ORDER BY created_at ASC, alias ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAliasRows(rows)
}

func (r *aliasRepo) ListActive(ctx context.Context) ([]enginerouting.Alias, error) {
	rows, err := r.tx.Query(ctx,
		`SELECT `+aliasColumns+` FROM gorouter_model_aliases
		 WHERE is_active = true ORDER BY created_at ASC, alias ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAliasRows(rows)
}

func (r *aliasRepo) SetActive(ctx context.Context, id uuid.UUID, active bool) error {
	_, err := r.tx.Exec(ctx, `
		UPDATE gorouter_model_aliases
		SET is_active = $1, updated_at = NOW()
		WHERE id = $2`, active, id)
	return err
}

func scanAliasRows(rows pgx.Rows) ([]enginerouting.Alias, error) {
	var out []enginerouting.Alias
	for rows.Next() {
		var a enginerouting.Alias
		if err := rows.Scan(&a.ID, &a.Alias, &a.Target, &a.ProviderID, &a.IsActive,
			&a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// mapUniqueViolation maps a PostgreSQL unique-violation (23505) to
// enginerouting.ErrAliasExists.
func mapUniqueViolation(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return enginerouting.ErrAliasExists
	}
	return err
}

// catalogRepo implements enginerouting.CatalogStore against the
// gorouter_provider_models table (000002_engine.up.sql). Reconciliation
// deletes are guarded to builtin+enabled rows so discovery never removes
// custom or admin-disabled models (decision #192).
type catalogRepo struct {
	tx pgx.Tx
}

// NewCatalogRepo creates an enginerouting.CatalogStore backed by the given
// transaction.
func NewCatalogRepo(tx pgx.Tx) enginerouting.CatalogStore {
	return &catalogRepo{tx: tx}
}

const catalogColumns = `id, provider_id, model_id, display_name, capabilities, max_tokens, is_builtin, is_enabled, created_at, updated_at`

func scanCatalogModel(row pgx.Row) (*enginerouting.CatalogModel, error) {
	m := &enginerouting.CatalogModel{}
	var displayName *string
	var maxTokens *int64
	err := row.Scan(&m.ID, &m.ProviderID, &m.ModelName, &displayName,
		&m.Capabilities, &maxTokens, &m.IsBuiltin, &m.IsEnabled,
		&m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if displayName != nil {
		m.DisplayName = *displayName
	}
	if maxTokens != nil {
		m.MaxTokens = *maxTokens
	}
	return m, nil
}

func (r *catalogRepo) ListAll(ctx context.Context) ([]enginerouting.CatalogModel, error) {
	rows, err := r.tx.Query(ctx,
		`SELECT `+catalogColumns+` FROM gorouter_provider_models
		 ORDER BY provider_id, model_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCatalogRows(rows)
}

func (r *catalogRepo) ListByProvider(ctx context.Context, providerID uuid.UUID) ([]enginerouting.CatalogModel, error) {
	rows, err := r.tx.Query(ctx,
		`SELECT `+catalogColumns+` FROM gorouter_provider_models
		 WHERE provider_id = $1 ORDER BY model_id`, providerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCatalogRows(rows)
}

func (r *catalogRepo) GetByProviderModel(ctx context.Context, providerID uuid.UUID, modelName string) (*enginerouting.CatalogModel, error) {
	row := r.tx.QueryRow(ctx,
		`SELECT `+catalogColumns+` FROM gorouter_provider_models
		 WHERE provider_id = $1 AND model_id = $2`, providerID, modelName)
	return scanCatalogModel(row)
}

func (r *catalogRepo) UpsertDiscovered(ctx context.Context, m enginerouting.CatalogModel) error {
	displayName := stringPtr(m.DisplayName)
	_, err := r.tx.Exec(ctx, `
		INSERT INTO gorouter_provider_models
			(id, provider_id, model_id, display_name, capabilities, max_tokens,
			 is_builtin, is_enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, true, true, NOW(), NOW())
		ON CONFLICT (provider_id, model_id) DO UPDATE SET
			display_name = EXCLUDED.display_name,
			capabilities = EXCLUDED.capabilities,
			max_tokens = EXCLUDED.max_tokens,
			updated_at = NOW()`,
		uuidOrNew(m.ID), m.ProviderID, m.ModelName, displayName, m.Capabilities, m.MaxTokens)
	return err
}

func (r *catalogRepo) InsertCustom(ctx context.Context, m *enginerouting.CatalogModel) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	_, err := r.tx.Exec(ctx, `
		INSERT INTO gorouter_provider_models
			(id, provider_id, model_id, display_name, capabilities, max_tokens,
			 is_builtin, is_enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, false, $7, NOW(), NOW())`,
		m.ID, m.ProviderID, m.ModelName, stringPtr(m.DisplayName),
		m.Capabilities, m.MaxTokens, m.IsEnabled)
	return err
}

func (r *catalogRepo) Update(ctx context.Context, m *enginerouting.CatalogModel) error {
	_, err := r.tx.Exec(ctx, `
		UPDATE gorouter_provider_models
		SET model_id = $1, display_name = $2, capabilities = $3, max_tokens = $4,
		    is_builtin = $5, is_enabled = $6, updated_at = NOW()
		WHERE id = $7`,
		m.ModelName, stringPtr(m.DisplayName), m.Capabilities, m.MaxTokens,
		m.IsBuiltin, m.IsEnabled, m.ID)
	return err
}

func (r *catalogRepo) SetEnabled(ctx context.Context, id uuid.UUID, enabled bool) error {
	_, err := r.tx.Exec(ctx, `
		UPDATE gorouter_provider_models
		SET is_enabled = $1, updated_at = NOW()
		WHERE id = $2`, enabled, id)
	return err
}

// DeleteStale removes a model only when it is a builtin enabled row — the
// only class discovery may remove (decision #192).
func (r *catalogRepo) DeleteStale(ctx context.Context, id uuid.UUID) error {
	_, err := r.tx.Exec(ctx, `
		DELETE FROM gorouter_provider_models
		WHERE id = $1 AND is_builtin = true AND is_enabled = true`, id)
	return err
}

// Delete removes any catalog row (explicit admin deletion, decision #192).
func (r *catalogRepo) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.tx.Exec(ctx, `DELETE FROM gorouter_provider_models WHERE id = $1`, id)
	return err
}

func scanCatalogRows(rows pgx.Rows) ([]enginerouting.CatalogModel, error) {
	var out []enginerouting.CatalogModel
	for rows.Next() {
		m, err := scanCatalogModel(rows)
		if err != nil {
			return nil, err
		}
		if m != nil {
			out = append(out, *m)
		}
	}
	return out, rows.Err()
}

func uuidOrNew(id uuid.UUID) uuid.UUID {
	if id == uuid.Nil {
		return uuid.New()
	}
	return id
}

func stringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
