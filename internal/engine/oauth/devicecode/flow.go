// Package devicecode implements the device-code OAuth flow.
package devicecode

import (
	"context"
	"fmt"
	"time"

	"gorouter/internal/domain/oauth"

	"github.com/google/uuid"
)

type DeviceCodeResponse struct {
	DeviceCode      string
	UserCode        string
	VerificationURI string
	Interval        int
	ExpiresIn       int
}

type TokenPollResponse struct {
	AccessToken  string
	RefreshToken string
	TokenType    string
	ExpiresIn    int
	Error        string
}

type Flow struct {
	repo oauth.Repository
}

func NewFlow(repo oauth.Repository) *Flow {
	return &Flow{repo: repo}
}

func (f *Flow) CreateSession(ctx context.Context, providerID uuid.UUID, flowID oauth.FlowID, deviceResp *DeviceCodeResponse) (*oauth.Session, error) {
	state := uuid.New().String()
	now := time.Now()
	s := &oauth.Session{
		ID:              uuid.New(),
		ProviderID:      providerID,
		FlowID:          flowID,
		Mechanism:       oauth.MechanismDeviceCode,
		State:           state,
		Status:          oauth.OAuthStatePending,
		DeviceCode:      &deviceResp.DeviceCode,
		UserCode:        &deviceResp.UserCode,
		VerificationURI: &deviceResp.VerificationURI,
		ExpiresAt:       now.Add(time.Duration(deviceResp.ExpiresIn) * time.Second),
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if flowID == oauth.FlowQwen || flowID == oauth.FlowQoder {
		s.Mechanism = oauth.MechanismDevicePKCE
	}
	if err := f.repo.Create(ctx, s); err != nil {
		return nil, fmt.Errorf("create device session: %w", err)
	}
	return s, nil
}

func (f *Flow) Poll(ctx context.Context, sessionID uuid.UUID) (*oauth.Session, error) {
	return f.repo.FindByID(ctx, sessionID)
}

func (f *Flow) Complete(ctx context.Context, sessionID uuid.UUID, tokenHash, refreshToken string, tokenExpiry time.Time) error {
	return f.repo.Complete(ctx, sessionID, tokenHash, refreshToken, tokenExpiry)
}
