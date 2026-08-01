package repositories

import (
	"context"
	"errors"
	"time"

	"gorouter/internal/domain/oauth"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// oauthRepo implements oauth.Repository backed by PostgreSQL.
type oauthRepo struct {
	tx pgx.Tx
}

// NewOAuthRepo creates an oauth.Repository backed by the given transaction.
func NewOAuthRepo(tx pgx.Tx) oauth.Repository {
	return &oauthRepo{tx: tx}
}

func scanOAuthSession(row pgx.Row) (*oauth.Session, error) {
	s := &oauth.Session{}
	var stateP, cvP, redirectP, errDetail, tokHash, refTok, devCode, userCode, verifURI *string
	var tokExp *time.Time
	err := row.Scan(
		&s.ID, &s.ProviderID, &s.AccountID,
		&stateP, &cvP, &redirectP,
		&s.FlowID, &s.Mechanism, &s.Status,
		&errDetail, &tokHash, &refTok, &tokExp,
		&devCode, &userCode, &verifURI,
		&s.ExpiresAt, &s.CompletedAt, &s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if stateP != nil {
		s.State = *stateP
	}
	if cvP != nil {
		s.CodeVerifier = *cvP
	}
	if redirectP != nil {
		s.RedirectURI = *redirectP
	}
	s.ErrorDetail = errDetail
	s.TokenHash = tokHash
	s.RefreshToken = refTok
	s.TokenExpiry = tokExp
	s.DeviceCode = devCode
	s.UserCode = userCode
	s.VerificationURI = verifURI
	return s, nil
}

func (r *oauthRepo) Create(ctx context.Context, s *oauth.Session) error {
	_, err := r.tx.Exec(ctx, `
		INSERT INTO gorouter_oauth_sessions
			(id, provider_id, account_id, state, code_verifier, redirect_uri,
			 flow_id, mechanism, status, error_detail, token_hash, refresh_token,
			 token_expiry, device_code, user_code, verification_uri,
			 expires_at, completed_at, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)`,
		s.ID, s.ProviderID, s.AccountID,
		ptrOrNil(s.State), ptrOrNil(s.CodeVerifier), ptrOrNil(s.RedirectURI),
		string(s.FlowID), string(s.Mechanism), string(s.Status),
		s.ErrorDetail, s.TokenHash, s.RefreshToken, s.TokenExpiry,
		s.DeviceCode, s.UserCode, s.VerificationURI,
		s.ExpiresAt, s.CompletedAt, s.CreatedAt, s.UpdatedAt,
	)
	return err
}

func (r *oauthRepo) FindByID(ctx context.Context, id uuid.UUID) (*oauth.Session, error) {
	row := r.tx.QueryRow(ctx, `
		SELECT id, provider_id, account_id, state, code_verifier, redirect_uri,
		       flow_id, mechanism, status, error_detail, token_hash, refresh_token,
		       token_expiry, device_code, user_code, verification_uri,
		       expires_at, completed_at, created_at, updated_at
		FROM gorouter_oauth_sessions WHERE id = $1`, id)
	return scanOAuthSession(row)
}

func (r *oauthRepo) FindByState(ctx context.Context, state string) (*oauth.Session, error) {
	row := r.tx.QueryRow(ctx, `
		SELECT id, provider_id, account_id, state, code_verifier, redirect_uri,
		       flow_id, mechanism, status, error_detail, token_hash, refresh_token,
		       token_expiry, device_code, user_code, verification_uri,
		       expires_at, completed_at, created_at, updated_at
		FROM gorouter_oauth_sessions WHERE state = $1`, state)
	return scanOAuthSession(row)
}

func (r *oauthRepo) FindPendingByProvider(ctx context.Context, providerID uuid.UUID) ([]oauth.Session, error) {
	rows, err := r.tx.Query(ctx, `
		SELECT id, provider_id, account_id, state, code_verifier, redirect_uri,
		       flow_id, mechanism, status, error_detail, token_hash, refresh_token,
		       token_expiry, device_code, user_code, verification_uri,
		       expires_at, completed_at, created_at, updated_at
		FROM gorouter_oauth_sessions
		WHERE provider_id = $1 AND status = 'pending'`, providerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []oauth.Session
	for rows.Next() {
		s, err := scanOAuthSession(rows)
		if err != nil {
			return nil, err
		}
		if s != nil {
			out = append(out, *s)
		}
	}
	return out, rows.Err()
}

func (r *oauthRepo) UpdateStatus(ctx context.Context, id uuid.UUID, status oauth.OAuthState, errorDetail *string) error {
	_, err := r.tx.Exec(ctx, `
		UPDATE gorouter_oauth_sessions
		SET status = $1, error_detail = $2, updated_at = NOW()
		WHERE id = $3`,
		string(status), errorDetail, id)
	return err
}

func (r *oauthRepo) Complete(ctx context.Context, id uuid.UUID, tokenHash, refreshToken string, tokenExpiry time.Time) error {
	_, err := r.tx.Exec(ctx, `
		UPDATE gorouter_oauth_sessions
		SET status = 'completed', token_hash = $1, refresh_token = $2,
		    token_expiry = $3, completed_at = NOW(), updated_at = NOW()
		WHERE id = $4`,
		tokenHash, refreshToken, tokenExpiry, id)
	return err
}

func (r *oauthRepo) CancelPending(ctx context.Context) (int64, error) {
	tag, err := r.tx.Exec(ctx, `
		UPDATE gorouter_oauth_sessions
		SET status = 'cancelled', updated_at = NOW()
		WHERE status = 'pending'`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (r *oauthRepo) DeleteExpired(ctx context.Context) (int64, error) {
	tag, err := r.tx.Exec(ctx, `
		DELETE FROM gorouter_oauth_sessions
		WHERE expires_at < NOW() AND status IN ('pending','failed','cancelled')`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (r *oauthRepo) Cleanup(ctx context.Context) (int64, error) {
	tag, err := r.tx.Exec(ctx, `
		DELETE FROM gorouter_oauth_sessions
		WHERE completed_at IS NOT NULL
		  AND completed_at < NOW() - INTERVAL '24 hours'`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ptrOrNil returns a pointer to s, or nil if s is empty.
func ptrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// strP returns a pointer to s (always non-nil, even for empty strings).
func strP(s string) *string {
	return &s
}

// Compile-time interface check
var _ oauth.Repository = (*oauthRepo)(nil)
