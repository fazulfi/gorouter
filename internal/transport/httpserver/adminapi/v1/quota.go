package v1

import (
	"context"
	"time"

	appauth "gorouter/internal/app/auth"
	appquota "gorouter/internal/app/quota"

	"github.com/google/uuid"
)

// QuotaService is the application seam for the quota group. The frozen
// contract has no standalone /quota path (quota state rides the usage and
// provider surfaces); this seam keeps the group's future wiring in one place.
type QuotaService interface {
	Status(ctx context.Context, actor *appauth.Actor, providerID uuid.UUID) (*appquota.QuotaStatus, error)
	Unlock(ctx context.Context, actor *appauth.Actor, providerID uuid.UUID) error
	Reset(ctx context.Context, actor *appauth.Actor, providerID uuid.UUID) error
}

type quotaGroup struct{ svc QuotaService }

// quotaStatusView is the wire projection of a quota window.
type quotaStatusView struct {
	ProviderID    uuid.UUID `json:"provider_id"`
	WindowStart   time.Time `json:"window_start"`
	CooldownUntil time.Time `json:"cooldown_until,omitempty"`
	LastError     string    `json:"last_error,omitempty"`
	ErrorKind     string    `json:"error_kind,omitempty"`
}

func projectQuotaStatus(s *appquota.QuotaStatus) quotaStatusView {
	return quotaStatusView{
		ProviderID: s.ProviderID, WindowStart: s.WindowStart,
		CooldownUntil: s.CooldownUntil, LastError: s.LastError, ErrorKind: string(s.ErrorKind),
	}
}
