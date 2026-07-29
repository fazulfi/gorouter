package repositories

import (
	"context"
	"errors"
	"net"

	"gorouter/internal/domain/auth"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// userRepo implements auth.UserRepository backed by pgx.
type userRepo struct {
	tx pgx.Tx
}

func (r *userRepo) FindByID(ctx context.Context, id uuid.UUID) (*auth.User, error) {
	row := r.tx.QueryRow(ctx,
		`SELECT id, email, password_hash, display_name, is_admin, is_active, last_login_at, created_at, updated_at
		 FROM users WHERE id = $1`, id)
	u := &auth.User{}
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.DisplayName,
		&u.IsAdmin, &u.IsActive, &u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return u, nil
}

func (r *userRepo) FindByEmail(ctx context.Context, email string) (*auth.User, error) {
	row := r.tx.QueryRow(ctx,
		`SELECT id, email, password_hash, display_name, is_admin, is_active, last_login_at, created_at, updated_at
		 FROM users WHERE email = $1`, email)
	u := &auth.User{}
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.DisplayName,
		&u.IsAdmin, &u.IsActive, &u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return u, nil
}

func (r *userRepo) Create(ctx context.Context, user *auth.User) error {
	_, err := r.tx.Exec(ctx,
		`INSERT INTO users (id, email, password_hash, display_name, is_admin, is_active, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		user.ID, user.Email, user.PasswordHash, user.DisplayName,
		user.IsAdmin, user.IsActive, user.CreatedAt, user.UpdatedAt)
	return err
}

func (r *userRepo) Update(ctx context.Context, user *auth.User) error {
	_, err := r.tx.Exec(ctx,
		`UPDATE users SET email=$1, password_hash=$2, display_name=$3, is_admin=$4, is_active=$5, updated_at=$6
		 WHERE id=$7`,
		user.Email, user.PasswordHash, user.DisplayName,
		user.IsAdmin, user.IsActive, user.UpdatedAt, user.ID)
	return err
}

func (r *userRepo) UpdateLastLogin(ctx context.Context, id uuid.UUID) error {
	_, err := r.tx.Exec(ctx, `UPDATE users SET last_login_at = NOW() WHERE id = $1`, id)
	return err
}

// sessionRepo implements auth.SessionRepository backed by pgx.
type sessionRepo struct {
	tx pgx.Tx
}

func (r *sessionRepo) FindByID(ctx context.Context, id uuid.UUID) (*auth.Session, error) {
	row := r.tx.QueryRow(ctx,
		`SELECT id, user_id, token_hash, ip_address, user_agent, expires_at, revoked_at, created_at
		 FROM sessions WHERE id = $1`, id)
	s := &auth.Session{}
	var ipBytes []byte
	err := row.Scan(&s.ID, &s.UserID, &s.TokenHash, &ipBytes, &s.UserAgent,
		&s.ExpiresAt, &s.RevokedAt, &s.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	s.IPAddress = net.IP(ipBytes)
	return s, nil
}

func (r *sessionRepo) FindByTokenHash(ctx context.Context, hash string) (*auth.Session, error) {
	row := r.tx.QueryRow(ctx,
		`SELECT id, user_id, token_hash, ip_address, user_agent, expires_at, revoked_at, created_at
		 FROM sessions WHERE token_hash = $1`, hash)
	s := &auth.Session{}
	var ipBytes []byte
	err := row.Scan(&s.ID, &s.UserID, &s.TokenHash, &ipBytes, &s.UserAgent,
		&s.ExpiresAt, &s.RevokedAt, &s.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	s.IPAddress = net.IP(ipBytes)
	return s, nil
}

func (r *sessionRepo) Create(ctx context.Context, session *auth.Session) error {
	ipBytes, _ := session.IPAddress.MarshalText()
	_, err := r.tx.Exec(ctx,
		`INSERT INTO sessions (id, user_id, token_hash, ip_address, user_agent, expires_at, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		session.ID, session.UserID, session.TokenHash, ipBytes,
		session.UserAgent, session.ExpiresAt, session.CreatedAt)
	return err
}

func (r *sessionRepo) Revoke(ctx context.Context, id uuid.UUID) error {
	_, err := r.tx.Exec(ctx, `UPDATE sessions SET revoked_at = NOW() WHERE id = $1`, id)
	return err
}

func (r *sessionRepo) DeleteExpired(ctx context.Context) error {
	_, err := r.tx.Exec(ctx, `DELETE FROM sessions WHERE expires_at < NOW() AND revoked_at IS NULL`)
	return err
}
