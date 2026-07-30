package provider

import (
	"context"

	"github.com/google/uuid"
)

// AccountRepository defines persistence operations for provider accounts.
type AccountRepository interface {
	FindByID(ctx context.Context, id uuid.UUID) (*Account, error)
	FindByProviderID(ctx context.Context, providerID uuid.UUID) ([]Account, error)
	Create(ctx context.Context, account *Account) error
	Update(ctx context.Context, account *Account) error
	Delete(ctx context.Context, id uuid.UUID) error
}
