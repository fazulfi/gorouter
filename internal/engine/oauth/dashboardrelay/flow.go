// Package dashboardrelay implements the dashboard relay callback flow.
package dashboardrelay

import (
	"context"
	"fmt"
	"time"

	"gorouter/internal/domain/oauth"

	"github.com/google/uuid"
)

type RelayMessage struct {
	Code      string
	State     string
	Token     string
	Error     string
	ErrorDesc string
}

type Flow struct {
	repo oauth.Repository
}

func NewFlow(repo oauth.Repository) *Flow {
	return &Flow{repo: repo}
}

func (f *Flow) CreateSession(ctx context.Context, providerID uuid.UUID, flowID oauth.FlowID, state string) (*oauth.Session, error) {
	now := time.Now()
	s := &oauth.Session{
		ID:         uuid.New(),
		ProviderID: providerID,
		FlowID:     flowID,
		State:      state,
		Status:     oauth.OAuthStatePending,
		ExpiresAt:  now.Add(10 * time.Minute),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	fd := oauth.LookupFlow(flowID)
	if fd != nil {
		s.Mechanism = fd.Mechanism
	}
	if err := f.repo.Create(ctx, s); err != nil {
		return nil, fmt.Errorf("create relay session: %w", err)
	}
	return s, nil
}

func (f *Flow) HandleRelay(ctx context.Context, msg *RelayMessage) (*oauth.Session, error) {
	session, err := f.repo.FindByState(ctx, msg.State)
	if err != nil {
		return nil, fmt.Errorf("find relay session: %w", err)
	}
	if session == nil {
		return nil, fmt.Errorf("session not found for state")
	}
	if session.IsTerminal() {
		return nil, fmt.Errorf("session already in terminal state: %s", session.Status)
	}
	if time.Now().After(session.ExpiresAt) {
		f.repo.UpdateStatus(ctx, session.ID, oauth.OAuthStateExpired, nil)
		return nil, fmt.Errorf("session expired")
	}
	if msg.Error != "" {
		errDetail := msg.Error + ": " + msg.ErrorDesc
		f.repo.UpdateStatus(ctx, session.ID, oauth.OAuthStateFailed, &errDetail)
		return nil, fmt.Errorf("relay error: %s", msg.Error)
	}
	now := time.Now()
	session.Status = oauth.OAuthStateCompleted
	session.CompletedAt = &now
	session.UpdatedAt = now
	return session, nil
}
