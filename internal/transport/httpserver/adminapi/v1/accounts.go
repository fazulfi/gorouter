package v1

import (
	"context"

	"gorouter/internal/domain/provider"

	"github.com/google/uuid"
)

// AccountsService is the application seam for provider accounts. The frozen
// contract has no standalone /accounts path (account management rides the
// provider surfaces); this seam keeps the group's future wiring in one place.
type AccountsService interface {
	List(ctx context.Context, providerID string) ([]provider.Account, error)
	Update(ctx context.Context, account *provider.Account) error
}

type accountsGroup struct{ svc AccountsService }

// accountView is the credential-safe projection: the credential reference is
// opaque (never the credential value itself).
type accountView struct {
	ID            uuid.UUID `json:"id"`
	ProviderID    uuid.UUID `json:"provider_id"`
	Label         string    `json:"label"`
	AuthType      string    `json:"auth_type"`
	Priority      int       `json:"priority"`
	IsEnabled     bool      `json:"is_enabled"`
	MaxConcurrent int       `json:"max_concurrent"`
	ModelFilters  []string  `json:"model_filters,omitempty"`
}

func projectAccount(a *provider.Account) accountView {
	return accountView{
		ID: a.ID, ProviderID: a.ProviderID,
		Label: a.Label, AuthType: a.AuthType, Priority: a.Priority,
		IsEnabled: a.IsEnabled, MaxConcurrent: a.MaxConcurrent, ModelFilters: a.ModelFilters,
	}
}
