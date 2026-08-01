// Package cookie implements the browser cookie import flow used by Kimchi.
package cookie

import (
	"context"
	"fmt"
	"time"

	"gorouter/internal/domain/oauth"

	"github.com/google/uuid"
)

type CookieCredentials struct {
	CookieName  string
	CookieValue string
	Domain      string
	TokenHash   string
}

type Flow struct {
	repo oauth.Repository
}

func NewFlow(repo oauth.Repository) *Flow {
	return &Flow{repo: repo}
}

func (f *Flow) ImportCredentials(ctx context.Context, providerID uuid.UUID, providerName string, creds *CookieCredentials) (*oauth.Session, error) {
	now := time.Now()
	var tokenHash *string
	if creds.TokenHash != "" {
		tokenHash = &creds.TokenHash
	}
	s := &oauth.Session{
		ID:          uuid.New(),
		ProviderID:  providerID,
		FlowID:      oauth.FlowKimchi,
		Mechanism:   oauth.MechanismCookieImport,
		State:       uuid.New().String(),
		Status:      oauth.OAuthStateCompleted,
		TokenHash:   tokenHash,
		CompletedAt: &now,
		ExpiresAt:   now.Add(2 * time.Minute),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := f.repo.Create(ctx, s); err != nil {
		return nil, fmt.Errorf("create cookie session: %w", err)
	}
	return s, nil
}
