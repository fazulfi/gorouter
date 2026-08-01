// Package patimport implements the Personal Access Token import flow.
package patimport

import (
	"context"
	"fmt"
	"time"

	"gorouter/internal/domain/oauth"

	"github.com/google/uuid"
)

type PATCredentials struct {
	Token     string
	TokenHash string
	Label     string
}

type Flow struct {
	repo oauth.Repository
}

func NewFlow(repo oauth.Repository) *Flow {
	return &Flow{repo: repo}
}

func deriveFlowID(providerName string) oauth.FlowID {
	fd := oauth.ProviderFlow(providerName)
	if fd != nil {
		return fd.FlowID
	}
	return oauth.FlowID(providerName)
}

func (f *Flow) ImportPAT(ctx context.Context, providerID uuid.UUID, providerName string, creds *PATCredentials) (*oauth.Session, error) {
	now := time.Now()
	var tokenHash *string
	if creds.TokenHash != "" {
		tokenHash = &creds.TokenHash
	}
	flowID := deriveFlowID(providerName)
	s := &oauth.Session{
		ID:          uuid.New(),
		ProviderID:  providerID,
		FlowID:      flowID,
		Mechanism:   oauth.MechanismPAT,
		State:       uuid.New().String(),
		Status:      oauth.OAuthStateCompleted,
		TokenHash:   tokenHash,
		CompletedAt: &now,
		ExpiresAt:   now.Add(2 * time.Minute),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := f.repo.Create(ctx, s); err != nil {
		return nil, fmt.Errorf("create pat session: %w", err)
	}
	return s, nil
}
