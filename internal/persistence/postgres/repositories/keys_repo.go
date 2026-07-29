package repositories

import (
	"context"
	"errors"

	"gorouter/internal/domain/keys"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// apiKeyRepo implements keys.APIKeyRepository backed by pgx.
type apiKeyRepo struct {
	tx pgx.Tx
}

func (r *apiKeyRepo) FindByID(ctx context.Context, id uuid.UUID) (*keys.APIKey, error) {
	row := r.tx.QueryRow(ctx,
		`SELECT id, user_id, key_prefix, key_hash, name, scopes, expires_at, last_used_at, revoked_at, created_at
		 FROM api_keys WHERE id = $1`, id)
	k := &keys.APIKey{}
	err := row.Scan(&k.ID, &k.UserID, &k.KeyPrefix, &k.KeyHash, &k.Name, &k.Scopes,
		&k.ExpiresAt, &k.LastUsedAt, &k.RevokedAt, &k.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return k, nil
}

func (r *apiKeyRepo) FindByHash(ctx context.Context, hash string) (*keys.APIKey, error) {
	row := r.tx.QueryRow(ctx,
		`SELECT id, user_id, key_prefix, key_hash, name, scopes, expires_at, last_used_at, revoked_at, created_at
		 FROM api_keys WHERE key_hash = $1`, hash)
	k := &keys.APIKey{}
	err := row.Scan(&k.ID, &k.UserID, &k.KeyPrefix, &k.KeyHash, &k.Name, &k.Scopes,
		&k.ExpiresAt, &k.LastUsedAt, &k.RevokedAt, &k.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return k, nil
}

func (r *apiKeyRepo) FindByUserID(ctx context.Context, userID uuid.UUID) ([]keys.APIKey, error) {
	rows, err := r.tx.Query(ctx,
		`SELECT id, user_id, key_prefix, key_hash, name, scopes, expires_at, last_used_at, revoked_at, created_at
		 FROM api_keys WHERE user_id = $1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []keys.APIKey
	for rows.Next() {
		var k keys.APIKey
		if err := rows.Scan(&k.ID, &k.UserID, &k.KeyPrefix, &k.KeyHash, &k.Name, &k.Scopes,
			&k.ExpiresAt, &k.LastUsedAt, &k.RevokedAt, &k.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (r *apiKeyRepo) Create(ctx context.Context, key *keys.APIKey) error {
	_, err := r.tx.Exec(ctx,
		`INSERT INTO api_keys (id, user_id, key_prefix, key_hash, name, scopes, expires_at, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		key.ID, key.UserID, key.KeyPrefix, key.KeyHash, key.Name, key.Scopes,
		key.ExpiresAt, key.CreatedAt)
	return err
}

func (r *apiKeyRepo) Revoke(ctx context.Context, id uuid.UUID) error {
	_, err := r.tx.Exec(ctx, `UPDATE api_keys SET revoked_at = NOW() WHERE id = $1`, id)
	return err
}

// patRepo implements keys.PATRepository backed by pgx.
type patRepo struct {
	tx pgx.Tx
}

func (r *patRepo) FindByID(ctx context.Context, id uuid.UUID) (*keys.PAT, error) {
	row := r.tx.QueryRow(ctx,
		`SELECT id, user_id, token_hash, description, expires_at, last_used_at, revoked_at, created_at
		 FROM personal_access_tokens WHERE id = $1`, id)
	p := &keys.PAT{}
	err := row.Scan(&p.ID, &p.UserID, &p.TokenHash, &p.Description,
		&p.ExpiresAt, &p.LastUsedAt, &p.RevokedAt, &p.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return p, nil
}

func (r *patRepo) FindByHash(ctx context.Context, hash string) (*keys.PAT, error) {
	row := r.tx.QueryRow(ctx,
		`SELECT id, user_id, token_hash, description, expires_at, last_used_at, revoked_at, created_at
		 FROM personal_access_tokens WHERE token_hash = $1`, hash)
	p := &keys.PAT{}
	err := row.Scan(&p.ID, &p.UserID, &p.TokenHash, &p.Description,
		&p.ExpiresAt, &p.LastUsedAt, &p.RevokedAt, &p.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return p, nil
}

func (r *patRepo) FindByUserID(ctx context.Context, userID uuid.UUID) ([]keys.PAT, error) {
	rows, err := r.tx.Query(ctx,
		`SELECT id, user_id, token_hash, description, expires_at, last_used_at, revoked_at, created_at
		 FROM personal_access_tokens WHERE user_id = $1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []keys.PAT
	for rows.Next() {
		var p keys.PAT
		if err := rows.Scan(&p.ID, &p.UserID, &p.TokenHash, &p.Description,
			&p.ExpiresAt, &p.LastUsedAt, &p.RevokedAt, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *patRepo) Create(ctx context.Context, pat *keys.PAT) error {
	_, err := r.tx.Exec(ctx,
		`INSERT INTO personal_access_tokens (id, user_id, token_hash, description, expires_at, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		pat.ID, pat.UserID, pat.TokenHash, pat.Description, pat.ExpiresAt, pat.CreatedAt)
	return err
}

func (r *patRepo) Revoke(ctx context.Context, id uuid.UUID) error {
	_, err := r.tx.Exec(ctx, `UPDATE personal_access_tokens SET revoked_at = NOW() WHERE id = $1`, id)
	return err
}
