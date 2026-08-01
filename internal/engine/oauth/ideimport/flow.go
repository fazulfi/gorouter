// Package ideimport implements the IDE/local credential discovery flow.
package ideimport

import (
	"context"
	"fmt"
	"time"

	"gorouter/internal/domain/oauth"

	"github.com/google/uuid"
)

type IDECredentials struct {
	Source       string
	Token        string
	TokenHash    string
	RefreshToken string
	ExpiresAt    *time.Time
}

type Flow struct {
	repo oauth.Repository
}

func NewFlow(repo oauth.Repository) *Flow {
	return &Flow{repo: repo}
}

func (f *Flow) ImportCredentials(ctx context.Context, providerID uuid.UUID, flowID oauth.FlowID, creds *IDECredentials) (*oauth.Session, error) {
	now := time.Now()
	th := creds.TokenHash
	s := &oauth.Session{
		ID:           uuid.New(),
		ProviderID:   providerID,
		FlowID:       flowID,
		Mechanism:    oauth.MechanismTokenImport,
		State:        uuid.New().String(),
		Status:       oauth.OAuthStateCompleted,
		TokenHash:    &th,
		RefreshToken: &creds.RefreshToken,
		TokenExpiry:  creds.ExpiresAt,
		CompletedAt:  &now,
		ExpiresAt:    now.Add(2 * time.Minute),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := f.repo.Create(ctx, s); err != nil {
		return nil, fmt.Errorf("create ide session: %w", err)
	}
	return s, nil
}
